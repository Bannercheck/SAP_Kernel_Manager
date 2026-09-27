// Package disk reports file system free space (df -Pk) and directory sizes
// (du -sk) through exec.Runner, portable across Linux and AIX.
package disk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
)

// Filesystem is one df line.
type Filesystem struct {
	Device  string `json:"device"`
	Mount   string `json:"mount"`
	SizeKB  int64  `json:"size_kb"`
	UsedKB  int64  `json:"used_kb"`
	AvailKB int64  `json:"avail_kb"`
	UsePct  int    `json:"use_pct"`
	Path    string `json:"path"` // the path that was asked about
}

// DirSize is one du line.
type DirSize struct {
	Path string `json:"path"`
	KB   int64  `json:"kb"`
}

// DF returns the file systems holding paths (one entry per distinct mount).
func DF(ctx context.Context, r exec.Runner, paths ...string) ([]Filesystem, error) {
	var out []Filesystem
	seen := map[string]bool{}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		res, err := r.Run(ctx, exec.Cmd{Path: "df", Args: []string{"-Pk", p}, Timeout: time.Minute})
		if err != nil || res.ExitCode != 0 {
			continue
		}
		fs, ok := ParseDF(res.Stdout)
		if !ok {
			continue
		}
		fs.Path = p
		if !seen[fs.Mount] {
			seen[fs.Mount] = true
			out = append(out, fs)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("df: no file system information for %s", strings.Join(paths, " "))
	}
	return out, nil
}

// ParseDF parses POSIX `df -Pk` output (header + one line; the device name
// may wrap onto its own line on some systems).
func ParseDF(out string) (Filesystem, bool) {
	var fields []string
	for i, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if i == 0 && strings.Contains(l, "Filesystem") {
			continue
		}
		fields = append(fields, strings.Fields(l)...)
	}
	if len(fields) < 6 {
		return Filesystem{}, false
	}
	n := len(fields)
	fs := Filesystem{Device: fields[0], Mount: fields[n-1]}
	fs.SizeKB, _ = strconv.ParseInt(fields[n-5], 10, 64)
	fs.UsedKB, _ = strconv.ParseInt(fields[n-4], 10, 64)
	fs.AvailKB, _ = strconv.ParseInt(fields[n-3], 10, 64)
	fs.UsePct, _ = strconv.Atoi(strings.TrimSuffix(fields[n-2], "%"))
	return fs, true
}

// DU returns the sizes of the direct children of each root, largest first.
func DU(ctx context.Context, r exec.Runner, roots ...string) ([]DirSize, error) {
	var args []string
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, en := range entries {
			if en.IsDir() && !strings.HasPrefix(en.Name(), ".") {
				args = append(args, filepath.Join(root, en.Name()))
			}
		}
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("du: nothing under %s", strings.Join(roots, " "))
	}
	res, err := r.Run(ctx, exec.Cmd{Path: "du", Args: append([]string{"-sk"}, args...), Timeout: 10 * time.Minute})
	if err != nil {
		return nil, err
	}
	sizes := ParseDU(res.Stdout) // du exits non-zero on unreadable subdirs but still prints totals
	if len(sizes) == 0 {
		return nil, fmt.Errorf("du failed: %s", strings.TrimSpace(res.Stderr))
	}
	return sizes, nil
}

// ParseDU parses `du -sk` lines: "<kb>\t<path>".
func ParseDU(out string) []DirSize {
	var sizes []DirSize
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		kb, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			continue
		}
		sizes = append(sizes, DirSize{Path: strings.Join(f[1:], " "), KB: kb})
	}
	sort.Slice(sizes, func(i, j int) bool { return sizes[i].KB > sizes[j].KB })
	return sizes
}

// Node is a directory with its cumulative size and its largest children.
type Node struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	KB       int64  `json:"kb"`
	Children []Node `json:"children,omitempty"`
}

// Tree measures root with one `du -k` pass and keeps directories down to
// maxDepth levels below root (children sorted by size, largest first).
// Hidden directories (".kernelman", ".snapshot") are left out.
func Tree(ctx context.Context, r exec.Runner, root string, maxDepth int) (*Node, error) {
	root = filepath.Clean(root)
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}
	res, err := r.Run(ctx, exec.Cmd{Path: "du", Args: []string{"-k", root}, Timeout: 15 * time.Minute})
	if err != nil {
		return nil, err
	}
	n := BuildTree(root, ParseDU(res.Stdout), maxDepth)
	if n == nil {
		return nil, fmt.Errorf("du -k %s: no output (%s)", root, strings.TrimSpace(res.Stderr))
	}
	return n, nil
}

// BuildTree turns `du -k` lines (every directory with its cumulative size)
// into a tree rooted at root, maxDepth levels deep.
func BuildTree(root string, sizes []DirSize, maxDepth int) *Node {
	kb := map[string]int64{}
	for _, d := range sizes {
		kb[filepath.Clean(d.Path)] = d.KB
	}
	rootKB, ok := kb[root]
	if !ok {
		return nil
	}
	node := &Node{Path: root, Name: root, KB: rootKB}
	var fill func(n *Node, depth int)
	fill = func(n *Node, depth int) {
		if depth >= maxDepth {
			return
		}
		for p, size := range kb {
			if filepath.Dir(p) != n.Path || p == n.Path || strings.HasPrefix(filepath.Base(p), ".") {
				continue
			}
			n.Children = append(n.Children, Node{Path: p, Name: filepath.Base(p), KB: size})
		}
		sort.Slice(n.Children, func(i, j int) bool {
			if n.Children[i].KB != n.Children[j].KB {
				return n.Children[i].KB > n.Children[j].KB
			}
			return n.Children[i].Name < n.Children[j].Name
		})
		for i := range n.Children {
			fill(&n.Children[i], depth+1)
		}
	}
	fill(node, 0)
	return node
}

// Human renders kilobytes as MB/GB/TB.
func Human(kb int64) string {
	switch {
	case kb >= 1<<30:
		return fmt.Sprintf("%.1f TB", float64(kb)/(1<<30))
	case kb >= 1<<20:
		return fmt.Sprintf("%.1f GB", float64(kb)/(1<<20))
	case kb >= 1<<10:
		return fmt.Sprintf("%.1f MB", float64(kb)/(1<<10))
	}
	return fmt.Sprintf("%d KB", kb)
}
