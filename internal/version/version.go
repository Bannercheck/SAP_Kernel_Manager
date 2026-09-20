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
	AppName     = "sapkernel"
	ProductName = "SAP Kernel Manager"
	EnvPrefix   = "SAPKERNEL_" // SAPKERNEL_COLOR, SAPKERNEL_UNICODE, SAPKERNEL_MENU ...
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a single-line description suitable for `sapkernel version`.
func String() string {
	return fmt.Sprintf("%s %s (commit %s, built %s, %s/%s, %s)",
		AppName, Version, Commit, Date, runtime.GOOS, runtime.GOARCH, runtime.Version())
}
