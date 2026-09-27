//go:build !linux && !aix && !darwin && !freebsd

package ops

import (
	"os"
	"time"
)

func changeTime(os.FileInfo) time.Time { return time.Time{} }
