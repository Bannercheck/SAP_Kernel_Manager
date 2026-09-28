//go:build darwin || freebsd

package ops

import (
	"os"
	"syscall"
	"time"
)

// changeTime returns the inode change time (BSD field name).
func changeTime(info os.FileInfo) time.Time {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return time.Unix(int64(st.Ctimespec.Sec), int64(st.Ctimespec.Nsec))
	}
	return time.Time{}
}
