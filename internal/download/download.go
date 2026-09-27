// Package download fetches SAP archives from softwaredownloads.sap.com with
// S-user credentials (HTTP Basic Auth, the way SAP supports for tools such
// as wget). Links come from the SAP for Me Download Basket export or are
// typed by hand; searching the Software Center needs a browser login and is
// out of scope.
package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Credentials is the S-user login. Password may be empty (asked each time).
type Credentials struct {
	User     string `json:"user"`
	Password string `json:"password,omitempty"`
}

const credentialsFile = "suser.json"

// LoadCredentials reads <dir>/suser.json; ok is false when there is none.
func LoadCredentials(dir string) (Credentials, bool, error) {
	b, err := os.ReadFile(filepath.Join(dir, credentialsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return Credentials{}, false, nil
		}
		return Credentials{}, false, err
	}
	var c Credentials
	if err := json.Unmarshal(b, &c); err != nil {
		return Credentials{}, false, err
	}
	return c, c.User != "", nil
}

// SaveCredentials writes <dir>/suser.json with mode 0600. The password is
// stored only when withPassword is set.
func SaveCredentials(dir string, c Credentials, withPassword bool) error {
	if !withPassword {
		c.Password = ""
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(filepath.Join(dir, credentialsFile), b, 0o600)
}

// Item is one file to download.
type Item struct {
	URL  string
	Name string // file name when known (from the basket export)
	Size int64  // bytes when known
}

var (
	urlRe  = regexp.MustCompile(`https?://[^\s,;"]+`)
	fileRe = regexp.MustCompile(`(?i)\b[\w.+-]+\.(sar|exe|zip|txt)\b`)
	sizeRe = regexp.MustCompile(`\b(\d+)\s*(KB|MB|GB)\b`)
)

// ParseBasket extracts download items from the SAP Download Basket text
// export (or any text with one link per line). Columns are tab or comma
// separated; the parser only relies on finding a URL, an archive name and,
// when present, a size.
func ParseBasket(text string) []Item {
	var items []Item
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		u := urlRe.FindString(line)
		if u == "" {
			continue
		}
		u = strings.TrimRight(u, ",;\t\r ")
		if seen[u] {
			continue
		}
		seen[u] = true
		it := Item{URL: u}
		rest := strings.Replace(line, u, "", 1)
		if m := fileRe.FindString(rest); m != "" {
			it.Name = m
		}
		if m := sizeRe.FindStringSubmatch(rest); m != nil {
			n, _ := strconv.ParseInt(m[1], 10, 64)
			switch strings.ToUpper(m[2]) {
			case "KB":
				it.Size = n << 10
			case "MB":
				it.Size = n << 20
			case "GB":
				it.Size = n << 30
			}
		}
		items = append(items, it)
	}
	return items
}

// Progress reports bytes received for the current file.
type Progress func(name string, done, total int64)

// Result describes one finished download.
type Result struct {
	Path     string
	Bytes    int64
	SHA256   string
	Resumed  bool
	Duration time.Duration
}

// Client downloads with S-user Basic Auth, resume and retries.
type Client struct {
	HTTP    *http.Client
	Cred    Credentials
	Retries int
	// TrustedHosts keeps the Authorization header across redirects to these
	// domain suffixes (Go drops it for other hosts).
	TrustedHosts []string
}

// WithJar makes the client send an authenticated session's cookies as well.
func (c *Client) WithJar(jar http.CookieJar) *Client {
	c.HTTP.Jar = jar
	return c
}

// NewClient returns a client that keeps credentials for *.sap.com redirects.
func NewClient(cred Credentials) *Client {
	c := &Client{Cred: cred, Retries: 3, TrustedHosts: []string{".sap.com"}}
	c.HTTP = &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: 30 * time.Second,
			ResponseHeaderTimeout: 2 * time.Minute, IdleConnTimeout: time.Minute},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if c.trusted(req.URL.Host) {
				req.SetBasicAuth(c.Cred.User, c.Cred.Password)
			}
			return nil
		},
	}
	return c
}

func (c *Client) trusted(host string) bool {
	h := strings.ToLower(host)
	if i := strings.LastIndex(h, ":"); i > 0 {
		h = h[:i]
	}
	for _, s := range c.TrustedHosts {
		if h == strings.TrimPrefix(s, ".") || strings.HasSuffix(h, s) {
			return true
		}
	}
	return false
}

// ErrAuth means SAP rejected the S-user or password.
var ErrAuth = errors.New("S-user or password rejected by SAP (HTTP 401/403)")

// ErrLoginPage means SAP answered with an HTML page instead of the file.
var ErrLoginPage = errors.New("SAP returned an HTML page instead of the file: the S-user needs a browser login (MFA) or lacks the download authorization")

// Download fetches item into dir, resuming <name>.part when present.
func (c *Client) Download(ctx context.Context, item Item, dir string, pr Progress) (Result, error) {
	var last error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return Result{}, ctx.Err()
			case <-time.After(time.Duration(attempt*attempt) * 2 * time.Second):
			}
		}
		res, err := c.download(ctx, item, dir, pr)
		if err == nil || errors.Is(err, ErrAuth) || errors.Is(err, ErrLoginPage) || ctx.Err() != nil {
			return res, err
		}
		last = err
	}
	return Result{}, fmt.Errorf("after %d attempts: %w", c.Retries+1, last)
}

func (c *Client) download(ctx context.Context, item Item, dir string, pr Progress) (Result, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		return Result{}, err
	}
	req.SetBasicAuth(c.Cred.User, c.Cred.Password)
	req.Header.Set("User-Agent", "KernelMan (SAP Kernel Manager)")

	name := item.Name
	part := ""
	var have int64
	if name != "" {
		part = filepath.Join(dir, name+".part")
		if st, err := os.Stat(part); err == nil && st.Size() > 0 {
			have = st.Size()
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return Result{}, ErrAuth
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		os.Remove(part)
		return c.download(ctx, item, dir, pr) // start over
	case resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent:
		return Result{}, fmt.Errorf("HTTP %s", resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(strings.ToLower(ct), "text/html") {
		return Result{}, ErrLoginPage
	}
	if name == "" {
		name = fileNameFrom(resp, item.URL)
		part = filepath.Join(dir, name+".part")
	}
	resumed := resp.StatusCode == http.StatusPartialContent && have > 0
	if !resumed {
		have = 0
	}
	total := resp.ContentLength
	if total > 0 {
		total += have
	} else if item.Size > 0 {
		total = item.Size
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, err
	}
	flags := os.O_CREATE | os.O_WRONLY
	if resumed {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return Result{}, err
	}
	h := sha256.New()
	if resumed { // hash what is already on disk
		old, err := os.Open(part)
		if err == nil {
			io.Copy(h, io.LimitReader(old, have))
			old.Close()
		}
	}
	buf := make([]byte, 1<<20)
	done := have
	lastReport := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return Result{}, werr
			}
			h.Write(buf[:n])
			done += int64(n)
			if pr != nil && (time.Since(lastReport) > 500*time.Millisecond || done == total) {
				pr(name, done, total)
				lastReport = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return Result{}, rerr
		}
	}
	if err := f.Close(); err != nil {
		return Result{}, err
	}
	if total > 0 && done != total {
		return Result{}, fmt.Errorf("incomplete: %d of %d bytes", done, total)
	}
	final := filepath.Join(dir, name)
	if err := os.Rename(part, final); err != nil {
		return Result{}, err
	}
	if pr != nil {
		pr(name, done, done)
	}
	return Result{Path: final, Bytes: done, SHA256: hex.EncodeToString(h.Sum(nil)), Resumed: resumed, Duration: time.Since(start)}, nil
}

// fileNameFrom takes the name from Content-Disposition, else the URL path.
func fileNameFrom(resp *http.Response, raw string) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil && params["filename"] != "" {
			return filepath.Base(params["filename"])
		}
	}
	if u, err := url.Parse(raw); err == nil {
		if b := path.Base(u.Path); b != "" && b != "/" && b != "." {
			return b
		}
	}
	return "download-" + strconv.FormatInt(time.Now().Unix(), 10)
}

// Reachable reports whether an HTTPS endpoint answers at all (any status
// code counts; proxies from the environment are honoured). It is the
// "does this host have internet access to SAP" check.
func Reachable(ctx context.Context, rawURL string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: timeout},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// IsSAR reports whether the file starts with the SAPCAR signature ("CAR 2.").
func IsSAR(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 8)
	n, _ := io.ReadFull(f, head)
	return n >= 6 && strings.HasPrefix(string(head[:n]), "CAR 2.")
}
