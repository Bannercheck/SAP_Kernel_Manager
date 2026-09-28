package ops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// sapcarNameRe accepts SAPCAR, sapcar, SAPCAR.exe, SAPCAR_1115-70006178.EXE, sapcar-1115.exe.
var sapcarNameRe = regexp.MustCompile(`(?i)^sapcar(?:[_-](\d+)(?:-\d+)?)?(?:\.exe)?$`)

// IsSAPCARName reports whether name is a SAPCAR executable, in any case.
func IsSAPCARName(name string) bool { return sapcarNameRe.MatchString(name) }

// SAPCARPatch is the patch number in a name like SAPCAR_1115-70006178.EXE (0 when absent).
func SAPCARPatch(name string) int {
	m := sapcarNameRe.FindStringSubmatch(name)
	if m == nil || m[1] == "" {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// ProgramBinDir is the bin/ directory KernelMan runs from ("" when it does
// not run from a distribution layout); tests replace it.
var ProgramBinDir = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exe, _ = filepath.EvalSymlinks(exe)
	if dir := filepath.Dir(exe); filepath.Base(dir) == "bin" {
		return dir
	}
	return ""
}

// StateBinDir is ~/.kernelman/bin, where a SAPCAR found elsewhere is kept.
func StateBinDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".kernelman", "bin")
}

// SAPCARSearch says where FindSAPCAR looked, for the error message.
var sapcarPlaces = "bin/ next to KernelMan, ~/.kernelman/bin, the download directory, the kernel directories, SAP Host Agent's exe, PATH and the whole server"

// FindSAPCAR returns the SAPCAR to run. It looks, in this order, next to
// KernelMan (bin/), in ~/.kernelman/bin, in dirs (where the archives are),
// in the kernel directories, in the SAP Host Agent directory, on PATH, among
// candidates the archive scan met, and finally by walking the whole server
// for any file named like SAPCAR in any case. Within one place the highest
// patch in the name wins, then the newest file. A SAPCAR that is not
// executable is chmod 755; one found outside the program and kernel
// directories is copied to ~/.kernelman/bin/SAPCAR and to bin/ next to
// KernelMan, so Send to Other Servers carries it along.
func FindSAPCAR(ctx context.Context, e *Env, dirs []string, candidates []string) (string, error) {
	var groups [][]string
	if b := ProgramBinDir(); b != "" {
		groups = append(groups, sapcarsIn(b))
	}
	if b := StateBinDir(); b != "" {
		groups = append(groups, sapcarsIn(b))
	}
	for _, d := range dirs {
		if d != "" {
			groups = append(groups, sapcarsIn(d))
		}
	}
	for _, d := range e.kernelDirs() {
		groups = append(groups, sapcarsIn(d))
	}
	if e.P != nil {
		groups = append(groups, sapcarsIn(e.P.HostctrlExeDir()))
	}
	if e.R != nil {
		if p, err := e.R.LookPath("SAPCAR"); err == nil {
			groups = append(groups, []string{p})
		} else if p, err := e.R.LookPath("sapcar"); err == nil {
			groups = append(groups, []string{p})
		}
	}
	groups = append(groups, candidates)
	for _, g := range groups {
		if p := bestSAPCAR(g); p != "" {
			return useSAPCAR(e, p)
		}
	}
	if e.Pr != nil {
		e.Pr.Info("SAPCAR not in the usual places: searching the whole server for a file named sapcar (any case)")
	}
	if p := bestSAPCAR(ScanForSAPCAR(ctx, DefaultScanRoots)); p != "" {
		return useSAPCAR(e, p)
	}
	return "", errors.New("SAPCAR not found (" + sapcarPlaces + "): put SAPCAR from the SAP Software Center (SAPCAR 7.53 → your platform, any file name such as SAPCAR_1115-70006178.EXE) into the download directory or bin/ next to KernelMan")
}

// sapcarsIn lists SAPCAR-named regular files directly in dir.
func sapcarsIn(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, de := range entries {
		if IsSAPCARName(de.Name()) {
			if info, err := os.Stat(filepath.Join(dir, de.Name())); err == nil && info.Mode().IsRegular() {
				out = append(out, filepath.Join(dir, de.Name()))
			}
		}
	}
	return out
}

// bestSAPCAR picks the highest patch in the name, then the newest file.
func bestSAPCAR(paths []string) string {
	type cand struct {
		path  string
		patch int
		mtime int64
	}
	var cs []cand
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			cs = append(cs, cand{p, SAPCARPatch(filepath.Base(p)), info.ModTime().UnixNano()})
		}
	}
	if len(cs) == 0 {
		return ""
	}
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].patch != cs[j].patch {
			return cs[i].patch > cs[j].patch
		}
		return cs[i].mtime > cs[j].mtime
	})
	return cs[0].path
}

// useSAPCAR makes p executable and keeps copies where KernelMan will find
// and ship them; it returns the path to run.
func useSAPCAR(e *Env, p string) (string, error) {
	info, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	var notes []string
	if info.Mode()&0o111 == 0 {
		if err := os.Chmod(p, 0o755); err != nil {
			return "", fmt.Errorf("%s is not executable and cannot be chmod 755: %w", p, err)
		}
		notes = append(notes, "chmod 755")
	}
	dir := filepath.Dir(p)
	inProgram := dir == ProgramBinDir() || dir == StateBinDir()
	inKernel := false
	for _, k := range e.kernelDirs() {
		if dir == k {
			inKernel = true
		}
	}
	if !inProgram && !inKernel {
		var kept []string
		for _, b := range []string{StateBinDir(), ProgramBinDir()} {
			if b == "" {
				continue
			}
			dst := filepath.Join(b, "SAPCAR")
			if _, err := os.Stat(dst); err == nil {
				continue
			}
			if os.MkdirAll(b, 0o755) == nil && copyExecutable(p, dst) == nil {
				kept = append(kept, dst)
			}
		}
		if len(kept) > 0 {
			notes = append(notes, "copy kept in "+strings.Join(kept, " and ")+" for other servers")
		}
	}
	if e.Pr != nil {
		msg := "SAPCAR: " + p
		if len(notes) > 0 {
			msg += " (" + strings.Join(notes, " · ") + ")"
		}
		e.Pr.Info(msg)
	}
	return p, nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ScanForSAPCAR walks roots (pruning what FindTodaySARs prunes) and returns
// every regular file named like SAPCAR, in any case.
func ScanForSAPCAR(ctx context.Context, roots []string) []string {
	var out []string
	visited := map[string]bool{}
	var walk func(dir string, level int)
	walk = func(dir string, level int) {
		if ctx.Err() != nil || level > 12 {
			return
		}
		real, err := filepath.EvalSymlinks(dir)
		if err != nil || visited[real] {
			return
		}
		visited[real] = true
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, de := range entries {
			path := filepath.Join(dir, de.Name())
			isDir := de.IsDir()
			if de.Type()&os.ModeSymlink != 0 {
				st, err := os.Stat(path)
				if err != nil {
					continue
				}
				isDir = st.IsDir()
			}
			if isDir {
				if !pruneNames[de.Name()] && !excluded(path, PrunePaths) {
					walk(path, level+1)
				}
				continue
			}
			if IsSAPCARName(de.Name()) {
				if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
					out = append(out, path)
				}
			}
		}
	}
	for _, r := range roots {
		walk(filepath.Clean(r), 0)
	}
	return out
}
