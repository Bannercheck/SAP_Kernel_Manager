// Package version holds the product identity and build-time information
// injected via -ldflags.
package version

import (
	"fmt"
	"runtime"
)

// AppName is the command / binary name; ProductName is the long form.
// Changing the tool's name means changing these two constants, the BIN
// variable in the Makefile and the launcher script file names.
const (
	AppName     = "kernelman"          // command / binary name
	DisplayName = "KernelMan"          // how the product is written in screens
	ProductName = "SAP Kernel Manager" // long form
	EnvPrefix   = "KERNELMAN_"         // KERNELMAN_COLOR, KERNELMAN_UNICODE, KERNELMAN_MENU ...
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a single-line description suitable for `kernelman version`.
func String() string {
	return fmt.Sprintf("%s %s (commit %s, built %s, %s/%s, %s)",
		AppName, Version, Commit, Date, runtime.GOOS, runtime.GOARCH, runtime.Version())
}
