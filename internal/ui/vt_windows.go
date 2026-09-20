//go:build windows

package ui

import (
	"os"
	"syscall"
	"unsafe"
)

const enableVirtualTerminalProcessing = 0x0004

// enableVirtualTerminal switches the Windows console to ANSI escape
// processing (Windows 10 1511+). It reports whether colours can be used.
func enableVirtualTerminal(f *os.File) bool {
	k32 := syscall.NewLazyDLL("kernel32.dll")
	getMode := k32.NewProc("GetConsoleMode")
	setMode := k32.NewProc("SetConsoleMode")
	h := syscall.Handle(f.Fd())
	var mode uint32
	if r, _, _ := getMode.Call(uintptr(h), uintptr(unsafe.Pointer(&mode))); r == 0 {
		return false
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return true
	}
	r, _, _ := setMode.Call(uintptr(h), uintptr(mode|enableVirtualTerminalProcessing))
	return r != 0
}
