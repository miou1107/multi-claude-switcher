//go:build windows

package main

import (
	"fmt"
	"strings"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// newProfileSupported reports whether the "New account profile…" action applies.
// It does only for the Store/MSIX build, whose profiles MCS creates and manages
// (the standalone build's profiles are just sibling data dirs the user picks).
func newProfileSupported() bool { return platform.MSIXAvailable() }

// runNewProfileFlow saves the current account as a profile and opens a fresh,
// signed-out Claude so the user can add a second account, then relaunches the
// tray so the new profile appears in the menu.
func runNewProfileFlow() {
	name := askText("Name the new account you want to add (e.g. Work). Your current account is saved and a fresh Claude opens for you to sign into the other account.", "")
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	current := platform.MSIXCurrentName()
	msg := fmt.Sprintf("Create profile \"%s\"?\n\nClaude will close and reopen signed out so you can log into another account. Your current account (\"%s\") is saved — you can switch back from the tray anytime.", name, current)
	if !confirmDialog(msg, "Create") {
		return
	}

	if err := plat.TerminateApp(); err != nil {
		notify("Couldn't close Claude", err.Error())
		return
	}
	if err := platform.MSIXNewProfile(name); err != nil {
		notify("Couldn't create profile", err.Error())
		return
	}
	notify("New profile ready",
		fmt.Sprintf("Sign into your other account in the Claude window that opened. Use the tray to switch between \"%s\" and \"%s\".", current, name))
	relaunchSelf() // rebuild the menu so the new profile shows up
}
