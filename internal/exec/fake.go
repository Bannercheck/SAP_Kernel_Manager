package exec

import (
	"context"
	"fmt"
	"sync"
)

// Fake is a scripted Runner for tests. Responses are keyed by Cmd.String().
type Fake struct {
	mu        sync.Mutex
	Responses map[string]Result
	Errors    map[string]error
	Paths     map[string]string // LookPath answers
	Calls     []Cmd
}

// NewFake returns an empty Fake.
func NewFake() *Fake {
	return &Fake{Responses: map[string]Result{}, Errors: map[string]error{}, Paths: map[string]string{}}
}

// On registers stdout and exit code for an exact command line.
func (f *Fake) On(cmdline, stdout string, exitCode int) *Fake {
	f.Responses[cmdline] = Result{Stdout: stdout, ExitCode: exitCode}
	return f
}

// OnError makes a command line fail with err.
func (f *Fake) OnError(cmdline string, err error) *Fake {
	f.Errors[cmdline] = err
	return f
}

// Run replays the scripted answer or fails on an unexpected command.
func (f *Fake) Run(_ context.Context, c Cmd) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, c)
	key := c.String()
	if err, ok := f.Errors[key]; ok {
		return Result{}, err
	}
	if r, ok := f.Responses[key]; ok {
		return r, nil
	}
	return Result{}, fmt.Errorf("fake runner: unexpected command %q", key)
}

// LookPath answers from Paths.
func (f *Fake) LookPath(file string) (string, error) {
	if p, ok := f.Paths[file]; ok {
		return p, nil
	}
	return "", fmt.Errorf("%w: %s", ErrNotFound, file)
}
