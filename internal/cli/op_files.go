package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/version"
)

// scanRoots lists where today's archives are searched; tests and the demo narrow it.
var scanRoots []string

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
	files, ok := pickArchives(ctx, t, *from, *yes)
	if !ok {
		return ExitError
	}
	e := newEnv(t)
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
func pickArchives(ctx context.Context, t *system.Target, from string, yes bool) ([]ops.SARFile, bool) {
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
			if !yes && !confirm(fmt.Sprintf("Copy these %d archive(s) into %d kernel directories?", len(res.Today), len(kernelDirs(t)))) {
				fmt.Fprintln(stdout, "  cancelled")
				return nil, false
			}
			return res.Today, true
		}
		fmt.Fprintf(stdout, "  %s no .SAR files dated today (%s) found under %s", pal.Cross(), time.Now().Format("2006-01-02"), strings.Join(res.Roots, " "))
		if res.Older > 0 {
			fmt.Fprintf(stdout, " · %d older archive(s) ignored", res.Older)
		}
		fmt.Fprintln(stdout)
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
	if len(roots) == 1 && roots[0] == "/" {
		where = "the whole server (/)"
	}
	fmt.Fprintf(stdout, "  %s\n", pal.Paint(ui.Dim, fmt.Sprintf("scanning %s for .SAR files dated today (%s); system directories, kernel directories and backups are skipped",
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
	fmt.Fprintf(stdout, "  %s\n", pal.Paint(ui.Dim, fmt.Sprintf("%d directories scanned", res.Dirs)))
	return res
}

// showArchives prints the archives in apply order plus the resulting level.
func showArchives(res *ops.ScanResult) {
	pal := currentPalette()
	rows := [][]string{{"#", "ARCHIVE", "COMPONENT", "PATCH", "SIZE", "MODIFIED", "FOUND IN"}}
	for i, f := range res.Today {
		kind := f.Label
		if f.Full {
			kind += " (full kernel)"
		}
		rows = append(rows, []string{fmt.Sprint(i + 1), f.Name, kind, fmt.Sprint(f.Patch), ops.HumanSize(f.Size),
			f.ModTime.Format("2006-01-02 15:04"), filepath.Dir(f.Path)})
	}
	fmt.Fprintf(stdout, "\n  %s\n", pal.Paint(ui.Cyan, "Archives dated today, in apply order (lowest patch first)"))
	for _, l := range ui.Table("    ", rows) {
		fmt.Fprintln(stdout, l)
	}
	fmt.Fprintf(stdout, "    → target level after apply: %s", pal.Paint(ui.Bold, fmt.Sprintf("patch %d", ops.TargetPatch(res.Today))))
	if res.Older > 0 {
		fmt.Fprintf(stdout, "   · %s", pal.Paint(ui.Dim, fmt.Sprintf("%d older archive(s) ignored", res.Older)))
	}
	if len(res.Duplicates) > 0 {
		fmt.Fprintf(stdout, "   · %s", pal.Paint(ui.Yellow, fmt.Sprintf("duplicates (newest kept): %s", strings.Join(res.Duplicates, ", "))))
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout)
}
