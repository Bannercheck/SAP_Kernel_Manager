package cli

import (
	"path/filepath"
	"sync"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
)

// sapSim is a stateful sapcontrol for the flow tests: StopService really
// takes sapstartsrv down until StartService brings it back, StopSystem and
// StartSystem switch the processes. Other functions go to the scripted fake.
type sapSim struct {
	mu      sync.Mutex
	up      map[string]bool // sapstartsrv answers
	running map[string]bool // processes GREEN
	calls   []string        // "00 StopService", in order
}

func newSim(up, running bool) *sapSim {
	s := &sapSim{up: map[string]bool{}, running: map[string]bool{}}
	for _, nr := range []string{"00", "01"} {
		s.up[nr], s.running[nr] = up, running
	}
	return s
}

func (s *sapSim) run(c exec.Cmd) (exec.Result, bool) {
	if filepath.Base(c.Path) != "sapcontrol" {
		return exec.Result{}, false
	}
	nr, fn := "", ""
	for i := 0; i+1 < len(c.Args); i++ {
		switch c.Args[i] {
		case "-nr":
			nr = c.Args[i+1]
		case "-function":
			fn = c.Args[i+1]
		}
	}
	switch fn {
	case "GetProcessList", "StopService", "StartService", "StopSystem", "StartSystem":
	default:
		return exec.Result{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if fn != "GetProcessList" {
		s.calls = append(s.calls, nr+" "+fn)
	}
	if fn == "StartService" {
		s.up[nr] = true
		return exec.Result{Stdout: okBody(fn)}, true
	}
	if !s.up[nr] {
		return down, true
	}
	switch fn {
	case "GetProcessList":
		if s.running[nr] {
			return procs("GREEN", 3), true
		}
		return procs("GRAY", 4), true
	case "StopService":
		s.up[nr] = false
	case "StopSystem":
		for k := range s.running {
			s.running[k] = false
		}
	case "StartSystem":
		for k := range s.running {
			s.running[k] = s.up[k]
		}
	}
	return exec.Result{Stdout: okBody(fn)}, true
}

func (s *sapSim) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}
