package main

import (
	"github.com/getlantern/systray"
	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
)

// fmtDate renders a review date, or "—" when unset.
func fmtDate(a core.ScannedAccount) string {
	if a.LastUpdated.IsZero() {
		return "—"
	}
	return a.LastUpdated.Format("2006-01-02")
}

// short truncates a UUID to its first 8 chars for display.
func short(uuid string) string {
	if len(uuid) > 8 {
		return uuid[:8]
	}
	return uuid
}

// runRescan is the "Rescan accounts…" handler: scan → review (browser) → persist
// → relaunch (the menu is static, so a rebuild is needed to reflect changes).
func runRescan() {
	plat := platform.New()
	profiles, err := plat.FindProfiles()
	if err != nil {
		notify("Rescan failed", err.Error())
		return
	}
	accounts := core.ScanAccounts(profiles)
	if len(accounts) == 0 {
		infoDialog("Rescan accounts", "No Claude accounts found on this machine.")
		return
	}
	folders, ok := pickAccountsViaBrowser(accounts, core.LoadManaged())
	if !ok {
		return // cancelled, timed out, or closed
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
