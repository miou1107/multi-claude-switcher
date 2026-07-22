//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideConsole makes cmd run without popping a console window. mcs-tray is a
// GUI-subsystem exe (-H=windowsgui) with no console of its own, so every console
// helper it spawns (powershell for dialogs/toasts, tasklist for the instance
// check) would otherwise flash its own black window. CREATE_NO_WINDOW
// (0x08000000) suppresses that. Do not apply it to GUI targets (explorer, the
// browser) whose own window must appear.
func hideConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
