package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config sets up a hub.
type Config struct {
	DataDir string
	Remote  Remote // nil: SSH
	Demo    bool
}

// Hub ties the inventory, the remote access, the job queue and the dashboard together.
type Hub struct {
	Store   *Store
	Library *Library
	Jobs    *Jobs
	Remote  Remote
	Demo    bool
	Started time.Time

	agents agentLookup
	sess   *sessions
	mu     sync.Mutex
	states map[string]HostState
}

// New opens the data directory and starts the job worker.
func New(cfg Config) (*Hub, error) {
	st, err := OpenStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	lib, err := OpenLibrary(filepath.Join(cfg.DataDir, "library"))
	if err != nil {
		return nil, err
	}
	h := &Hub{Store: st, Library: lib, Remote: cfg.Remote, Demo: cfg.Demo, Started: time.Now(), agents: loadAgent, sess: newSessions(), states: map[string]HostState{}}
	if h.Remote == nil {
		h.Remote = &SSHRemote{Store: st}
	}
	if b, err := os.ReadFile(h.statePath()); err == nil {
		_ = json.Unmarshal(b, &h.states)
		for id, s := range h.states {
			s.Refreshing = false
			h.states[id] = s
		}
	}
	if h.Jobs, err = OpenJobs(filepath.Join(cfg.DataDir, "jobs"), h.runJob); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *Hub) statePath() string { return filepath.Join(h.Store.Dir(), "state.json") }

// target opens a host's stored secret with the vault key.
func (h *Hub) target(host Host) (Target, error) {
	t := Target{Host: host}
	if host.Secret == "" {
		return t, nil
	}
	plain, err := Open(h.sess.vaultKey(), host.Secret)
	if err != nil {
		return t, fmt.Errorf("%s: %w", host.Name, err)
	}
	if host.Auth == "key" {
		t.Passphrase = plain
	} else {
		t.Password = plain
	}
	return t, nil
}

// State returns the last known state of a host.
func (h *Hub) State(id string) HostState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.states[id]
}

func (h *Hub) setState(id string, fn func(*HostState)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.states[id]
	fn(&s)
	h.states[id] = s
	if b, err := json.Marshal(h.states); err == nil {
		_ = os.WriteFile(h.statePath(), b, 0o600)
	}
}

func (h *Hub) forget(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.states, id)
}

// Refresh connects to a host, installs or updates the agent and reads its status.
func (h *Hub) Refresh(ctx context.Context, id string) error {
	host, ok := h.Store.Host(id)
	if !ok {
		return fmt.Errorf("host %s not found", id)
	}
	h.setState(id, func(s *HostState) { s.Refreshing = true })
	err := h.refresh(ctx, host)
	if err != nil {
		h.setState(id, func(s *HostState) { s.Refreshing, s.OK, s.Error, s.Checked = false, false, err.Error(), time.Now() })
	}
	return err
}

func (h *Hub) refresh(ctx context.Context, host Host) error {
	t, err := h.target(host)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	c, err := h.Remote.Dial(ctx, t)
	if err != nil {
		return err
	}
	defer c.Close()
	ri, err := ensureAgent(ctx, c, host, h.agents, func(string) {})
	if err != nil {
		return err
	}
	var out, errb bytes.Buffer
	statusSID := "" // status reads run as the login user, or as root when the host runs operations as root
	cmd, stdin := agentCmd(t, ri, statusSID, "status", "--output", "json")
	code, err := c.Run(ctx, cmd, stdin, &out, &errb)
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	st := HostState{Checked: time.Now(), OK: true, OS: ri.OS, Machine: ri.Machine, Fingerprint: c.Fingerprint()}
	if err := json.Unmarshal(out.Bytes(), &st.Report); err != nil {
		msg := lastLine(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("status (exit %d): %s", code, msg)
	}
	h.setState(host.ID, func(s *HostState) { *s = st })
	return nil
}

// RefreshAll refreshes every host, six at a time.
func (h *Hub) RefreshAll(ctx context.Context) {
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for _, host := range h.Store.Hosts() {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			_ = h.Refresh(ctx, id)
		}(host.ID)
	}
	wg.Wait()
}

// Systems groups all host reports by SID.
func (h *Hub) Systems() []SystemView {
	hosts := h.Store.Hosts()
	h.mu.Lock()
	states := make(map[string]HostState, len(h.states))
	for k, v := range h.states {
		states[k] = v
	}
	h.mu.Unlock()
	sys := buildSystems(hosts, states)
	for i := range sys {
		if until, ok := h.Store.Unlocked(sys[i].SID); ok {
			sys[i].Unlocked = &until
		}
	}
	return sys
}

// System returns one system.
func (h *Hub) System(sid string) (SystemView, bool) {
	for _, s := range h.Systems() {
		if strings.EqualFold(s.SID, sid) {
			return s, true
		}
	}
	return SystemView{}, false
}

// runJob executes the steps in order and stops at the first failure.
func (h *Hub) runJob(ctx context.Context, j *Job) error {
	n := len(j.Steps)
	touched := map[string]bool{}
	var failed error
	for i, st := range j.Steps {
		touched[st.Host] = true
		if failed == nil && j.stopRequested() {
			failed = errStopped
		}
		if failed != nil || ctx.Err() != nil {
			j.update(func() { st.State = "skipped" })
			continue
		}
		j.update(func() { st.State, st.Started = "running", time.Now() })
		j.log(st.HostName, "step", fmt.Sprintf("[%d/%d] %s", i+1, n, st.Title))
		err := h.runStep(ctx, j, st)
		j.update(func() {
			st.Ended = time.Now()
			st.State = "ok"
			if err != nil {
				st.State, st.Detail = "failed", err.Error()
			}
		})
		if err != nil {
			if ctx.Err() != nil {
				err = errors.New("cancelled")
			}
			j.log(st.HostName, "fail", fmt.Sprintf("✘ %s: %v", st.Title, err))
			failed = err
			continue
		}
		j.log(st.HostName, "ok", fmt.Sprintf("✔ %s (%s)", st.Title, st.Ended.Sub(st.Started).Round(time.Second)))
	}
	go func() { // the dashboard shows the new state right after the job
		for id := range touched {
			_ = h.Refresh(context.Background(), id)
		}
	}()
	if failed == nil && ctx.Err() == nil {
		j.log("", "ok", "✔ "+j.Title+" finished")
	}
	return failed
}

func (h *Hub) runStep(ctx context.Context, j *Job, st *Step) error {
	switch st.Kind {
	case "refresh":
		return h.Refresh(ctx, st.Host)
	case "preflight":
		return h.preflight(ctx, j, st)
	case "verify":
		return h.verify(ctx, j, st)
	}
	host, ok := h.Store.Host(st.Host)
	if !ok {
		return fmt.Errorf("host %s was removed", st.HostName)
	}
	c, err := h.Remote.Dial(ctx, j.targets[st.Host])
	if err != nil {
		return err
	}
	defer c.Close()
	if c.NewKey() {
		j.log(st.HostName, "info", "new host key trusted: "+c.Fingerprint())
	}
	ri, err := ensureAgent(ctx, c, host, h.agents, func(s string) { j.log(st.HostName, "info", s) })
	if err != nil {
		return err
	}
	if st.Kind == "push" {
		return h.push(ctx, j, st, c)
	}
	var args []string
	for i := 0; i < len(st.Args); i++ {
		a := st.Args[i]
		if a == "{pushdir}" {
			a = j.pushDirs[st.Host]
		}
		args = append(args, a)
	}
	lw := &lineWriter{emit: func(s string) { j.log(st.HostName, "out", s) }}
	cmd, stdin := agentCmd(j.targets[st.Host], ri, j.SID, args...)
	code, err := c.Run(ctx, cmd, stdin, lw, lw)
	lw.Flush()
	j.update(func() { st.Exit = code })
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("kernelman %s ended with exit code %d", args[0], code)
	}
	return nil
}

// pushDirScript prints the directory archives go to: the configured one
// when it can be created and written, else ~/.kernelman/download.
const pushDirScript = `d="$1"; if mkdir -p "$d" 2>/dev/null && test -w "$d"; then :; else d="$HOME/.kernelman/download"; mkdir -p "$d" || exit 1; fi; echo "$d"`

// push copies library files to the host and proves them with cksum.
func (h *Hub) push(ctx context.Context, j *Job, st *Step, c Conn) error {
	out, err := output(ctx, c, shCmd(pushDirScript, h.Store.Settings().RemoteDir))
	if err != nil {
		return fmt.Errorf("prepare remote directory: %w", err)
	}
	dir := strings.TrimSpace(out)
	j.log(st.HostName, "info", "target directory "+dir)
	for _, name := range st.Archives {
		a, err := h.Library.Get(name)
		if err != nil {
			return err
		}
		remote := dir + "/" + name
		if have, _ := output(ctx, c, shCmd(`cksum "$1" 2>/dev/null || true`, remote)); sumOf(have) == a.Cksum {
			// already there: re-date it, Kernel File Transfer takes only files placed today
			if _, err := output(ctx, c, shCmd(`touch "$1"`, remote)); err != nil {
				return err
			}
			j.log(st.HostName, "out", fmt.Sprintf("  %s already on the host, cksum matches", name))
			continue
		}
		f, err := os.Open(h.Library.Path(name))
		if err != nil {
			return err
		}
		pr := &progressReader{r: f, size: a.Size, report: func(pct int, done int64) {
			j.log(st.HostName, "info", fmt.Sprintf("  ↑ %s %3d%%  %s of %s", name, pct, humanBytes(done), humanBytes(a.Size)))
		}}
		err = c.Upload(ctx, pr, remote, 0o644)
		f.Close()
		if err != nil {
			return err
		}
		have, err := output(ctx, c, shCmd(`cksum "$1"`, remote))
		if err != nil {
			return fmt.Errorf("cksum %s: %w", remote, err)
		}
		if got := sumOf(have); got != a.Cksum {
			return fmt.Errorf("%s arrived damaged: cksum %s on the host, %s in the hub", name, got, a.Cksum)
		}
		j.log(st.HostName, "out", fmt.Sprintf("  %s ... ok  %s, cksum %s verified", name, humanBytes(a.Size), strings.Fields(a.Cksum)[0]))
	}
	j.update(func() { j.pushDirs[st.Host] = dir })
	return nil
}

func sumOf(cksumOutput string) string {
	s, _ := parseCksumLine(strings.TrimSpace(cksumOutput))
	return s
}

// progressReader reports every 10%.
type progressReader struct {
	r      io.Reader
	size   int64
	done   int64
	last   int
	report func(pct int, done int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.size > 0 {
		if pct := int(p.done * 100 / p.size); pct/10 > p.last/10 || (pct == 100 && p.last != 100) {
			p.last = pct
			p.report(pct, p.done)
		}
	}
	return n, err
}

func humanBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	}
	return fmt.Sprintf("%d B", b)
}
