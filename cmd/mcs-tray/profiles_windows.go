//go:build windows

package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// newProfileSupported reports whether the "New account profile…" action applies.
// It does only for the Store/MSIX build, whose profiles MCS creates and manages
// (the standalone build's profiles are just sibling data dirs the user picks).
func newProfileSupported() bool { return platform.MSIXAvailable() }

// newProfileMenuLabel is the menu text for the create-profile action (Store build).
func newProfileMenuLabel() string { return "New account profile…" }

// runNewProfileFlow saves the current account as a profile and opens a fresh,
// signed-out Claude so the user can add a second account, then relaunches the
// tray so the new profile appears in the menu. After the user signs in, a
// background watcher (startMigrationWatcher) brings that account's saved sessions
// across automatically.
func runNewProfileFlow() {
	name := askText("Name the account you want to switch to (e.g. Work). Your current account is saved, then a fresh Claude opens for you to sign into the other account — its saved conversations are brought over automatically.", "")
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	current := platform.MSIXCurrentName()
	msg := fmt.Sprintf("Set up \"%s\"?\n\nClaude will close and reopen signed out so you can log into that account once. Your current account (\"%s\") is saved — you can switch back from the tray anytime, and nothing is deleted.", name, current)
	if !confirmDialog(msg, "Set up") {
		return
	}

	if err := plat.TerminateApp(); err != nil {
		notify("Couldn't close Claude", err.Error())
		return
	}
	if err := platform.MSIXNewProfile(name); err != nil {
		notify("Couldn't set up the account", err.Error())
		return
	}
	notify("Sign into your other account",
		fmt.Sprintf("Log into the account in the Claude window that opened. Its saved conversations will appear automatically. Then use the tray to switch between \"%s\" and \"%s\".", current, name))
	relaunchSelf() // rebuild the menu (and restart the migration watcher) with the new profile
}

// startMigrationWatcher, if a first-login migration is queued, polls until the
// user signs into the freshly created account, then copies that account's saved
// sessions into it. Runs only after a create; a no-op otherwise.
func startMigrationWatcher() {
	if !platform.MSIXPendingMigration() {
		return
	}
	go func() {
		// Poll ~15 minutes (5s cadence) for the sign-in, then give up quietly.
		for i := 0; i < 180; i++ {
			copied, done := platform.MSIXAttemptMigration()
			if done {
				if copied > 0 {
					notify("Conversations restored",
						fmt.Sprintf("Brought %d saved conversation(s) into your other account.", copied))
				}
				return
			}
			time.Sleep(5 * time.Second)
		}
		platform.MSIXCancelMigration()
	}()
}
