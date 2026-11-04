package hub

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type client struct {
	t    *testing.T
	base string
	c    *http.Client
}

func (c client) do(method, path, body string, csrf bool) (*http.Response, map[string]any) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if csrf {
		req.Header.Set("X-KernelMan", "1")
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var v map[string]any
	b, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(b, &v)
	return resp, v
}

func TestAPISecurityAndFlow(t *testing.T) {
	h, r := demoHub(t)
	r.Delay = 2 * time.Millisecond
	srv := httptest.NewServer(h.Handler(false))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := client{t: t, base: srv.URL, c: &http.Client{Jar: jar}}

	resp, _ := c.do("GET", "/", "", false)
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Errorf("security headers missing: %v", resp.Header)
	}
	if resp, _ := c.do("GET", "/api/overview", "", false); resp.StatusCode != 401 {
		t.Errorf("overview without login: %d", resp.StatusCode)
	}
	if resp, _ := c.do("POST", "/api/login", `{"user":"admin","password":"kernelman1"}`, false); resp.StatusCode != 400 {
		t.Errorf("login without the anti-CSRF header: %d", resp.StatusCode)
	}
	if resp, _ := c.do("POST", "/api/login", `{"user":"admin","password":"nope-nope-nope"}`, true); resp.StatusCode != 401 {
		t.Errorf("wrong password: %d", resp.StatusCode)
	}
	resp, _ = c.do("POST", "/api/login", `{"user":"admin","password":"kernelman1"}`, true)
	if resp.StatusCode != 200 {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	if ck := resp.Header.Get("Set-Cookie"); !strings.Contains(ck, "HttpOnly") || !strings.Contains(ck, "SameSite=Strict") {
		t.Errorf("cookie flags: %s", ck)
	}
	if resp, _ := c.do("POST", "/api/systems/QAS/lock", `{"unlock":true}`, false); resp.StatusCode != 403 {
		t.Errorf("unlock without the anti-CSRF header: %d", resp.StatusCode)
	}
	_, ov := c.do("GET", "/api/overview", "", false)
	if kpi := ov["kpi"].(map[string]any); kpi["systems"].(float64) != 3 || kpi["hosts"].(float64) != 7 {
		t.Errorf("kpi %v", kpi)
	}
	hosts := ov["hosts"].([]any)
	if strings.Contains(strings.ToLower(mustJSON(hosts)), `"secret"`) {
		t.Error("host secrets leak into the overview")
	}
	if resp, v := c.do("POST", "/api/jobs", `{"kind":"backup","sid":"QAS"}`, true); resp.StatusCode != 400 || !strings.Contains(v["error"].(string), "read-only") {
		t.Errorf("job on a locked system: %d %v", resp.StatusCode, v)
	}
	c.do("POST", "/api/systems/QAS/lock", `{"unlock":true}`, true)
	resp, job := c.do("POST", "/api/jobs", `{"kind":"backup","sid":"QAS"}`, true)
	if resp.StatusCode != 200 {
		t.Fatalf("submit: %d %v", resp.StatusCode, job)
	}
	// the console streams until the job ends
	req, _ := http.NewRequest("GET", srv.URL+"/api/jobs/"+job["id"].(string)+"/events?after=0", nil)
	sse, err := c.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer sse.Body.Close()
	sc := bufio.NewScanner(sse.Body)
	var events []string
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "event: ") {
			events = append(events, strings.TrimPrefix(line, "event: "))
			if line == "event: end" {
				break
			}
		}
	}
	all := strings.Join(events, " ")
	if !strings.Contains(all, "line") || !strings.Contains(all, "job") || !strings.HasSuffix(all, "end") {
		t.Errorf("events: %s", all)
	}
	_, done := c.do("GET", "/api/jobs/"+job["id"].(string), "", false)
	if done["state"] != "ok" {
		t.Errorf("job state %v", done["state"])
	}
	c.do("POST", "/api/logout", "", true)
	if resp, _ := c.do("GET", "/api/overview", "", false); resp.StatusCode != 401 {
		t.Errorf("overview after logout: %d", resp.StatusCode)
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
