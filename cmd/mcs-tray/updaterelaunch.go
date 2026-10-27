package main

import (
	"fmt"
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
// profile. Otherwise it returns no target and, when the step is one where
// something happened to Claude, the reason it was left alone, worded for the
// log. A step where Claude's version and accounts are unchanged says nothing:
// the watcher looks every 20 seconds and would otherwise fill the log.
//
// The reason is there because the macOS guard once declined a real relaunch in
// silence (2026-10-24 01:34), and the log gave no way to tell which condition
// had failed. Here every condition that fails says so.
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
func updateRelaunchTarget(prev, cur claudeSnapshot, defaultPath, recordedPath string) (target, reason string) {
	if len(prev.profiles) == 0 || len(cur.profiles) == 0 || defaultPath == "" {
		// The first look, or nothing to compare against.
		return "", ""
	}
	if prev.version == cur.version && sameProfileSet(prev.profiles, cur.profiles) {
		return "", ""
	}
	step := fmt.Sprintf("Claude Desktop went from %v (version %q) to %v (version %q)",
		prev.profiles, prev.version, cur.profiles, cur.version)
	onDefaultAlone := len(cur.profiles) == 1 && platform.SamePath(cur.profiles[0], defaultPath)
	if !onDefaultAlone {
		if prev.version == cur.version {
			// Windows opened or closed at the same version: the user's own doing.
			return "", ""
		}
		if prev.version == "" || cur.version == "" {
			return "", step + "; the version could not be read on one side, so whether Claude updated is unknown; it is not on the default account alone, so nothing to do."
		}
		if sameProfileSet(prev.profiles, cur.profiles) {
			return "", step + "; the update kept the accounts that were open, nothing to do."
		}
		return "", step + "; Claude is not on the default account alone, which is not what the updater's relaunch looks like; leaving it alone."
	}
	if prev.version == "" || cur.version == "" {
		return "", step + "; the version could not be read from Claude's command line, so this cannot be told apart from the user opening the default account by hand; leaving it alone."
	}
	if prev.version == cur.version {
		return "", step + "; same version, so Claude was not updated and this is the user's own doing; leaving it alone."
	}
	if recordedPath == "" {
		return "", step + "; there is no record of which account the user last chose, so nothing to put back."
	}
	if platform.SamePath(recordedPath, defaultPath) {
		return "", step + "; the user chose the default account in the switcher, so this is where they want to be."
	}
	for _, p := range prev.profiles {
		if platform.SamePath(p, recordedPath) {
			return recordedPath, ""
		}
	}
	return "", fmt.Sprintf("%s; the recorded account %s was not open before the update, so nothing to put back.", step, recordedPath)
}

// relaunchWatch is what the watcher remembers between looks.
type relaunchWatch struct {
	prev claudeSnapshot
	// held is set when the last look was set aside as one taken mid-update.
	held bool
}

// look judges one snapshot against the last one and returns the profile to put
// the user back on, or the reason it was left alone, worded for the log.
//
// A look on the same accounts whose version cannot be read is set aside once,
// silently, and the next look is judged against the one before it. That is what
// a look taken mid-update sees: Squirrel has closed the old main process and not
// yet started the new one, and the old helpers still carry the old
// --user-data-dir but, being helpers, no version. Judged against that look, the
// updater's relaunch has an unreadable "before" and is declined (issue #56).
// Only once in a row: an update takes seconds and the watcher looks every 20, so
// a version that stays unreadable is something else, and is reported then.
func (w *relaunchWatch) look(cur claudeSnapshot, defaultPath, recordedPath string) (target, reason string) {
	target, reason = updateRelaunchTarget(w.prev, cur, defaultPath, recordedPath)
	if target != "" {
		// Look again from scratch next tick, so a refusal is not asked twice.
		*w = relaunchWatch{}
		return target, ""
	}
	if !w.held && cur.version == "" && w.prev.version != "" && sameProfileSet(w.prev.profiles, cur.profiles) {
		w.held = true
		return "", ""
	}
	w.prev, w.held = cur, false
	return "", reason
}

// sameProfileSet reports whether a and b name the same profiles, in any order.
func sameProfileSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, p := range a {
		found := false
		for _, q := range b {
			if platform.SamePath(p, q) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
