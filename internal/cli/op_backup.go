package cli

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// BackupOp implements Kernel Backup: kernel dir → exe_<date>, then the listing.
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
	if !*yes && !confirm(fmt.Sprintf("Copy %s to %s/%s?", t.KernelDir, parentDir(t.KernelDir), ops.BackupName(parentDir(t.KernelDir), time.Now())), true) {
		fmt.Fprintln(stdout, "  cancelled")
		return ExitError
	}
	res, err := ops.Backup(ctx, e)
	if res != nil && res.Listing != "" {
		e.Pr.Block("Backup directory "+res.Dest, res.Listing)
	}
	if err != nil {
		return fail(err)
	}
	fmt.Fprintf(stdout, "  %s Backup ready: %s  (%d files, %s)\n", pal.Check(), pal.Paint(ui.Bold, res.Dest), res.Files, ops.HumanSize(res.Bytes))
	return ExitOK
}
