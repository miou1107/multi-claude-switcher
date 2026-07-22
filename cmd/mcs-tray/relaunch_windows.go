//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// createNewProcessGroup is CREATE_NEW_PROCESS_GROUP. Starting the relaunched
// process in its own group means it is not signalled when the old process exits.
const createNewProcessGroup = 0x00000200

// detachRelaunch makes the relaunched process outlive this one.
func detachRelaunch(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}
