package main

import (
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// relaunchSkipInstanceCheckFlag is passed by the self-updater when it relaunches
// the app, so the fresh instance does not refuse to start while the old one is
// still quitting. It is a hidden internal flag, not a user-facing option.
const relaunchSkipInstanceCheckFlag = "--mcs-relaunch"

// hasSkipInstanceFlag reports whether the relaunch skip flag is in args.
func hasSkipInstanceFlag(args []string) bool {
	for _, a := range args {
		if a == relaunchSkipInstanceCheckFlag {
			return true
		}
	}
	return false
}

// otherTrayRunning parses `ps -axo pid=,command=` output and reports whether any
// process OTHER than selfPID is an mcs-tray. Pure so it is unit-testable.
func otherTrayRunning(psOutput string, selfPID int) bool {
	for _, line := range strings.Split(psOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, " ", 2)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid == selfPID {
			continue
		}
		if strings.Contains(fields[1], "mcs-tray") {
			return true
		}
	}
	return false
}

// anotherInstanceRunning reports whether another mcs-tray process is running.
// Fail-open: if ps cannot be run, assume none so startup is never blocked.
func anotherInstanceRunning() bool {
	out, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		log.Printf("instance check: ps failed: %v (assuming no other instance)", err)
		return false
	}
	return otherTrayRunning(string(out), os.Getpid())
}
