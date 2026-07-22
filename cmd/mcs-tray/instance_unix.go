//go:build !windows

package main

import (
	"log"
	"os"
	"os/exec"
)

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
