package main

import (
	"log"

	"github.com/getlantern/systray"
	"github.com/miou1107/multi-claude-switcher/core"
)

// autoSyncChoice is the user's response to the enable-time warning.
type autoSyncChoice int

const (
	choiceCancel autoSyncChoice = iota
	choiceEnable
	choiceEnableDontAsk
)

// shouldWarnAutoSync reports whether to show the enable-time warning: only when
// turning the toggle ON and the user has not dismissed it. Turning OFF never warns.
func shouldWarnAutoSync(enabling, dismissed bool) bool {
	return enabling && !dismissed
}

// askEnableAutoSync shows the enable-time warning and returns the user's choice.
func askEnableAutoSync() autoSyncChoice {
	msg := "With this on, every account switch bidirectionally syncs — both accounts' conversations will merge. Enable?"
	return askEnableAutoSyncChoice(msg)
}

// toggleAutoSync flips the auto sync-on-switch setting and syncs the menu
// checkbox. Enabling shows a one-time warning (unless previously dismissed).
func toggleAutoSync(m *systray.MenuItem) {
	if core.AutoSyncOnSwitch() {
		if err := core.SetAutoSyncOnSwitch(false); err != nil {
			log.Printf("Disable auto sync failed: %v", err)
			notify("Couldn't update Auto Sync", err.Error())
			return
		}
		m.Uncheck()
		log.Println("Auto sync on switch disabled")
		return
	}

	if shouldWarnAutoSync(true, core.AutoSyncWarningDismissed()) {
		switch askEnableAutoSync() {
		case choiceCancel:
			log.Println("Auto sync enable cancelled by user")
			return
		case choiceEnableDontAsk:
			if err := core.SetAutoSyncWarningDismissed(true); err != nil {
				log.Printf("Could not persist warning-dismissed: %v", err)
			}
		case choiceEnable:
			// proceed
		}
	}

	if err := core.SetAutoSyncOnSwitch(true); err != nil {
		log.Printf("Enable auto sync failed: %v", err)
		notify("Couldn't update Auto Sync", err.Error())
		return
	}
	m.Check()
	log.Println("Auto sync on switch enabled")
}
