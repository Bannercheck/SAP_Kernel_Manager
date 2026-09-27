package download

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const sarBody = "CAR 2.01\x00fake archive body for the download test ..................................."

// sapLike mimics softwaredownloads.sap.com: Basic Auth, a redirect from
// /file/<id> to /download/<name>, Range support, HTML for bad users.
func sapLike(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		u, p, ok := r.BasicAuth()
		if !ok || u != "S0001234567" || p != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("/file/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		http.Redirect(w, r, "/download/SAPEXE_403-80007807.SAR", http.StatusFound)
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		body := []byte(sarBody)
		w.Header().Set("Content-Disposition", `attachment; filename="SAPEXE_403-80007807.SAR"`)
		if rg := r.Header.Get("Range"); strings.HasPrefix(rg, "bytes=") {
			from, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"))
			w.Header().Set("Content-Length", fmt.Sprint(len(body)-from))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(body[from:])
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Write(body)
	})
	mux.HandleFunc("/html/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html>login</html>"))
	})
	return httptest.NewServer(mux)
}

func TestParseBasket(t *testing.T) {
	text := "https://softwaredownloads.sap.com/file/0020000000123452026\tSAPEXE_403-80007807.SAR\tSAP KERNEL 7.93 64-BIT UC\t1234 MB\n" +
		"junk line without link\n" +
		"https://softwaredownloads.sap.com/file/0020000000123462026,dw_423-80007541.sar,disp+work,45 MB\n" +
		"https://softwaredownloads.sap.com/file/0020000000123452026\tduplicate\n"
	items := ParseBasket(text)
	if len(items) != 2 || items[0].Name != "SAPEXE_403-80007807.SAR" || items[0].Size != 1234<<20 ||
		items[1].Name != "dw_423-80007541.sar" || items[1].Size != 45<<20 {
		t.Errorf("items = %+v", items)
	}
}

func TestCredentials(t *testing.T) {
	dir := t.TempDir()
	if _, ok, err := LoadCredentials(dir); ok || err != nil {
		t.Fatalf("empty dir: %v %v", ok, err)
	}
	if err := SaveCredentials(dir, Credentials{User: "S0001234567", Password: "secret"}, false); err != nil {
		t.Fatal(err)
	}
	c, ok, _ := LoadCredentials(dir)
	if !ok || c.User != "S0001234567" || c.Password != "" {
		t.Errorf("without password: %+v %v", c, ok)
	}
	if st, _ := os.Stat(filepath.Join(dir, "suser.json")); st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o", st.Mode().Perm())
	}
	SaveCredentials(dir, Credentials{User: "S0001234567", Password: "secret"}, true)
	if c, _, _ = LoadCredentials(dir); c.Password != "secret" {
		t.Errorf("with password: %+v", c)
	}
}

func TestDownloadResumeAndErrors(t *testing.T) {
	srv := sapLike(t)
	defer srv.Close()
	dir := t.TempDir()
	c := NewClient(Credentials{User: "S0001234567", Password: "secret"})
	c.TrustedHosts = []string{"127.0.0.1"}
	c.Retries = 0

	var reports int
	res, err := c.Download(context.Background(), Item{URL: srv.URL + "/file/0020000000123452026"}, dir, func(string, int64, int64) { reports++ })
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(res.Path) != "SAPEXE_403-80007807.SAR" || res.Bytes != int64(len(sarBody)) || res.SHA256 == "" || reports == 0 || !IsSAR(res.Path) {
		t.Errorf("res = %+v reports=%d", res, reports)
	}

	// resume: leave a partial file behind
	os.Remove(res.Path)
	part := filepath.Join(dir, "SAPEXE_403-80007807.SAR.part")
	os.WriteFile(part, []byte(sarBody[:10]), 0o644)
	res2, err := c.Download(context.Background(), Item{URL: srv.URL + "/file/0020000000123452026", Name: "SAPEXE_403-80007807.SAR"}, dir, nil)
	if err != nil || !res2.Resumed || res2.Bytes != int64(len(sarBody)) || res2.SHA256 != res.SHA256 {
		t.Errorf("resume: %+v %v", res2, err)
	}

	bad := NewClient(Credentials{User: "S0001234567", Password: "wrong"})
	bad.TrustedHosts, bad.Retries = []string{"127.0.0.1"}, 0
	if _, err := bad.Download(context.Background(), Item{URL: srv.URL + "/file/1"}, dir, nil); err != ErrAuth {
		t.Errorf("wrong password: %v", err)
	}
	if _, err := c.Download(context.Background(), Item{URL: srv.URL + "/html/x"}, dir, nil); err != ErrLoginPage {
		t.Errorf("html: %v", err)
	}
}
