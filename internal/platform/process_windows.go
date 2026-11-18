//go:build windows

package platform

import "os"

// ProcessAlive reports whether a process with this ID runs (Windows opens a
// handle only for an existing process).
func ProcessAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
