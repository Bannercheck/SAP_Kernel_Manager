//go:build !windows

package platform

import (
	"os"
	"runtime"
	"strings"
)

// unixBase holds behaviour shared by every Unix flavour.
type unixBase struct{}

func (unixBase) Arch() string                 { return runtime.GOARCH }
func (unixBase) KernelDirName() string        { return kernelDirName() }
func (unixBase) ExeSuffix() string            { return "" }
func (unixBase) SIDAdmUser(sid string) string { return strings.ToLower(sid) + "adm" }
func (unixBase) IsPrivileged() bool           { return os.Geteuid() == 0 }
func (unixBase) HostctrlExeDir() string       { return "/usr/sap/hostctrl/exe" }
func (unixBase) SapservicesPath() string      { return "/usr/sap/sapservices" }
