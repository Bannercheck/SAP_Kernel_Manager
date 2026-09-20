// Package version holds build-time information injected via -ldflags.
package version

import (
	"fmt"
	"runtime"
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a single-line description suitable for `skm version`.
func String() string {
	return fmt.Sprintf("skm %s (commit %s, built %s, %s/%s, %s)",
		Version, Commit, Date, runtime.GOOS, runtime.GOARCH, runtime.Version())
}
