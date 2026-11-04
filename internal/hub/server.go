package hub

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"embed"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

//go:embed static
var staticFS embed.FS

const cookieName = "km_session"

// Handler is the dashboard and its JSON API.
func (h *Hub) Handler(secure bool) http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		b, _ := staticFS.ReadFile("static/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	})
	h.routes(mux, secure)
	return securityHeaders(mux, secure)
}

func securityHeaders(next http.Handler, secure bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("Cross-Origin-Opener-Policy", "same-origin")
		if secure {
			hd.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			hd.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// guard checks the anti-CSRF header on every request that changes something.
// Browsers do not send custom headers cross-site without CORS, which the hub never allows.
func guard(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	return r.Header.Get("X-KernelMan") == "1"
}

// authed wraps API handlers that need a logged-in session.
func (h *Hub) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || !h.sess.valid(c.Value) {
			httpError(w, http.StatusUnauthorized, "log in first")
			return
		}
		if !guard(r) {
			httpError(w, http.StatusForbidden, "missing X-KernelMan header")
			return
		}
		next(w, r)
	}
}

func (h *Hub) setCookie(w http.ResponseWriter, tok string, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: tok, Path: "/", HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteStrictMode, MaxAge: int(sessionIdle / time.Second)})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// Serve listens on addr. A loopback address is served over plain HTTP; any
// other address needs TLS: certFile/keyFile, or a self-signed certificate
// generated once in the data directory.
func (h *Hub) Serve(ctx context.Context, addr, certFile, keyFile string) (url string, done <-chan error, err error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", nil, fmt.Errorf("listen address %q: %w", addr, err)
	}
	ip := net.ParseIP(host)
	secure := certFile != "" || !(host == "localhost" || (ip != nil && ip.IsLoopback()))
	if secure && certFile == "" {
		if certFile, keyFile, err = h.selfSigned(); err != nil {
			return "", nil, err
		}
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, err
	}
	srv := &http.Server{Handler: h.Handler(secure), ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 2 * time.Minute,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	ch := make(chan error, 1)
	go func() {
		if secure {
			ch <- srv.ServeTLS(ln, certFile, keyFile)
		} else {
			ch <- srv.Serve(ln)
		}
	}()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	go h.autoRefresh(ctx)
	scheme, shown := "http", host
	if secure {
		scheme = "https"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		shown, _ = os.Hostname()
	}
	return fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(shown, fmt.Sprint(port))), ch, nil
}

// autoRefresh reads every host's status every 15 minutes while someone has
// logged in (the vault key is needed) and no job is running.
func (h *Hub) autoRefresh(ctx context.Context) {
	t := time.NewTicker(15 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if h.sess.vaultKey() != nil && !h.Jobs.Busy() {
				h.RefreshAll(ctx)
			}
		}
	}
}

// selfSigned creates (once) a 3-year ECDSA certificate for this host name.
func (h *Hub) selfSigned() (string, string, error) {
	certFile, keyFile := filepath.Join(h.Store.Dir(), "tls-cert.pem"), filepath.Join(h.Store.Dir(), "tls-key.pem")
	if _, err := os.Stat(certFile); err == nil {
		return certFile, keyFile, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	name, _ := os.Hostname()
	tpl := &x509.Certificate{
		SerialNumber: new(big.Int).SetBytes(random(16)),
		Subject:      pkix.Name{CommonName: name, Organization: []string{"KernelMan Hub"}},
		NotBefore:    time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(3, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{name, "localhost"},
		IPAddresses: localIPs(),
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		return "", "", err
	}
	return certFile, keyFile, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

func localIPs() []net.IP {
	ips := []net.IP{net.IPv4(127, 0, 0, 1)}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() {
			ips = append(ips, n.IP)
		}
	}
	return ips
}
