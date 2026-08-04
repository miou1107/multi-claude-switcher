//go:build darwin

package main

import (
	"os"
	"os/exec"
	"strings"

	"github.com/miou1107/multi-claude-switcher/core/diagnostics"
)

// The clipboard lives in internal/clip so both hosts share one implementation:
// see that package's doc comment for why the write is awaited.

// buildDiagnostics gathers what the report needs. Everything that does not
// differ between the two hosts lives in diagnostics.Gather now — this used to
// be a 60-line copy of cmd/mcs-tray/paneldiagnostics_windows.go, byte-identical
// apart from the three values passed in below.
func buildDiagnostics() diagnostics.Input {
	return diagnostics.Gather(mustFindProfiles(), plat, osVersion(), "~", os.Getenv("USER"))
}

// osVersion is best effort: an unknown OS version costs one line of a report,
// so it is never worth failing the screen over.
func osVersion() string {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
