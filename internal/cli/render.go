package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/status"
)

const labelWidth = 28

func kv(w io.Writer, label, value string) {
	if value == "" {
		value = "-"
	}
	fmt.Fprintf(w, "  %-*s %s\n", labelWidth, label, value)
}

func boolWord(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

// RenderStatus writes the human readable status screen.
func RenderStatus(w io.Writer, rep *status.Report) {
	fmt.Fprintf(w, "skm status · %s · %s\n\n", rep.Host.Hostname, rep.GeneratedAt.Format("2006-01-02 15:04:05"))

	fmt.Fprintln(w, "HOST")
	kv(w, "Hostname", rep.Host.Hostname)
	kv(w, "OS", rep.Host.OSVersion)
	kv(w, "OS architecture", fmt.Sprintf("%s/%s (SAP platform dir: %s)", rep.Host.OS, rep.Host.Arch, rep.Host.KernelDirName))
	kv(w, "User", fmt.Sprintf("%s (%s)", rep.Host.User, boolWord(rep.Host.Privileged, "privileged", "not privileged")))
	ha := rep.HostAgent
	switch {
	case !ha.Installed:
		kv(w, "SAP Host Agent", "not installed · "+ha.Error)
	default:
		kv(w, "SAP Host Agent", fmt.Sprintf("%s · %s · %s", boolWord(ha.Running, "running", "NOT running"), ha.Version, ha.Path))
	}

	for _, sys := range rep.Systems {
		fmt.Fprintf(w, "\nSYSTEM %s · %s · status %s\n", sys.SID, sys.Type, sys.Status)
		kv(w, "SID", sys.SID)
		kv(w, "SAP system type", sys.Type)
		kv(w, "Database", strings.TrimSpace(sys.Database.DisplayName()+" "+dbDetail(sys.Database)))
		if sys.Kernel.Release > 0 {
			kv(w, "Kernel version", fmt.Sprintf("%d (%s)", sys.Kernel.Release, sys.Kernel.ReleaseDotted()))
			kv(w, "Kernel patch level", patchDetail(sys))
		} else {
			kv(w, "Kernel version", "unknown")
			kv(w, "Kernel patch level", "unknown")
		}
		kv(w, "Kernel platform", strings.TrimSpace(strings.Join(nonEmpty(sys.Kernel.Platform, sys.Kernel.CompilationMode, sys.Kernel.CompiledFor), " · ")))
		kv(w, "Kernel source", sys.KernelSource)
		kv(w, "SAP_BASIS (kernel supports)", basisRange(sys.Kernel.SupportedBasis)+"  [actual SAP_BASIS needs DB/RFC access: planned]")
		kv(w, "Kernel directory", sys.DirExeRoot+"  (DIR_EXE_ROOT)")
		kv(w, "DIR_CT_RUN", sys.DirCtRun)
		kv(w, "Global kernel directory", sys.GlobalKernelDir)

		fmt.Fprintf(w, "  %-*s\n", labelWidth, "Local kernel directories")
		for _, in := range sys.Instances {
			if in.Local {
				fmt.Fprintf(w, "  %-*s %-8s %s\n", labelWidth, "", in.Name, orDash(in.DirExecutable))
			}
		}

		fmt.Fprintln(w, "  Instances")
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "    NR\tNAME\tTYPE\tHOST\tSAPSTARTSRV\tSTATUS\tPROFILE")
		for _, in := range sys.Instances {
			fmt.Fprintf(tw, "    %s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				in.Nr, orDash(in.Name), in.TypeDesc, in.Host, in.Sapstartsrv, in.Status, orDash(in.Profile))
		}
		tw.Flush()
		for _, in := range sys.Instances {
			if in.Error != "" {
				fmt.Fprintf(w, "    ! instance %s: %s\n", in.Nr, in.Error)
			}
		}
		for _, e := range sys.Errors {
			fmt.Fprintf(w, "    ! %s\n", e)
		}
	}

	if len(rep.Warnings) > 0 {
		fmt.Fprintln(w, "\nWARNINGS")
		for _, wmsg := range rep.Warnings {
			fmt.Fprintln(w, "  -", wmsg)
		}
	}
}

func dbDetail(db status.DB) string {
	var parts []string
	if db.Name != "" {
		parts = append(parts, "name "+db.Name)
	}
	if db.Host != "" {
		parts = append(parts, "host "+db.Host)
	}
	if len(parts) == 0 {
		return ""
	}
	return "· " + strings.Join(parts, " · ")
}

func patchDetail(sys status.System) string {
	s := fmt.Sprintf("%d", sys.Kernel.Patch)
	if sys.Kernel.Changelist > 0 {
		s += fmt.Sprintf(" (changelist %d)", sys.Kernel.Changelist)
	}
	if sys.Kernel.CompileTime != "" {
		s += " · compiled " + sys.Kernel.CompileTime
	}
	return s
}

func basisRange(rels []string) string {
	switch len(rels) {
	case 0:
		return "unknown"
	case 1:
		return rels[0]
	default:
		return rels[0] + "–" + rels[len(rels)-1]
	}
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
