package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/download"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/kernel"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/swdc"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// newSWDCSession builds the SAP for Me session; tests point it at a local server.
var newSWDCSession = func(cred download.Credentials) *swdc.Session { return swdc.New(cred.User, cred.Password) }

// reachabilityURL is probed before asking for credentials; tests point it locally.
var reachabilityURL = swdc.DefaultLaunchpad + "/"

// checkInternet explains, instead of failing later, when SAP is not reachable.
func checkInternet(ctx context.Context) bool {
	pal := currentPalette()
	fmt.Fprintf(stdout, "  %s ", pal.Paint(ui.Dim, "checking access to SAP ..."))
	if err := download.Reachable(ctx, reachabilityURL, 10*time.Second); err != nil {
		fmt.Fprintln(stdout, pal.Cross())
		fmt.Fprintf(stdout, "  %s this host cannot reach SAP (%v)\n", pal.Paint(ui.Yellow, "!"), err)
		fmt.Fprintln(stdout, "  Kernel Download is optional: copy the .SAR files to this server (scp/sftp) and use Kernel File Transfer.")
		return false
	}
	fmt.Fprintln(stdout, pal.Paint(ui.Green, "ok"))
	return true
}

// swdcLogPath is where every login/search step is recorded for diagnosis.
func swdcLogPath() string { return filepath.Join(credentialsDir(), "swdc.log") }

// autoSelect logs in to the Software Center, searches the kernel of the
// target system and returns the proposed archives in apply order.
func autoSelect(ctx context.Context, t *system.Target, cred download.Credentials) ([]download.Item, *swdc.Session, error) {
	pal := currentPalette()
	want, err := wantFor(ctx, t)
	if err != nil {
		return nil, nil, err
	}
	fmt.Fprintf(stdout, "  %s\n", pal.Paint(ui.Dim, fmt.Sprintf("looking for: kernel %d · %s · %s · current patch %d",
		want.Release, want.PlatformDir, strings.ToUpper(want.DB), want.CurrentPatch)))

	os.MkdirAll(credentialsDir(), 0o700)
	logf, _ := os.OpenFile(swdcLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if logf != nil {
		defer logf.Close()
		fmt.Fprintf(logf, "\n=== %s kernel download for %s ===\n", time.Now().Format(time.RFC3339), t.SID)
	}
	s := newSWDCSession(cred)
	s.Log = func(f string, a ...any) {
		if logf != nil {
			fmt.Fprintf(logf, time.Now().Format("15:04:05")+" "+f+"\n", a...)
		}
	}
	s.MFAPrompt = func(prompt string) string { return askSecret(prompt) }

	fmt.Fprintf(stdout, "  %s ", pal.Paint(ui.Dim, "signing in to SAP for Me as "+cred.User+" ..."))
	if err := s.Login(ctx); err != nil {
		fmt.Fprintln(stdout)
		return nil, nil, fmt.Errorf("SAP for Me login: %w (details: %s)", err, swdcLogPath())
	}
	fmt.Fprintln(stdout, pal.Paint(ui.Green, "ok"))

	var all []swdc.Result
	seen := map[string]bool{}
	for _, q := range swdc.SearchQueries(want) {
		res, err := s.Search(ctx, q)
		if err != nil {
			fmt.Fprintf(stdout, "  %s search %q: %v\n", pal.Paint(ui.Yellow, "!"), q, err)
			continue
		}
		for _, r := range res {
			if !seen[strings.ToLower(r.Title)] {
				seen[strings.ToLower(r.Title)] = true
				all = append(all, r)
			}
		}
	}
	fmt.Fprintf(stdout, "  %s\n", pal.Paint(ui.Dim, fmt.Sprintf("%d catalogue entries examined", len(all))))
	sel := swdc.Choose(want, all)
	for _, n := range sel.Notes {
		fmt.Fprintf(stdout, "  %s %s\n", pal.Paint(ui.Dim, "·"), pal.Paint(ui.Dim, n))
	}
	if len(sel.Picks) == 0 {
		return nil, s, fmt.Errorf("no matching kernel archives found in the Software Center (log: %s)", swdcLogPath())
	}
	rows := [][]string{pal.Headers("#", "ARCHIVE", "COMPONENT", "PATCH", "SIZE", "DATE", "DESCRIPTION")}
	var items []download.Item
	for i, p := range sel.Picks {
		kind := p.Component
		if p.Full {
			kind += " (full kernel)"
		}
		size := "?"
		if p.SizeKB > 0 {
			size = ops.HumanSize(p.SizeKB << 10)
		}
		rows = append(rows, []string{fmt.Sprint(i + 1), p.Title, kind, fmt.Sprint(p.Patch), size, p.Date, p.Description})
		items = append(items, download.Item{URL: p.Link, Name: p.Title, Size: p.SizeKB << 10})
	}
	fmt.Fprintf(stdout, "\n  %s\n", pal.Header(fmt.Sprintf("Proposed download for kernel %d (stack %d → target patch %d)", want.Release, sel.StackLevel, sel.TargetLevel)))
	for _, l := range ui.Table("    ", rows) {
		fmt.Fprintln(stdout, l)
	}
	fmt.Fprintln(stdout)
	return items, s, nil
}

// wantFor derives release, platform and database of the target system.
func wantFor(ctx context.Context, t *system.Target) (swdc.Want, error) {
	w := swdc.Want{PlatformDir: platformNow().KernelDirName()}
	if v, err := kernel.Probe(ctx, runner, platformNow(), t.KernelDir); err == nil {
		w.Release, w.CurrentPatch = v.Release, v.Patch
		if v.Platform != "" {
			w.PlatformDir = v.Platform
		}
	} else {
		for _, in := range t.Instances {
			if in.Release > 0 {
				w.Release, w.CurrentPatch = in.Release, in.Patch
				break
			}
		}
	}
	if w.Release == 0 {
		return w, fmt.Errorf("cannot determine the current kernel release of %s (disp+work -V and saphostctrl both unavailable)", t.SID)
	}
	if t.Sapcontrol != "" {
		for _, in := range t.Instances {
			if v, err := t.Client(runner, in.Nr, 20*time.Second).ParameterValue(ctx, "dbms/type"); err == nil && v != "" {
				w.DB = strings.ToLower(v)
				break
			}
		}
	}
	return w, nil
}
