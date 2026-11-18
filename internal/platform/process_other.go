//go:build !unix && !windows

package platform

// ProcessAlive cannot tell on this platform and errs on the safe side.
func ProcessAlive(int) bool { return true }
