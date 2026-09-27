// kernelman — SAP Kernel Manager command line entry point.
package main

import (
	"fmt"
	"os"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/cli"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	pal := ui.Detect(os.Stdout)
	args, demoMode := stripDemoFlag(args)
	if demoMode || os.Getenv(version.EnvPrefix+"DEMO") == "1" {
		if err := cli.EnableDemo(os.Getenv(version.EnvPrefix + "DEMO_ROOT")); err != nil {
			fmt.Fprintln(os.Stderr, version.AppName+":", err)
			return cli.ExitError
		}
	}
	if len(args) == 0 {
		if ui.IsTerminal(os.Stdin) || os.Getenv(version.EnvPrefix+"MENU") == "1" {
			return cli.Menu(os.Stdin, os.Stdout, pal)
		}
		cli.Usage(os.Stderr)
		return cli.ExitUsage
	}
	switch args[0] {
	case "help", "-h", "--help":
		cli.Usage(os.Stdout)
		return cli.ExitOK
	case "-v", "--version":
		return cli.Version(nil)
	case "menu":
		return cli.Menu(os.Stdin, os.Stdout, pal)
	}
	op, ok := cli.FindOp(args[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "%s: unknown command %q\n\n", version.AppName, args[0])
		cli.Usage(os.Stderr)
		return cli.ExitUsage
	}
	return cli.Dispatch(op, args[1:])
}

// stripDemoFlag removes `demo` / `--demo` from the arguments and reports it.
// `kernelman demo` opens the menu on the simulated host; `kernelman --demo status`
// runs one command against it.
func stripDemoFlag(args []string) ([]string, bool) {
	var out []string
	found := false
	for _, a := range args {
		if a == "demo" || a == "--demo" {
			found = true
			continue
		}
		out = append(out, a)
	}
	return out, found
}
