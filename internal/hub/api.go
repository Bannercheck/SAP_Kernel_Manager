package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/version"
)

func (h *Hub) routes(mux *http.ServeMux, secure bool) {
	mux.HandleFunc("GET /api/session", h.apiSession)
	mux.HandleFunc("POST /api/setup", func(w http.ResponseWriter, r *http.Request) { h.apiSetup(w, r, secure) })
	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) { h.apiLogin(w, r, secure) })
	mux.HandleFunc("POST /api/logout", h.authed(h.apiLogout))
	mux.HandleFunc("POST /api/password", h.authed(h.apiPassword))
	mux.HandleFunc("GET /api/overview", h.authed(h.apiOverview))
	mux.HandleFunc("GET /api/hosts", h.authed(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, h.hostViews()) }))
	mux.HandleFunc("POST /api/hosts", h.authed(h.apiPutHost))
	mux.HandleFunc("PUT /api/hosts/{id}", h.authed(h.apiPutHost))
	mux.HandleFunc("DELETE /api/hosts/{id}", h.authed(h.apiDeleteHost))
	mux.HandleFunc("POST /api/hosts/{id}/refresh", h.authed(h.apiRefreshHost))
	mux.HandleFunc("POST /api/refresh", h.authed(h.apiRefreshAll))
	mux.HandleFunc("GET /api/systems/{sid}", h.authed(h.apiSystem))
	mux.HandleFunc("POST /api/systems/{sid}/lock", h.authed(h.apiLock))
	mux.HandleFunc("GET /api/jobs", h.authed(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, h.Jobs.List(r.URL.Query().Get("sid"), 200))
	}))
	mux.HandleFunc("POST /api/plan", h.authed(h.apiPlan))
	mux.HandleFunc("POST /api/jobs", h.authed(h.apiSubmit))
	mux.HandleFunc("GET /api/jobs/{id}", h.authed(h.apiJob))
	mux.HandleFunc("GET /api/jobs/{id}/events", h.authed(h.apiEvents))
	mux.HandleFunc("POST /api/jobs/{id}/cancel", h.authed(h.apiCancel))
	mux.HandleFunc("GET /api/library", h.authed(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, nonNil(h.Library.List())) }))
	mux.HandleFunc("POST /api/library", h.authed(h.apiUpload))
	mux.HandleFunc("DELETE /api/library/{name}", h.authed(h.apiDeleteArchive))
	mux.HandleFunc("GET /api/settings", h.authed(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, h.Store.Settings()) }))
	mux.HandleFunc("PUT /api/settings", h.authed(h.apiSettings))
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func (h *Hub) apiSession(w http.ResponseWriter, r *http.Request) {
	logged := false
	if c, err := r.Cookie(cookieName); err == nil {
		logged = h.sess.valid(c.Value)
	}
	name, _ := os.Hostname()
	resp := map[string]any{"setup": h.Store.Auth() == nil, "logged_in": logged, "demo": h.Demo, "version": version.Version,
		"commit": version.Commit, "hub_host": name}
	if logged {
		resp["user"] = h.Store.Auth().User
		resp["data_dir"] = h.Store.Dir()
	}
	writeJSON(w, 200, resp)
}

type credentials struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

func (h *Hub) apiSetup(w http.ResponseWriter, r *http.Request, secure bool) {
	var c credentials
	if !guard(r) || readJSON(r, &c) != nil {
		httpError(w, 400, "bad request")
		return
	}
	if h.Store.Auth() != nil {
		httpError(w, 409, "the hub is already set up")
		return
	}
	a, err := NewAuth(strings.TrimSpace(c.User), c.Password)
	if err != nil {
		httpError(w, 400, err.Error())
		return
	}
	key, _ := a.Key(c.Password)
	if err := h.Store.SetAuth(a, nil); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	h.setCookie(w, h.sess.create(key), secure)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Hub) apiLogin(w http.ResponseWriter, r *http.Request, secure bool) {
	var c credentials
	if !guard(r) || readJSON(r, &c) != nil {
		httpError(w, 400, "bad request")
		return
	}
	a := h.Store.Auth()
	if a == nil {
		httpError(w, 409, "set up the hub first")
		return
	}
	if h.sess.locked() {
		httpError(w, 429, "too many failed logins, wait a minute")
		return
	}
	key, ok := a.Verify(c.User, c.Password)
	if !ok {
		h.sess.failed()
		time.Sleep(400 * time.Millisecond)
		httpError(w, 401, "wrong user or password")
		return
	}
	h.setCookie(w, h.sess.create(key), secure)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Hub) apiLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		h.sess.drop(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Hub) apiPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if readJSON(r, &req) != nil {
		httpError(w, 400, "bad request")
		return
	}
	old := h.Store.Auth()
	oldKey, ok := old.Verify(old.User, req.Current)
	if !ok {
		httpError(w, 403, "current password is wrong")
		return
	}
	a, err := NewAuth(old.User, req.New)
	if err != nil {
		httpError(w, 400, err.Error())
		return
	}
	newKey, _ := a.Key(req.New)
	err = h.Store.SetAuth(a, func(sealed string) (string, error) {
		plain, err := Open(oldKey, sealed)
		if err != nil {
			return "", err
		}
		return Seal(newKey, plain)
	})
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	h.sess.mu.Lock()
	h.sess.key = newKey
	h.sess.mu.Unlock()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// hostViews lists the inventory with states, secrets left out.
func (h *Hub) hostViews() []HostView {
	out := []HostView{}
	for _, host := range h.Store.Hosts() {
		st := h.State(host.ID)
		v := HostView{ID: host.ID, Name: host.Name, Port: host.Port, User: host.User, Auth: host.Auth, KeyPath: host.KeyPath,
			HasSecret: host.Secret != "", Become: host.Become, SudoPW: host.SudoPW, Elevate: host.Elevate, Label: host.Label, State: st, SIDs: []string{}}
		v.State.Report = nil
		if st.Report != nil {
			v.Hostname = st.Report.Host.Hostname
			for _, s := range st.Report.Systems {
				v.SIDs = append(v.SIDs, s.SID)
			}
		}
		out = append(out, v)
	}
	return out
}

type hostForm struct {
	Name    string `json:"name"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	Auth    string `json:"auth"`
	KeyPath string `json:"key_path"`
	Secret  string `json:"secret"` // password or passphrase in clear text; sealed before it is stored
	Become  string `json:"become"`
	SudoPW  bool   `json:"sudo_pw"`
	Elevate string `json:"elevate"`
	Label   string `json:"label"`
}

func (h *Hub) apiPutHost(w http.ResponseWriter, r *http.Request) {
	var f hostForm
	if err := readJSON(r, &f); err != nil {
		httpError(w, 400, "bad request: "+err.Error())
		return
	}
	host := Host{ID: r.PathValue("id"), Name: f.Name, Port: f.Port, User: f.User, Auth: f.Auth, KeyPath: strings.TrimSpace(f.KeyPath),
		Become: f.Become, SudoPW: f.SudoPW, Elevate: f.Elevate, Label: strings.TrimSpace(f.Label)}
	if f.Secret != "" {
		sealed, err := Seal(h.sess.vaultKey(), f.Secret)
		if err != nil {
			httpError(w, 500, err.Error())
			return
		}
		host.Secret = sealed
	}
	if host.ID == "" && host.Auth != "key" && host.Secret == "" {
		httpError(w, 400, "enter the SSH password")
		return
	}
	saved, err := h.Store.PutHost(host)
	if err != nil {
		httpError(w, 400, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	refreshErr := h.Refresh(ctx, saved.ID) // test the login right away
	resp := map[string]any{"id": saved.ID}
	if refreshErr != nil {
		resp["error"] = refreshErr.Error()
	}
	writeJSON(w, 200, resp)
}

func (h *Hub) apiDeleteHost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.Store.DeleteHost(id); err != nil {
		httpError(w, 404, err.Error())
		return
	}
	h.forget(id)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Hub) apiRefreshHost(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	if err := h.Refresh(ctx, r.PathValue("id")); err != nil {
		writeJSON(w, 200, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Hub) apiRefreshAll(w http.ResponseWriter, r *http.Request) {
	go h.RefreshAll(context.Background())
	writeJSON(w, 202, map[string]bool{"started": true})
}

func (h *Hub) apiSystem(w http.ResponseWriter, r *http.Request) {
	sys, ok := h.System(r.PathValue("sid"))
	if !ok {
		httpError(w, 404, "unknown SAP system")
		return
	}
	writeJSON(w, 200, map[string]any{"system": sys, "jobs": h.Jobs.List(sys.SID, 20)})
}

func (h *Hub) apiLock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Unlock bool `json:"unlock"`
	}
	if readJSON(r, &req) != nil {
		httpError(w, 400, "bad request")
		return
	}
	sys, ok := h.System(r.PathValue("sid"))
	if !ok {
		httpError(w, 404, "unknown SAP system")
		return
	}
	until, err := h.Store.SetLock(sys.SID, req.Unlock)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	resp := map[string]any{"sid": sys.SID, "unlocked": req.Unlock}
	if req.Unlock {
		resp["until"] = until
	}
	writeJSON(w, 200, resp)
}

func (h *Hub) apiPlan(w http.ResponseWriter, r *http.Request) {
	var req JobRequest
	if err := readJSON(r, &req); err != nil {
		httpError(w, 400, "bad request: "+err.Error())
		return
	}
	req.Confirm = req.SID // a preview never runs anything; the real request must repeat the SID
	j, err := h.Plan(req)
	if err != nil {
		httpError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, j.Summary())
}

func (h *Hub) apiSubmit(w http.ResponseWriter, r *http.Request) {
	var req JobRequest
	if err := readJSON(r, &req); err != nil {
		httpError(w, 400, "bad request: "+err.Error())
		return
	}
	j, err := h.Plan(req)
	if err != nil {
		httpError(w, 400, err.Error())
		return
	}
	h.Jobs.Submit(j)
	writeJSON(w, 200, j.Summary())
}

func (h *Hub) apiJob(w http.ResponseWriter, r *http.Request) {
	j, ok := h.Jobs.Get(r.PathValue("id"))
	if !ok {
		httpError(w, 404, "unknown job")
		return
	}
	info := j.Summary()
	info.Lines, _ = j.LinesFrom(0)
	writeJSON(w, 200, info)
}

func (h *Hub) apiCancel(w http.ResponseWriter, r *http.Request) {
	if !h.Jobs.Cancel(r.PathValue("id")) {
		httpError(w, 409, "the job is not running")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// apiEvents streams a job's console and state as server-sent events.
func (h *Hub) apiEvents(w http.ResponseWriter, r *http.Request) {
	j, ok := h.Jobs.Get(r.PathValue("id"))
	if !ok {
		httpError(w, 404, "unknown job")
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		httpError(w, 500, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	ch, unsub := j.Subscribe()
	defer unsub()
	send := func(event string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	}
	for {
		lines, done := j.LinesFrom(after)
		for _, l := range lines {
			send("line", l)
			after = l.N
		}
		send("job", j.Summary())
		fl.Flush()
		if done {
			send("end", map[string]bool{"done": true})
			fl.Flush()
			return
		}
		select {
		case <-ch:
		case <-time.After(15 * time.Second):
			fmt.Fprint(w, ": keep-alive\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (h *Hub) apiUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<30)
	mr, err := r.MultipartReader()
	if err != nil {
		httpError(w, 400, "expected a multipart upload")
		return
	}
	saved := []Archive{}
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		if part.FileName() == "" {
			continue
		}
		a, err := h.Library.Save(filepath.Base(part.FileName()), part)
		if err != nil {
			httpError(w, 400, err.Error())
			return
		}
		saved = append(saved, a)
	}
	writeJSON(w, 200, saved)
}

func (h *Hub) apiDeleteArchive(w http.ResponseWriter, r *http.Request) {
	if err := h.Library.Delete(r.PathValue("name")); err != nil {
		httpError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Hub) apiSettings(w http.ResponseWriter, r *http.Request) {
	var s Settings
	if err := readJSON(r, &s); err != nil {
		httpError(w, 400, "bad request")
		return
	}
	if err := h.Store.PutSettings(s); err != nil {
		httpError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, h.Store.Settings())
}
