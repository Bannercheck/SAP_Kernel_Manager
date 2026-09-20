// Package cli implements the skm sub-commands. It only parses flags and
// renders results; all SAP logic lives in internal/sap.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/platform"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/status"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/version"
)

// Exit codes (see docs/ARCHITECTURE.md §5.9).
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// Usage prints the top-level help.
func Usage(w io.Writer) {
	fmt.Fprint(w, `skm — SAP Kernel Manager

Usage:
  skm status  [--sid SID] [--output table|json] [--timeout 30s]   show host, kernel and instance state
  skm version                                                     show build information
  skm help                                                        show this help
`)
}

// Version implements `skm version`.
func Version(_ []string) int {
	fmt.Println(version.String())
	return ExitOK
}

// Status implements `skm status`.
func Status(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	sid := fs.String("sid", "", "only show this SAP system")
	output := fs.String("output", "table", "output format: table | json")
	timeout := fs.Duration("timeout", 30*time.Second, "timeout per sapcontrol call")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *output != "table" && *output != "json" {
		fmt.Fprintf(os.Stderr, "skm status: invalid --output %q (table|json)\n", *output)
		return ExitUsage
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	rep := status.Collect(ctx, exec.NewReal(), platform.Current(), status.Options{SID: *sid, Timeout: *timeout})

	if *output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintln(os.Stderr, "skm status:", err)
			return ExitError
		}
	} else {
		RenderStatus(os.Stdout, rep)
	}
	if len(rep.Systems) == 0 {
		return ExitError
	}
	return ExitOK
}
