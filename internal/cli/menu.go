package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/platform"
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
	results := map[string]int{} // op ID → last exit code in this session
	summary := summaryFunc(pal)
	for {
		printMenu(out, pal, results, summary)
		fmt.Fprint(out, "Select an operation (number or name, q to quit): ")
		line, err := rd.ReadString('\n')
		choice := strings.TrimSpace(line)
		if err != nil && choice == "" {
			fmt.Fprintln(out)
			return ExitOK
		}
		fmt.Fprintln(out, choice)
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
		prevOut, prevPal := stdout, palette
		stdout, palette = out, &pal
		code := Dispatch(op, nil)
		stdout, palette = prevOut, prevPal
		results[op.ID] = code
		mark := pal.Check() + " " + pal.Paint(ui.Green, "done")
		if code != 0 {
			mark = pal.Cross() + " " + pal.Paint(ui.Red, fmt.Sprintf("failed (exit code %d)", code))
		}
		fmt.Fprintf(out, "\n--- %s: %s. Press Enter to continue.\n", op.Name, mark)
		if _, err := rd.ReadString('\n'); err != nil {
			return code
		}
		summary = summaryFunc(pal) // SAP Stop/Start change the lights
	}
}

func printMenu(w io.Writer, pal ui.Palette, results map[string]int, summary []string) {
	host, _ := os.Hostname()
	fmt.Fprintf(w, "%s   %s\n\n", pal.Paint(ui.Bold, version.DisplayName+" — "+version.ProductName), pal.Paint(ui.Dim, "host "+host))
	for _, l := range summary {
		fmt.Fprintln(w, "  "+l)
	}
	fmt.Fprintln(w)
	for i, op := range Ops {
		mark := " "
		if code, ran := results[op.ID]; ran {
			mark = pal.Check()
			if code != 0 {
				mark = pal.Cross()
			}
		}
		name := op.Name
		if op.Run == nil {
			name = pal.Paint(ui.Dim, name)
		}
		fmt.Fprintf(w, "  %2d) %s %s\n", i+1, mark, name)
	}
	fmt.Fprintln(w, "   q)   Quit")
	fmt.Fprintln(w, pal.Paint(ui.Dim, "\n  dim = not available yet · details: "+version.AppName+" help"))
	fmt.Fprintln(w)
}

// collectStatus gathers the live report; tests and transcripts replace it.
var collectStatus = liveCollect

func liveCollect(ctx context.Context, opts status.Options) *status.Report {
	return status.Collect(ctx, exec.NewReal(), platform.Current(), opts)
}

// liveSummary probes the host and renders the system overview above the menu.
func liveSummary(pal ui.Palette) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return SummaryLines(collectStatus(ctx, status.Options{Timeout: 15 * time.Second}), pal)
}

// SummaryLines renders the overview above the menu: one light per SAP
// system and per instance, the kernel level, and the SAP Host Agent.
// Words are left out on purpose; the light carries the state.
func SummaryLines(rep *status.Report, pal ui.Palette) []string {
	var rows [][]string
	for _, sys := range rep.Systems {
		var insts []string
		for _, in := range sys.Instances {
			name := in.Name
			if name == "" {
				name = in.Host + "/" + in.Nr
			}
			insts = append(insts, name+" "+statusLight(pal, in.Status))
		}
		rows = append(rows, []string{statusLight(pal, sys.Status), sys.SID, sys.Type,
			"kernel " + sys.Kernel.String(), strings.Join(insts, "  ")})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{pal.Light(ui.Dim), pal.Paint(ui.Dim, "no SAP instances found on this host"), "", "", ""})
	}
	ha := rep.HostAgent
	switch {
	case !ha.Installed:
		rows = append(rows, []string{pal.Light(ui.Red), "SAP Host Agent", pal.Paint(ui.Dim, "not installed"), "", ""})
	case ha.Running:
		rows = append(rows, []string{pal.Light(ui.Green), "SAP Host Agent", "", ha.Version.String(), ""})
	default:
		rows = append(rows, []string{pal.Light(ui.Red), "SAP Host Agent", "", ha.Version.String(), ""})
	}
	return ui.Table("", rows)
}

func menuChoice(s string) (Op, bool) {
	if n, err := strconv.Atoi(s); err == nil {
		if n >= 1 && n <= len(Ops) {
			return Ops[n-1], true
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
