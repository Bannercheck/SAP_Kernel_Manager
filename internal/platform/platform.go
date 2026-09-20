// Package platform isolates every operating-system difference (paths, users,
// privilege checks, library path variable). Nothing outside this package may
// contain OS-specific logic.
package platform

import (
	"context"
	"runtime"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
)

// Platform describes the host kernelman runs on.
type Platform interface {
	Name() string                 // "linux" | "aix" | "windows"
	Arch() string                 // Go arch: amd64, ppc64le, ppc64
	KernelDirName() string        // SAP platform directory: linuxx86_64, rs6000_64, NTAMD64 ...
	ExeSuffix() string            // "" or ".exe"
	LibPathVar() string           // LD_LIBRARY_PATH | LIBPATH | PATH
	SIDAdmUser(sid string) string // abcadm | ABCadm
	IsPrivileged() bool           // root / local administrator
	HostctrlExeDir() string       // SAP Host Agent executable directory
	SapservicesPath() string      // /usr/sap/sapservices or "" when not applicable
	OSVersion(ctx context.Context, r exec.Runner) string
}

// Current returns the Platform for the running binary.
func Current() Platform { return current() }

// kernelDirNames maps GOOS/GOARCH to the SAP kernel platform directory name.
var kernelDirNames = map[string]string{
	"linux/amd64":   "linuxx86_64",
	"linux/ppc64le": "linuxppc64le",
	"linux/ppc64":   "linuxppc64",
	"linux/s390x":   "linuxs390x",
	"aix/ppc64":     "rs6000_64",
	"windows/amd64": "NTAMD64",
}

func kernelDirName() string {
	if n, ok := kernelDirNames[runtime.GOOS+"/"+runtime.GOARCH]; ok {
		return n
	}
	return runtime.GOOS + "_" + runtime.GOARCH
}
