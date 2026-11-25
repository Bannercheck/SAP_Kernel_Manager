package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/discovery"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/status"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// ServicesOp implements SAP Services: the SAP Host Agent and sapstartsrv of
// every local instance with its profile (pf=), and starting what is down.
// sapcontrol only works once sapstartsrv runs with the instance profile, so
// this is the screen to use when SAP Status shows "sapstartsrv not running".
func ServicesOp(args []string) int {
	fs := flag.NewFlagSet("services", flag.ContinueOnError)
	sid := fs.String("sid", "", "only this SAP system")
	start := fs.String("start", "", "non-interactive: hostagent | sapstartsrv | all")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	pal := currentPalette()
	rep := collectStatus(ctx, status.Options{SID: *sid, Timeout: 30 * time.Second})
	down, haDown := servicesScreen(rep)
	if len(down) == 0 && !haDown {
		fmt.Fprintf(stdout, "  %s SAP Host Agent and every sapstartsrv are running\n", pal.Check())
		return ExitOK
	}
	doHA, doSrv := false, false
	if *start != "" {
		doHA = haDown && (*start == "hostagent" || *start == "all")
		doSrv = len(down) > 0 && (*start == "sapstartsrv" || *start == "all")
	} else {
		var opts []choice
		if haDown {
			label := "Start SAP Host Agent"
			if rep.HostAgent.Running {
				label = "Restart SAP Host Agent"
			}
			opts = append(opts, choice{"H", label, []string{"hostagent", "host"}})
		}
		if len(down) > 0 {
			opts = append(opts, choice{"S", fmt.Sprintf("Start sapstartsrv (%d instance(s))", len(down)), []string{"sapstartsrv", "start"}})
		}
		if haDown && len(down) > 0 {
			opts = append(opts, choice{"A", "Start all", []string{"all", "hepsi"}})
		}
		switch choose("", append(opts, mainMenu)...) {
		case "H":
			doHA = true
		case "S":
			doSrv = true
		case "A":
			doHA, doSrv = true, true
		default:
			return ExitOK
		}
	}
	fmt.Fprintln(stdout)
	n := len(down)
	if !doSrv {
		n = 0
	}
	if doHA {
		n++
	}
	i := 0
	var failed error
	if doHA {
		i++
		if err := ops.StartHostAgent(ctx, hostEnv(), i, n); err != nil {
			failed = err
		}
	}
	if doSrv {
		for _, sid := range sidsOf(down) {
			t, _, err := resolveTarget(ctx, sid)
			if err != nil {
				failed = fmt.Errorf("resolve system %s: %w", sid, err)
				break
			}
			var list []ops.ServiceInstance
			for _, d := range down {
				if d.sid != sid {
					continue
				}
				si := ops.ServiceInstance{Nr: d.in.Nr, Name: d.in.Name, Profile: d.in.Profile, ExeDir: d.in.DirExecutable}
				for _, ti := range t.Instances { // discovery read the sapservices line itself: prefer it
					if ti.Nr == d.in.Nr {
						if ti.Profile != "" {
							si.Profile = ti.Profile
						}
						if ti.ExeDir != "" {
							si.ExeDir = ti.ExeDir
						}
					}
				}
				list = append(list, si)
			}
			if err := ops.StartSapstartsrv(ctx, newEnv(t), list, i+1, n); err != nil {
				failed = err
				break
			}
			i += len(list)
		}
	}
	fmt.Fprintln(stdout)
	rep = collectStatus(ctx, status.Options{SID: *sid, Timeout: 30 * time.Second})
	down, haDown = servicesScreen(rep)
	if failed != nil {
		return fail(failed)
	}
	if len(down) == 0 && !haDown {
		fmt.Fprintf(stdout, "  %s SAP Host Agent and every sapstartsrv are running · sapcontrol works again\n", pal.Check())
	}
	return ExitOK
}

// downInstance is a local instance whose sapstartsrv does not answer.
type downInstance struct {
	sid string
	in  status.Instance
}

// servicesScreen prints the SAP Host Agent line and the sapstartsrv table and
// returns what is down.
func servicesScreen(rep *status.Report) (down []downInstance, haDown bool) {
	pal := currentPalette()
	ha := rep.HostAgent
	switch {
	case !ha.Installed:
		fmt.Fprintf(stdout, "  SAP Host Agent   %s  %s\n", hostAgentBadge(pal, ha), ha.Error)
	case ha.Healthy():
		fmt.Fprintf(stdout, "  SAP Host Agent   %s  %s · %s\n", hostAgentBadge(pal, ha), ha.Path, ha.Version.String())
	default:
		haDown = true
		fmt.Fprintf(stdout, "  SAP Host Agent   %s  %s · %s · start: saphostexec -restart (root)\n", hostAgentBadge(pal, ha), ha.Path, ha.Version.String())
	}
	if ha.Installed {
		fmt.Fprintf(stdout, "  %s\n", hostAgentComponents(pal, ha))
	}
	lines, downList := serviceTable(rep, pal, false)
	if len(lines) == 0 {
		fmt.Fprintf(stdout, "  %s\n", pal.Paint(ui.Dim, "no local SAP instances found"))
		return nil, haDown
	}
	fmt.Fprintf(stdout, "\n  %s  %s\n", pal.Header("SAPSTARTSRV"), pal.Paint(ui.Dim, "one per instance · sapcontrol only works while it runs with the instance profile (pf=)"))
	for _, l := range lines {
		fmt.Fprintln(stdout, "  "+l)
	}
	fmt.Fprintln(stdout)
	return downList, haDown
}

// ServiceLines is the home-screen block: every local instance with the
// state of SAP itself, of its sapstartsrv and its profile, and a hint that
// names what is down.
func ServiceLines(rep *status.Report, pal ui.Palette) []string {
	lines, down := serviceTable(rep, pal, true)
	if len(lines) == 0 {
		return nil
	}
	var parts []string
	if rep.HostAgent.Installed && !rep.HostAgent.Healthy() {
		parts = append(parts, "SAP Host Agent")
	}
	for _, d := range down {
		name := d.in.Name
		if name == "" {
			name = d.in.Nr
		}
		parts = append(parts, "sapstartsrv of "+d.sid+" "+name)
	}
	hint := pal.Paint(ui.Dim, "SAP = the instance's processes · SAPSTARTSRV = its service (sapcontrol), started with pf=")
	if len(parts) > 0 {
		verb := " is down"
		if len(parts) > 1 {
			verb = " are down"
		}
		hint = pal.Paint(ui.Yellow, strings.Join(parts, ", ")+verb+" → 5) SAP Stop / Start → F starts it")
	}
	return append([]string{pal.Header("SERVICES") + "  " + hint}, lines...)
}

// sapCell is the state of an instance's processes; without its sapstartsrv
// nothing can be known about them.
func sapCell(pal ui.Palette, status string, srvUp bool) string {
	if !srvUp {
		return pal.Light(ui.Dim) + " unknown"
	}
	switch status {
	case "GREEN":
		return pal.Light(ui.Green) + " running"
	case "GRAY", "":
		return pal.Light(ui.Red) + " stopped"
	}
	return pal.Light(ui.Yellow) + " partial"
}

// srvCell is the state of the instance's sapstartsrv: "up" or "down", so it
// never reads like the state of SAP itself.
func srvCell(pal ui.Palette, up bool) string {
	if up {
		return pal.Light(ui.Green) + " up"
	}
	return pal.Light(ui.Red) + " " + pal.Paint(ui.Red, "down")
}

// stoppedNote explains a stopped instance whose sapstartsrv is up.
const stoppedNote = "sapstartsrv stays up after a stop on purpose: sapcontrol keeps answering and 5) S starts SAP again without pf="

func serviceTable(rep *status.Report, pal ui.Palette, withSID bool) ([]string, []downInstance) {
	rows := [][]string{pal.Headers("INSTANCE", "SAP", "SAPSTARTSRV", "PROFILE (pf=)")}
	var down []downInstance
	stoppedUp := false
	for _, sys := range rep.Systems {
		for _, in := range sys.Instances {
			if !in.Local {
				continue
			}
			name := in.Name
			if name == "" {
				name = in.Nr
			}
			name = sys.SID + " " + name
			up := in.Sapstartsrv == "running"
			srv := srvCell(pal, up)
			if !up {
				down = append(down, downInstance{sys.SID, in})
				if in.Sapstartsrv != "not running" {
					srv = pal.Light(ui.Yellow) + " " + in.Sapstartsrv
				}
			}
			if up && (in.Status == "GRAY" || in.Status == "") {
				stoppedUp = true
			}
			rows = append(rows, []string{name, sapCell(pal, in.Status, up), srv, orDash(in.Profile)})
		}
	}
	if len(rows) == 1 {
		return nil, nil
	}
	lines := ui.Table("", rows)
	if stoppedUp {
		lines = append(lines, pal.Paint(ui.Dim, stoppedNote))
	}
	return lines, down
}

// sidsOf lists the systems of the down instances, in order, once each.
func sidsOf(down []downInstance) []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range down {
		if !seen[d.sid] {
			seen[d.sid] = true
			out = append(out, d.sid)
		}
	}
	return out
}

// hostAgentBadge is RUNNING (all three components), PARTIAL (saphostexec
// runs but sapstartsrv or saposcol does not), STOPPED or MISSING.
func hostAgentBadge(pal ui.Palette, ha discovery.HostAgent) string {
	switch {
	case !ha.Installed:
		return pal.Badge(ui.Red, "MISSING")
	case ha.Healthy():
		return pal.Badge(ui.Green, "RUNNING")
	case ha.Running:
		return pal.Badge(ui.Yellow, "PARTIAL")
	}
	return pal.Badge(ui.Red, "STOPPED")
}

// hostAgentComponents renders "● saphostexec running (pid = 4242)   ● sapstartsrv …   ○ saposcol not running".
func hostAgentComponents(pal ui.Palette, ha discovery.HostAgent) string {
	var parts []string
	for _, c := range ha.Components() {
		light := pal.Light(ui.Green)
		detail := c.Detail
		if !c.Running {
			light = pal.Light(ui.Red)
			detail = pal.Paint(ui.Red, detail)
		}
		parts = append(parts, light+" "+c.Name+" "+detail)
	}
	return strings.Join(parts, "   ")
}

// hostEnv is an operation environment without a target system.
func hostEnv() *ops.Env {
	return &ops.Env{R: runner, P: platformNow(), Pr: progress{w: stdout, pal: currentPalette()}, IsRoot: isRoot, User: currentUser()}
}
