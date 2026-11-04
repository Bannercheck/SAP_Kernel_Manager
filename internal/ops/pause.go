package ops

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/sapcontrol"
)

// A running sapstartsrv keeps its executable and the libraries next to it
// open: SAPCAR and cp cannot overwrite them ("Text file busy" on Linux and
// AIX), and a library rewritten in place can crash the process. SAP Stop
// therefore leaves sapstartsrv running (it was started with the instance
// profile, pf=, and the operator would have to start it by hand again), and
// only Kernel Update and Rollback stop it, right before the files change,
// and start exactly those instances again with their profile right after.

// runningServices lists the local instances whose sapstartsrv answers,
// with the profile and exe directory needed to start it again. One whose
// profile cannot be found is left running and named in kept.
func runningServices(ctx context.Context, e *Env) (svc []ServiceInstance, kept []string) {
	for _, st := range Probe(ctx, e) {
		if !st.Sapstartsrv {
			continue
		}
		in, _ := e.instance(st.Nr)
		si := ServiceInstance{Nr: in.Nr, Name: in.Name, Profile: in.Profile, ExeDir: in.ExeDir}
		c := e.T.Client(e.R, in.Nr, 30*time.Second)
		if si.Profile == "" {
			si.Profile, _ = c.ParameterValue(ctx, "SAPPROFILE")
		}
		if si.ExeDir == "" {
			si.ExeDir, _ = c.ParameterValue(ctx, "DIR_EXECUTABLE")
		}
		if si.Profile == "" || si.ExeDir == "" {
			kept = append(kept, in.Name)
			continue
		}
		svc = append(svc, si)
	}
	return svc, kept
}

// pauseServices stops each sapstartsrv as steps first.. of n and waits
// until it no longer answers.
func pauseServices(ctx context.Context, e *Env, svc []ServiceInstance, kept []string, first, n int) error {
	for _, k := range kept {
		e.Pr.Info(fmt.Sprintf("! sapstartsrv %s keeps running: its profile is unknown, so KernelMan could not start it again — if a file is reported busy, stop it yourself", k))
	}
	for i, in := range svc {
		in := in
		if err := e.step(first+i, n, fmt.Sprintf("Stop sapstartsrv %s (%s) while its files are replaced", in.Name, in.Nr), func() (string, error) {
			c := e.T.Client(e.R, in.Nr, time.Minute)
			c.RunAs = e.asAdm()
			if err := c.StopService(ctx); err != nil && !sapcontrol.IsConnRefused(err) {
				return "", err
			}
			if err := waitServiceDown(ctx, e, in.Nr); err != nil {
				return "", err
			}
			return "started again with pf=" + filepath.Base(in.Profile) + " after the files", nil
		}); err != nil {
			servicesDown(e, svc[:i+1])
			return err
		}
	}
	return nil
}

// waitServiceDown polls until sapstartsrv of nr refuses connections.
func waitServiceDown(ctx context.Context, e *Env, nr string) error {
	deadline := e.now().Add(time.Minute)
	for {
		_, err := e.T.Client(e.R, nr, 20*time.Second).GetProcessList(ctx)
		if sapcontrol.IsConnRefused(err) {
			return nil
		}
		if !e.now().Before(deadline) {
			return fmt.Errorf("sapstartsrv %s still answers a minute after StopService", nr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// servicesDown tells the operator which sapstartsrv stays stopped after a
// failed kernel change and how it comes back (with its profile).
func servicesDown(e *Env, svc []ServiceInstance) {
	if len(svc) == 0 {
		return
	}
	var names []string
	for _, s := range svc {
		names = append(names, s.Name)
	}
	e.Pr.Info(fmt.Sprintf("! sapstartsrv of %s stays stopped while the kernel is inconsistent: 7) Kernel Rollback or 5) SAP Stop / Start → S starts it again with its profile", strings.Join(names, ", ")))
}
