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
		code := Dispatch(op, nil)
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
	fmt.Fprintf(w, "%s   %s\n\n", pal.Paint(ui.Bold, version.AppName+" — "+version.ProductName), pal.Paint(ui.Dim, "host "+host))
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
		state := ""
		if op.Run == nil {
			state = pal.Paint(ui.Dim, fmt.Sprintf("   (planned: step %d)", op.Step))
		}
		fmt.Fprintf(w, "  %2d) %s %-16s %s%s\n", i+1, mark, op.Name, op.Summary, state)
	}
	fmt.Fprintln(w, "   q)   Quit")
	fmt.Fprintln(w)
}

// liveSummary probes the host and renders one traffic light per SAP system.
func liveSummary(pal ui.Palette) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rep := status.Collect(ctx, exec.NewReal(), platform.Current(), status.Options{Timeout: 15 * time.Second})
	return SummaryLines(rep, pal)
}

// SummaryLines renders the per-system traffic lights used above the menu.
func SummaryLines(rep *status.Report, pal ui.Palette) []string {
	if len(rep.Systems) == 0 {
		return []string{pal.Light(ui.Dim) + " " + pal.Paint(ui.Dim, "no SAP instances found on this host")}
	}
	var lines []string
	for _, sys := range rep.Systems {
		running, local := 0, 0
		for _, in := range sys.Instances {
			if in.Local {
				local++
				if in.Sapstartsrv == "running" {
					running++
				}
			}
		}
		lines = append(lines, fmt.Sprintf("%s %-4s %-12s %-7s kernel %-14s sapstartsrv %d/%d",
			statusLight(pal, sys.Status), sys.SID, sys.Type, statusWord(sys.Status), sys.Kernel, running, local))
	}
	return lines
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
