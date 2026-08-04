//go:build windows

package main

import (
	"os"
	"os/exec"
	"strings"

	"github.com/miou1107/multi-claude-switcher/core/diagnostics"
)

// The clipboard lives in internal/clip so both hosts share one implementation:
// see that package's doc comment for why the write is awaited.

// panelBuildDiagnostics gathers what the report needs. Everything that does
// not differ between the two hosts lives in diagnostics.Gather now — this
// used to be a 60-line copy of cmd/mcs-menubar/diagnostics.go, byte-identical
// apart from the three values passed in below.
func panelBuildDiagnostics() diagnostics.Input {
	return diagnostics.Gather(panelMustFindProfiles(), panelPlat, osVersion(), "%USERPROFILE%", os.Getenv("USERNAME"))
}

// osVersion is best effort: an unknown OS version costs one line of a report,
// so it is never worth failing the screen over.
func osVersion() string {
	cmd := exec.Command("cmd", "/c", "ver")
	// mcs-tray is a -H=windowsgui exe with no console of its own (see
	// hidewindow_windows.go); every other console spawn in this binary already
	// calls hideConsole, and this one flashed a black window on every render of
	// the Debug info screen without it.
	hideConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
