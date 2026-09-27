package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ship"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// locateProgram finds or assembles the KernelMan distribution to send along; tests replace it.
var locateProgram = func(tmp string) (ship.Program, error) { return ship.Locate(tmp) }

// ShipOp implements "Send to Other Servers": today's archives plus KernelMan
// itself go to the given hosts over scp and are verified with cksum.
func ShipOp(args []string) int {
	fs := flag.NewFlagSet("ship", flag.ContinueOnError)
	sid := fs.String("sid", "", "SAP system")
	hosts := fs.String("hosts", "", "target hosts, comma separated")
	user := fs.String("user", "", "remote login (default: the SAP system's <sid>adm)")
	to := fs.String("to", "/usr/sap/download", "remote directory")
	from := fs.String("from", "", "search only this directory for archives")
	noArchives := fs.Bool("program-only", false, "send KernelMan only, no archives")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Hour)
	defer cancel()
	pal := currentPalette()
	t, err := target(ctx, *sid)
	if err != nil {
		return fail(err)
	}

	var files []ops.SARFile
	if !*noArchives {
		var ok bool
		files, ok = pickArchivesForShip(ctx, t, *from, *yes)
		if !ok && *yes {
			return ExitError
		}
	}
	hostList := splitHosts(*hosts)
	if len(hostList) == 0 && !*yes {
		hostList = splitHosts(ask("Target servers (comma or space separated)", ""))
	}
	if len(hostList) == 0 {
		return fail(fmt.Errorf("no target servers given"))
	}
	login := *user
	if login == "" {
		login = t.SIDAdm
		if !*yes {
			login = ask("Remote login", login)
		}
	}
	remoteDir := *to
	if !*yes {
		remoteDir = ask("Remote directory", remoteDir)
	}

	tmp, err := os.MkdirTemp("", "kernelman-ship-")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(tmp)
	prog, err := locateProgram(tmp)
	if err != nil {
		return fail(fmt.Errorf("locate KernelMan distribution: %w", err))
	}

	fmt.Fprintf(stdout, "\n  %s\n", pal.Header("Shipment"))
	fmt.Fprintf(stdout, "    archives   %d file(s)\n", len(files))
	fmt.Fprintf(stdout, "    program    %s (%d files%s)\n", prog.Dir, len(prog.Files), map[bool]string{true: ", assembled from the running binary", false: ""}[prog.Created])
	fmt.Fprintf(stdout, "    to         %s@{%s}:%s\n", login, strings.Join(hostList, ","), remoteDir)
	fmt.Fprintf(stdout, "    %s\n\n", pal.Paint(ui.Dim, "ssh keys are used when set up; otherwise ssh asks for the password of each host"))
	if !*yes && !confirm(fmt.Sprintf("Send to %d server(s)?", len(hostList))) {
		fmt.Fprintln(stdout, "  cancelled")
		return ExitError
	}

	e := newEnv(t)
	results := ship.Send(ctx, e, ship.Options{Hosts: hostList, User: login, RemoteDir: remoteDir, Archives: files, Program: prog})
	failed := 0
	rows := [][]string{pal.Headers("HOST", "RESULT", "VERIFIED", "TIME")}
	for _, r := range results {
		if r.Listing != "" {
			e.Pr.Block(r.Host+":"+remoteDir, r.Listing)
		}
		if r.OK {
			rows = append(rows, []string{r.Host, pal.Check() + " sent", fmt.Sprintf("%d files", r.Verified), r.Duration.Round(time.Second).String()})
		} else {
			failed++
			rows = append(rows, []string{r.Host, pal.Cross() + " " + pal.Paint(ui.Red, r.Err.Error()), "", r.Duration.Round(time.Second).String()})
		}
	}
	for _, l := range ui.Table("    ", rows) {
		fmt.Fprintln(stdout, l)
	}
	fmt.Fprintf(stdout, "\n  On each server: cd %s/kernelman && ./kernelman.sh\n", remoteDir)
	if failed > 0 {
		return fail(fmt.Errorf("%d of %d server(s) failed", failed, len(results)))
	}
	fmt.Fprintf(stdout, "  %s %d server(s) ready: archives dated today under %s, KernelMan under %s/kernelman\n", pal.Check(), len(results), remoteDir, remoteDir)
	return ExitOK
}

// pickArchivesForShip finds today's archives like Kernel File Transfer does;
// when there are none the shipment can still carry KernelMan alone.
func pickArchivesForShip(ctx context.Context, t *system.Target, from string, yes bool) ([]ops.SARFile, bool) {
	pal := currentPalette()
	roots := scanRoots
	if from != "" {
		roots = []string{from}
	} else if len(roots) == 0 {
		roots = ops.DefaultScanRoots
	}
	res := scanFor(ctx, t, roots)
	if len(res.Today) == 0 {
		fmt.Fprintf(stdout, "  %s no .SAR files placed here today; KernelMan alone will be sent\n", pal.Paint(ui.Yellow, "!"))
		return nil, false
	}
	showArchives(res)
	if !yes && !confirm(fmt.Sprintf("Include these %d archive(s) in the shipment?", len(res.Today))) {
		return nil, false
	}
	return res.Today, true
}

func splitHosts(s string) []string {
	var out []string
	for _, h := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' || r == '\n' }) {
		if h = strings.TrimSpace(h); h != "" {
			out = append(out, h)
		}
	}
	return out
}
