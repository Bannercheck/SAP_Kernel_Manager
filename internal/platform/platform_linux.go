//go:build linux

package platform

import (
	"context"
	"os"
	"strings"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
)

type linux struct{ unixBase }

func current() Platform { return linux{} }

func (linux) Name() string       { return "linux" }
func (linux) LibPathVar() string { return "LD_LIBRARY_PATH" }

// OSVersion returns the distribution name from /etc/os-release plus the
// kernel release from uname -r.
func (linux) OSVersion(ctx context.Context, r exec.Runner) string {
	name := "Linux"
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
				name = strings.Trim(v, `"`)
				break
			}
		}
	}
	if res, err := r.Run(ctx, exec.Cmd{Path: "uname", Args: []string{"-r"}}); err == nil && res.ExitCode == 0 {
		name += " (kernel " + strings.TrimSpace(res.Stdout) + ")"
	}
	return name
}
