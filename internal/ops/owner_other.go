//go:build !unix

package ops

import "os"

func fileUID(os.FileInfo) (int, bool) { return 0, false }
