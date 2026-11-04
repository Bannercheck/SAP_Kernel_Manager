package hub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Step is one unit of a job, run on one host.
type Step struct {
	Host     string    `json:"host"` // host ID
	HostName string    `json:"host_name"`
	Title    string    `json:"title"`
	Kind     string    `json:"kind"`               // agent | push | refresh
	Args     []string  `json:"args,omitempty"`     // agent arguments; "{pushdir}" is replaced by the push target
	Archives []string  `json:"archives,omitempty"` // push: library names
	State    string    `json:"state"`              // pending running ok failed skipped
	Exit     int       `json:"exit"`
	Detail   string    `json:"detail,omitempty"`
	Started  time.Time `json:"started,omitempty"`
	Ended    time.Time `json:"ended,omitempty"`
}

// Line is one line of a job's console.
type Line struct {
	N    int       `json:"n"`
	T    time.Time `json:"t"`
	Host string    `json:"host,omitempty"`
	Text string    `json:"text"`
	Kind string    `json:"kind"` // out | info | step | ok | fail
}

// JobInfo is the stored part of a job.
type JobInfo struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Title   string    `json:"title"`
	SID     string    `json:"sid,omitempty"`
	Host    string    `json:"host,omitempty"` // set when the job runs on one host only
	State   string    `json:"state"`          // queued running ok failed cancelled interrupted
	Created time.Time `json:"created"`
	Started time.Time `json:"started,omitempty"`
	Ended   time.Time `json:"ended,omitempty"`
	Steps   []*Step   `json:"steps"`
	Lines   []Line    `json:"lines,omitempty"`
}

// Job is an operation on one system or host: ordered steps and a console.
type Job struct {
	JobInfo

	mu       sync.Mutex
	stopReq  bool // stop after the running step (never cut SAPCAR or a backup in half)
	subs     map[chan struct{}]struct{}
	cancel   context.CancelFunc
	targets  map[string]Target // opened secrets per host ID, never stored
	pushDirs map[string]string // host ID → directory the archives were pushed to
}

const maxLines = 20000

// Summary is the job without its console, for lists.
func (j *Job) Summary() JobInfo {
	j.mu.Lock()
	defer j.mu.Unlock()
	s := JobInfo{ID: j.ID, Kind: j.Kind, Title: j.Title, SID: j.SID, Host: j.Host, State: j.State, Created: j.Created, Started: j.Started, Ended: j.Ended}
	for _, st := range j.Steps {
		c := *st
		s.Steps = append(s.Steps, &c)
	}
	return s
}

// LinesFrom returns the console lines after n and whether the job has finished.
func (j *Job) LinesFrom(n int) ([]Line, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if n < 0 || n > len(j.Lines) {
		n = len(j.Lines)
	}
	return append([]Line(nil), j.Lines[n:]...), j.finished()
}

func (j *Job) finished() bool {
	switch j.State {
	case "ok", "failed", "cancelled", "interrupted":
		return true
	}
	return false
}

// Subscribe returns a channel that is signalled on every change.
func (j *Job) Subscribe() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	j.mu.Lock()
	if j.subs == nil {
		j.subs = map[chan struct{}]struct{}{}
	}
	j.subs[ch] = struct{}{}
	j.mu.Unlock()
	return ch, func() {
		j.mu.Lock()
		delete(j.subs, ch)
		j.mu.Unlock()
	}
}

func (j *Job) notify() {
	for ch := range j.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// log appends a console line.
func (j *Job) log(host, kind, text string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.Lines) >= maxLines {
		return
	}
	j.Lines = append(j.Lines, Line{N: len(j.Lines) + 1, T: time.Now(), Host: host, Text: text, Kind: kind})
	j.notify()
}

// update changes job or step fields under the lock.
func (j *Job) update(fn func()) {
	j.mu.Lock()
	defer j.mu.Unlock()
	fn()
	j.notify()
}

// Jobs queues jobs and runs them one at a time, keeping a history on disk.
type Jobs struct {
	dir   string
	mu    sync.Mutex
	list  []*Job
	queue chan *Job
	run   func(context.Context, *Job) error
}

// OpenJobs loads the history from dir and starts the worker.
func OpenJobs(dir string, run func(context.Context, *Job) error) (*Jobs, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	m := &Jobs{dir: dir, queue: make(chan *Job, 100), run: run}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		j := &Job{}
		if err != nil || json.Unmarshal(b, j) != nil {
			continue
		}
		if !j.finished() { // the hub stopped while it ran
			j.State = "interrupted"
		}
		m.list = append(m.list, j)
	}
	sort.Slice(m.list, func(a, b int) bool { return m.list[a].Created.Before(m.list[b].Created) })
	if len(m.list) > 300 {
		m.list = m.list[len(m.list)-300:]
	}
	go m.worker()
	return m, nil
}

// Submit queues a job.
func (m *Jobs) Submit(j *Job) {
	j.State, j.Created = "queued", time.Now()
	m.mu.Lock()
	m.list = append(m.list, j)
	m.mu.Unlock()
	m.save(j)
	m.queue <- j
}

// Get returns a job by ID.
func (m *Jobs) Get(id string) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.list {
		if j.ID == id {
			return j, true
		}
	}
	return nil, false
}

// List returns summaries, newest first, optionally only for one SID.
func (m *Jobs) List(sid string, limit int) []JobInfo {
	m.mu.Lock()
	jobs := append([]*Job(nil), m.list...)
	m.mu.Unlock()
	out := []JobInfo{}
	for i := len(jobs) - 1; i >= 0 && (limit <= 0 || len(out) < limit); i-- {
		if sid == "" || jobs[i].SID == sid {
			out = append(out, jobs[i].Summary())
		}
	}
	return out
}

// Busy reports whether a job is queued or running.
func (m *Jobs) Busy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.list {
		if j.State == "queued" || j.State == "running" {
			return true
		}
	}
	return false
}

// errStopped ends a job whose operator asked it to stop.
var errStopped = errors.New("stopped by the operator before the next step")

// Cancel stops a queued job, or a running one after its current step: a
// kernel extraction or backup is never interrupted half way.
func (m *Jobs) Cancel(id string) bool {
	j, ok := m.Get(id)
	if !ok {
		return false
	}
	j.mu.Lock()
	switch j.State {
	case "queued":
		j.State = "cancelled"
	case "running":
		j.stopReq = true
	default:
		j.mu.Unlock()
		return false
	}
	j.notify()
	running := j.State == "running"
	j.mu.Unlock()
	if running {
		j.log("", "info", "stop requested: the current step finishes, the remaining steps are skipped")
	}
	return true
}

func (j *Job) stopRequested() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.stopReq
}

func (m *Jobs) worker() {
	for j := range m.queue {
		ctx, cancel := context.WithCancel(context.Background())
		skip := false
		j.update(func() {
			if j.State == "cancelled" {
				skip = true
				return
			}
			j.State, j.Started, j.cancel = "running", time.Now(), cancel
		})
		if !skip {
			err := m.run(ctx, j)
			j.update(func() {
				j.Ended = time.Now()
				switch {
				case ctx.Err() != nil || errors.Is(err, errStopped):
					j.State = "cancelled"
				case err != nil:
					j.State = "failed"
				default:
					j.State = "ok"
				}
				j.targets = nil // drop the opened secrets
			})
		}
		cancel()
		m.save(j)
	}
}

func (m *Jobs) save(j *Job) {
	j.mu.Lock()
	b, err := json.Marshal(j)
	j.mu.Unlock()
	if err == nil {
		_ = os.WriteFile(filepath.Join(m.dir, j.ID+".json"), b, 0o600)
	}
}
