package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// pickerResultSentinel prefixes the one stdout line mcs-picker prints; matches
// resultSentinel in cmd/mcs-picker.
const pickerResultSentinel = "MCS_RESULT "

// pickerBinaryName is the picker executable that ships beside the tray binary.
var pickerBinaryName = func() string {
	if runtime.GOOS == "windows" {
		return "mcs-picker.exe"
	}
	return "mcs-picker"
}()

// pickViaHelper launches the mcs-picker sibling binary — a separate process, so
// its native window does not fight systray for the macOS main run loop — and
// parses its result line. Returns the chosen folders, or ok=false on
// cancel/close/any failure.
func pickViaHelper() ([]string, bool) {
	exe, err := os.Executable()
	if err != nil {
		notify("Rescan failed", "could not locate the app: "+err.Error())
		return nil, false
	}
	helper := filepath.Join(filepath.Dir(exe), pickerBinaryName)
	out, err := exec.Command(helper).Output()
	if err != nil {
		notify("Rescan failed", "the account picker could not start: "+err.Error())
		return nil, false
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, pickerResultSentinel) {
			continue
		}
		var r struct {
			OK      bool     `json:"ok"`
			Folders []string `json:"folders"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, pickerResultSentinel)), &r) == nil {
			return r.Folders, r.OK
		}
	}
	return nil, false
}
