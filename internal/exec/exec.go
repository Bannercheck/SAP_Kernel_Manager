// Package exec is the single gateway for running external programs
// (sapcontrol, saphostctrl, disp+work, ...). All business logic depends on
// the Runner interface so it can be tested with Fake.
package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	osexec "os/exec"
	"strings"
	"time"
)

// ErrNotFound is returned when the executable does not exist.
var ErrNotFound = errors.New("executable not found")

// Cmd describes one external program invocation.
type Cmd struct {
	Path    string
	Args    []string
	Env     []string      // KEY=VALUE pairs appended to the current environment
	Dir     string        // working directory ("" = inherit)
	RunAs   string        // Unix only: run via sudo -u <user>; "" = current user
	Timeout time.Duration // 0 = runner default
}

// String renders the command line; it is also the lookup key used by Fake.
func (c Cmd) String() string {
	if len(c.Args) == 0 {
		return c.Path
	}
	return c.Path + " " + strings.Join(c.Args, " ")
}

// Result is the outcome of a finished process. A non-zero ExitCode is not an
// error: several SAP tools (sapcontrol) encode status in the exit code.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// Runner executes commands and resolves executables.
type Runner interface {
	Run(ctx context.Context, c Cmd) (Result, error)
	LookPath(file string) (string, error)
}

// Real runs commands on the local host.
type Real struct {
	DefaultTimeout time.Duration
	SudoCmd        []string // prefix used when Cmd.RunAs is set, e.g. ["sudo", "-n"]
}

// NewReal returns a Real runner with sane defaults.
func NewReal() *Real {
	return &Real{DefaultTimeout: 2 * time.Minute, SudoCmd: []string{"sudo", "-n"}}
}

// LookPath resolves file in PATH.
func (r *Real) LookPath(file string) (string, error) {
	p, err := osexec.LookPath(file)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNotFound, file)
	}
	return p, nil
}

// Run executes c and captures its output.
func (r *Real) Run(ctx context.Context, c Cmd) (Result, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = r.DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	path, args := c.Path, c.Args
	if c.RunAs != "" {
		if len(r.SudoCmd) == 0 {
			return Result{}, errors.New("RunAs requested but no sudo command configured")
		}
		args = append(append(append([]string{}, r.SudoCmd[1:]...), "-u", c.RunAs, path), c.Args...)
		path = r.SudoCmd[0]
	}

	cmd := osexec.CommandContext(ctx, path, args...)
	cmd.Dir = c.Dir
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String(), Duration: time.Since(start)}

	var exitErr *osexec.ExitError
	switch {
	case err == nil:
		return res, nil
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
		if ctx.Err() != nil {
			return res, fmt.Errorf("%s: timed out after %s", c.Path, timeout)
		}
		return res, nil
	case errors.Is(err, osexec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		return res, fmt.Errorf("%w: %s", ErrNotFound, path)
	default:
		return res, fmt.Errorf("%s: %w", c.Path, err)
	}
}
