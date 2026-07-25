package main

import (
	"github.com/getlantern/systray"
	"github.com/miou1107/multi-claude-switcher/core"
)

// runRescan is the "Rescan accounts…" handler: launch the native picker helper,
// persist the chosen set, and relaunch (the menu is static, so a rebuild is
// needed to reflect changes).
func runRescan() {
	folders, ok := pickViaHelper()
	// Guard: !ok covers cancel/close/helper-failure; len(folders) == 0 covers a
	// confirmed-but-empty selection. Never persist an empty managed set —
	// SetManaged(nil) writes an authoritative-empty registry that would hide
	// every profile from the tray menu.
	if !ok || len(folders) == 0 {
		return
	}
	if err := core.SetManaged(folders); err != nil {
		notify("Rescan failed", err.Error())
		return
	}
	relaunchSelf()
}

// wireRescan attaches the rescan handler to its menu item.
func wireRescan(mRescan *systray.MenuItem) {
	go func() {
		for range mRescan.ClickedCh {
			go runRescan()
		}
	}()
}
