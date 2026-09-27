package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

func parentDir(p string) string { return filepath.Dir(p) }

// FilesOp implements Kernel Files: today's *.SAR from the download directory
// into the kernel directory, then chown and a listing.
func FilesOp(args []string) int {
	fs := flag.NewFlagSet("files", flag.ContinueOnError)
	sid := fs.String("sid", "", "SAP system")
	from := fs.String("from", "", "download directory holding the .SAR files")
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
	files, dir, ok := pickArchives(t, *from, *yes)
	if !ok {
		return ExitError
	}
	e := newEnv(t)
	res, err := ops.CopySARs(ctx, e, files)
	if res != nil && res.Listing != "" {
		e.Pr.Block("Archives now in "+res.Dest, res.Listing)
	}
	if err != nil {
		return fail(err)
	}
	pal := currentPalette()
	fmt.Fprintf(stdout, "  %s %d archive(s) copied from %s · chown %s\n", pal.Check(), len(res.Copied), dir, res.ChownNote)
	return ExitOK
}

// pickArchives asks for the download directory, scans it for today's
// archives, shows them in apply order and asks for confirmation.
func pickArchives(t *system.Target, from string, yes bool) ([]ops.SARFile, string, bool) {
	pal := currentPalette()
	dir := from
	if dir == "" {
		def := ""
		if t.Snapshot != nil {
			def = t.Snapshot.LastDownloadDir
		}
		if def == "" {
			def, _ = os.Getwd()
		}
		dir = ask("Directory with the downloaded .SAR files", def)
	}
	today, older, err := ops.ScanSARs(dir, time.Now())
	if err != nil {
		fail(err)
		return nil, dir, false
	}
	if len(today) == 0 {
		fmt.Fprintf(stdout, "  %s no .SAR files dated today (%s) in %s", pal.Cross(), time.Now().Format("2006-01-02"), dir)
		if len(older) > 0 {
			fmt.Fprintf(stdout, " · %d older archive(s) ignored", len(older))
		}
		fmt.Fprintln(stdout)
		return nil, dir, false
	}
	showArchives(today, older)
	if !yes && !confirm(fmt.Sprintf("Copy these %d archive(s) into %s?", len(today), t.KernelDir), true) {
		fmt.Fprintln(stdout, "  cancelled")
		return nil, dir, false
	}
	return today, dir, true
}

// showArchives prints the archives in apply order plus the resulting level.
func showArchives(files, older []ops.SARFile) {
	pal := currentPalette()
	rows := [][]string{{"#", "ARCHIVE", "COMPONENT", "PATCH", "SIZE", "MODIFIED"}}
	for i, f := range files {
		kind := f.Component
		if f.Full {
			kind += " (full kernel)"
		}
		rows = append(rows, []string{fmt.Sprint(i + 1), f.Name, kind, fmt.Sprint(f.Patch), ops.HumanSize(f.Size), f.ModTime.Format("2006-01-02 15:04")})
	}
	fmt.Fprintf(stdout, "\n  %s\n", pal.Paint(ui.Cyan, "Archives dated today, in apply order (lowest patch first)"))
	for _, l := range ui.Table("    ", rows) {
		fmt.Fprintln(stdout, l)
	}
	fmt.Fprintf(stdout, "    → target level after apply: %s", pal.Paint(ui.Bold, fmt.Sprintf("patch %d", ops.TargetPatch(files))))
	if len(older) > 0 {
		fmt.Fprintf(stdout, "   · %s", pal.Paint(ui.Dim, fmt.Sprintf("%d older archive(s) ignored", len(older))))
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout)
}
