// Package cli implements the kernelman sub-commands. It only parses flags and
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

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/status"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/version"
)

// stdout is where operations write; the menu redirects it to its own writer.
var stdout io.Writer = os.Stdout

// palette overrides auto-detection when set (the menu passes its own).
var palette *ui.Palette

func currentPalette() ui.Palette {
	if palette != nil {
		return *palette
	}
	return ui.Detect(os.Stdout)
}

// Exit codes (see docs/ARCHITECTURE.md §5.9).
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// Version implements `kernelman version`.
func Version(_ []string) int {
	fmt.Fprintln(stdout, version.String())
	return ExitOK
}

// Status implements `kernelman status`.
func Status(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	sid := fs.String("sid", "", "only show this SAP system")
	output := fs.String("output", "table", "output format: table | json")
	timeout := fs.Duration("timeout", 30*time.Second, "timeout per sapcontrol call")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *output != "table" && *output != "json" {
		fmt.Fprintf(os.Stderr, "%s status: invalid --output %q (table|json)\n", version.AppName, *output)
		return ExitUsage
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	rep := collectStatus(ctx, status.Options{SID: *sid, Timeout: *timeout})

	if *output == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintln(os.Stderr, version.AppName+" status:", err)
			return ExitError
		}
	} else {
		RenderStatus(stdout, rep, currentPalette())
	}
	if len(rep.Systems) == 0 {
		return ExitError
	}
	return ExitOK
}
