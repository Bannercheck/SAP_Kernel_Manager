package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// UpdateOp implements Kernel Update: stopped system + today's backup →
// SAPCAR in ascending patch order → chown → saproot.sh → verify → start.
func UpdateOp(args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	sid := fs.String("sid", "", "SAP system")
	from := fs.String("from", "", "download directory (when the archives were not copied with Kernel Files)")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	start := fs.Bool("start", false, "start the system afterwards without asking")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()
	t, err := target(ctx, *sid)
	if err != nil {
		return fail(err)
	}
	e := newEnv(t)
	pal := currentPalette()

	if !ensureStopped(ctx, e, *yes) {
		return ExitError
	}
	if !ensureBackup(ctx, e, *yes) {
		return ExitError
	}

	files, ok := pickArchives(ctx, t, *from, true)
	if !ok {
		return ExitError
	}
	dir := filepath.Dir(files[0].Path)
	if !*yes && !confirm(fmt.Sprintf("Extract these %d archive(s) into %d kernel directories in this order?", len(files), len(kernelDirs(t)))) {
		fmt.Fprintln(stdout, "  cancelled")
		return ExitError
	}
	sapcar, err := ops.FindSAPCAR(ctx, e, []string{dir}, lastSAPCARs)
	if err != nil {
		return fail(err)
	}
	res, err := ops.Extract(ctx, e, sapcar, files)
	if err != nil {
		return fail(err)
	}
	fmt.Fprintf(stdout, "\n  %s Kernel %s: %s → %s\n", pal.Check(), t.SID, res.Before, pal.Paint(ui.Bold, res.After.Long()))
	if res.SaprootNote != "done" || res.ChownNote != "done" {
		fmt.Fprintf(stdout, "  %s not root: run chown -R %s:%s and saproot.sh %s as root before starting\n", pal.Paint(ui.Yellow, "!"), t.SIDAdm, t.Group, t.SID)
	}
	return offerStart(ctx, e, *start)
}

// RollbackOp copies the latest exe_<date> backup back over the kernel directory.
func RollbackOp(args []string) int {
	fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
	sid := fs.String("sid", "", "SAP system")
	backup := fs.String("backup", "", "backup directory (default: latest exe_<date> next to the kernel directory)")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()
	t, err := target(ctx, *sid)
	if err != nil {
		return fail(err)
	}
	e := newEnv(t)
	pal := currentPalette()
	backups := map[string]string{}
	for _, d := range kernelDirs(t) {
		if t.Snapshot != nil && t.Snapshot.LastBackups[d] != "" {
			if _, err := os.Stat(t.Snapshot.LastBackups[d]); err == nil {
				backups[d] = t.Snapshot.LastBackups[d]
				continue
			}
		}
		if latest, ok := ops.LatestBackup(d); ok {
			backups[d] = latest
		}
	}
	if *backup != "" {
		backups[t.KernelDir] = *backup
	}
	fmt.Fprintf(stdout, "  %s\n", pal.Paint(ui.Cyan, "Backups → kernel directories"))
	for _, d := range kernelDirs(t) {
		b := backups[d]
		if b == "" {
			b = pal.Paint(ui.Red, "none found")
		}
		fmt.Fprintf(stdout, "    %s  →  %s\n", b, d)
	}
	if !*yes {
		if b := ask("Backup for "+t.KernelDir, backups[t.KernelDir]); b != "" {
			backups[t.KernelDir] = b
		}
	}
	if !ensureStopped(ctx, e, *yes) {
		return ExitError
	}
	if !*yes && !confirm(fmt.Sprintf("Copy the backups over %d kernel directories?", len(kernelDirs(t)))) {
		fmt.Fprintln(stdout, "  cancelled")
		return ExitError
	}
	res, err := ops.Restore(ctx, e, backups)
	if err != nil {
		return fail(err)
	}
	fmt.Fprintf(stdout, "\n  %s Kernel %s restored: %s → %s\n", pal.Check(), t.SID, res.Before, pal.Paint(ui.Bold, res.After.String()))
	return offerStart(ctx, e, false)
}

// ensureStopped shows the lights and offers to stop a running system.
func ensureStopped(ctx context.Context, e *ops.Env, yes bool) bool {
	if lights(ctx, e) {
		return true
	}
	pal := currentPalette()
	fmt.Fprintf(stdout, "  %s system %s is running; the kernel can only be replaced while it is stopped.\n", pal.Paint(ui.Yellow, "!"), e.T.SID)
	if !yes && !confirm("Stop it now?") {
		fmt.Fprintln(stdout, "  cancelled")
		return false
	}
	return runStop(ctx, e) == ExitOK
}

// ensureBackup requires a backup taken today, offering to take one.
func ensureBackup(ctx context.Context, e *ops.Env, yes bool) bool {
	pal := currentPalette()
	s := e.T.Snapshot
	if s != nil && s.LastBackup != "" && sameDay(s.LastBackupAt, time.Now()) {
		if _, err := os.Stat(s.LastBackup); err == nil {
			fmt.Fprintf(stdout, "  %s backup from today: %s\n", pal.Check(), s.LastBackup)
			return true
		}
	}
	fmt.Fprintf(stdout, "  %s no kernel backup from today.\n", pal.Paint(ui.Yellow, "!"))
	if !yes && !confirm("Take a Kernel Backup now?") {
		fmt.Fprintln(stdout, "  cancelled: an update without a fresh backup is not allowed")
		return false
	}
	res, err := ops.Backup(ctx, e)
	if err != nil {
		fail(err)
		return false
	}
	fmt.Fprintf(stdout, "  %s backup ready: %d directories, %d files\n", pal.Check(), len(res.Dirs), res.Files())
	return true
}

func offerStart(ctx context.Context, e *ops.Env, start bool) int {
	if start || confirm("Start the system now?") {
		return runStart(ctx, e)
	}
	return ExitOK
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
