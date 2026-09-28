package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

var (
	centralScope = choice{"C", "Central only (recommended)", []string{"central", "merkezi"}}
	allScope     = choice{"A", "All kernel directories on this host", []string{"all", "hepsi"}}
)

// chooseScope decides what Kernel File Transfer and Kernel Update touch.
// On a single host nothing changes: every kernel directory here, no
// question. When the system has instances on other hosts (ASCS here,
// application servers elsewhere) it shows the layout and offers the central
// copy: only DIR_CT_RUN, the shared directory each host's sapcpe copies from
// when its instance starts. central preselects that (--central).
func chooseScope(ctx context.Context, e *ops.Env, central, yes bool) []ops.RemoteInstance {
	e.CentralOnly = central
	remote := ops.RemoteInstances(ctx, e)
	if len(remote) == 0 {
		return nil
	}
	pal := currentPalette()
	var others, local []string
	for _, r := range remote {
		others = append(others, r.Label())
	}
	for _, in := range e.T.Instances {
		local = append(local, in.Name)
	}
	fmt.Fprintf(stdout, "\n  %s\n", pal.Header("Scope"))
	fmt.Fprintf(stdout, "    this host    central %s + %d local instance director%s (%s)\n", e.T.KernelDir, len(e.T.KernelDirs)-1, plural(len(e.T.KernelDirs)-1, "y", "ies"), strings.Join(local, ", "))
	fmt.Fprintf(stdout, "    other hosts  %d instance(s): %s — sapcpe copies the central kernel into their local exe when they start\n", len(remote), strings.Join(others, "  "))
	if missing := ops.WithoutSapcpe(e.ProfileDir(), remote); len(missing) > 0 {
		fmt.Fprintf(stdout, "    %s %s: profile without sapcpe — run KernelMan there too (8) Send to Other Servers)\n", pal.Paint(ui.Yellow, "!"), strings.Join(missing, ", "))
	}
	if yes || central {
		e.CentralOnly = central
	} else {
		e.CentralOnly = choose("", centralScope, allScope) == "C"
	}
	if e.CentralOnly {
		fmt.Fprintf(stdout, "  %s\n", pal.Paint(ui.Dim, "central only: one copy in "+e.T.KernelDir+"; every instance takes it at its next start"))
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

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
