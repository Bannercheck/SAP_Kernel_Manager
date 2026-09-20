// skm — SAP Kernel Manager command line entry point.
package main

import (
	"fmt"
	"os"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/cli"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		if cli.IsTerminal(os.Stdin) || os.Getenv("SKM_MENU") == "1" {
			return cli.Menu(os.Stdin, os.Stdout)
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
		return cli.Menu(os.Stdin, os.Stdout)
	}
	op, ok := cli.FindOp(args[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "skm: unknown command %q\n\n", args[0])
		cli.Usage(os.Stderr)
		return cli.ExitUsage
	}
	return cli.Dispatch(op, args[1:])
}
