package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
)

// BackupResult describes a finished Kernel Backup.
type BackupResult struct {
	Source  string
	Dest    string
	Files   int
	Bytes   int64
	Listing string // ls -la of the backup directory
}

// BackupName returns exe_<YYYYMMDD>, with a time suffix when that exists.
func BackupName(parent string, now time.Time) string {
	name := "exe_" + now.Format("20060102")
	if _, err := os.Stat(filepath.Join(parent, name)); err == nil {
		name += "_" + now.Format("150405")
	}
	return name
}

// Backup copies the kernel directory next to itself as exe_<date>, verifies
// the file count and returns the directory listing as proof.
func Backup(ctx context.Context, e *Env) (*BackupResult, error) {
	src := e.T.KernelDir
	res := &BackupResult{Source: src}
	parent := filepath.Dir(src)
	res.Dest = filepath.Join(parent, BackupName(parent, e.now()))
	const n = 4

	var srcFiles int
	if err := e.step(1, n, "Check kernel directory "+src, func() (string, error) {
		if st, err := os.Stat(src); err != nil || !st.IsDir() {
			return "", fmt.Errorf("kernel directory %s: not a directory", src)
		}
		var err error
		srcFiles, res.Bytes, err = treeStats(src)
		return fmt.Sprintf("%d files, %s", srcFiles, HumanSize(res.Bytes)), err
	}); err != nil {
		return res, err
	}

	if err := e.step(2, n, "Copy to "+res.Dest, func() (string, error) {
		_, err := e.run(ctx, exec.Cmd{Path: "cp", Args: []string{"-pR", src, res.Dest}, RunAs: e.asAdm(), Timeout: 2 * time.Hour})
		return "cp -pR", err
	}); err != nil {
		return res, err
	}

	if err := e.step(3, n, "Verify copy", func() (string, error) {
		var err error
		res.Files, _, err = treeStats(res.Dest)
		if err != nil {
			return "", err
		}
		if res.Files != srcFiles {
			return "", fmt.Errorf("file count differs: source %d, backup %d", srcFiles, res.Files)
		}
		return fmt.Sprintf("%d files match", res.Files), nil
	}); err != nil {
		return res, err
	}

	if err := e.step(4, n, "List backup directory", func() (string, error) {
		out, err := e.run(ctx, exec.Cmd{Path: "ls", Args: []string{"-la", res.Dest}})
		res.Listing = out.Stdout
		return "ls -la", err
	}); err != nil {
		return res, err
	}
	_ = e.T.SaveSnapshot(func(s *system.Snapshot) { s.LastBackup, s.LastBackupAt = res.Dest, e.now() })
	return res, nil
}

// LatestBackup returns the most recent exe_* directory next to the kernel dir.
func LatestBackup(kernelDir string) (string, bool) {
	entries, err := os.ReadDir(filepath.Dir(kernelDir))
	if err != nil {
		return "", false
	}
	var best string
	var bestTime time.Time
	for _, en := range entries {
		if !en.IsDir() || len(en.Name()) < 12 || en.Name()[:4] != "exe_" {
			continue
		}
		info, err := en.Info()
		if err != nil {
			continue
		}
		if best == "" || info.ModTime().After(bestTime) {
			best, bestTime = filepath.Join(filepath.Dir(kernelDir), en.Name()), info.ModTime()
		}
	}
	return best, best != ""
}
