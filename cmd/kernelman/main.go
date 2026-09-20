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
