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
	Seq       map[string][]Result // consumed in order; the last one sticks
	Errors    map[string]error
	Paths     map[string]string // LookPath answers
	Calls     []Cmd
}

// NewFake returns an empty Fake.
func NewFake() *Fake {
	return &Fake{Responses: map[string]Result{}, Seq: map[string][]Result{}, Errors: map[string]error{}, Paths: map[string]string{}}
}

// On registers stdout and exit code for an exact command line.
func (f *Fake) On(cmdline, stdout string, exitCode int) *Fake {
	f.Responses[cmdline] = Result{Stdout: stdout, ExitCode: exitCode}
	return f
}

// OnSeq registers successive answers for one command line: each call
// consumes the next one, the last answer repeats forever.
func (f *Fake) OnSeq(cmdline string, results ...Result) *Fake {
	f.Seq[cmdline] = results
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
	if seq, ok := f.Seq[key]; ok && len(seq) > 0 {
		r := seq[0]
		if len(seq) > 1 {
			f.Seq[key] = seq[1:]
		}
		return r, nil
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
