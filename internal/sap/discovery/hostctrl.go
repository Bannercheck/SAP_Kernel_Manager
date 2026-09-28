package discovery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/platform"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/kernel"
)

// listInstRe matches: " Inst Info : ABC - 00 - sapci - 793, patch 200, changelist 2123456"
var listInstRe = regexp.MustCompile(
	`Inst Info\s*:\s*([A-Z][A-Z0-9]{2})\s*-\s*(\d{2})\s*-\s*(\S+)\s*-\s*(\d+),\s*patch\s*(\d+)(?:,\s*changelist\s*(\d+))?`)

// ParseListInstances parses `saphostctrl -function ListInstances` output.
func ParseListInstances(out string) []Instance {
	var res []Instance
	for _, m := range listInstRe.FindAllStringSubmatch(out, -1) {
		in := Instance{SID: m[1], Nr: m[2], Host: m[3], Source: "saphostctrl"}
		in.Release, _ = strconv.Atoi(m[4])
		in.Patch, _ = strconv.Atoi(m[5])
		in.Changelist, _ = strconv.Atoi(m[6])
		res = append(res, in)
	}
	return res
}

// HostctrlTool resolves a SAP Host Agent executable (saphostctrl, saphostexec).
func HostctrlTool(r exec.Runner, p platform.Platform, name string) (string, error) {
	bin := filepath.Join(p.HostctrlExeDir(), name+p.ExeSuffix())
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}
	return r.LookPath(name)
}

// ListInstances asks the SAP Host Agent for the instances on this host.
func ListInstances(ctx context.Context, r exec.Runner, p platform.Platform) ([]Instance, error) {
	bin, err := HostctrlTool(r, p, "saphostctrl")
	if err != nil {
		return nil, fmt.Errorf("saphostctrl: %w", err)
	}
	res, err := r.Run(ctx, exec.Cmd{Path: bin, Args: []string{"-function", "ListInstances"}})
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("saphostctrl ListInstances failed (exit %d): %s", res.ExitCode,
			strings.TrimSpace(res.Stdout+res.Stderr))
	}
	return ParseListInstances(res.Stdout), nil
}

// HostAgent describes the SAP Host Agent installation.
type HostAgent struct {
	Installed bool           `json:"installed"`
	Running   bool           `json:"running"`
	Path      string         `json:"path,omitempty"`
	Version   kernel.Version `json:"version"`
	Processes []string       `json:"processes,omitempty"` // lines of saphostexec -status
	Error     string         `json:"error,omitempty"`
}

// Component is one SAP Host Agent process as saphostexec -status lists it.
type Component struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Detail  string `json:"detail,omitempty"` // e.g. "running (pid = 4242)"
}

// hostAgentComponents are the processes a healthy SAP Host Agent runs.
var hostAgentComponents = []string{"saphostexec", "sapstartsrv", "saposcol"}

// Components reports saphostexec, sapstartsrv (the agent's own) and saposcol
// from the -status lines; one that is not listed counts as not running.
func (ha HostAgent) Components() []Component {
	var out []Component
	for _, name := range hostAgentComponents {
		c := Component{Name: name, Detail: "not running"}
		for _, l := range ha.Processes {
			f := strings.Fields(l)
			if len(f) < 2 || f[0] != name {
				continue
			}
			c.Detail = strings.TrimSpace(strings.TrimPrefix(l, name))
			c.Running = f[1] == "running"
		}
		out = append(out, c)
	}
	return out
}

// Healthy reports an installed agent with all three components running.
func (ha HostAgent) Healthy() bool {
	if !ha.Installed || !ha.Running {
		return false
	}
	for _, c := range ha.Components() {
		if !c.Running {
			return false
		}
	}
	return true
}

// CheckHostAgent inspects saphostexec (-status, -version).
func CheckHostAgent(ctx context.Context, r exec.Runner, p platform.Platform) HostAgent {
	ha := HostAgent{}
	bin, err := HostctrlTool(r, p, "saphostexec")
	if err != nil {
		ha.Error = "SAP Host Agent not found (" + p.HostctrlExeDir() + ")"
		return ha
	}
	ha.Installed, ha.Path = true, bin
	if res, err := r.Run(ctx, exec.Cmd{Path: bin, Args: []string{"-status"}}); err == nil {
		for _, l := range strings.Split(res.Stdout, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				ha.Processes = append(ha.Processes, l)
				if strings.HasPrefix(l, "saphostexec running") {
					ha.Running = true
				}
			}
		}
	} else {
		ha.Error = err.Error()
	}
	if res, err := r.Run(ctx, exec.Cmd{Path: bin, Args: []string{"-version"}}); err == nil {
		if v, perr := kernel.Parse(res.Stdout); perr == nil {
			ha.Version = v
		}
	}
	return ha
}
