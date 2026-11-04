// kernelman-hub — KernelMan Hub: one server (Windows or Linux) that runs
// SAP kernel updates on every SAP host over SSH, driven from a web dashboard.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/hub"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/platform"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/version"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	// A desktop (Windows, macOS) serves only itself; a server (Linux without a
	// display) serves the network over HTTPS so the dashboard opens from any PC.
	defListen := "0.0.0.0:8443"
	if platform.HasDesktop() {
		defListen = "127.0.0.1:8090"
	}
	fs := flag.NewFlagSet("kernelman-hub", flag.ContinueOnError)
	listen := fs.String("listen", defListen, "address to serve the dashboard on (non-loopback addresses use HTTPS)")
	data := fs.String("data", hub.DefaultDataDir(), "data directory (inventory, encrypted secrets, jobs, archives)")
	demo := fs.Bool("demo", false, "simulated SAP landscape, nothing real is touched (login admin / kernelman1)")
	cert := fs.String("tls-cert", "", "TLS certificate (PEM); default: self-signed, created in the data directory")
	key := fs.String("tls-key", "", "TLS private key (PEM)")
	noBrowser := fs.Bool("no-browser", false, "do not open the browser")
	showVersion := fs.Bool("version", false, "print the version and exit")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "KernelMan Hub %s — SAP kernel updates for every server, from one dashboard\n\nUsage: kernelman-hub [flags]\n\n", version.Version)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Println("kernelman-hub " + strings.TrimPrefix(version.String(), version.AppName+" "))
		return 0
	}
	cfg := hub.Config{DataDir: *data}
	if *demo {
		cfg.DataDir = filepath.Join(os.TempDir(), "kernelman-hub-demo")
		_ = os.RemoveAll(cfg.DataDir) // a fresh landscape every start
		cfg.Remote, cfg.Demo = hub.NewDemoRemote(), true
	}
	h, err := hub.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kernelman-hub:", err)
		return 1
	}
	if *demo {
		if err := hub.SetupDemo(h, "admin", "kernelman1"); err != nil {
			fmt.Fprintln(os.Stderr, "kernelman-hub demo:", err)
			return 1
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	url, done, err := h.Serve(ctx, *listen, *cert, *key)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kernelman-hub:", err)
		return 1
	}
	banner(url, *listen, cfg.DataDir, *demo, *cert)
	if !*noBrowser && platform.HasDesktop() {
		platform.OpenURL(url)
	}
	select {
	case err := <-done:
		if err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "kernelman-hub:", err)
			return 1
		}
	case <-ctx.Done():
		fmt.Println("\nstopping…")
	}
	return 0
}

// banner prints every address the dashboard can be reached on.
func banner(url, listen, data string, demo bool, ownCert string) {
	fmt.Printf("\n  KernelMan Hub %s\n\n  Dashboard  %s\n", version.Version, url)
	host, port, _ := net.SplitHostPort(listen)
	if host == "0.0.0.0" || host == "::" || host == "" {
		scheme := url[:strings.Index(url, "://")]
		addrs, _ := net.InterfaceAddrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
				fmt.Printf("             %s://%s\n", scheme, net.JoinHostPort(n.IP.String(), port))
			}
		}
	}
	if strings.HasPrefix(url, "https") && ownCert == "" {
		if fp := certFingerprint(filepath.Join(data, "tls-cert.pem")); fp != "" {
			fmt.Printf("\n  Self-signed certificate: the browser warns once. Compare its SHA-256 fingerprint:\n  %s\n", fp)
		}
		fmt.Printf("  Firewall (RHEL): firewall-cmd --permanent --add-port=%s/tcp && firewall-cmd --reload\n", port)
	}
	if demo {
		fmt.Println("\n  DEMO — simulated SAP hosts, nothing real is touched. Login: admin / kernelman1")
	}
	fmt.Printf("\n  Data       %s\n  Stop       Ctrl+C\n\n", data)
}

func certFingerprint(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return ""
	}
	if _, err := x509.ParseCertificate(blk.Bytes); err != nil {
		return ""
	}
	sum := sha256.Sum256(blk.Bytes)
	parts := make([]string, len(sum))
	for i, c := range sum {
		parts[i] = fmt.Sprintf("%02X", c)
	}
	return strings.Join(parts, ":")
}
