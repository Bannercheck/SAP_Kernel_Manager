package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/discovery"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/sapcontrol"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// ControlOp implements the SAP Stop / Start screen: the whole system (S/K),
// one instance (I), the SAP Host Agent (H) and the sapstartsrv processes (V)
// with their profiles — everything that must run before sapcontrol works.
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
	states, ha := controlScreen(ctx, e)
	var down []ops.ServiceInstance
	for _, st := range states {
		if !st.Sapstartsrv {
			for _, in := range t.Instances {
				if in.Nr == st.Nr {
					down = append(down, ops.ServiceInstance{Nr: in.Nr, Name: in.Name, Profile: in.Profile, ExeDir: in.ExeDir})
				}
			}
		}
	}
	haDown := ha.Installed && !ha.Healthy()
	opts := []choice{{"S", "Start SAP", []string{"start"}}, {"K", "Stop SAP (Kapat)", []string{"stop", "kapat"}}, {"I", "One instance", []string{"instance"}}}
	if haDown || len(down) > 0 { // one key for everything that must run before sapcontrol works
		var what []string
		if haDown {
			what = append(what, "Host Agent")
		}
		if len(down) > 0 {
			what = append(what, fmt.Sprintf("%d sapstartsrv", len(down)))
		}
		opts = append(opts, choice{"F", "Start stopped services (" + strings.Join(what, ", ") + ")", []string{"fix", "services"}})
	}
	switch choose("", append(opts, mainMenu)...) {
	case "S":
		return runStart(ctx, e)
	case "K":
		return runStop(ctx, e)
	case "I":
		return instanceMenu(ctx, e, states)
	case "F":
		return runServices(ctx, e, haDown, down)
	}
	return ExitOK
}

// controlScreen shows the system lights, the SAP Host Agent with its three
// processes and every local sapstartsrv with its profile (pf=).
func controlScreen(ctx context.Context, e *ops.Env) ([]ops.InstanceState, discovery.HostAgent) {
	pal := currentPalette()
	states := ops.Probe(ctx, e)
	lightsFor(states)
	ha := discovery.CheckHostAgent(ctx, runner, platformNow())
	switch {
	case !ha.Installed:
		fmt.Fprintf(stdout, "  SAP Host Agent   %s  %s\n", hostAgentBadge(pal, ha), ha.Error)
	default:
		fmt.Fprintf(stdout, "  SAP Host Agent   %s  %s\n  %s\n", hostAgentBadge(pal, ha), ha.Path, hostAgentComponents(pal, ha))
	}
	rows := [][]string{pal.Headers("INSTANCE", "SAPSTARTSRV", "PROFILE (pf=)")}
	for _, st := range states {
		state := pal.Light(ui.Green) + " running"
		if !st.Sapstartsrv {
			state = pal.Light(ui.Red) + " " + pal.Paint(ui.Red, "not running")
		}
		profile := "-"
		for _, in := range e.T.Instances {
			if in.Nr == st.Nr && in.Profile != "" {
				profile = in.Profile
			}
		}
		rows = append(rows, []string{st.Name + " (" + st.Nr + ")", state, profile})
	}
	fmt.Fprintf(stdout, "  %s  %s\n", pal.Header("SAPSTARTSRV"), pal.Paint(ui.Dim, "sapcontrol only works while it runs with the instance profile"))
	for _, l := range ui.Table("  ", rows) {
		fmt.Fprintln(stdout, l)
	}
	return states, ha
}

// runServices starts the SAP Host Agent and/or the given sapstartsrv processes, then shows the screen again.
func runServices(ctx context.Context, e *ops.Env, hostAgent bool, down []ops.ServiceInstance) int {
	pal := currentPalette()
	n := len(down)
	if hostAgent {
		n++
	}
	fmt.Fprintln(stdout)
	var failed error
	i := 0
	if hostAgent {
		i++
		if err := ops.StartHostAgent(ctx, hostEnv(), i, n); err != nil {
			failed = err
		}
	}
	if len(down) > 0 && failed == nil {
		if err := ops.StartSapstartsrv(ctx, e, down, i+1, n); err != nil {
			failed = err
		}
	}
	fmt.Fprintln(stdout)
	states, ha := controlScreen(ctx, e)
	if failed != nil {
		return fail(failed)
	}
	allUp := !ha.Installed || ha.Healthy()
	for _, st := range states {
		allUp = allUp && st.Sapstartsrv
	}
	if allUp {
		fmt.Fprintf(stdout, "  %s SAP Host Agent and every sapstartsrv are running · sapcontrol works again\n", pal.Check())
	}
	return ExitOK
}

// instanceMenu starts or stops one local instance.
func instanceMenu(ctx context.Context, e *ops.Env, states []ops.InstanceState) int {
	pal := currentPalette()
	var opts []choice
	for i, st := range states {
		opts = append(opts, choice{fmt.Sprint(i + 1), st.Name, nil})
	}
	k := choose("Instance", append(opts, mainMenu)...)
	if k == "M" {
		return ExitOK
	}
	var st ops.InstanceState
	for i := range states {
		if fmt.Sprint(i+1) == k {
			st = states[i]
		}
	}
	switch choose(st.Name+" ("+st.Nr+")", choice{"S", "Start", []string{"start"}}, choice{"K", "Stop (Kapat)", []string{"stop", "kapat"}}, mainMenu) {
	case "S":
		fmt.Fprintf(stdout, "\n  %s\n", pal.Paint(ui.Bold, "Start "+st.Name))
		if err := ops.StartInstance(ctx, e, st.Nr); err != nil {
			lights(ctx, e)
			return fail(err)
		}
		fmt.Fprintf(stdout, "  %s instance %s started\n", pal.Check(), st.Name)
	case "K":
		fmt.Fprintf(stdout, "\n  %s\n", pal.Paint(ui.Bold, "Stop "+st.Name))
		if err := ops.StopInstance(ctx, e, st.Nr); err != nil {
			lights(ctx, e)
			return fail(err)
		}
		fmt.Fprintf(stdout, "  %s instance %s stopped\n", pal.Check(), st.Name)
	default:
		return ExitOK
	}
	lights(ctx, e)
	return ExitOK
}

// StopOp / StartOp are the non-interactive command line forms.
func StopOp(args []string) int  { return controlCmd(args, "stop") }
func StartOp(args []string) int { return controlCmd(args, "start") }

func controlCmd(args []string, what string) int {
	fs := flag.NewFlagSet(what, flag.ContinueOnError)
	sid := fs.String("sid", "", "SAP system")
	nr := fs.String("nr", "", "only this instance number")
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
	if *nr != "" {
		if what == "stop" {
			err = ops.StopInstance(ctx, e, *nr)
		} else {
			err = ops.StartInstance(ctx, e, *nr)
		}
		lights(ctx, e)
		if err != nil {
			return fail(err)
		}
		return ExitOK
	}
	if what == "stop" {
		return runStop(ctx, e)
	}
	return runStart(ctx, e)
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

// lightsFor prints the state badge and one light per local instance.
func lightsFor(states []ops.InstanceState) bool {
	pal := currentPalette()
	var parts []string
	var agg []string
	for _, st := range states {
		switch {
		case !st.Sapstartsrv:
			parts = append(parts, fmt.Sprintf("%s %s (sapstartsrv down)", pal.Light(ui.Red), st.Name))
			agg = append(agg, "GRAY")
		default:
			parts = append(parts, fmt.Sprintf("%s %s %s", statusLight(pal, st.Status), st.Name, st.Status))
			agg = append(agg, st.Status)
		}
	}
	sys := sapcontrol.Aggregate(agg)
	fmt.Fprintf(stdout, "  %s   %s\n", pal.Badge(statusColour(sys), stateWord(sys)), strings.Join(parts, "   "))
	return ops.IsStopped(states)
}
