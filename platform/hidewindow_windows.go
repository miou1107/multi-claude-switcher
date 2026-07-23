//go:build windows

package platform

import (
	"os/exec"
	"syscall"
)

// hideConsole makes cmd run without popping a console window. mcs-tray is built
// as a GUI-subsystem exe (-H=windowsgui) and so owns no console; every console
// helper it spawns (powershell, taskkill, …) would otherwise allocate and flash
// its own black window on screen. CREATE_NO_WINDOW (0x08000000) suppresses that.
//
// Apply it to console helpers only — never to a GUI target (e.g. launching Claude
// Desktop), whose own window must stay visible.
//
// Deliberately does NOT set SysProcAttr.HideWindow (SW_HIDE): that would also hide
// any GUI window a helper opens. CREATE_NO_WINDOW alone suppresses the console
// without affecting GUI windows.
func hideConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
