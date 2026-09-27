package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// BackupOp implements Kernel Backup: every kernel directory is copied next
// to itself as <name>_<date>, then the listings are shown.
func BackupOp(args []string) int {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	sid := fs.String("sid", "", "SAP system")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
	defer cancel()
	t, err := target(ctx, *sid)
	if err != nil {
		return fail(err)
	}
	e := newEnv(t)
	pal := currentPalette()
	now := time.Now()
	fmt.Fprintf(stdout, "  %s\n", pal.Paint(ui.Cyan, "Kernel directories → backups"))
	for _, d := range kernelDirs(t) {
		fmt.Fprintf(stdout, "    %s  →  %s\n", d, ops.BackupName(d, now))
	}
	if !*yes && !confirm(fmt.Sprintf("Back up these %d directories?", len(kernelDirs(t)))) {
		fmt.Fprintln(stdout, "  cancelled")
		return ExitError
	}
	res, err := ops.Backup(ctx, e)
	for _, d := range res.Dirs {
		e.Pr.Block("Backup "+d.Dest, d.Listing)
	}
	if err != nil {
		return fail(err)
	}
	var dests []string
	for _, d := range res.Dirs {
		dests = append(dests, d.Dest)
	}
	fmt.Fprintf(stdout, "  %s Backup ready: %d directories, %d files, %s\n    %s\n    full listings: %s\n", pal.Check(),
		len(res.Dirs), res.Files(), ops.HumanSize(res.Bytes()), pal.Paint(ui.Bold, strings.Join(dests, "\n    ")), res.LogFile)
	return ExitOK
}
