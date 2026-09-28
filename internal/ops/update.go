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
}

func (e *Env) kernelDirs() []string {
	if e.CentralOnly {
		return []string{e.T.KernelDir}
	}
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
	n := len(dirs)*len(files) + len(dirs) + 2 // extract per dir/file, chown per dir, saproot.sh, version
	res.Before, _ = kernel.Probe(ctx, e.R, e.P, e.T.KernelDir)
	if res.Before.Release > 0 {
		e.Pr.Info("current kernel: " + res.Before.String())
	}
	if !e.IsRoot { // SAPCAR as <sid>adm cannot overwrite the root-owned setuid files saproot.sh leaves behind
		for _, dir := range dirs {
			if n, ex := rootOwnedFiles(dir); n > 0 {
				e.Pr.Info(fmt.Sprintf("! %d file(s) in %s belong to root (%s): SAPCAR as %s will fail on them — run KernelMan as root", n, dir, strings.Join(ex, ", "), e.User))
			}
		}
	}
	step := 0
	for _, dir := range dirs {
		for _, f := range files {
			st := ExtractStep{Dir: dir, File: f.Name}
			step++
			if err := e.step(step, n, fmt.Sprintf("SAPCAR -xvf %s  (%s %d) in %s", f.Name, f.Label, f.Patch, dir), func() (string, error) {
				src := filepath.Join(dir, f.Name) // Kernel Files placed a copy in every kernel directory
				if _, err := os.Stat(src); err != nil {
					src = f.Path
				}
				// as the current user: root overwrites the root-owned setuid files too; chown -R and saproot.sh follow
				out, err := e.run(ctx, exec.Cmd{Path: sapcar, Args: []string{"-xvf", src}, Dir: dir, Timeout: time.Hour})
				st.Files = countExtracted(out.Stdout)
				st.Err = err
				return fmt.Sprintf("%d files", st.Files), err
			}); err != nil {
				res.Steps = append(res.Steps, st)
				return res, err
			}
			res.Steps = append(res.Steps, st)
		}
	}
	return finishKernelChange(ctx, e, res, step+1, n)
}

// Restore copies each directory's backup back over it (Kernel Rollback).
// backups maps kernel directory → backup directory.
func Restore(ctx context.Context, e *Env, backups map[string]string) (*UpdateResult, error) {
	res := &UpdateResult{}
	dirs := e.kernelDirs()
	n := 2*len(dirs) + 2 // copy per dir, chown per dir, saproot.sh, version
	for _, dir := range dirs {
		b := backups[dir]
		if st, err := os.Stat(b); b == "" || err != nil || !st.IsDir() {
			return res, fmt.Errorf("no backup for %s", dir)
		}
	}
	res.Before, _ = kernel.Probe(ctx, e.R, e.P, e.T.KernelDir)
	step := 0
	for _, dir := range dirs {
		step++
		if err := e.step(step, n, "Copy "+backups[dir]+" over "+dir, func() (string, error) {
			_, err := e.run(ctx, exec.Cmd{Path: "cp", Args: []string{"-pR", backups[dir] + "/.", dir + "/"}, Timeout: 2 * time.Hour})
			return "cp -pR", err
		}); err != nil {
			return res, err
		}
	}
	return finishKernelChange(ctx, e, res, step+1, n)
}

// finishKernelChange runs the common tail: chown every directory,
// saproot.sh in the central one, version probe.
func finishKernelChange(ctx context.Context, e *Env, res *UpdateResult, step, n int) (*UpdateResult, error) {
	for _, dir := range e.kernelDirs() {
		if err := e.step(step, n, fmt.Sprintf("chown -R %s:%s %s", e.T.SIDAdm, e.T.Group, dir), func() (string, error) {
			return chownTree(ctx, e, dir, &res.ChownNote)
		}); err != nil {
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
		return res, err
	}
	step++
	err := e.step(step, n, "Read kernel version (disp+work -V)", func() (string, error) {
		v, err := kernel.Probe(ctx, e.R, e.P, e.T.KernelDir)
		res.After = v
		return v.String(), err
	})
	if err == nil {
		_ = e.T.SaveSnapshot(func(s *system.Snapshot) { s.LastUpdateAt = e.now() })
	}
	return res, err
}

// rootOwnedFiles counts regular files in dir owned by uid 0 and names a few.
func rootOwnedFiles(dir string) (int, []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, nil
	}
	n := 0
	var ex []string
	for _, de := range entries {
		info, err := de.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if uid, ok := fileUID(info); ok && uid == 0 {
			n++
			if len(ex) < 4 {
				ex = append(ex, de.Name())
			}
		}
	}
	return n, ex
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
