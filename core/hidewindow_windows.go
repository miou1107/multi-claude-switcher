//go:build windows

package core

import (
	"os/exec"
	"syscall"
)

// hideConsole makes cmd run without popping a console window. The tray is a
// GUI-subsystem process with no console of its own, so a spawned console helper
// (here, reg.exe for the start-at-login Run key) would otherwise flash its own
// window. CREATE_NO_WINDOW (0x08000000) suppresses that.
// Deliberately does NOT set SysProcAttr.HideWindow (SW_HIDE), which would also
// hide GUI windows; CREATE_NO_WINDOW alone suppresses the console.
func hideConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
