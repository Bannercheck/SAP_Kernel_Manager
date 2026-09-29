package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/status"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// noteRemote tells the operator about instances of the system on other
// hosts (a distributed system). Nothing changes in what the operation does:
// every kernel directory on this host is handled, and the other hosts take
// the central kernel through sapcpe when their instances start — or need
// KernelMan run there when a profile has no sapcpe.
func noteRemote(ctx context.Context, e *ops.Env) []ops.RemoteInstance {
	remote := ops.RemoteInstances(ctx, e)
	if len(remote) == 0 {
		return nil
	}
	pal := currentPalette()
	var others []string
	for _, r := range remote {
		others = append(others, r.Label())
	}
	fmt.Fprintf(stdout, "  %s %d instance(s) on other hosts: %s — they take the central kernel (%s) via sapcpe when they start\n",
		pal.Paint(ui.Dim, "other hosts"), len(remote), strings.Join(others, "  "), e.T.KernelDir)
	if missing := ops.WithoutSapcpe(e.ProfileDir(), remote); len(missing) > 0 {
		fmt.Fprintf(stdout, "  %s %s: profile without sapcpe — run KernelMan there too (8) Send to Other Servers)\n", pal.Paint(ui.Yellow, "!"), strings.Join(missing, ", "))
	}
	return remote
}

// runningRemote lists remote instances whose processes are not GRAY.
func runningRemote(remote []ops.RemoteInstance) []string {
	var out []string
	for _, r := range remote {
		if r.Status != "" && r.Status != "GRAY" {
			out = append(out, r.Label()+" "+r.Status)
		}
	}
	return out
}

// syncWarnings lists local instances whose exe directory is not at the
// central kernel level — the check that keeps a half-updated system from
// going unnoticed until the start fails.
func syncWarnings(rep *status.Report, pal ui.Palette) []string {
	var out []string
	for _, sys := range rep.Systems {
		for _, in := range sys.Instances {
			if in.Local && in.ExeChecked > 0 && len(in.ExeDiffers) > 0 {
				out = append(out, fmt.Sprintf("%s %s %s: %s is not at the central kernel level (%s differ) → 6) Kernel Update",
					pal.Cross(), sys.SID, in.Name, in.DirExecutable, strings.Join(in.ExeDiffers, ", ")))
			}
		}
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
