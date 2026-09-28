//go:build unix

package ops

import (
	"os"
	"syscall"
)

// fileUID returns the owner of a file when the platform reports it.
func fileUID(info os.FileInfo) (int, bool) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid), true
	}
	return 0, false
}
