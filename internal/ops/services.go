package ops

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/discovery"
)

// ServiceInstance is one local instance whose sapstartsrv is to be started.
type ServiceInstance struct {
	Nr, Name string
	Profile  string // instance profile, the pf= of /usr/sap/sapservices
	ExeDir   string // instance executable directory holding sapstartsrv
}

// StartSapstartsrv starts sapstartsrv for the given instances of e.T, as
// numbered steps first.. of n. First choice is what SAP documents:
// sapcontrol -nr NR -function StartService SID (as <sid>adm when root).
// When that fails or nothing answers afterwards, the service is started the
// way /usr/sap/sapservices does it at boot:
//
//	<exe>/sapstartsrv pf=<profile> -D [-u <sid>adm]
//
// with the instance's library path, which needs no working sapcontrol at all.
func StartSapstartsrv(ctx context.Context, e *Env, insts []ServiceInstance, first, n int) error {
	for i, in := range insts {
		in := in
		if err := e.step(first+i, n, fmt.Sprintf("Start sapstartsrv %s (%s)", in.Name, in.Nr), func() (string, error) {
			c := e.T.Client(e.R, in.Nr, 2*time.Minute)
			c.RunAs = e.asAdm()
			serr := c.StartService(ctx, e.T.SID)
			if serr == nil {
				if serr = waitForSapstartsrv(ctx, e, in.Nr); serr == nil {
					return "sapcontrol StartService · answers", nil
				}
			}
			if in.Profile == "" {
				return "", fmt.Errorf("StartService: %w (no profile known, cannot start sapstartsrv directly)", serr)
			}
			cmd := exec.Cmd{Path: filepath.Join(in.ExeDir, "sapstartsrv"), Args: []string{"pf=" + in.Profile, "-D"}, Timeout: 2 * time.Minute,
				Env: []string{e.P.LibPathVar() + "=" + in.ExeDir}}
			if e.IsRoot {
				cmd.Args = append(cmd.Args, "-u", e.T.SIDAdm)
			}
			if _, rerr := e.run(ctx, cmd); rerr != nil {
				return "", fmt.Errorf("StartService: %v · sapstartsrv pf=%s: %w", serr, in.Profile, rerr)
			}
			if werr := waitForSapstartsrv(ctx, e, in.Nr); werr != nil {
				return "", fmt.Errorf("sapstartsrv pf=%s started but %w", in.Profile, werr)
			}
			return "StartService failed (" + lastLines(serr.Error(), 1) + ") → sapstartsrv pf=" + in.Profile + " -D · answers", nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// StartHostAgent starts the SAP Host Agent as step i of n: saphostexec
// -restart (which also starts a stopped agent and its saposcol), then
// hostexecstart -start as a fallback, verified with saphostexec -status
// until saphostexec, sapstartsrv and saposcol all run. Both need root: the
// agent runs as sapadm and only root may start it.
func StartHostAgent(ctx context.Context, e *Env, i, n int) error {
	return e.step(i, n, "Start SAP Host Agent", func() (string, error) {
		bin, err := discovery.HostctrlTool(e.R, e.P, "saphostexec")
		if err != nil {
			return "", fmt.Errorf("SAP Host Agent not found in %s: %w", e.P.HostctrlExeDir(), err)
		}
		if !e.IsRoot {
			return "", fmt.Errorf("needs root: run  sudo %s -restart", bin)
		}
		var attempts []string
		if _, rerr := e.run(ctx, exec.Cmd{Path: bin, Args: []string{"-restart"}, Timeout: 3 * time.Minute}); rerr != nil {
			attempts = append(attempts, rerr.Error())
		} else if hostAgentUp(ctx, e) {
			return "saphostexec -restart · saphostexec, sapstartsrv, saposcol running", nil
		}
		if start, serr := discovery.HostctrlTool(e.R, e.P, "hostexecstart"); serr == nil {
			if _, rerr := e.run(ctx, exec.Cmd{Path: start, Args: []string{"-start"}, Timeout: 3 * time.Minute}); rerr != nil {
				attempts = append(attempts, rerr.Error())
			} else if hostAgentUp(ctx, e) {
				return "hostexecstart -start · running", nil
			}
		}
		if len(attempts) == 0 {
			attempts = append(attempts, "saphostexec -status does not report it running")
		}
		return "", fmt.Errorf("SAP Host Agent not fully up (%s): %s", componentSummary(ctx, e), lastLines(fmt.Sprint(attempts), 2))
	})
}

// componentSummary is "saphostexec running, sapstartsrv running, saposcol not running".
func componentSummary(ctx context.Context, e *Env) string {
	var parts []string
	for _, c := range discovery.CheckHostAgent(ctx, e.R, e.P).Components() {
		state := "not running"
		if c.Running {
			state = "running"
		}
		parts = append(parts, c.Name+" "+state)
	}
	return strings.Join(parts, ", ")
}

// hostAgentUp polls saphostexec -status for up to a minute.
func hostAgentUp(ctx context.Context, e *Env) bool {
	deadline := e.now().Add(time.Minute)
	for {
		if discovery.CheckHostAgent(ctx, e.R, e.P).Healthy() {
			return true
		}
		if !e.now().Before(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(3 * time.Second):
		}
	}
}
