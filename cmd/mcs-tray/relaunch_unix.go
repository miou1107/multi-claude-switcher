//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachRelaunch makes the relaunched process outlive this one by starting it in
// its own process group, so quitting the old app does not signal the new one.
func detachRelaunch(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
