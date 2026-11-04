package ops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
)

// A file that SAPCAR or cp has to replace but cannot open for writing (as
// <sid>adm: a root-owned setuid file saproot.sh left behind) stops an
// extraction half way and leaves a mixed kernel. KernelMan therefore looks
// before it changes anything: every file an archive or backup brings that
// exists and is not writable is renamed to <name>.kernelman_<date_time> in
// the same directory (the directory's owner may always do that, the old file
// stays there), so the new one can be written; saproot.sh as root sets the
// owner and setuid bit again.

// blockedFile is one file a kernel change cannot overwrite.
type blockedFile struct{ Dir, Name string }

func (b blockedFile) path() string { return filepath.Join(b.Dir, b.Name) }

var sapcarModeRe = regexp.MustCompile(`^[-dl][-rwxsStT]{9}$`)

// archiveEntries lists the file names SAPCAR -tvf reports for archive.
func archiveEntries(ctx context.Context, e *Env, sapcar, archive string) ([]string, error) {
	res, err := e.run(ctx, exec.Cmd{Path: sapcar, Args: []string{"-tvf", archive}, Timeout: 10 * time.Minute})
	if err != nil {
		return nil, err
	}
	var names []string
	for _, l := range strings.Split(res.Stdout, "\n") {
		if f := strings.Fields(l); len(f) >= 2 && sapcarModeRe.MatchString(f[0]) {
			names = append(names, f[len(f)-1])
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("SAPCAR -tvf %s listed no files", filepath.Base(archive))
	}
	return names, nil
}

// writeCheck opens path for writing without changing it; tests replace it.
var writeCheck = func(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0) // no O_TRUNC: nothing changes
	if err == nil {
		f.Close()
	}
	return err
}

// unwritable returns the names that exist in dir as regular files and
// cannot be opened for writing for lack of permission (not: busy).
func unwritable(dir string, names []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		p := filepath.Join(dir, n)
		if st, err := os.Lstat(p); err != nil || !st.Mode().IsRegular() {
			continue
		}
		if err := writeCheck(p); errors.Is(err, fs.ErrPermission) {
			out = append(out, n)
		}
	}
	return out
}

// blockedFiles checks every directory against the names it will receive.
// When the names cannot be known, any unwritable file in the directory is
// reported as an error so nothing is touched.
func blockedFiles(dirs []string, names func(dir string) ([]string, error)) ([]blockedFile, error) {
	var out []blockedFile
	for _, dir := range dirs {
		list, err := names(dir)
		if err != nil {
			entries, _ := os.ReadDir(dir)
			var all []string
			for _, de := range entries {
				all = append(all, de.Name())
			}
			if bad := unwritable(dir, all); len(bad) > 0 {
				return nil, fmt.Errorf("%s: %d file(s) cannot be overwritten (%s) and the archive contents are unknown (%v): run KernelMan as root",
					dir, len(bad), strings.Join(firstN(bad, 4), ", "), err)
			}
			continue
		}
		for _, n := range unwritable(dir, list) {
			out = append(out, blockedFile{Dir: dir, Name: n})
		}
	}
	return out, nil
}

// moveAside renames the blocked files as step i of n; a failed rename undoes the others.
func moveAside(e *Env, blocked []blockedFile, i, n int) ([]string, error) {
	if len(blocked) == 0 {
		return nil, nil
	}
	suffix := ".kernelman_" + e.now().Format("20060102_150405")
	who := e.User
	if who == "" {
		who = "this user"
	}
	var moved []string
	err := e.step(i, n, fmt.Sprintf("Move aside %d file(s) %s cannot overwrite (%s)", len(blocked), who, names(blocked)), func() (string, error) {
		for k, b := range blocked {
			if err := os.Rename(b.path(), b.path()+suffix); err != nil {
				for j := k - 1; j >= 0; j-- {
					_ = os.Rename(blocked[j].path()+suffix, blocked[j].path())
				}
				moved = nil
				return "", fmt.Errorf("rename %s: %w — nothing was changed: run KernelMan as root", b.path(), err)
			}
			moved = append(moved, b.path()+suffix)
		}
		return "kept as <name>" + suffix + " · saproot.sh as root sets owner and setuid of the new files", nil
	})
	return moved, err
}

func names(blocked []blockedFile) string {
	var out []string
	seen := map[string]bool{}
	for _, b := range blocked {
		if !seen[b.Name] {
			seen[b.Name] = true
			out = append(out, b.Name)
		}
	}
	return strings.Join(firstN(out, 4), ", ")
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return append(append([]string{}, s[:n]...), "…")
	}
	return s
}
