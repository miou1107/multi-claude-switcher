//go:build windows

package main

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// migrationWatcherRunning guards against more than one migration poller at a time.
// The panel now asks the tray to (re)start the watcher after every create, so two
// creates before the user signs in would otherwise run two goroutines both calling
// MSIXAttemptMigration on the same on-disk state. One poller reads that state every
// few seconds and handles whatever is currently queued, so a second is redundant.
var migrationWatcherRunning atomic.Bool

// newProfileSupported reports whether "Add another account" applies. It does on
// both Windows builds, as it already did on macOS.
//
// This was MSIXAvailable() until the standalone half caught up, and stayed that
// way after it had. WindowsPlatform.CreateProfile grew a standalone branch that
// makes %APPDATA%\Claude_<name>, and the recover-a-ghost-account flow has been
// calling it through this same ProfileCreator on standalone installs ever since,
// ungated — a plain add is that same sequence minus the copy step. So the gate
// was hiding the entry point to a path the app was already running.
//
// What it cost: a standalone user with two accounts had no way to add the second
// from the panel at all, and #14's "make a throwaway account and remove it"
// cannot be carried out on a standalone install while this reads MSIXAvailable.
func newProfileSupported() bool { return true }

// newProfileMenuLabel is the menu text for the create-profile action (Store build).
func newProfileMenuLabel() string { return "New account profile…" }

// runNewProfileFlow saves the current account as a profile and opens a fresh,
// signed-out Claude so the user can add a second account, then relaunches the
// tray so the new profile appears in the menu. After the user signs in, a
// background watcher (startMigrationWatcher) brings that account's saved sessions
// across automatically.
func runNewProfileFlow() {
	name := askText("Name the account you want to switch to (e.g. Work). Your current account is saved, then a fresh Claude opens for you to sign into the other account, and its saved conversations are brought over automatically.", "")
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	current := platform.MSIXCurrentName()
	msg := fmt.Sprintf("Set up \"%s\"?\n\nClaude will close and reopen signed out so you can log into that account once. Your current account (\"%s\") is saved, so you can switch back from the tray anytime, and nothing is deleted.", name, current)
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
	if !migrationWatcherRunning.CompareAndSwap(false, true) {
		return // a poller is already running; it reads fresh state each tick and
		// will pick up a migration queued by a later create on its own.
	}
	go func() {
		defer migrationWatcherRunning.Store(false)
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
