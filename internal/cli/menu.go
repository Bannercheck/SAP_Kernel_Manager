package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// IsTerminal reports whether f is an interactive terminal (no x/term needed).
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Menu runs the interactive operation menu until the user quits.
// It reads choices from in so it can be driven by tests and transcripts.
func Menu(in io.Reader, out io.Writer) int {
	rd := bufio.NewReader(in)
	for {
		printMenu(out)
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
			fmt.Fprintf(out, "\n! unknown choice %q\n\n", choice)
			continue
		}
		fmt.Fprintf(out, "\n=== %s ===\n", op.Name)
		code := Dispatch(op, nil)
		fmt.Fprintf(out, "\n--- %s finished (exit code %d). Press Enter to continue.\n", op.Name, code)
		if _, err := rd.ReadString('\n'); err != nil {
			return code
		}
	}
}

func printMenu(w io.Writer) {
	fmt.Fprintln(w, "skm — SAP Kernel Manager")
	fmt.Fprintln(w)
	for i, op := range Ops {
		state := ""
		if op.Run == nil {
			state = fmt.Sprintf("   (planned: step %d)", op.Step)
		}
		fmt.Fprintf(w, "  %2d) %-16s %s%s\n", i+1, op.Name, op.Summary, state)
	}
	fmt.Fprintln(w, "   q) Quit")
	fmt.Fprintln(w)
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
