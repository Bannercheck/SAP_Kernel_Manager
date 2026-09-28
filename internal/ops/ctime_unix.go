//go:build linux || aix

package ops

import (
	"os"
	"syscall"
	"time"
)

// changeTime returns the inode change time (set when the file was created
// or copied on this host), or the zero time when unavailable.
func changeTime(info os.FileInfo) time.Time {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return time.Unix(int64(st.Ctim.Sec), int64(st.Ctim.Nsec))
	}
	return time.Time{}
}
