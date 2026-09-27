package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/disk"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/status"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

const labelWidth = 28

func kv(w io.Writer, label, value string) {
	if value == "" {
		value = "-"
	}
	fmt.Fprintf(w, "  %-*s %s\n", labelWidth, label, value)
}

// statusColour maps SAP display statuses to traffic-light colours.
// GRAY (stopped) is shown red on purpose: a stopped system needs attention.
func statusColour(s string) ui.Colour {
	switch strings.ToUpper(s) {
	case "GREEN":
		return ui.Green
	case "YELLOW":
		return ui.Yellow
	case "RED", "GRAY":
		return ui.Red
	}
	return ui.Dim
}

func statusLight(pal ui.Palette, s string) string { return pal.Light(statusColour(s)) }

// stateWord turns a SAP display status into the word shown on badges.
func stateWord(s string) string {
	switch strings.ToUpper(s) {
	case "GREEN":
		return "RUNNING"
	case "YELLOW":
		return "PARTIAL"
	case "RED":
		return "ERROR"
	case "GRAY":
		return "STOPPED"
	}
	return "UNKNOWN"
}

func sapstartsrvLight(pal ui.Palette, s string) string {
	switch s {
	case "running":
		return pal.Light(ui.Green)
	case "not running":
		return pal.Light(ui.Red)
	case "remote":
		return pal.Light(ui.Dim)
	}
	return pal.Light(ui.Yellow)
}

// RenderStatus writes the human readable status screen.
func RenderStatus(w io.Writer, rep *status.Report, pal ui.Palette) {
	fmt.Fprintf(w, "%s\n", pal.Paint(ui.Bold, fmt.Sprintf("SAP Status · Hostname %s · %s", rep.Host.Hostname,
		rep.GeneratedAt.Format("2006-01-02 15:04:05"))))
	fmt.Fprintf(w, "  %s Running   %s Partial   %s Stopped   %s Remote/Unknown\n\n",
		pal.Light(ui.Green), pal.Light(ui.Yellow), pal.Light(ui.Red), pal.Light(ui.Dim))

	fmt.Fprintln(w, pal.Paint(ui.Cyan, "HOST"))
	kv(w, "Hostname", rep.Host.Hostname)
	kv(w, "OS", rep.Host.OSVersion)
	kv(w, "OS Architecture", fmt.Sprintf("%s/%s (SAP platform dir: %s)", rep.Host.OS, rep.Host.Arch, rep.Host.KernelDirName))
	priv := "not privileged"
	if rep.Host.Privileged {
		priv = "privileged"
	}
	kv(w, "User", fmt.Sprintf("%s (%s)", rep.Host.User, priv))
	ha := rep.HostAgent
	switch {
	case !ha.Installed:
		kv(w, "SAP Host Agent", pal.Badge(ui.Red, "NOT INSTALLED")+" · "+ha.Error)
	case ha.Running:
		kv(w, "SAP Host Agent", fmt.Sprintf("%s · %s · %s", pal.Badge(ui.Green, "RUNNING"), ha.Version, ha.Path))
	default:
		kv(w, "SAP Host Agent", fmt.Sprintf("%s · %s · %s", pal.Badge(ui.Red, "STOPPED"), ha.Version, ha.Path))
	}

	for _, sys := range rep.Systems {
		fmt.Fprintf(w, "\n%s %s %s\n", pal.Paint(ui.Cyan, fmt.Sprintf("SYSTEM %s · %s · Hostname %s ·", sys.SID, sys.Type, rep.Host.Hostname)),
			pal.Badge(statusColour(sys.Status), stateWord(sys.Status)), pal.Paint(ui.Dim, "("+sys.Status+")"))
		kv(w, "SID", sys.SID)
		kv(w, "Hostname", rep.Host.Hostname)
		kv(w, "SAP System Type", sys.Type)
		kv(w, "Database", strings.TrimSpace(sys.Database.DisplayName()+" "+dbDetail(sys.Database)))
		if sys.Kernel.Release > 0 {
			kv(w, "Kernel Version", fmt.Sprintf("%d (%s)", sys.Kernel.Release, sys.Kernel.ReleaseDotted()))
			kv(w, "Kernel Patch Level", patchDetail(sys))
		} else {
			kv(w, "Kernel Version", "unknown")
			kv(w, "Kernel Patch Level", "unknown")
		}
		kv(w, "Kernel Platform", strings.Join(nonEmpty(sys.Kernel.Platform, sys.Kernel.CompilationMode, sys.Kernel.CompiledFor), " · "))
		kv(w, "Kernel Source", sys.KernelSource)
		kv(w, "SAP_BASIS (Kernel Supports)", basisRange(sys.Kernel.SupportedBasis)+"  [actual SAP_BASIS level needs DB/RFC access: planned]")
		kv(w, "Kernel Directory", suffixIf(sys.DirExeRoot, "  (DIR_EXE_ROOT)"))
		kv(w, "DIR_CT_RUN", sys.DirCtRun)
		kv(w, "Global Kernel Directory", sys.GlobalKernelDir)

		fmt.Fprintf(w, "  %-*s\n", labelWidth, "Local Kernel Directories")
		for _, in := range sys.Instances {
			if in.Local {
				fmt.Fprintf(w, "  %-*s %-8s %s\n", labelWidth, "", in.Name, orDash(in.DirExecutable))
			}
		}

		fmt.Fprintln(w, "  Instances")
		rows := [][]string{pal.Headers("NR", "NAME", "TYPE", "HOST", "SAPSTARTSRV", "STATUS", "PROFILE")}
		for _, in := range sys.Instances {
			rows = append(rows, []string{in.Nr, orDash(in.Name), in.TypeDesc, in.Host,
				sapstartsrvLight(pal, in.Sapstartsrv),
				statusLight(pal, in.Status) + " " + in.Status, orDash(in.Profile)})
		}
		for _, l := range ui.Table("    ", rows) {
			fmt.Fprintln(w, l)
		}
		for _, in := range sys.Instances {
			if in.Error != "" {
				fmt.Fprintf(w, "    %s instance %s: %s\n", pal.Cross(), in.Nr, in.Error)
			}
		}
		for _, e := range sys.Errors {
			fmt.Fprintf(w, "    %s %s\n", pal.Cross(), e)
		}
	}

	renderDisk(w, rep.Disk, pal)

	if len(rep.Warnings) > 0 {
		fmt.Fprintln(w, "\n"+pal.Paint(ui.Yellow, "WARNINGS"))
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
		s += fmt.Sprintf(" (Changelist %d)", sys.Kernel.Changelist)
	}
	if sys.Kernel.CompileTime != "" {
		s += " · Compiled " + sys.Kernel.CompileTime
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

// suffixIf appends suffix only when s is not empty.
func suffixIf(s, suffix string) string {
	if s == "" {
		return ""
	}
	return s + suffix
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// freeColour grades free space: green above 20 %, yellow above 10 %, red below.
func freeColour(fs disk.Filesystem) ui.Colour {
	switch {
	case fs.SizeKB == 0:
		return ui.Dim
	case fs.AvailKB*100/fs.SizeKB > 20:
		return ui.Green
	case fs.AvailKB*100/fs.SizeKB > 10:
		return ui.Yellow
	}
	return ui.Red
}

// diskTreeLines renders one root as a tree: the root line carries the free
// space light, children carry their size; at most maxKids per node, the
// rest summarised.
func diskTreeLines(d status.Disk, root disk.Node, pal ui.Palette, maxKids, depth int) []string {
	free := pal.Paint(ui.Dim, "free space n/a")
	for _, fs := range d.Filesystems {
		if fs.Path == root.Path {
			free = fmt.Sprintf("%s %s free of %s (%d%% used, %s)", pal.Light(freeColour(fs)), disk.Human(fs.AvailKB), disk.Human(fs.SizeKB), fs.UsePct, fs.Mount)
		}
	}
	lines := []string{fmt.Sprintf("%s  %s   %s", pal.Paint(ui.Bold, root.Path), pal.Paint(ui.Bold, disk.Human(root.KB)), free)}
	var walk func(n disk.Node, prefix string, level int)
	walk = func(n disk.Node, prefix string, level int) {
		if level > depth {
			return
		}
		kids := n.Children
		var rest int64
		hidden := 0
		if len(kids) > maxKids {
			for _, k := range kids[maxKids:] {
				rest += k.KB
			}
			hidden = len(kids) - maxKids
			kids = kids[:maxKids]
		}
		for i, k := range kids {
			last := i == len(kids)-1 && hidden == 0
			lines = append(lines, fmt.Sprintf("%s%s%-14s %s", prefix, pal.Branch(last), k.Name, disk.Human(k.KB)))
			walk(k, prefix+pal.Trunk(last), level+1)
		}
		if hidden > 0 {
			lines = append(lines, fmt.Sprintf("%s%s%s", prefix, pal.Branch(true), pal.Paint(ui.Dim, fmt.Sprintf("… %d more (%s)", hidden, disk.Human(rest)))))
		}
	}
	walk(root, "", 1)
	return lines
}

// renderDisk prints the file systems and the size tree of every SAP root.
func renderDisk(w io.Writer, d status.Disk, pal ui.Palette) {
	fmt.Fprintln(w, "\n"+pal.Paint(ui.Cyan, "DISK"))
	if d.Error != "" && len(d.Filesystems) == 0 {
		fmt.Fprintf(w, "  %s %s\n", pal.Cross(), d.Error)
		return
	}
	rows := [][]string{pal.Headers("FILE SYSTEM", "MOUNT", "SIZE", "USED", "FREE", "USE%", "")}
	for _, fs := range d.Filesystems {
		rows = append(rows, []string{fs.Device, fs.Mount, disk.Human(fs.SizeKB), disk.Human(fs.UsedKB), disk.Human(fs.AvailKB),
			fmt.Sprintf("%d%%", fs.UsePct), pal.Light(freeColour(fs))})
	}
	for _, l := range ui.Table("  ", rows) {
		fmt.Fprintln(w, l)
	}
	for _, tree := range d.Trees {
		fmt.Fprintln(w)
		for _, l := range diskTreeLines(d, tree, pal, 8, 2) {
			fmt.Fprintln(w, "  "+l)
		}
	}
	if d.Error != "" {
		fmt.Fprintf(w, "  %s %s\n", pal.Paint(ui.Yellow, "!"), d.Error)
	}
}
