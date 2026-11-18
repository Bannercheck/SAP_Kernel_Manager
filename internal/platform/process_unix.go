//go:build unix

package platform

import (
	"errors"
	"syscall"
)

// ProcessAlive reports whether a process with this ID runs.
func ProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
