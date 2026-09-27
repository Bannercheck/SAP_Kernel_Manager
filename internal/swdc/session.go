// Package swdc talks to the SAP Software Download Center behind SAP for Me:
// it logs in through the SAP ID service (accounts.sap.com, HTML forms, SAML
// auto-post, optional MFA code), searches the catalogue and hands the
// direct download links to the download package.
//
// The login is written as a generic form follower rather than a fixed
// sequence, because SAP changes the pages: every HTML answer is inspected
// for a form, the form is filled from what it asks for (user name,
// password, one-time code) or auto-submitted (SAML), and the loop ends when
// the catalogue answers JSON. Every step is written to the log so that a
// failing run can be diagnosed from the log alone.
package swdc

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultLaunchpad = "https://launchpad.support.sap.com"
	searchPath       = "/services/odata/svt/swdcuisrv/SearchResultSet"
	maxSteps         = 15
)

// ErrLogin means SAP rejected the credentials.
var ErrLogin = errors.New("SAP ID service rejected the S-user or password")

// ErrNoForm means a page without a usable form was reached before login completed.
var ErrNoForm = errors.New("login page without a recognisable form (JavaScript-only page or changed layout); see the log")

// Session is an authenticated SAP for Me session.
type Session struct {
	HTTP         *http.Client
	User         string
	Password     string
	Launchpad    string                     // base URL, DefaultLaunchpad
	AllowedHosts []string                   // hosts that may receive the credentials (suffix match)
	MFAPrompt    func(prompt string) string // asked for a one-time code when a page wants one
	Log          func(format string, a ...any)
	loggedIn     bool
}

// New returns a session with a cookie jar and SAP hosts trusted.
func New(user, password string) *Session {
	jar, _ := cookiejar.New(nil)
	s := &Session{User: user, Password: password, Launchpad: DefaultLaunchpad, AllowedHosts: []string{".sap.com"},
		Log: func(string, ...any) {}}
	s.HTTP = &http.Client{Jar: jar, Timeout: 3 * time.Minute,
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: 30 * time.Second}}
	return s
}

func (s *Session) allowed(host string) bool {
	h := strings.ToLower(host)
	if i := strings.LastIndex(h, ":"); i > 0 {
		h = h[:i]
	}
	for _, a := range s.AllowedHosts {
		if h == strings.TrimPrefix(a, ".") || strings.HasSuffix(h, a) {
			return true
		}
	}
	return false
}

// form is an HTML form found on a page.
type form struct {
	Action string
	Method string
	Fields map[string]string // name → value (hidden values kept)
	Types  map[string]string // name → input type
}

var (
	formRe  = regexp.MustCompile(`(?is)<form\b([^>]*)>(.*?)</form>`)
	inputRe = regexp.MustCompile(`(?is)<(input|button)\b([^>]*)>`)
	attrRe  = regexp.MustCompile(`(?is)([a-zA-Z_:-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	titleRe = regexp.MustCompile(`(?is)<title>(.*?)</title>`)
)

func attrs(tag string) map[string]string {
	m := map[string]string{}
	for _, a := range attrRe.FindAllStringSubmatch(tag, -1) {
		v := a[2] + a[3] + a[4]
		m[strings.ToLower(a[1])] = html.UnescapeString(v)
	}
	return m
}

// parseForms extracts the forms of a page.
func parseForms(body string) []form {
	var forms []form
	for _, fm := range formRe.FindAllStringSubmatch(body, -1) {
		fa := attrs(fm[1])
		f := form{Action: fa["action"], Method: strings.ToUpper(fa["method"]), Fields: map[string]string{}, Types: map[string]string{}}
		if f.Method == "" {
			f.Method = "GET"
		}
		for _, in := range inputRe.FindAllStringSubmatch(fm[2], -1) {
			ia := attrs(in[2])
			name := ia["name"]
			if name == "" {
				continue
			}
			typ := strings.ToLower(ia["type"])
			if in[1] == "button" && typ != "submit" && typ != "" {
				continue
			}
			if typ == "submit" && in[1] == "button" && ia["value"] == "" {
				continue
			}
			f.Fields[name] = ia["value"]
			f.Types[name] = typ
		}
		forms = append(forms, f)
	}
	return forms
}

// kind classifies what a form asks for.
func (f form) kind() string {
	has := func(pred func(name, typ string) bool) bool {
		for n, t := range f.Types {
			if pred(strings.ToLower(n), t) {
				return true
			}
		}
		return false
	}
	switch {
	case has(func(n, t string) bool { return n == "samlresponse" || n == "samlrequest" }):
		return "saml"
	case has(func(n, t string) bool { return t == "password" }):
		return "password"
	case has(func(n, t string) bool {
		return t != "hidden" && (strings.Contains(n, "otp") || strings.Contains(n, "passcode") || strings.Contains(n, "mfa") ||
			strings.Contains(n, "totp") || strings.Contains(n, "verificationcode") || n == "code")
	}):
		return "mfa"
	case has(func(n, t string) bool {
		return t != "hidden" && (n == "j_username" || strings.Contains(n, "username") || n == "user" || n == "email" || strings.Contains(n, "login"))
	}):
		return "username"
	}
	return "other"
}

func (f form) fill(kind, user, password, code string) url.Values {
	v := url.Values{}
	for n, val := range f.Fields {
		ln := strings.ToLower(n)
		t := f.Types[n]
		switch {
		case t == "password":
			v.Set(n, password)
		case kind == "mfa" && t != "hidden" && (strings.Contains(ln, "otp") || strings.Contains(ln, "passcode") || strings.Contains(ln, "mfa") ||
			strings.Contains(ln, "totp") || strings.Contains(ln, "verificationcode") || ln == "code"):
			v.Set(n, code)
		case t != "hidden" && (ln == "j_username" || strings.Contains(ln, "username") || ln == "user" || ln == "email" || strings.Contains(ln, "login")):
			v.Set(n, user)
		default:
			v.Set(n, val)
		}
	}
	return v
}

// Login walks the SAP ID service until the catalogue answers JSON.
func (s *Session) Login(ctx context.Context) error {
	if s.loggedIn {
		return nil
	}
	current, err := url.Parse(s.Launchpad + "/")
	if err != nil {
		return err
	}
	resp, body, err := s.do(ctx, http.MethodGet, current.String(), nil)
	if err != nil {
		return err
	}
	postedPassword := false
	for step := 1; step <= maxSteps; step++ {
		if s.probe(ctx) {
			s.loggedIn = true
			s.Log("login: catalogue answers JSON after %d step(s)", step)
			return nil
		}
		forms := parseForms(body)
		var f *form
		for i := range forms { // prefer the form that asks for something
			if k := forms[i].kind(); k != "other" {
				f = &forms[i]
				break
			}
		}
		if f == nil && len(forms) > 0 {
			f = &forms[0]
		}
		if f == nil {
			s.Log("login: step %d: page %q has no form", step, pageTitle(body))
			if postedPassword && looksLikeRejection(body) {
				return ErrLogin
			}
			return ErrNoForm
		}
		kind := f.kind()
		action, err := resp.Request.URL.Parse(f.Action)
		if err != nil {
			return err
		}
		s.Log("login: step %d: %s form → %s %s fields=%v", step, kind, f.Method, action, fieldNames(f))
		code := ""
		switch kind {
		case "password":
			if postedPassword && looksLikeRejection(body) {
				return ErrLogin
			}
			postedPassword = true
		case "mfa":
			if s.MFAPrompt == nil {
				return errors.New("SAP asks for a one-time code (MFA) but no prompt is available")
			}
			code = s.MFAPrompt("One-time code (MFA) for " + s.User)
		case "username", "saml", "other":
		}
		if (kind == "password" || kind == "username" || kind == "mfa") && !s.allowed(action.Host) {
			return fmt.Errorf("refusing to send credentials to %s", action.Host)
		}
		values := f.fill(kind, s.User, s.Password, code)
		if f.Method == "GET" {
			action.RawQuery = values.Encode()
			resp, body, err = s.do(ctx, http.MethodGet, action.String(), nil)
		} else {
			resp, body, err = s.do(ctx, http.MethodPost, action.String(), strings.NewReader(values.Encode()))
		}
		if err != nil {
			return err
		}
	}
	return fmt.Errorf("login did not finish within %d steps; see the log", maxSteps)
}

// probe asks the catalogue a trivial question; JSON back means logged in.
func (s *Session) probe(ctx context.Context) bool {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.Launchpad+searchPath+"?SEARCH_MAX_RESULT=1&RESULT_PER_PAGE=1&SEARCH_STRING=SAPCAR", nil)
	req.Header.Set("Accept", "application/json")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	ok := resp.StatusCode == http.StatusOK && strings.Contains(resp.Header.Get("Content-Type"), "json")
	s.Log("probe: %s → %d %s", req.URL.Path, resp.StatusCode, resp.Header.Get("Content-Type"))
	return ok
}

func (s *Session) do(ctx context.Context, method, u string, body io.Reader) (*http.Response, string, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) KernelMan")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	s.Log("%s %s → %d (%s) %q", method, redact(u), resp.StatusCode, resp.Header.Get("Content-Type"), pageTitle(string(b)))
	return resp, string(b), nil
}

func fieldNames(f *form) []string {
	var names []string
	for n := range f.Fields {
		names = append(names, n)
	}
	return names
}

func pageTitle(body string) string {
	if m := titleRe.FindStringSubmatch(body); m != nil {
		return strings.TrimSpace(html.UnescapeString(m[1]))
	}
	return ""
}

func looksLikeRejection(body string) bool {
	b := strings.ToLower(body)
	for _, w := range []string{"could not authenticate", "invalid user", "incorrect", "wrong password", "authentication failed", "locked"} {
		if strings.Contains(b, w) {
			return true
		}
	}
	return false
}

func redact(u string) string {
	if i := strings.Index(u, "?"); i > 0 {
		return u[:i] + "?…"
	}
	return u
}
