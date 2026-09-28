package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	osuser "os/user"
	"strconv"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/disk"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/status"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/version"
)

// summaryFunc produces the system traffic-light lines shown above the menu.
// It is a variable so tests and transcripts can replace the live probe.
var summaryFunc = liveSummary

// Menu runs the interactive operation menu until the user quits. Choices
// are read from in so the menu can be driven by tests and transcripts.
func Menu(in io.Reader, out io.Writer, pal ui.Palette) int {
	rd := bufio.NewReader(in)
	if !pal.Unicode {
		out = ui.ASCIIWriter(out) // non-UTF-8 terminal: no mojibake from decorative glyphs
	}
	prevIn, prevOut, prevPal := input, stdout, palette
	input, stdout, palette = rd, out, &pal
	defer func() { input, stdout, palette = prevIn, prevOut, prevPal }()
	results := map[string]int{} // op ID → last exit code in this session
	summary := summaryFunc(pal)
	for {
		printMenu(out, pal, results, summary)
		fmt.Fprint(out, "Select an operation (number or name, q to quit): ")
		choice, _, err := readLine()
		if err != nil && choice == "" {
			return ExitOK
		}
		switch strings.ToLower(choice) {
		case "q", "quit", "exit", "0":
			return ExitOK
		case "":
			continue
		}
		op, ok := menuChoice(choice)
		if !ok {
			fmt.Fprintf(out, "\n%s unknown choice %q\n\n", pal.Cross(), choice)
			continue
		}
		fmt.Fprintf(out, "\n%s\n", pal.Paint(ui.Bold, "=== "+op.Name+" ==="))
		code := Dispatch(op, nil)
		results[op.ID] = code
		mark := pal.Check() + " " + pal.Paint(ui.Green, "done")
		if code != 0 {
			mark = pal.Cross() + " " + pal.Paint(ui.Red, fmt.Sprintf("failed (exit code %d)", code))
		}
		fmt.Fprintf(out, "\n--- %s: %s\n", op.Name, mark)
		if choose("", mainMenu, quit) == "Q" {
			return code
		}
		summary = summaryFunc(pal) // SAP Stop/Start change the lights
	}
}

func printMenu(w io.Writer, pal ui.Palette, results map[string]int, summary []string) {
	host, _ := os.Hostname()
	badge := ""
	if demoRoot != "" {
		badge = "  " + pal.Paint(ui.Yellow, "DEMO · simulated SAP host in "+demoRoot)
	}
	user := os.Getenv("USER")
	if user == "" {
		if u, err := osuser.Current(); err == nil {
			user = u.Username
		}
	}
	fmt.Fprintf(w, "%s   %s%s\n%s\n\n", pal.Paint(ui.Bold, version.DisplayName+" — "+version.ProductName),
		pal.Paint(ui.Dim, version.Version), badge,
		fmt.Sprintf("  Hostname %s · User %s · %s", pal.Paint(ui.Bold, host), user, time.Now().Format("2006-01-02 15:04")))
	for _, l := range summary {
		fmt.Fprintln(w, "  "+l)
	}
	fmt.Fprintln(w)
	for i, op := range menuOps() {
		mark := " "
		if code, ran := results[op.ID]; ran {
			mark = pal.Check()
			if code != 0 {
				mark = pal.Cross()
			}
		}
		fmt.Fprintf(w, "  %2d) %s %-21s %s\n", i+1, mark, op.Name, pal.Paint(ui.Dim, op.Summary))
	}
	fmt.Fprintln(w, "   q)   Quit")
	fmt.Fprintln(w)
}

// liveSummary probes the host and renders the system overview above the menu.
func liveSummary(pal ui.Palette) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return SummaryLines(collectStatus(ctx, status.Options{Timeout: 15 * time.Second, DirSizes: true, DiskRoots: diskRoots()}), pal)
}

// SummaryLines renders the overview above the menu: a big badge per SAP
// system (RUNNING / PARTIAL / STOPPED), its type, hostname, kernel level and
// one light per instance, plus the SAP Host Agent.
func SummaryLines(rep *status.Report, pal ui.Palette) []string {
	rows := [][]string{pal.Headers("STATE", "SYSTEM", "TYPE", "HOSTNAME", "KERNEL", "INSTANCES")}
	for _, sys := range rep.Systems {
		var insts []string
		for _, in := range sys.Instances {
			name := in.Name
			if name == "" {
				name = in.Host + "/" + in.Nr
			}
			insts = append(insts, name+" "+statusLight(pal, in.Status))
		}
		rows = append(rows, []string{pal.Badge(statusColour(sys.Status), stateWord(sys.Status)), sys.SID, sys.Type, rep.Host.Hostname,
			"Kernel " + sys.Kernel.String(), strings.Join(insts, "  ")})
	}
	if len(rows) == 1 {
		rows = append(rows, []string{pal.Badge(ui.Dim, "NO SAP"), pal.Paint(ui.Dim, "no SAP instances found on this host"), "", rep.Host.Hostname, "", ""})
	}
	ha := rep.HostAgent
	switch {
	case !ha.Installed:
		rows = append(rows, []string{pal.Badge(ui.Red, "MISSING"), "SAP Host Agent", "", rep.Host.Hostname, "", ""})
	case ha.Running:
		rows = append(rows, []string{pal.Badge(ui.Green, "RUNNING"), "SAP Host Agent", "", rep.Host.Hostname, "Kernel " + ha.Version.String(), ""})
	default:
		rows = append(rows, []string{pal.Badge(ui.Red, "STOPPED"), "SAP Host Agent", "", rep.Host.Hostname, "Kernel " + ha.Version.String(), ""})
	}
	lines := ui.Table("", rows)
	if sl := ServiceLines(rep, pal); len(sl) > 0 {
		lines = append(lines, "")
		lines = append(lines, sl...)
	}
	if dl := DiskLines(rep.Disk, pal); len(dl) > 0 {
		lines = append(lines, "")
		lines = append(lines, dl...)
	}
	return lines
}

// DiskLines renders the disk part of the home screen: each SAP root as a
// one-level tree with the free-space light on the root line.
func DiskLines(d status.Disk, pal ui.Palette) []string {
	if len(d.Roots) == 0 {
		return nil
	}
	lines := []string{pal.Header("DISK")}
	if len(d.Trees) == 0 {
		for _, fs := range d.Filesystems {
			lines = append(lines, fmt.Sprintf("%s  %s %s free of %s (%d%% used, %s)", pal.Paint(ui.Bold, fs.Path),
				pal.Light(freeColour(fs)), disk.Human(fs.AvailKB), disk.Human(fs.SizeKB), fs.UsePct, fs.Mount))
		}
		return lines
	}
	for _, tree := range d.Trees {
		lines = append(lines, diskTreeLines(d, tree, pal, 6, 1)...)
	}
	return lines
}

// diskRoots returns the SAP trees to measure (the demo root in demo mode).
func diskRoots() []string {
	if demoRoot != "" {
		return []string{demoRoot}
	}
	return status.DefaultDiskRoots
}

// menuOps are the operations listed in the interactive menu.
func menuOps() []Op {
	var out []Op
	for _, op := range Ops {
		if op.Menu {
			out = append(out, op)
		}
	}
	return out
}

func menuChoice(s string) (Op, bool) {
	if n, err := strconv.Atoi(s); err == nil {
		m := menuOps()
		if n >= 1 && n <= len(m) {
			return m[n-1], true
		}
		return Op{}, false
	}
	if op, ok := FindOp(strings.ToLower(s)); ok {
		return op, true
	}
	for _, op := range Ops {
		if strings.EqualFold(op.Name, s) {
			return op, true
		}
	}
	return Op{}, false
}
