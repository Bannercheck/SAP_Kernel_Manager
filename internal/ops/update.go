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

// ExtractStep is the outcome of one SAPCAR -xvf in one directory.
type ExtractStep struct {
	Dir   string
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
	Moved       []string // files renamed aside because they could not be overwritten
}

func (e *Env) kernelDirs() []string {
	if len(e.T.KernelDirs) > 0 {
		return e.T.KernelDirs
	}
	return []string{e.T.KernelDir}
}

// Extract applies the archives in order into every kernel directory
// (central first, then each instance directory), then fixes ownership,
// runs saproot.sh when possible and reads the new level.
func Extract(ctx context.Context, e *Env, sapcar string, files []SARFile) (*UpdateResult, error) {
	res := &UpdateResult{}
	if len(files) == 0 {
		return res, errors.New("no archives to extract")
	}
	SortForApply(files)
	dirs := e.kernelDirs()
	src := func(dir string, f SARFile) string {
		if _, err := os.Stat(filepath.Join(dir, f.Name)); err == nil { // Kernel Files placed a copy in every kernel directory
			return filepath.Join(dir, f.Name)
		}
		return f.Path
	}
	listed := map[string][]string{}
	blocked, err := blockedFiles(dirs, func(dir string) ([]string, error) {
		var all []string
		for _, f := range files {
			names, ok := listed[f.Name]
			if !ok {
				var lerr error
				if names, lerr = archiveEntries(ctx, e, sapcar, src(dir, f)); lerr != nil {
					return nil, lerr
				}
				listed[f.Name] = names
			}
			all = append(all, names...)
		}
		return all, nil
	})
	if err != nil {
		return res, err
	}
	svc, kept := runningServices(ctx, e)
	mv := min(len(blocked), 1)
	// move aside, stop sapstartsrv, extract per dir/file, chown per dir, saproot.sh, start sapstartsrv, version, sync check
	n := mv + len(dirs)*len(files) + len(dirs) + 3 + 2*len(svc)
	res.Before, _ = kernel.Probe(ctx, e.R, e.P, e.T.KernelDir)
	if res.Before.Release > 0 {
		e.Pr.Info("current kernel: " + res.Before.String())
	}
	if res.Moved, err = moveAside(e, blocked, 1, n); err != nil {
		return res, err
	}
	if err := pauseServices(ctx, e, svc, kept, 1+mv, n); err != nil {
		return res, err
	}
	step := mv + len(svc)
	for _, dir := range dirs {
		for _, f := range files {
			st := ExtractStep{Dir: dir, File: f.Name}
			step++
			if err := e.step(step, n, fmt.Sprintf("SAPCAR -xvf %s  (%s %d) in %s", f.Name, f.Label, f.Patch, dir), func() (string, error) {
				// as the current user: root overwrites the root-owned setuid files too; chown -R and saproot.sh follow
				out, err := e.run(ctx, exec.Cmd{Path: sapcar, Args: []string{"-xvf", src(dir, f)}, Dir: dir, Timeout: time.Hour})
				st.Files = countExtracted(out.Stdout)
				st.Err = err
				return fmt.Sprintf("%d files", st.Files), err
			}); err != nil {
				res.Steps = append(res.Steps, st)
				servicesDown(e, svc)
				return res, err
			}
			res.Steps = append(res.Steps, st)
		}
	}
	return finishKernelChange(ctx, e, res, svc, step+1, n)
}

// Restore copies each directory's backup back over it (Kernel Rollback).
// backups maps kernel directory → backup directory.
func Restore(ctx context.Context, e *Env, backups map[string]string) (*UpdateResult, error) {
	res := &UpdateResult{}
	dirs := e.kernelDirs()
	for _, dir := range dirs {
		b := backups[dir]
		if st, err := os.Stat(b); b == "" || err != nil || !st.IsDir() {
			return res, fmt.Errorf("no backup for %s", dir)
		}
	}
	blocked, err := blockedFiles(dirs, func(dir string) ([]string, error) {
		entries, err := os.ReadDir(backups[dir])
		var names []string
		for _, de := range entries {
			names = append(names, de.Name())
		}
		return names, err
	})
	if err != nil {
		return res, err
	}
	svc, kept := runningServices(ctx, e)
	mv := min(len(blocked), 1)
	n := mv + 2*len(dirs) + 3 + 2*len(svc) // move aside, stop sapstartsrv, copy per dir, chown per dir, saproot.sh, start sapstartsrv, version, sync check
	res.Before, _ = kernel.Probe(ctx, e.R, e.P, e.T.KernelDir)
	if res.Moved, err = moveAside(e, blocked, 1, n); err != nil {
		return res, err
	}
	if err := pauseServices(ctx, e, svc, kept, 1+mv, n); err != nil {
		return res, err
	}
	step := mv + len(svc)
	for _, dir := range dirs {
		step++
		if err := e.step(step, n, "Copy "+backups[dir]+" over "+dir, func() (string, error) {
			_, err := e.run(ctx, exec.Cmd{Path: "cp", Args: []string{"-pR", backups[dir] + "/.", dir + "/"}, Timeout: 2 * time.Hour})
			return "cp -pR", err
		}); err != nil {
			servicesDown(e, svc)
			return res, err
		}
	}
	return finishKernelChange(ctx, e, res, svc, step+1, n)
}

// finishKernelChange runs the common tail: chown every directory,
// saproot.sh in the central one, sapstartsrv started again from the new
// files, version probe, instance directory check.
func finishKernelChange(ctx context.Context, e *Env, res *UpdateResult, svc []ServiceInstance, step, n int) (*UpdateResult, error) {
	for _, dir := range e.kernelDirs() {
		if err := e.step(step, n, fmt.Sprintf("chown -R %s:%s %s", e.T.SIDAdm, e.T.Group, dir), func() (string, error) {
			return chownTree(ctx, e, dir, &res.ChownNote)
		}); err != nil {
			servicesDown(e, svc)
			return res, err
		}
		step++
	}
	if err := e.step(step, n, "saproot.sh "+e.T.SID, func() (string, error) {
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
		servicesDown(e, svc)
		return res, err
	}
	step++
	if err := StartSapstartsrv(ctx, e, svc, step, n); err != nil {
		return res, err
	}
	step += len(svc)
	if err := e.step(step, n, "Read kernel version (disp+work -V)", func() (string, error) {
		v, err := kernel.Probe(ctx, e.R, e.P, e.T.KernelDir)
		res.After = v
		return v.String(), err
	}); err != nil {
		return res, err
	}
	_ = e.T.SaveSnapshot(func(s *system.Snapshot) { s.LastUpdateAt = e.now() })
	step++
	// The control the operator asked for: no instance directory may be left
	// at another level than the central kernel, or the system will not start.
	return res, e.step(step, n, "Verify every instance directory matches the central kernel", func() (string, error) {
		var ok, bad []string
		for _, s := range KernelSync(e) {
			switch {
			case s.Checked == 0:
			case s.InSync():
				ok = append(ok, filepath.Base(filepath.Dir(s.Dir))+"/"+filepath.Base(s.Dir))
			default:
				bad = append(bad, fmt.Sprintf("%s differs (%s)", s.Dir, strings.Join(s.Differs, ", ")))
			}
		}
		if len(bad) > 0 {
			return "", fmt.Errorf("NOT at the central kernel level: %s — run Kernel Update again so every directory is extracted", strings.Join(bad, "; "))
		}
		if len(ok) == 0 {
			return "no local instance directory to compare", nil
		}
		return strings.Join(ok, ", ") + " = central", nil
	})
}

// KernelSync compares every local instance directory with the central kernel directory.
func KernelSync(e *Env) []kernel.Sync {
	var out []kernel.Sync
	for _, d := range e.T.KernelDirs {
		if d == e.T.KernelDir {
			continue
		}
		out = append(out, kernel.CompareDirs(e.T.KernelDir, d))
	}
	return out
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
