//go:build windows

package main

import (
	"fmt"
	"os"

	"github.com/miou1107/multi-claude-switcher/core/diagnostics"
	"golang.org/x/sys/windows/registry"
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

// currentVersionKey holds what Windows reports about itself. Reading it is how
// osVersion avoids `cmd /c ver`, which was the previous implementation and had
// two problems on a real machine.
//
// It returned its text in the console code page, not UTF-8. On a Traditional
// Chinese install that made the report's first line
// "Microsoft Windows [<4 invalid bytes> 10.0.19045.7548]" — the four bytes
// being "版本" in CP950. Invalid UTF-8, in the report a user is about to paste
// into a GitHub issue, on every non-English Windows.
//
// And it spawned a console process on every render of the Debug info screen,
// which had to be kept invisible by hand: the hideConsole call it needed was
// missed once already and added back in review. The registry needs no process,
// so that whole class of bug goes away with it.
const currentVersionKey = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`

// osVersion reports the Windows version as "10.0.19045.7548 (22H2)".
//
// Best effort: an unknown OS version costs one line of a report, so it is never
// worth failing the screen over. Every part is optional except the build
// number, because a version string with no build in it says nothing useful.
func osVersion() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, currentVersionKey, registry.QUERY_VALUE)
	if err != nil {
		return "unknown"
	}
	defer k.Close()

	build, _, err := k.GetStringValue("CurrentBuildNumber")
	if err != nil || build == "" {
		return "unknown"
	}

	// These four are ASCII digits or absent, never localized text, which is the
	// whole point of reading them here rather than parsing `ver` output.
	major, _, majErr := k.GetIntegerValue("CurrentMajorVersionNumber")
	minor, _, minErr := k.GetIntegerValue("CurrentMinorVersionNumber")
	ver := build
	if majErr == nil && minErr == nil {
		ver = fmt.Sprintf("%d.%d.%s", major, minor, build)
	}
	if ubr, _, err := k.GetIntegerValue("UBR"); err == nil {
		ver += fmt.Sprintf(".%d", ubr)
	}
	// DisplayVersion ("22H2") is absent before Windows 10 20H2, where
	// ReleaseId ("1909") served the same purpose.
	name, _, err := k.GetStringValue("DisplayVersion")
	if err != nil || name == "" {
		name, _, err = k.GetStringValue("ReleaseId")
		if err != nil {
			name = ""
		}
	}
	if name != "" {
		ver += " (" + name + ")"
	}
	return ver
}
