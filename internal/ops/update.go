package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/kernel"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
)

// ExtractStep is the outcome of one SAPCAR -xvf.
type ExtractStep struct {
	File  string
	Files int // entries extracted
	Err   error
}

// UpdateResult describes a finished Kernel Update or Rollback.
type UpdateResult struct {
	Steps       []ExtractStep
	Before      kernel.Version
	After       kernel.Version
	ChownNote   string
	SaprootNote string
}

// FindSAPCAR looks for SAPCAR in the kernel directory, PATH and extra dirs
// (the download directory often holds SAPCAR_<n>-<n>.EXE).
func FindSAPCAR(e *Env, extraDirs ...string) (string, error) {
	cands := []string{filepath.Join(e.T.KernelDir, "SAPCAR")}
	if p, err := e.R.LookPath("SAPCAR"); err == nil {
		cands = append(cands, p)
	}
	for _, d := range extraDirs {
		if d == "" {
			continue
		}
		cands = append(cands, filepath.Join(d, "SAPCAR"))
		if m, _ := filepath.Glob(filepath.Join(d, "SAPCAR*")); len(m) > 0 {
			cands = append(cands, m...)
		}
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return c, nil
		}
	}
	return "", errors.New("SAPCAR not found (kernel directory, PATH, download directory)")
}

// Extract applies the archives in order into the kernel directory, then
// fixes ownership, runs saproot.sh when possible and reads the new level.
func Extract(ctx context.Context, e *Env, sapcar string, files []SARFile) (*UpdateResult, error) {
	res := &UpdateResult{}
	if len(files) == 0 {
		return res, errors.New("no archives to extract")
	}
	SortForApply(files)
	n := len(files) + 3
	res.Before, _ = kernel.Probe(ctx, e.R, e.P, e.T.KernelDir)
	if res.Before.Release > 0 {
		e.Pr.Info("current kernel: " + res.Before.String())
	}
	for i, f := range files {
		st := ExtractStep{File: f.Name}
		if err := e.step(i+1, n, fmt.Sprintf("SAPCAR -xvf %s  (%s %d)", f.Name, f.Component, f.Patch), func() (string, error) {
			src := f.Path
			if src == "" {
				src = filepath.Join(e.T.KernelDir, f.Name)
			}
			out, err := e.run(ctx, exec.Cmd{Path: sapcar, Args: []string{"-xvf", src}, Dir: e.T.KernelDir, RunAs: e.asAdm(), Timeout: time.Hour})
			st.Files = countExtracted(out.Stdout)
			st.Err = err
			return fmt.Sprintf("%d files", st.Files), err
		}); err != nil {
			res.Steps = append(res.Steps, st)
			return res, err
		}
		res.Steps = append(res.Steps, st)
	}
	return finishKernelChange(ctx, e, res, len(files)+1, n)
}

// Restore copies a backup over the kernel directory (Kernel Rollback).
func Restore(ctx context.Context, e *Env, backupDir string) (*UpdateResult, error) {
	res := &UpdateResult{}
	const n = 4
	if st, err := os.Stat(backupDir); err != nil || !st.IsDir() {
		return res, fmt.Errorf("backup %s: not a directory", backupDir)
	}
	res.Before, _ = kernel.Probe(ctx, e.R, e.P, e.T.KernelDir)
	if err := e.step(1, n, "Copy "+backupDir+" over "+e.T.KernelDir, func() (string, error) {
		_, err := e.run(ctx, exec.Cmd{Path: "cp", Args: []string{"-pR", backupDir + "/.", e.T.KernelDir + "/"}, RunAs: e.asAdm(), Timeout: 2 * time.Hour})
		return "cp -pR", err
	}); err != nil {
		return res, err
	}
	return finishKernelChange(ctx, e, res, 2, n)
}

// finishKernelChange runs the common tail: chown, saproot.sh, version probe.
func finishKernelChange(ctx context.Context, e *Env, res *UpdateResult, i, n int) (*UpdateResult, error) {
	if err := e.step(i, n, fmt.Sprintf("chown -R %s:%s %s", e.T.SIDAdm, e.T.Group, e.T.KernelDir), func() (string, error) {
		return chownTree(ctx, e, e.T.KernelDir, &res.ChownNote)
	}); err != nil {
		return res, err
	}
	if err := e.step(i+1, n, "saproot.sh "+e.T.SID, func() (string, error) {
		script := filepath.Join(e.T.KernelDir, "saproot.sh")
		if _, err := os.Stat(script); err != nil {
			res.SaprootNote = "skipped: saproot.sh not in kernel directory"
			return res.SaprootNote, nil
		}
		if !e.IsRoot {
			res.SaprootNote = "skipped: needs root (later: " + script + " " + e.T.SID + ")"
			return res.SaprootNote, nil
		}
		_, err := e.run(ctx, exec.Cmd{Path: script, Args: []string{e.T.SID}, Dir: e.T.KernelDir, Timeout: 10 * time.Minute})
		if err == nil {
			res.SaprootNote = "done"
		}
		return res.SaprootNote, err
	}); err != nil {
		return res, err
	}
	err := e.step(i+2, n, "Read kernel version (disp+work -V)", func() (string, error) {
		v, err := kernel.Probe(ctx, e.R, e.P, e.T.KernelDir)
		res.After = v
		return v.String(), err
	})
	if err == nil {
		_ = e.T.SaveSnapshot(func(s *system.Snapshot) { s.LastUpdateAt = e.now() })
	}
	return res, err
}

// countExtracted counts "x <file>" lines in SAPCAR output.
func countExtracted(out string) int {
	c := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "x ") {
			c++
		}
	}
	return c
}
