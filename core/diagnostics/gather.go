package diagnostics

import (
	"os"
	"runtime"
	"strings"
	"unicode"

	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
)

// Gather builds an Input from live profiles and the current process
// environment.
//
// This used to be two 60-line copies, one in cmd/mcs-menubar/diagnostics.go
// and one in cmd/mcs-tray/paneldiagnostics_windows.go, byte-identical apart
// from the home-replacement token, the OS user-name env var, and the OS
// version helper. This repo has already been burned by exactly that
// pattern once — see the comment on buildProfiles in cmd/mcs-menubar/main.go
// about panelui.BuildProfiles, extracted after the same kind of copy drifted
// and SignedIn ended up set on one host and not the other. Reintroducing it
// here would mean a masking-relevant field could exist in one host's report
// and not the other's without either host's tests ever noticing.
//
// What genuinely differs by host is passed in rather than duplicated:
// osVersion (shells out differently, and the Windows spawn has its own
// console-hiding concern), homeReplacement ("~" vs "%USERPROFILE%"), and
// userNameEnv (the already-read value of $USER or %USERNAME%, since which
// env var to read is itself host-specific).
func Gather(profiles []*platform.ProfileInfo, plat platform.Platform, osVersion, homeReplacement, userNameEnv string) Input {
	running, _ := plat.DetectRunningProfile()

	in := Input{
		Version:         core.Version,
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		// Sanitized for the same reason log lines are (see readLogTail): the
		// value comes from whatever the OS handed back, and on a Windows whose
		// console code page is not UTF-8 that is not valid UTF-8 — a
		// Traditional Chinese machine's `cmd /c ver` returns "版本" in CP950,
		// which lands in the report's very first line as four invalid bytes.
		// The host helpers avoid producing those now; this is the backstop, so
		// no future OS-version source can put invalid UTF-8 into the report.
		OSVersion:       strings.ToValidUTF8(osVersion, string(unicode.ReplacementChar)),
		Install:         plat.InstallKind(),
		AutoSync:        core.AutoSyncOnSwitch(),
		LoginItem:       core.LoginItemEnabled(),
		ActiveRecord:    core.LoadActiveProfile(),
		HomeReplacement: homeReplacement,
		LogDir:          core.LogDir(),
	}
	// Gathered here rather than in each host: this is the second thing both
	// hosts would have had to remember to do the same way, and the first one
	// (the panel's own state) had already drifted once before it was made
	// shared. NewBackupManager("") is the same root every caller uses.
	usage := core.NewBackupManager("").Usage()
	in.BackupCount, in.BackupBytes = usage.Snapshots, usage.Bytes
	in.BackupStaged, in.BackupStagedBytes = usage.Staged, usage.StagedBytes
	in.BackupOther, in.BackupOtherBytes = usage.Other, usage.OtherBytes
	in.BackupReadErr = usage.Err

	in.Home, _ = os.UserHomeDir()
	in.HostName, _ = os.Hostname()
	in.UserName = userNameEnv

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
		in.Profiles = append(in.Profiles, Profile{
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
