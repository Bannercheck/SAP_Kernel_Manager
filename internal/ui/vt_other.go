//go:build !windows

package ui

import "os"

func enableVirtualTerminal(*os.File) bool { return true }
