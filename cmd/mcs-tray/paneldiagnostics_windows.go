//go:build windows

package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/core/diagnostics"
	"github.com/miou1107/multi-claude-switcher/platform"
)

// The clipboard lives in internal/clip so both hosts share one implementation:
// see that package's doc comment for why the write is awaited.

// panelBuildDiagnostics gathers what the report needs. Raw values throughout:
// masking happens once, inside diagnostics.Build, so no caller can forget.
func panelBuildDiagnostics() diagnostics.Input {
	profiles := panelMustFindProfiles()
	running, _ := panelPlat.DetectRunningProfile()

	in := diagnostics.Input{
		Version:         core.Version,
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		OSVersion:       osVersion(),
		Install:         panelPlat.InstallKind(),
		AutoSync:        core.AutoSyncOnSwitch(),
		LoginItem:       core.LoginItemEnabled(),
		ActiveRecord:    core.LoadActiveProfile(),
		HomeReplacement: "%USERPROFILE%",
		LogDir:          core.LogDir(),
	}
	in.Home, _ = os.UserHomeDir()
	in.HostName, _ = os.Hostname()
	in.UserName = os.Getenv("USERNAME")

	// One scan for every address. ScanAccounts is the only exported route to an
	// account's email — core.readLocalStorageIdentity is unexported, and copying
	// a locked LevelDB is not something to reimplement for a report.
	emails := map[string]string{}
	for _, a := range core.ScanAccounts(profiles, core.LoadPending()) {
		if a.Email != "" {
			emails[a.UUID] = a.Email
		}
	}

	for _, p := range profiles {
		uuid, uuidErr := platform.GetProfileAccountUUID(p.Path)
		org, _ := platform.GetProfileActiveOrgUUID(p.Path)
		in.Profiles = append(in.Profiles, diagnostics.Profile{
			Folder:      p.Name,
			AccountUUID: uuid,
			Email:       emails[uuid],
			OrgUUID:     org,
			Path:        p.Path,
			SignedIn:    uuidErr == nil,
			Running:     running != "" && platform.SamePath(p.Path, running),
			Convos:      p.UUIDBuckets[uuid],
		})
	}

	// Versions come from whichever profile can answer; they describe the install,
	// not the account, so the first readable one is the answer for all of them.
	for _, p := range profiles {
		if in.ClaudeVer == "" {
			v, err := platform.GetProfileClaudeVersion(p.Path)
			if err == nil {
				in.ClaudeVer = v
			} else if in.ClaudeVerErr == "" {
				in.ClaudeVerErr = err.Error()
			}
		}
		if in.ClaudeCodeVer == "" {
			v, err := platform.GetProfileClaudeCodeVersion(p.Path)
			if err == nil {
				in.ClaudeCodeVer = v
			} else if in.ClaudeCodeVerErr == "" {
				in.ClaudeCodeVerErr = err.Error()
			}
		}
	}
	return in
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
