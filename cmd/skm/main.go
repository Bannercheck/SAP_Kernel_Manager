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
		cli.Usage(os.Stderr)
		return cli.ExitUsage
	}
	switch args[0] {
	case "status":
		return cli.Status(args[1:])
	case "version", "-v", "--version":
		return cli.Version(args[1:])
	case "help", "-h", "--help":
		cli.Usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "skm: unknown command %q\n\n", args[0])
		cli.Usage(os.Stderr)
		return cli.ExitUsage
	}
}
