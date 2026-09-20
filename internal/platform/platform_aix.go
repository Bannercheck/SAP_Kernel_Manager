//go:build aix

package platform

import (
	"context"
	"strings"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
)

type aix struct{ unixBase }

func current() Platform { return aix{} }

func (aix) Name() string       { return "aix" }
func (aix) LibPathVar() string { return "LIBPATH" }

// OSVersion returns the AIX technology level, e.g. "AIX 7200-05-03-2148".
func (aix) OSVersion(ctx context.Context, r exec.Runner) string {
	if res, err := r.Run(ctx, exec.Cmd{Path: "oslevel", Args: []string{"-s"}}); err == nil && res.ExitCode == 0 {
		return "AIX " + strings.TrimSpace(res.Stdout)
	}
	return "AIX"
}
