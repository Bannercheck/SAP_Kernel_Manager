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

// hostctrlTool resolves a SAP Host Agent executable (saphostctrl, saphostexec).
func hostctrlTool(r exec.Runner, p platform.Platform, name string) (string, error) {
	bin := filepath.Join(p.HostctrlExeDir(), name+p.ExeSuffix())
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}
	return r.LookPath(name)
}

// ListInstances asks the SAP Host Agent for the instances on this host.
func ListInstances(ctx context.Context, r exec.Runner, p platform.Platform) ([]Instance, error) {
	bin, err := hostctrlTool(r, p, "saphostctrl")
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

// CheckHostAgent inspects saphostexec (-status, -version).
func CheckHostAgent(ctx context.Context, r exec.Runner, p platform.Platform) HostAgent {
	ha := HostAgent{}
	bin, err := hostctrlTool(r, p, "saphostexec")
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
