//go:build windows

package main

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
)

// updateRelaunchPoll is how often the tray looks at what Claude Desktop is
// running on. Each look starts PowerShell, so it is kept slow; the cost of being
// slow is the user sitting on the wrong account for up to this long after an
// update, which happens a few times a week at most.
const updateRelaunchPoll = 20 * time.Second

// startUpdateRelaunchWatcher puts the user back on their account after Claude
// Desktop's updater reopens it on the default one. See updaterelaunch.go for why
// that happens and how it is told apart from the user opening Claude by hand.
//
// Standalone build only. The Store build has no --user-data-dir to lose: it keeps
// the active profile in the one folder it always opens, so its updater cannot
// move the user anywhere.
func startUpdateRelaunchWatcher() {
	if plat.InstallKind() != "standalone" {
		return
	}
	defaultPath := filepath.Join(plat.AppSupportDir(), "Claude")
	go func() {
		var prev claudeSnapshot
		for !trayQuitting.Load() {
			time.Sleep(updateRelaunchPoll)
			cur, ok := snapshotClaude()
			if !ok {
				// Nothing running, or the look failed. Keep what was last seen: the
				// updater closes Claude before reopening it, and that gap is exactly
				// the moment the watcher must not forget where the user was.
				continue
			}
			identity, recordedPath := recordedProfile()
			target, reason := updateRelaunchTarget(prev, cur, defaultPath, recordedPath)
			if target != "" {
				log.Printf("[Update relaunch] Claude %s -> %s reopened on %s; the user was on %s", prev.version, cur.version, defaultPath, target)
				restoreAfterUpdate(defaultPath, target, identity)
				// Look again from scratch next tick, so a refusal is not asked twice.
				prev = claudeSnapshot{}
				continue
			}
			if reason != "" {
				// Something happened to Claude and the watcher chose to leave it.
				// Said once per change, so a user left on the default account can
				// read why.
				log.Printf("[Update relaunch] %s", reason)
			}
			prev = cur
		}
	}()
}

// snapshotClaude reports what Claude Desktop is running on now, and false when
// it is not running or that could not be found out.
func snapshotClaude() (claudeSnapshot, bool) {
	running, cmdLines, err := plat.IsAppRunning()
	if err != nil || !running {
		return claudeSnapshot{}, false
	}
	profiles, err := plat.DetectRunningProfiles()
	if err != nil || len(profiles) == 0 {
		return claudeSnapshot{}, false
	}
	return claudeSnapshot{profiles: profiles, version: claudeVersionInCmdLines(cmdLines)}, true
}

// recordedProfile returns the account MCS last switched the user to and its
// folder, or empty strings when there is no record or it no longer exists.
func recordedProfile() (identity, path string) {
	identity = core.LoadActiveProfile()
	if identity == "" {
		return "", ""
	}
	profiles, err := plat.FindProfiles()
	if err != nil {
		return "", ""
	}
	for _, p := range profiles {
		if p.Name == identity {
			return identity, p.Path
		}
	}
	return "", ""
}

// restoreAfterUpdate asks before switching, because switching closes the Claude
// window the user is looking at. Then it checks again: the user may have started
// working on the default account while the question was up, or switched in the
// panel themselves.
func restoreAfterUpdate(defaultPath, target, identity string) {
	name := core.DisplayName(identity)
	msg := fmt.Sprintf("Claude just updated itself and reopened on another account.\n\nSwitch back to \"%s\"? Claude will close and reopen.", name)
	if !confirmDialog(msg, "Switch back") {
		log.Printf("[Update relaunch] User chose to stay on the default account.")
		return
	}
	cur, ok := snapshotClaude()
	if !ok || len(cur.profiles) != 1 || !platform.SamePath(cur.profiles[0], defaultPath) {
		log.Printf("[Update relaunch] Claude is no longer on the default account alone; not switching.")
		return
	}
	if err := switcher.SafeSwitch(defaultPath, target, identity); err != nil {
		log.Printf("[Update relaunch] Switch back failed: %v", err)
		notify("Couldn't switch back", err.Error())
	}
}
