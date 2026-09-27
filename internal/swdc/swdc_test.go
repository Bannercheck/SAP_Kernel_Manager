package swdc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeSAP imitates launchpad.support.sap.com + accounts.sap.com on one host:
// / → SAML request → user name form → password form → (MFA form) → SAML
// auto-post → session cookie → catalogue JSON.
func fakeSAP(t *testing.T, mfa bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	loggedIn := func(r *http.Request) bool { c, err := r.Cookie("SAPSESSION"); return err == nil && c.Value == "ok" }
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if loggedIn(r) {
			fmt.Fprint(w, "<html><title>SAP for Me</title><body>home</body></html>")
			return
		}
		http.Redirect(w, r, "/saml2/idp/sso?SAMLRequest=abc&RelayState=xyz", http.StatusFound)
	})
	mux.HandleFunc("/saml2/idp/sso", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><title>SAP ID Service</title><body><form id="logOnForm" method="post" action="/saml2/idp/usernamePassword">
<input type="hidden" name="authenticity_token" value="tok1"><input type="hidden" name="spId" value="launchpad">
<input type="text" name="j_username" id="j_username" placeholder="E-mail or User ID"><button type="submit" name="continue" value="Continue">Continue</button></form></body></html>`)
	})
	mux.HandleFunc("/saml2/idp/usernamePassword", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("j_password") == "" {
			if r.Form.Get("j_username") != "S0001234567" {
				fmt.Fprint(w, "<html><title>SAP ID Service</title><body>Sorry, we could not authenticate you.</body></html>")
				return
			}
			fmt.Fprintf(w, `<html><title>SAP ID Service</title><body><form method="post" action="/saml2/idp/usernamePassword">
<input type="hidden" name="authenticity_token" value="tok2"><input type="hidden" name="j_username" value="%s">
<input type="password" name="j_password"><button type="submit">Log On</button></form></body></html>`, r.Form.Get("j_username"))
			return
		}
		if r.Form.Get("j_password") != "secret" {
			fmt.Fprint(w, `<html><title>SAP ID Service</title><body><p>Sorry, we could not authenticate you. Try again.</p>
<form method="post" action="/saml2/idp/usernamePassword"><input type="hidden" name="j_username" value="S0001234567"><input type="password" name="j_password"></form></body></html>`)
			return
		}
		if mfa {
			fmt.Fprint(w, `<html><title>SAP ID Service</title><body><form method="post" action="/saml2/idp/mfa">
<input type="hidden" name="authenticity_token" value="tok3"><input type="text" name="passcode" autocomplete="one-time-code"><button type="submit">Verify</button></form></body></html>`)
			return
		}
		samlPost(w)
	})
	mux.HandleFunc("/saml2/idp/mfa", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("passcode") != "123456" {
			fmt.Fprint(w, "<html><body>Invalid code</body></html>")
			return
		}
		samlPost(w)
	})
	mux.HandleFunc("/saml2/sp/acs", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("SAMLResponse") != "signed-assertion" {
			http.Error(w, "bad assertion", http.StatusForbidden)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "SAPSESSION", Value: "ok", Path: "/"})
		http.Redirect(w, r, "/", http.StatusFound)
	})
	mux.HandleFunc(searchPath, func(w http.ResponseWriter, r *http.Request) {
		if !loggedIn(r) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html><title>Log On</title></html>")
			return
		}
		w.Header().Set("Content-Type", "application/json;charset=utf-8")
		q := r.URL.Query().Get("SEARCH_STRING")
		rows := []map[string]any{}
		add := func(title, desc, info, key, size string) {
			if strings.Contains(strings.ToUpper(title), strings.ToUpper(strings.Fields(q)[0])) {
				rows = append(rows, map[string]any{"Title": title, "Description": desc, "Infotype": info, "Fastkey": key,
					"DownloadDirectLink": "https://softwaredownloads.sap.com/file/" + key, "Filesize": size, "ChangeDate": "20260915"})
			}
		}
		add("SAPEXE_400-80007807.SAR", "SAP KERNEL 7.93 64-BIT UNICODE", "Linux on x86_64 64bit | #DATABASE INDEPENDENT", "0020000000000400", "1234567")
		add("SAPEXE_403-80007807.SAR", "SAP KERNEL 7.93 64-BIT UNICODE", "Linux on x86_64 64bit | #DATABASE INDEPENDENT", "0020000000000403", "1234567")
		add("SAPEXE_403-80007900.SAR", "SAP KERNEL 7.93 64-BIT UNICODE", "AIX 64bit | #DATABASE INDEPENDENT", "0020000000000499", "1234567")
		add("SAPEXE_120-80006000.SAR", "SAP KERNEL 7.89 64-BIT UNICODE", "Linux on x86_64 64bit | #DATABASE INDEPENDENT", "0020000000000120", "1234567")
		add("SAPEXEDB_403-80007808.SAR", "SAP KERNEL 7.93 64-BIT UNICODE", "Linux on x86_64 64bit | SAP HANA DATABASE", "0020000000000413", "234567")
		add("SAPEXEDB_403-80007809.SAR", "SAP KERNEL 7.93 64-BIT UNICODE", "Linux on x86_64 64bit | ORACLE", "0020000000000414", "234567")
		add("dw_401-80007541.sar", "SAP KERNEL 7.93 64-BIT UNICODE", "Linux on x86_64 64bit | #DATABASE INDEPENDENT", "0020000000000401", "45678")
		add("dw_421-80007541.sar", "SAP KERNEL 7.93 64-BIT UNICODE", "Linux on x86_64 64bit | #DATABASE INDEPENDENT", "0020000000000421", "45678")
		add("dw_423-80007541.sar", "SAP KERNEL 7.93 64-BIT UNICODE", "Linux on x86_64 64bit | #DATABASE INDEPENDENT", "0020000000000423", "45678")
		add("igsexe_12-80003187.sar", "SAP IGS 7.93", "Linux on x86_64 64bit", "0020000000000012", "9999")
		json.NewEncoder(w).Encode(map[string]any{"d": map[string]any{"results": rows}})
	})
	return httptest.NewServer(mux)
}

func samlPost(w http.ResponseWriter) {
	fmt.Fprint(w, `<html><body onload="document.forms[0].submit()"><form method="post" action="/saml2/sp/acs">
<input type="hidden" name="SAMLResponse" value="signed-assertion"><input type="hidden" name="RelayState" value="xyz"><noscript><input type="submit"></noscript></form></body></html>`)
}

func newTestSession(srv *httptest.Server, user, pass string) *Session {
	s := New(user, pass)
	s.Launchpad = srv.URL
	s.AllowedHosts = []string{"127.0.0.1"}
	return s
}

func TestLoginAndSearch(t *testing.T) {
	srv := fakeSAP(t, false)
	defer srv.Close()
	var log []string
	s := newTestSession(srv, "S0001234567", "secret")
	s.Log = func(f string, a ...any) { log = append(log, fmt.Sprintf(f, a...)) }
	if err := s.Login(context.Background()); err != nil {
		t.Fatalf("login: %v\n%s", err, strings.Join(log, "\n"))
	}
	res, err := s.Search(context.Background(), "SAPEXE_ 793")
	if err != nil || len(res) != 4 {
		t.Fatalf("search: %v %d\n%s", err, len(res), strings.Join(log, "\n"))
	}
	if res[0].Title != "SAPEXE_400-80007807.SAR" || res[0].Link != "https://softwaredownloads.sap.com/file/0020000000000400" || res[0].SizeKB != 1234567 ||
		!strings.Contains(res[0].Info, "Linux on x86_64") {
		t.Errorf("result = %+v", res[0])
	}
	joined := strings.Join(log, "\n")
	if strings.Contains(joined, "secret") {
		t.Error("log leaks the password")
	}
}

func TestLoginMFAAndRejection(t *testing.T) {
	srv := fakeSAP(t, true)
	defer srv.Close()
	s := newTestSession(srv, "S0001234567", "secret")
	asked := ""
	s.MFAPrompt = func(p string) string { asked = p; return "123456" }
	if err := s.Login(context.Background()); err != nil {
		t.Fatalf("mfa login: %v", err)
	}
	if !strings.Contains(asked, "S0001234567") {
		t.Errorf("MFA prompt = %q", asked)
	}
	bad := newTestSession(srv, "S0001234567", "wrong")
	if err := bad.Login(context.Background()); err != ErrLogin {
		t.Errorf("wrong password: %v", err)
	}
}

func TestChoose(t *testing.T) {
	srv := fakeSAP(t, false)
	defer srv.Close()
	s := newTestSession(srv, "S0001234567", "secret")
	var all []Result
	for _, q := range []string{"SAPEXE", "SAPEXEDB", "dw_", "igsexe"} {
		r, err := s.Search(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, r...)
	}
	sel := Choose(Want{Release: 793, PlatformDir: "linuxx86_64", DB: "hdb", CurrentPatch: 200}, all)
	var names []string
	for _, p := range sel.Picks {
		names = append(names, p.Title)
	}
	want := "SAPEXE_403-80007807.SAR SAPEXEDB_403-80007808.SAR dw_421-80007541.sar dw_423-80007541.sar"
	if strings.Join(names, " ") != want || sel.StackLevel != 403 || sel.TargetLevel != 423 {
		t.Errorf("picks = %v stack=%d target=%d notes=%v", names, sel.StackLevel, sel.TargetLevel, sel.Notes)
	}
	joined := strings.Join(sel.Notes, "; ")
	if !strings.Contains(joined, "other kernel releases") || !strings.Contains(joined, "other platforms") || !strings.Contains(joined, "other database") {
		t.Errorf("notes = %v", sel.Notes)
	}
	// igsexe 12 is below the stack level, so it is not proposed; AIX and 7.89 archives are ignored
	for _, n := range names {
		if strings.Contains(n, "igsexe") || strings.Contains(n, "80007900") || strings.Contains(n, "SAPEXE_120") {
			t.Errorf("unexpected pick %s", n)
		}
	}
}

func TestParseSearchShapes(t *testing.T) {
	v4 := []byte(`{"value":[{"Title":"dw_423-1.sar","Description":"SAP KERNEL 7.93 64-BIT UNICODE","Fastkey":"1","Filesize":"12 MB"}]}`)
	r, err := ParseSearch(v4)
	if err != nil || len(r) != 1 || r[0].SizeKB != 12<<10 || r[0].Link != "https://softwaredownloads.sap.com/file/1" {
		t.Errorf("v4: %v %+v", err, r)
	}
	if _, err := ParseSearch([]byte("<html>")); err == nil {
		t.Error("html parsed as search result")
	}
	if r, err := ParseSearch([]byte(`{"d":{"results":null}}`)); err != nil || len(r) != 0 {
		t.Errorf("null results: %v %v", r, err)
	}
}
