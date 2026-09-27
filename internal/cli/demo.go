package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/demo"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
)

// demoRoot is set when the simulated host is active (shown as a badge).
var demoRoot string

// DefaultDemoRoot is ~/.kernelman/demo.
func DefaultDemoRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.TempDir()
	}
	return filepath.Join(home, ".kernelman", "demo")
}

// EnableDemo builds the simulated SAP host under root and points every
// operation at it. File operations stay real; SAP tools are simulated.
func EnableDemo(root string) error {
	if root == "" {
		root = DefaultDemoRoot()
	}
	layout, err := demo.Setup(root, platformNow())
	if err != nil {
		return fmt.Errorf("demo setup in %s: %w", root, err)
	}
	plat = demo.NewPlatform(platformNow(), layout)
	runner = demo.NewRunner(layout, exec.NewReal())
	system.UsrSap = root
	isRoot = false
	demoRoot = root
	defaultDownloadDir = layout.Download
	scanRoots = []string{root}
	return nil
}

// DemoActive reports whether the simulated host is in use.
func DemoActive() bool { return demoRoot != "" }
