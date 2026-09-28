// Package ops implements the KernelMan operations (Kernel Backup, Kernel
// Files, SAP Stop/Start, Kernel Update, Rollback) on top of exec.Runner and
// the resolved system.Target. It reports progress through Progress and
// never prints on its own.
package ops

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/platform"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
)

// Progress receives step-by-step feedback. The CLI renders it with colours.
type Progress interface {
	Begin(i, n int, title string)
	End(err error, detail string, d time.Duration)
	Info(line string)
	Block(title, text string)
}

// Env bundles what every operation needs.
type Env struct {
	R            exec.Runner
	P            platform.Platform
	T            *system.Target
	Pr           Progress
	IsRoot       bool
	CentralOnly  bool   // touch only DIR_CT_RUN (distributed systems: the other hosts take it via sapcpe)
	User         string // login name of the current user
	Now          func() time.Time
	StopTimeout  time.Duration
	StartTimeout time.Duration
}

func (e *Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// asAdm is the user file operations run as: <sid>adm when we are root,
// otherwise the current user (RunAs "" = no switch).
func (e *Env) asAdm() string {
	if e.IsRoot {
		return e.T.SIDAdm
	}
	return ""
}

// Step runs fn as numbered step i of n and reports the outcome through Pr.
func (e *Env) Step(i, n int, title string, fn func() (string, error)) error {
	e.Pr.Begin(i, n, title)
	start := e.now()
	detail, err := fn()
	e.Pr.End(err, detail, e.now().Sub(start))
	return err
}

// step is the internal alias of Step.
func (e *Env) step(i, n int, title string, fn func() (string, error)) error {
	return e.Step(i, n, title, fn)
}

// run executes a command and turns a non-zero exit into an error.
func (e *Env) run(ctx context.Context, c exec.Cmd) (exec.Result, error) {
	res, err := e.R.Run(ctx, c)
	if err != nil {
		return res, err
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return res, fmt.Errorf("%s failed (exit %d): %s", filepath.Base(c.Path), res.ExitCode, lastLines(msg, 3))
	}
	return res, nil
}

// treeStats counts regular files and sums their sizes under dir.
func treeStats(dir string) (files int, bytes int64, err error) {
	err = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.Type().IsRegular() {
			info, ierr := d.Info()
			if ierr != nil {
				return ierr
			}
			files++
			bytes += info.Size()
		}
		return nil
	})
	return files, bytes, err
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// HumanSize renders bytes as KB/MB/GB.
func HumanSize(b int64) string {
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
