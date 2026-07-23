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
// (0x08000000) suppresses that.
//
// Deliberately does NOT set SysProcAttr.HideWindow: that sets STARTF_USESHOWWINDOW
// with SW_HIDE, which also hides the *WinForms dialog* a powershell helper opens
// (runPS), so the About/Rename/Sync/new-profile dialogs would silently never
// appear. CREATE_NO_WINDOW alone hides the console without touching GUI windows.
func hideConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
