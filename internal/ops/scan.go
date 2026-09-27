package ops

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultScanRoots: the whole server. Pseudo file systems, OS library trees
// and database data areas are pruned (see PrunePaths / pruneNames) because
// nobody downloads kernel archives there and they hold most of the entries.
var DefaultScanRoots = []string{"/"}

// PrunePaths are absolute subtrees never descended into.
var PrunePaths = []string{"/proc", "/sys", "/dev", "/run", "/boot", "/lost+found", "/usr/lib", "/usr/lib64", "/usr/libexec",
	"/usr/share", "/usr/include", "/usr/src", "/var/lib", "/var/cache", "/var/log", "/var/spool", "/var/run", "/etc",
	"/hana/data", "/hana/log", "/System", "/Library", "/private/var", "/Applications"}

// pruneNames are directory names never descended into, wherever they are.
var pruneNames = map[string]bool{"proc": true, "sys": true, "dev": true, "lost+found": true, ".snapshot": true,
	"node_modules": true, ".git": true, ".Trash": true, "Library": true, "sapdata1": true, "sapdata2": true, "sapdata3": true,
	"sapdata4": true, "origlogA": true, "origlogB": true, "mirrlogA": true, "mirrlogB": true, "oraarch": true, "saparch": true}

// ScanOptions controls FindTodaySARs.
type ScanOptions struct {
	Roots    []string // directories to search (DefaultScanRoots when empty)
	Day      time.Time
	Exclude  []string // subtrees to skip, e.g. the kernel directories and their backups
	MaxDepth int      // 0 = 12
	Progress func(dirs int, current string)
}

// ScanResult is what the search found.
type ScanResult struct {
	Today      []SARFile // archives whose modification date is Day, apply order, one per file name
	Older      int       // archives with another date (ignored, reported as a count)
	Duplicates []string  // names found in more than one place (the newest copy is kept)
	Dirs       int       // directories visited
	Roots      []string  // roots that existed and were searched
	Unreadable int       // directories that could not be read (permissions)
	UnreadEx   []string  // a few examples of unreadable directories
}

// FindTodaySARs walks the roots and returns the *.SAR/*.sar files whose
// modification date is opts.Day — exactly "today's files", nothing older.
// Directory symlinks are followed once, pseudo file systems and excluded
// subtrees are skipped, unreadable directories are counted instead of
// aborting the scan.
func FindTodaySARs(ctx context.Context, opts ScanOptions) (*ScanResult, error) {
	roots := opts.Roots
	if len(roots) == 0 {
		roots = DefaultScanRoots
	}
	depth := opts.MaxDepth
	if depth == 0 {
		depth = 12
	}
	y, m, d := opts.Day.Date()
	sameDay := func(t time.Time) bool {
		ty, tm, td := t.Date()
		return ty == y && tm == m && td == d
	}
	res := &ScanResult{}
	byName := map[string]SARFile{}
	visited := map[string]bool{} // real paths of directories already walked (symlink loops)
	seenRoot := map[string]bool{}

	var walk func(dir string, level int) error
	walk = func(dir string, level int) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		real, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return nil
		}
		if visited[real] {
			return nil
		}
		visited[real] = true
		entries, err := os.ReadDir(dir)
		if err != nil {
			res.Unreadable++
			if len(res.UnreadEx) < 3 {
				res.UnreadEx = append(res.UnreadEx, dir)
			}
			return nil
		}
		res.Dirs++
		if opts.Progress != nil && res.Dirs%2000 == 0 {
			opts.Progress(res.Dirs, dir)
		}
		for _, de := range entries {
			path := filepath.Join(dir, de.Name())
			isDir := de.IsDir()
			if de.Type()&os.ModeSymlink != 0 { // follow links to directories, look at links to files
				st, err := os.Stat(path)
				if err != nil {
					continue
				}
				isDir = st.IsDir()
			}
			if isDir {
				if pruneNames[de.Name()] || excluded(path, PrunePaths) || excluded(path, opts.Exclude) || level+1 > depth {
					continue
				}
				if err := walk(path, level+1); err != nil {
					return err
				}
				continue
			}
			if !strings.EqualFold(filepath.Ext(de.Name()), ".sar") {
				continue
			}
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			f, ok := ParseSARName(de.Name())
			if !ok {
				f = SARFile{Name: de.Name(), Component: "?", Label: "?"}
			}
			f.Path, f.Size, f.ModTime = path, info.Size(), info.ModTime()
			if !sameDay(f.ModTime) {
				res.Older++
				continue
			}
			if prev, dup := byName[f.Name]; dup {
				res.Duplicates = append(res.Duplicates, f.Name)
				if !f.ModTime.After(prev.ModTime) {
					continue
				}
			}
			byName[f.Name] = f
		}
		return nil
	}

	for _, root := range roots {
		root = filepath.Clean(root)
		if seenRoot[root] {
			continue
		}
		seenRoot[root] = true
		st, err := os.Stat(root)
		if err != nil || !st.IsDir() {
			continue
		}
		res.Roots = append(res.Roots, root)
		if err := walk(root, 0); err != nil {
			return res, err
		}
	}
	for _, f := range byName {
		res.Today = append(res.Today, f)
	}
	SortForApply(res.Today)
	sort.Strings(res.Duplicates)
	return res, nil
}

func excluded(path string, subtrees []string) bool {
	for _, ex := range subtrees {
		if ex == "" {
			continue
		}
		if path == ex || strings.HasPrefix(path, strings.TrimSuffix(ex, "/")+"/") {
			return true
		}
	}
	return false
}

// BackupSiblings returns the backup directories that sit next to dir
// (<base>_<YYYYMMDD>[_HHMMSS]); they are excluded from scans.
func BackupSiblings(dir string) []string {
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		return nil
	}
	var out []string
	prefix := filepath.Base(dir) + "_"
	for _, en := range entries {
		if en.IsDir() && strings.HasPrefix(en.Name(), prefix) && isBackupSuffix(strings.TrimPrefix(en.Name(), prefix)) {
			out = append(out, filepath.Join(filepath.Dir(dir), en.Name()))
		}
	}
	return out
}

func isBackupSuffix(s string) bool {
	if len(s) != 8 && len(s) != 15 {
		return false
	}
	for i, c := range s {
		if i == 8 {
			if c != '_' {
				return false
			}
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
