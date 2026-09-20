//go:build windows

package platform

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
)

type windows struct{}

func current() Platform { return windows{} }

func (windows) Name() string                 { return "windows" }
func (windows) Arch() string                 { return runtime.GOARCH }
func (windows) KernelDirName() string        { return kernelDirName() }
func (windows) ExeSuffix() string            { return ".exe" }
func (windows) LibPathVar() string           { return "PATH" }
func (windows) SIDAdmUser(sid string) string { return strings.ToUpper(sid) + "adm" }
func (windows) SapservicesPath() string      { return "" }

// IsPrivileged reports whether the process runs elevated. Opening the raw
// physical drive succeeds only for administrators; this avoids a dependency
// on golang.org/x/sys for now.
func (windows) IsPrivileged() bool {
	f, err := os.Open(`\\.\PHYSICALDRIVE0`)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

func (windows) HostctrlExeDir() string {
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	return filepath.Join(pf, "SAP", "hostctrl", "exe")
}

// OSVersion returns the output of `ver`, e.g. "Microsoft Windows [Version 10.0.20348.2340]".
func (windows) OSVersion(ctx context.Context, r exec.Runner) string {
	if res, err := r.Run(ctx, exec.Cmd{Path: "cmd", Args: []string{"/c", "ver"}}); err == nil && res.ExitCode == 0 {
		return strings.TrimSpace(res.Stdout)
	}
	return "Windows"
}
