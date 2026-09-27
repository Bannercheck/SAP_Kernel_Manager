package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
)

// SARFile is a kernel archive found in the download directory.
type SARFile struct {
	Path      string
	Name      string
	Size      int64
	ModTime   time.Time
	Component string // SAPEXE, SAPEXEDB, DW, IGSEXE, LIB_DBSL ...
	Patch     int    // 402, 403, 421 ...
	Number    string // SAP's archive number, informational
	Full      bool   // complete kernel archive (SAPEXE / SAPEXEDB)
}

// sarNameRe accepts SAPEXE_403-80007807.SAR, SAPEXE.402_7805.SAR,
// dw_421-80007541.sar, lib_dbsl_403-80007808.sar, SAPHOSTAGENT65_65-80004822.SAR.
var sarNameRe = regexp.MustCompile(`^(?i)([A-Za-z][A-Za-z0-9_+]*?)[._-](\d{1,4})[._-](\d+)\.sar$`)

// ParseSARName extracts component and patch level from an archive name.
func ParseSARName(name string) (SARFile, bool) {
	m := sarNameRe.FindStringSubmatch(name)
	if m == nil {
		return SARFile{}, false
	}
	patch, _ := strconv.Atoi(m[2])
	comp := strings.ToUpper(m[1])
	return SARFile{Name: name, Component: comp, Patch: patch, Number: m[3], Full: comp == "SAPEXE" || comp == "SAPEXEDB"}, true
}

// ScanSARs lists *.SAR/*.sar files in dir. Files modified on `day` are the
// ones to apply; older ones are returned separately so they can be shown as skipped.
func ScanSARs(dir string, day time.Time) (today, older []SARFile, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	y, m, d := day.Date()
	for _, en := range entries {
		if en.IsDir() || !strings.EqualFold(filepath.Ext(en.Name()), ".sar") {
			continue
		}
		info, err := en.Info()
		if err != nil {
			continue
		}
		f, ok := ParseSARName(en.Name())
		if !ok {
			f = SARFile{Name: en.Name(), Component: "?"}
		}
		f.Path, f.Size, f.ModTime = filepath.Join(dir, en.Name()), info.Size(), info.ModTime()
		fy, fm, fd := f.ModTime.Date()
		if fy == y && fm == m && fd == d {
			today = append(today, f)
		} else {
			older = append(older, f)
		}
	}
	SortForApply(today)
	SortForApply(older)
	return today, older, nil
}

// SortForApply orders archives the way they must be extracted: ascending
// patch level, and within one level the full archives first (SAPEXE, then
// SAPEXEDB), then single-component patches alphabetically. Extracting in
// this order means every later archive overwrites the earlier one, so
// after the last hotfix the kernel is at the highest level.
func SortForApply(files []SARFile) {
	rank := func(f SARFile) int {
		switch f.Component {
		case "SAPEXE":
			return 0
		case "SAPEXEDB":
			return 1
		}
		return 2
	}
	sort.SliceStable(files, func(i, j int) bool {
		a, b := files[i], files[j]
		if a.Patch != b.Patch {
			return a.Patch < b.Patch
		}
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		return a.Name < b.Name
	})
}

// TargetPatch is the level the kernel reaches after applying files in order.
func TargetPatch(files []SARFile) int {
	max := 0
	for _, f := range files {
		if f.Patch > max {
			max = f.Patch
		}
	}
	return max
}

// CopyResult describes a finished Kernel Files operation.
type CopyResult struct {
	Dest      string
	Copied    []SARFile
	Listing   string
	ChownNote string
}

// CopySARs copies the archives into the kernel directory and hands them to
// <sid>adm:sapsys, then lists them as proof.
func CopySARs(ctx context.Context, e *Env, files []SARFile) (*CopyResult, error) {
	res := &CopyResult{Dest: e.T.KernelDir}
	if len(files) == 0 {
		return res, fmt.Errorf("no archives to copy")
	}
	const n = 3
	if err := e.step(1, n, fmt.Sprintf("Copy %d archive(s) to %s", len(files), res.Dest), func() (string, error) {
		args := []string{"-p"}
		for _, f := range files {
			args = append(args, f.Path)
		}
		args = append(args, res.Dest+"/")
		_, err := e.run(ctx, exec.Cmd{Path: "cp", Args: args, RunAs: e.asAdm(), Timeout: time.Hour})
		if err == nil {
			res.Copied = files
		}
		return "cp -p", err
	}); err != nil {
		return res, err
	}
	if err := e.step(2, n, fmt.Sprintf("chown -R %s:%s %s", e.T.SIDAdm, e.T.Group, res.Dest), func() (string, error) {
		return chownTree(ctx, e, res.Dest, &res.ChownNote)
	}); err != nil {
		return res, err
	}
	if err := e.step(3, n, "List archives in kernel directory", func() (string, error) {
		args := []string{"-la"}
		for _, f := range files {
			args = append(args, filepath.Join(res.Dest, f.Name))
		}
		out, err := e.run(ctx, exec.Cmd{Path: "ls", Args: args})
		res.Listing = out.Stdout
		return "ls -la", err
	}); err != nil {
		return res, err
	}
	_ = e.T.SaveSnapshot(func(s *system.Snapshot) {
		s.LastDownloadDir = filepath.Dir(files[0].Path)
		s.CopiedSARs = s.CopiedSARs[:0]
		for _, f := range files {
			s.CopiedSARs = append(s.CopiedSARs, f.Name)
		}
	})
	return res, nil
}

// chownTree runs chown -R <sid>adm:sapsys when root; otherwise it explains why not.
func chownTree(ctx context.Context, e *Env, dir string, note *string) (string, error) {
	if !e.IsRoot {
		*note = "skipped: not root"
		return *note, nil
	}
	_, err := e.run(ctx, exec.Cmd{Path: "chown", Args: []string{"-R", e.T.SIDAdm + ":" + e.T.Group, dir}, Timeout: time.Hour})
	if err == nil {
		*note = "done"
	}
	return *note, err
}
