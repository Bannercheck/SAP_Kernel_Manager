package cli

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// ControlOp implements the SAP Stop / Start screen: K stops, S starts.
func ControlOp(args []string) int {
	fs := flag.NewFlagSet("control", flag.ContinueOnError)
	sid := fs.String("sid", "", "SAP system")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	t, err := target(ctx, *sid)
	if err != nil {
		return fail(err)
	}
	e := newEnv(t)
	lights(ctx, e)
	switch choose("", choice{"S", "Start SAP", []string{"start"}}, choice{"K", "Stop SAP (Kapat)", []string{"stop", "kapat"}}, mainMenu) {
	case "S":
		return runStart(ctx, e)
	case "K":
		return runStop(ctx, e)
	}
	return ExitOK
}

// StopOp / StartOp are the non-interactive command line forms.
func StopOp(args []string) int  { return controlCmd(args, "stop") }
func StartOp(args []string) int { return controlCmd(args, "start") }

func controlCmd(args []string, what string) int {
	fs := flag.NewFlagSet(what, flag.ContinueOnError)
	sid := fs.String("sid", "", "SAP system")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	t, err := target(ctx, *sid)
	if err != nil {
		return fail(err)
	}
	if what == "stop" {
		return runStop(ctx, newEnv(t))
	}
	return runStart(ctx, newEnv(t))
}

func runStop(ctx context.Context, e *ops.Env) int {
	pal := currentPalette()
	fmt.Fprintf(stdout, "\n  %s\n", pal.Paint(ui.Bold, "SAP Stop "+e.T.SID))
	if err := ops.Stop(ctx, e); err != nil {
		lights(ctx, e)
		return fail(err)
	}
	fmt.Fprintf(stdout, "  %s system %s stopped\n", pal.Check(), e.T.SID)
	lights(ctx, e)
	return ExitOK
}

func runStart(ctx context.Context, e *ops.Env) int {
	pal := currentPalette()
	fmt.Fprintf(stdout, "\n  %s\n", pal.Paint(ui.Bold, "SAP Start "+e.T.SID))
	if err := ops.Start(ctx, e); err != nil {
		lights(ctx, e)
		return fail(err)
	}
	fmt.Fprintf(stdout, "  %s system %s started\n", pal.Check(), e.T.SID)
	lights(ctx, e)
	return ExitOK
}
