package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/disk"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
)

// DirBackup is the backup of one kernel directory.
type DirBackup struct {
	Source  string
	Dest    string
	Files   int
	Bytes   int64
	Listing string // ls -la of the backup directory (shortened for the screen)
}

// BackupResult describes a finished Kernel Backup of every kernel directory.
type BackupResult struct {
	Dirs    []DirBackup
	LogFile string // full listings
}

// Files and Bytes total over all directories.
func (r *BackupResult) Files() (n int) {
	for _, d := range r.Dirs {
		n += d.Files
	}
	return n
}

func (r *BackupResult) Bytes() (n int64) {
	for _, d := range r.Dirs {
		n += d.Bytes
	}
	return n
}

// BackupName returns <base>_<YYYYMMDD> next to dir (exe → exe_20260927,
// linuxx86_64 → linuxx86_64_20260927), with a time suffix when that exists.
func BackupName(dir string, now time.Time) string {
	name := filepath.Base(dir) + "_" + now.Format("20060102")
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), name)); err == nil {
		name += "_" + now.Format("150405")
	}
	return filepath.Join(filepath.Dir(dir), name)
}

// Backup copies every kernel directory next to itself under its own name
// plus the date, verifies the file counts and keeps the listings as proof.
func Backup(ctx context.Context, e *Env) (*BackupResult, error) {
	dirs := e.T.KernelDirs
	if len(dirs) == 0 {
		dirs = []string{e.T.KernelDir}
	}
	res := &BackupResult{LogFile: filepath.Join(e.T.StateDir, "backup_"+e.now().Format("20060102_150405")+".log")}
	var log strings.Builder
	n := 3*len(dirs) + 2
	step := 1
	if err := e.step(step, n, "Check free space", func() (string, error) {
		return checkFreeSpace(ctx, e, dirs)
	}); err != nil {
		return res, err
	}
	for _, src := range dirs {
		db := DirBackup{Source: src, Dest: BackupName(src, e.now())}
		var srcFiles int
		step++
		if err := e.step(step, n, "Check "+src, func() (string, error) {
			if st, err := os.Stat(src); err != nil || !st.IsDir() {
				return "", fmt.Errorf("%s: not a directory", src)
			}
			var err error
			srcFiles, db.Bytes, err = treeStats(src)
			return fmt.Sprintf("%d files, %s", srcFiles, HumanSize(db.Bytes)), err
		}); err != nil {
			return res, err
		}
		step++
		if err := e.step(step, n, "Copy to "+db.Dest, func() (string, error) {
			_, err := e.run(ctx, exec.Cmd{Path: "cp", Args: []string{"-pR", src, db.Dest}, RunAs: e.asAdm(), Timeout: 2 * time.Hour})
			return "cp -pR", err
		}); err != nil {
			return res, err
		}
		step++
		if err := e.step(step, n, "Verify "+filepath.Base(db.Dest), func() (string, error) {
			var err error
			db.Files, _, err = treeStats(db.Dest)
			if err != nil {
				return "", err
			}
			if db.Files != srcFiles {
				return "", fmt.Errorf("file count differs: source %d, backup %d", srcFiles, db.Files)
			}
			out, err := e.run(ctx, exec.Cmd{Path: "ls", Args: []string{"-la", db.Dest}})
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&log, "== %s\n%s\n", db.Dest, out.Stdout)
			db.Listing = shorten(out.Stdout, 12)
			return fmt.Sprintf("%d files match", db.Files), nil
		}); err != nil {
			return res, err
		}
		res.Dirs = append(res.Dirs, db)
	}
	if err := e.step(n, n, "Write listing "+res.LogFile, func() (string, error) {
		return fmt.Sprintf("%d directories", len(res.Dirs)), os.WriteFile(res.LogFile, []byte(log.String()), 0o644)
	}); err != nil {
		return res, err
	}
	_ = e.T.SaveSnapshot(func(s *system.Snapshot) {
		s.LastBackup, s.LastBackupAt = res.Dirs[0].Dest, e.now()
		s.LastBackups = map[string]string{}
		for _, d := range res.Dirs {
			s.LastBackups[d.Source] = d.Dest
		}
	})
	return res, nil
}

// checkFreeSpace makes sure every file system that will receive a backup
// has room for it (sizes summed per mount, 10 % headroom).
func checkFreeSpace(ctx context.Context, e *Env, dirs []string) (string, error) {
	needKB := map[string]int64{}
	fsByMount := map[string]disk.Filesystem{}
	for _, d := range dirs {
		_, bytes, err := treeStats(d)
		if err != nil {
			return "", err
		}
		fss, err := disk.DF(ctx, e.R, filepath.Dir(d))
		if err != nil {
			return "", err
		}
		needKB[fss[0].Mount] += bytes / 1024
		fsByMount[fss[0].Mount] = fss[0]
	}
	var parts []string
	for mount, need := range needKB {
		fs := fsByMount[mount]
		if fs.AvailKB < need+need/10 {
			return "", fmt.Errorf("not enough space on %s: need %s, free %s", mount, disk.Human(need), disk.Human(fs.AvailKB))
		}
		parts = append(parts, fmt.Sprintf("%s: need %s, free %s", mount, disk.Human(need), disk.Human(fs.AvailKB)))
	}
	sort.Strings(parts)
	return strings.Join(parts, " · "), nil
}

// shorten keeps the first max lines of a listing and says how many follow.
func shorten(listing string, max int) string {
	lines := strings.Split(strings.TrimRight(listing, "\n"), "\n")
	if len(lines) <= max {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:max], "\n") + fmt.Sprintf("\n... %d more entries", len(lines)-max)
}

// LatestBackup returns the most recent <base>_<date> directory next to dir.
func LatestBackup(dir string) (string, bool) {
	var best string
	var bestTime time.Time
	for _, b := range BackupSiblings(dir) {
		info, err := os.Stat(b)
		if err != nil {
			continue
		}
		if best == "" || info.ModTime().After(bestTime) {
			best, bestTime = b, info.ModTime()
		}
	}
	return best, best != ""
}
