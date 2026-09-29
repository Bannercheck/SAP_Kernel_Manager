package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/discovery"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/sapcontrol"
)

const (
	defaultStopTimeout  = 10 * time.Minute
	defaultStartTimeout = 15 * time.Minute
	waitDelay           = 5 * time.Second
	waitReport          = 30 * time.Second
)

// InstanceState is what SAP Stop/Start need to know per local instance.
type InstanceState struct {
	Nr, Name    string
	Sapstartsrv bool   // answers sapcontrol
	Status      string // GREEN, YELLOW, RED, GRAY or "" when sapstartsrv is down
}

// Probe asks every local instance for its state.
func Probe(ctx context.Context, e *Env) []InstanceState {
	var out []InstanceState
	for _, in := range e.T.Instances {
		st := InstanceState{Nr: in.Nr, Name: in.Name}
		procs, err := e.T.Client(e.R, in.Nr, 30*time.Second).GetProcessList(ctx)
		if err == nil {
			st.Sapstartsrv = true
			var s []string
			for _, p := range procs {
				s = append(s, p.DispStatus)
			}
			st.Status = sapcontrol.Aggregate(s)
		}
		out = append(out, st)
	}
	return out
}

// IsStopped reports whether no local instance has running processes.
func IsStopped(states []InstanceState) bool {
	for _, st := range states {
		if st.Sapstartsrv && st.Status != "GRAY" {
			return false
		}
	}
	return true
}

func (e *Env) stopTimeout() time.Duration {
	if e.StopTimeout > 0 {
		return e.StopTimeout
	}
	return defaultStopTimeout
}

func (e *Env) startTimeout() time.Duration {
	if e.StartTimeout > 0 {
		return e.StartTimeout
	}
	return defaultStartTimeout
}

// Stop stops the whole SAP system and then every local sapstartsrv:
// StopSystem ALL → wait per instance → StopService per instance.
func Stop(ctx context.Context, e *Env) error {
	insts := e.T.Instances
	n := 1 + 2*len(insts)
	var ref *sapcontrol.Client
	for _, st := range Probe(ctx, e) {
		if st.Sapstartsrv {
			ref = e.T.Client(e.R, st.Nr, 2*time.Minute)
			break
		}
	}
	if err := e.step(1, n, "StopSystem ALL", func() (string, error) {
		if ref == nil {
			return "no sapstartsrv answers: system already down", nil
		}
		return "instance " + ref.Nr, ref.StopSystem(ctx)
	}); err != nil {
		return err
	}
	i := 1
	for _, in := range insts {
		i++
		if err := e.step(i, n, fmt.Sprintf("Wait until %s (%s) is stopped", in.Name, in.Nr), func() (string, error) {
			return waitProcesses(ctx, e, in.Name, in.Nr, "GRAY", e.stopTimeout())
		}); err != nil {
			return err
		}
	}
	for _, in := range insts {
		i++
		if err := e.step(i, n, fmt.Sprintf("StopService %s (%s)", in.Name, in.Nr), func() (string, error) {
			err := e.T.Client(e.R, in.Nr, time.Minute).StopService(ctx)
			if sapcontrol.IsConnRefused(err) {
				return "already stopped", nil
			}
			return "sapstartsrv stopped", err
		}); err != nil {
			return err
		}
	}
	return nil
}

// Start starts every local sapstartsrv as <sid>adm, then the whole system:
// StartService per instance → StartSystem ALL → wait per instance.
func Start(ctx context.Context, e *Env) error {
	insts := e.T.Instances
	n := 1 + 2*len(insts)
	for _, s := range KernelSync(e) { // an instance directory at another level than the central kernel will not start
		if s.Checked > 0 && !s.InSync() {
			e.Pr.Info(fmt.Sprintf("! %s is not at the central kernel level (%s differ): run 6) Kernel Update so every directory is extracted", s.Dir, strings.Join(s.Differs, ", ")))
		}
	}
	i := 0
	for _, in := range insts {
		i++
		if err := e.step(i, n, fmt.Sprintf("StartService %s (%s)", in.Name, in.Nr), func() (string, error) {
			c := e.T.Client(e.R, in.Nr, 2*time.Minute)
			c.RunAs = e.asAdm()
			if err := c.StartService(ctx, e.T.SID); err != nil {
				return "", err
			}
			return "sapstartsrv up", waitForSapstartsrv(ctx, e, in.Nr)
		}); err != nil {
			return err
		}
	}
	for _, r := range RemoteInstances(ctx, e) { // StartSystem ALL only reaches a running sapstartsrv
		c := e.T.Client(e.R, r.Nr, 15*time.Second)
		c.Host = r.Host
		if _, err := c.GetProcessList(ctx); sapcontrol.IsConnRefused(err) {
			e.Pr.Info(fmt.Sprintf("! %s: sapstartsrv not running there, StartSystem ALL cannot start that instance — on %s run KernelMan 5 → F (or sapcontrol -nr %s -function StartService %s)", r.Label(), r.Host, r.Nr, e.T.SID))
		}
	}
	i++
	if err := e.step(i, n, "StartSystem ALL", func() (string, error) {
		c := e.T.Client(e.R, insts[0].Nr, 2*time.Minute)
		return "instance " + c.Nr, friendly(c.StartSystem(ctx), insts[0].Name, insts[0].Nr)
	}); err != nil {
		return err
	}
	for _, in := range insts {
		i++
		if err := e.step(i, n, fmt.Sprintf("Wait until %s (%s) is running", in.Name, in.Nr), func() (string, error) {
			return waitProcesses(ctx, e, in.Name, in.Nr, "GREEN", e.startTimeout())
		}); err != nil {
			return err
		}
	}
	return nil
}

// waitProcesses polls GetProcessList every few seconds until every process
// of the instance shows want (GREEN or GRAY) or timeout passes, reporting
// every half minute so a long stop or start never looks stuck. sapcontrol's
// own WaitforStopped/WaitforStarted would block silently for the whole time.
func waitProcesses(ctx context.Context, e *Env, name, nr, want string, timeout time.Duration) (string, error) {
	start := e.now()
	deadline := start.Add(timeout)
	lastReport := start
	for {
		procs, err := e.T.Client(e.R, nr, 30*time.Second).GetProcessList(ctx)
		switch {
		case sapcontrol.IsConnRefused(err) && want == "GRAY":
			return "sapstartsrv already down", nil
		case err != nil:
			return "", friendly(err, name, nr)
		}
		pending := 0
		var names []string
		for _, p := range procs {
			if p.DispStatus != want {
				pending++
				if len(names) < 3 {
					names = append(names, p.Name+" "+p.DispStatus)
				}
			}
		}
		if pending == 0 {
			return "all processes " + want, nil
		}
		now := e.now()
		if !now.Before(deadline) {
			return "", fmt.Errorf("%s (%s): %d process(es) still not %s after %s: %s", name, nr, pending, want, timeout.Round(time.Second), strings.Join(names, ", "))
		}
		if now.Sub(lastReport) >= waitReport {
			lastReport = now
			e.Pr.Info(fmt.Sprintf("  … %s: %d process(es) not yet %s (%s), waiting up to %s", now.Sub(start).Round(time.Second), pending, want, strings.Join(names, ", "), timeout.Round(time.Second)))
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(waitDelay):
		}
	}
}

// friendly turns sapcontrol's "NIECONN_REFUSED ... plugin_fopen" into words.
func friendly(err error, name, nr string) error {
	if sapcontrol.IsConnRefused(err) {
		return fmt.Errorf("sapstartsrv of %s (%s) is not running, sapcontrol cannot connect (plugin_fopen / connection refused): start it first — 5 → F", name, nr)
	}
	return err
}

// StartInstance starts one local instance: its sapstartsrv when needed,
// then Start and a wait. The rest of the system is left alone.
func StartInstance(ctx context.Context, e *Env, nr string) error {
	in, ok := e.instance(nr)
	if !ok {
		return fmt.Errorf("instance %s is not on this host", nr)
	}
	const n = 3
	if _, err := e.T.Client(e.R, nr, 30*time.Second).GetProcessList(ctx); err == nil {
		if err := e.step(1, n, fmt.Sprintf("sapstartsrv %s (%s)", in.Name, nr), func() (string, error) { return "already running", nil }); err != nil {
			return err
		}
	} else if err := StartSapstartsrv(ctx, e, []ServiceInstance{{Nr: nr, Name: in.Name, Profile: in.Profile, ExeDir: in.ExeDir}}, 1, n); err != nil {
		return err
	}
	if err := e.step(2, n, fmt.Sprintf("Start %s (%s)", in.Name, nr), func() (string, error) {
		return "sapcontrol Start", friendly(e.T.Client(e.R, nr, 2*time.Minute).Start(ctx), in.Name, nr)
	}); err != nil {
		return err
	}
	return e.step(3, n, fmt.Sprintf("Wait until %s (%s) is running", in.Name, nr), func() (string, error) {
		return waitProcesses(ctx, e, in.Name, nr, "GREEN", e.startTimeout())
	})
}

// StopInstance stops one local instance's processes (Stop, wait); its
// sapstartsrv keeps running so sapcontrol still answers.
func StopInstance(ctx context.Context, e *Env, nr string) error {
	in, ok := e.instance(nr)
	if !ok {
		return fmt.Errorf("instance %s is not on this host", nr)
	}
	const n = 2
	if err := e.step(1, n, fmt.Sprintf("Stop %s (%s)", in.Name, nr), func() (string, error) {
		return "sapcontrol Stop", friendly(e.T.Client(e.R, nr, 2*time.Minute).Stop(ctx), in.Name, nr)
	}); err != nil {
		return err
	}
	return e.step(2, n, fmt.Sprintf("Wait until %s (%s) is stopped", in.Name, nr), func() (string, error) {
		return waitProcesses(ctx, e, in.Name, nr, "GRAY", e.stopTimeout())
	})
}

func (e *Env) instance(nr string) (discovery.Instance, bool) {
	for _, in := range e.T.Instances {
		if in.Nr == nr {
			return in, true
		}
	}
	return discovery.Instance{}, false
}

// waitForSapstartsrv polls GetProcessList until sapstartsrv answers.
func waitForSapstartsrv(ctx context.Context, e *Env, nr string) error {
	deadline := e.now().Add(90 * time.Second)
	var last error
	for e.now().Before(deadline) {
		_, last = e.T.Client(e.R, nr, 20*time.Second).GetProcessList(ctx)
		if last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	if last == nil {
		last = errors.New("timeout")
	}
	return fmt.Errorf("sapstartsrv %s did not come up: %w", nr, last)
}
