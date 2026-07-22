//go:build windows

package main

import (
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// anotherInstanceRunning reports whether another mcs-tray.exe process is running.
// Fail-open: if tasklist cannot be run, assume none so startup is never blocked.
func anotherInstanceRunning() bool {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq mcs-tray.exe", "/FO", "CSV", "/NH").Output()
	if err != nil {
		log.Printf("instance check: tasklist failed: %v (assuming no other instance)", err)
		return false
	}
	return otherTrayInTasklist(string(out), os.Getpid())
}

// otherTrayInTasklist parses `tasklist /FO CSV /NH` output whose rows look like
// "mcs-tray.exe","1234","Console","1","10,000 K" and reports whether any row's
// PID differs from selfPID. tasklist is already filtered to mcs-tray.exe, so a
// differing PID means another instance. The "INFO: No tasks..." no-match line
// (which does not start with a quote) is ignored. Pure so it is unit-testable.
func otherTrayInTasklist(output string, selfPID int) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, `"`) {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(strings.Trim(fields[1], `" `))
		if err != nil || pid == selfPID {
			continue
		}
		return true
	}
	return false
}
