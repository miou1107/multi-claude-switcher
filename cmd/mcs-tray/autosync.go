package main

import (
	"log"
	"strings"

	"github.com/getlantern/systray"
	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
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

// autoSyncWarningMessage builds the enable-time warning. When any profile is a
// Team account, it appends a note that Code conversations cannot be imported into
// those accounts (Auto Sync only exports out of them).
func autoSyncWarningMessage(teamNames []string) string {
	base := "With this on, every account switch bidirectionally syncs — both accounts' conversations will merge. Enable?"
	if len(teamNames) == 0 {
		return base
	}
	names := strings.Join(teamNames, ", ")
	if len(teamNames) == 1 {
		return base + "\n\n⚠️ " + names +
			" is a Team account — Code conversations cannot be imported into it. Auto Sync will only export out of it, never merge others' conversations in."
	}
	return base + "\n\n⚠️ " + names +
		" are Team accounts — Code conversations cannot be imported into them. Auto Sync will only export out of them, never merge others' conversations in."
}

// teamProfileNames returns the display names of profiles detected as Team.
func teamProfileNames(profiles []*platform.ProfileInfo) []string {
	var names []string
	for _, p := range profiles {
		if getAcctType(p.Path) == core.AccountTeam {
			names = append(names, core.DisplayName(p.Name))
		}
	}
	return names
}

// askEnableAutoSync shows the enable-time warning and returns the user's choice.
func askEnableAutoSync(teamNames []string) autoSyncChoice {
	return askEnableAutoSyncChoice(autoSyncWarningMessage(teamNames))
}

// toggleAutoSync flips the auto sync-on-switch setting and syncs the menu
// checkbox. Enabling shows a one-time warning (unless previously dismissed).
func toggleAutoSync(m *systray.MenuItem, teamNames []string) {
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
		switch askEnableAutoSync(teamNames) {
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
