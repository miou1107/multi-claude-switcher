package main

import (
	"strings"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// updaterelaunch.go decides when Claude Desktop's own updater has put the user on
// the wrong account. No build tag, so the decision is tested on every CI runner;
// the polling that feeds it is Windows-only (updaterelaunch_windows.go).
//
// The standalone Windows build updates itself through Squirrel. Once the new
// version is installed, Squirrel closes Claude and starts the new claude.exe with
// no arguments at all, so no --user-data-dir, so Claude opens the default
// profile, %APPDATA%\Claude. A user who had switched to any other account finds
// themselves on the default one after every update, without having done anything.
//
// Seen on a real install: Squirrel-ProcessStart.log recorded "About to launch:
// '...\app-<new>\claude.exe': " with an empty argument list on 2026-10-15,
// 2026-10-16 and 2026-10-19, and each time the next switch the user made in MCS
// started from the default profile although their last switch had gone to the
// work account.

// claudeSnapshot is what the watcher saw Claude Desktop running on at one tick.
type claudeSnapshot struct {
	profiles []string // profile paths, as DetectRunningProfiles reports them
	version  string   // the app-<version> folder the main process runs from
}

// claudeVersionInCmdLines returns the version of the Claude Desktop main process
// among cmdLines, taken from the app-<version> folder Squirrel installs each
// version into, or "" when it cannot be told. Helper processes carry --type= and
// are skipped: during an update the old version's helpers can still be alive
// next to the new main process.
func claudeVersionInCmdLines(cmdLines []string) string {
	for _, line := range cmdLines {
		if strings.Contains(line, electronChildFlagTray) {
			continue
		}
		lower := strings.ToLower(line)
		const marker = `\app-`
		i := strings.Index(lower, marker)
		if i < 0 {
			continue
		}
		rest := line[i+len(marker):]
		if end := strings.IndexByte(rest, '\\'); end > 0 {
			return rest[:end]
		}
	}
	return ""
}

// electronChildFlagTray mirrors platform's electronChildFlag: Chromium gives
// every helper process a --type=, and the app's own process runs without one.
const electronChildFlagTray = "--type="

// updateRelaunchTarget returns the profile to put the user back on when the step
// from prev to cur looks like Claude's updater reopening it on the default
// profile, or "" when it does not.
//
// Every condition is there to leave alone something the user did on purpose:
//   - The version changed. Opening the default account from the Start menu by
//     hand looks exactly like the updater's relaunch, except that it does not
//     change the version.
//   - recordedPath, the account MCS last switched the user to, is not the default
//     one. Choosing the default account in MCS records it, so it is never undone.
//   - That account was open before the update and only the default one is open
//     now. Someone with both open, or who closed it themselves before the update,
//     is not owed anything.
func updateRelaunchTarget(prev, cur claudeSnapshot, defaultPath, recordedPath string) string {
	if prev.version == "" || cur.version == "" || prev.version == cur.version {
		return ""
	}
	if recordedPath == "" || defaultPath == "" || platform.SamePath(recordedPath, defaultPath) {
		return ""
	}
	if len(cur.profiles) != 1 || !platform.SamePath(cur.profiles[0], defaultPath) {
		return ""
	}
	for _, p := range prev.profiles {
		if platform.SamePath(p, recordedPath) {
			return recordedPath
		}
	}
	return ""
}
