//go:build !linux && !aix && !windows

package platform

import (
	"context"
	"runtime"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
)

// other lets kernelman compile on developer machines (e.g. macOS); it is not a
// supported SAP host platform.
type other struct{ unixBase }

func current() Platform { return other{} }

func (other) Name() string                                  { return runtime.GOOS }
func (other) LibPathVar() string                            { return "LD_LIBRARY_PATH" }
func (other) OSVersion(context.Context, exec.Runner) string { return runtime.GOOS + " (unsupported)" }
