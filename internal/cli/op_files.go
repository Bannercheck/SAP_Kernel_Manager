package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/version"
)

// scanRoots lists where today's archives are searched; tests and the demo narrow it.
var scanRoots []string

// lastSAPCARs are the SAPCAR files the last archive scan met, so Kernel
// Update and Send to Other Servers need not walk the server again.
var lastSAPCARs []string

func kernelDirs(t *system.Target) []string {
	if len(t.KernelDirs) > 0 {
		return t.KernelDirs
	}
	return []string{t.KernelDir}
}

// FilesOp implements Kernel File Transfer: find today's *.SAR anywhere on
// the server, copy them into every kernel directory, chown, list.
func FilesOp(args []string) int {
	fs := flag.NewFlagSet("files", flag.ContinueOnError)
	sid := fs.String("sid", "", "SAP system")
	from := fs.String("from", "", "search only this directory instead of the whole server")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	t, err := target(ctx, *sid)
	if err != nil {
		return fail(err)
	}
	e := newEnv(t)
	noteRemote(ctx, e)
	files, ok := pickArchives(ctx, t, *from, *yes, len(kernelDirs(t)))
	if !ok {
		return ExitError
	}
	res, err := ops.CopySARs(ctx, e, files)
	if res != nil && res.Listing != "" {
		e.Pr.Block("Archives now in "+t.KernelDir, res.Listing)
	}
	if err != nil {
		return fail(err)
	}
	pal := currentPalette()
	fmt.Fprintf(stdout, "  %s %d archive(s) copied into %d kernel directories · chown %s\n", pal.Check(), len(res.Copied), len(res.Dests), res.ChownNote)
	return ExitOK
}

// pickArchives scans the server for archives dated today, shows them in
// apply order and asks for confirmation. With from set only that directory
// is searched. When nothing is found the user may type a directory.
func pickArchives(ctx context.Context, t *system.Target, from string, yes bool, ndirs int) ([]ops.SARFile, bool) {
	pal := currentPalette()
	roots := scanRoots
	if from != "" {
		roots = []string{from}
	} else if env := os.Getenv(version.EnvPrefix + "SCAN_ROOTS"); env != "" && len(roots) == 0 {
		roots = filepath.SplitList(env)
	}
	if len(roots) == 0 {
		roots = ops.DefaultScanRoots // the whole server
	}
	for {
		res := scanFor(ctx, t, roots)
		if len(res.Today) > 0 {
			showArchives(res)
			if yes {
				return res.Today, true
			}
			return chooseArchives(res.Today, fmt.Sprintf("Copy %s into %d kernel director%s?", "%s", ndirs, plural(ndirs, "y", "ies")))
		}
		fmt.Fprintf(stdout, "  %s no .SAR files placed today (%s) under %s", pal.Cross(), time.Now().Format("2006-01-02"), strings.Join(res.Roots, " "))
		if res.Older > 0 {
			fmt.Fprintf(stdout, " · %d archive(s) from other days ignored", res.Older)
		}
		fmt.Fprintln(stdout)
		fmt.Fprintf(stdout, "  %s\n", pal.Paint(ui.Dim, "an archive counts when it was modified or copied onto this server today; to use an older file: touch <file>"))
		if yes {
			return nil, false
		}
		dir := ask("Directory to search instead (Enter = cancel)", "")
		if dir == "" {
			return nil, false
		}
		roots = []string{dir}
	}
}

// scanFor runs the scanner with the kernel directories and their backups excluded.
func scanFor(ctx context.Context, t *system.Target, roots []string) *ops.ScanResult {
	pal := currentPalette()
	var exclude []string
	for _, d := range kernelDirs(t) {
		exclude = append(exclude, d)
		exclude = append(exclude, ops.BackupSiblings(d)...)
	}
	where := strings.Join(roots, " ")
	if len(roots) == 1 && roots[0] == ops.DefaultScanRoots[0] {
		where = "the whole server (" + roots[0] + ") including every user's home directory"
	}
	fmt.Fprintf(stdout, "  %s\n", pal.Paint(ui.Dim, fmt.Sprintf("scanning %s for .SAR files placed today (%s); system directories, kernel directories and backups are skipped",
		where, time.Now().Format("2006-01-02"))))
	var progress func(int, string)
	if f, ok := stdout.(*os.File); ok && ui.IsTerminal(f) {
		progress = func(n int, cur string) {
			fmt.Fprintf(stdout, "\r  %s", pal.Paint(ui.Dim, fmt.Sprintf("%d directories scanned … %s", n, cur)))
		}
	}
	res, err := ops.FindTodaySARs(ctx, ops.ScanOptions{Roots: roots, Day: time.Now(), Exclude: exclude, Progress: progress})
	if progress != nil {
		fmt.Fprint(stdout, "\r\033[K")
	}
	if err != nil {
		fmt.Fprintf(stdout, "  %s scan: %v\n", pal.Paint(ui.Yellow, "!"), err)
	}
	if res == nil {
		res = &ops.ScanResult{}
	}
	lastSAPCARs = res.SAPCARs
	summary := fmt.Sprintf("%d directories scanned", res.Dirs)
	if res.KernelDirs > 0 {
		summary += fmt.Sprintf(" · %d kernel directories/backups not searched", res.KernelDirs)
		if res.InKernel > 0 {
			summary += fmt.Sprintf(" (%d archive(s) inside them are copies already in place)", res.InKernel)
		}
	}
	if res.Homes > 0 {
		summary += fmt.Sprintf(" · %d home directories from %s", res.Homes, ops.PasswdFile)
	}
	if res.Unreadable > 0 {
		summary += fmt.Sprintf(" · %d not readable by %s (e.g. %s) — run as root to search them too", res.Unreadable, currentUser(), strings.Join(res.UnreadEx, ", "))
	}
	fmt.Fprintf(stdout, "  %s\n", pal.Paint(ui.Dim, summary))
	return res
}

// showArchives prints the archives in apply order plus the resulting level.
func showArchives(res *ops.ScanResult) {
	pal := currentPalette()
	showArchiveTable(res.Today, "Archives placed on this server today, in apply order (lowest patch first)")
	if res.Older > 0 {
		fmt.Fprintf(stdout, "   · %s", pal.Paint(ui.Dim, fmt.Sprintf("%d older archive(s) ignored", res.Older)))
	}
	if len(res.Duplicates) > 0 {
		fmt.Fprintf(stdout, "   · %s", pal.Paint(ui.Yellow, fmt.Sprintf("duplicates (newest kept): %s", strings.Join(res.Duplicates, ", "))))
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout)
}

// showArchiveTable prints numbered archives in apply order and the level they lead to.
func showArchiveTable(files []ops.SARFile, title string) {
	pal := currentPalette()
	rows := [][]string{pal.Headers("#", "ARCHIVE", "COMPONENT", "PATCH", "SIZE", "PLACED", "FOUND IN")}
	for i, f := range files {
		kind := f.Label
		if f.Full {
			kind += " (full kernel)"
		}
		rows = append(rows, []string{fmt.Sprint(i + 1), f.Name, kind, fmt.Sprint(f.Patch), ops.HumanSize(f.Size),
			f.Placed.Format("2006-01-02 15:04"), filepath.Dir(f.Path)})
	}
	fmt.Fprintf(stdout, "\n  %s\n", pal.Paint(ui.Cyan, title))
	for _, l := range ui.Table("    ", rows) {
		fmt.Fprintln(stdout, l)
	}
	fmt.Fprintf(stdout, "    → target level after apply: %s", pal.Paint(ui.Bold, fmt.Sprintf("patch %d", ops.TargetPatch(files))))
}

var (
	yesAll     = choice{"Y", "Yes, all", []string{"yes", "all", "e", "evet", "hepsi"}}
	selectSome = choice{"S", "Select", []string{"select", "sec", "seç"}}
)

// chooseArchives asks whether to take all listed archives, a selection by
// number, or none. question has one %s for "these N archive(s)".
func chooseArchives(files []ops.SARFile, question string) ([]ops.SARFile, bool) {
	pal := currentPalette()
	switch choose(fmt.Sprintf(question, fmt.Sprintf("these %d archive(s)", len(files))), yesAll, selectSome, no) {
	case "Y":
		return files, true
	case "N":
		fmt.Fprintln(stdout, "  cancelled")
		return nil, false
	}
	for {
		in := ask("Numbers to take, e.g. 1,3-4 (Enter = cancel)", "")
		if in == "" {
			fmt.Fprintln(stdout, "  cancelled")
			return nil, false
		}
		idx, err := parseSelection(in, len(files))
		if err != nil {
			fmt.Fprintf(stdout, "  %s %v\n", pal.Paint(ui.Yellow, "!"), err)
			continue
		}
		var sel []ops.SARFile
		for _, i := range idx {
			sel = append(sel, files[i-1])
		}
		ops.SortForApply(sel)
		showArchiveTable(sel, fmt.Sprintf("Selected %d of %d archive(s), in apply order", len(sel), len(files)))
		fmt.Fprintln(stdout)
		if confirm(fmt.Sprintf(question, fmt.Sprintf("these %d archive(s)", len(sel)))) {
			return sel, true
		}
		fmt.Fprintln(stdout, "  cancelled")
		return nil, false
	}
}

// parseSelection turns "1,3-4 6" into sorted unique 1-based indexes within 1..n.
func parseSelection(s string, n int) ([]int, error) {
	seen := map[int]bool{}
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		lo, hi := tok, tok
		if i := strings.IndexAny(tok, "-–"); i > 0 {
			lo, hi = tok[:i], strings.TrimLeft(tok[i:], "-–")
		}
		a, err1 := strconv.Atoi(lo)
		b, err2 := strconv.Atoi(hi)
		if err1 != nil || err2 != nil || a < 1 || b > n || a > b {
			return nil, fmt.Errorf("%q is not a number or range between 1 and %d", tok, n)
		}
		for i := a; i <= b; i++ {
			seen[i] = true
		}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("nothing selected")
	}
	var out []int
	for i := range seen {
		out = append(out, i)
	}
	sort.Ints(out)
	return out, nil
}
