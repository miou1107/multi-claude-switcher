package main

import (
	"log"
	"os/exec"
	"strings"

	"github.com/getlantern/systray"
	"github.com/miou1107/multi-claude-switcher/core"
)

// autoAlignChoice is the user's response to the enable-time warning.
type autoAlignChoice int

const (
	choiceCancel autoAlignChoice = iota
	choiceEnable
	choiceEnableDontAsk
)

// shouldWarnAutoAlign reports whether to show the enable-time warning: only when
// turning the toggle ON and the user has not dismissed it. Turning OFF never warns.
func shouldWarnAutoAlign(enabling, dismissed bool) bool {
	return enabling && !dismissed
}

// parseAutoAlignChoice maps an osascript `display dialog` result to a choice.
// The cancel button makes osascript exit non-zero (runErr != nil); otherwise
// stdout is "button returned:<label>".
func parseAutoAlignChoice(out string, runErr error) autoAlignChoice {
	if runErr != nil {
		return choiceCancel
	}
	if strings.Contains(strings.ToLower(out), "don't ask") {
		return choiceEnableDontAsk
	}
	return choiceEnable
}

// askEnableAutoAlign shows the enable-time warning and returns the user's choice.
func askEnableAutoAlign() autoAlignChoice {
	msg := "With this on, every account switch bidirectionally syncs — both accounts' conversations will merge. Enable?"
	script := "display dialog " + osaQuote(msg) +
		` buttons {"Cancel", "Enable", "Enable, don't ask again"}` +
		` default button "Enable" cancel button "Cancel" with title "Multi-Claude Switcher"`
	out, err := exec.Command("osascript", "-e", script).Output()
	return parseAutoAlignChoice(string(out), err)
}

// toggleAutoAlign flips the auto-align-on-switch setting and syncs the menu
// checkbox. Enabling shows a one-time warning (unless previously dismissed).
func toggleAutoAlign(m *systray.MenuItem) {
	if core.AutoAlignOnSwitch() {
		if err := core.SetAutoAlignOnSwitch(false); err != nil {
			log.Printf("Disable auto-align failed: %v", err)
			notify("Couldn't update Auto-Align", err.Error())
			return
		}
		m.Uncheck()
		log.Println("Auto-align on switch disabled")
		return
	}

	if shouldWarnAutoAlign(true, core.AutoAlignWarningDismissed()) {
		switch askEnableAutoAlign() {
		case choiceCancel:
			log.Println("Auto-align enable cancelled by user")
			return
		case choiceEnableDontAsk:
			if err := core.SetAutoAlignWarningDismissed(true); err != nil {
				log.Printf("Could not persist warning-dismissed: %v", err)
			}
		case choiceEnable:
			// proceed
		}
	}

	if err := core.SetAutoAlignOnSwitch(true); err != nil {
		log.Printf("Enable auto-align failed: %v", err)
		notify("Couldn't update Auto-Align", err.Error())
		return
	}
	m.Check()
	log.Println("Auto-align on switch enabled")
}
