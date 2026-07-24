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

// anyComplete reports whether at least one account is Complete (i.e. has a
// live login and is actually switchable). Ghost accounts alone don't count.
func anyComplete(accounts []core.ScannedAccount) bool {
	for _, a := range accounts {
		if a.Complete {
			return true
		}
	}
	return false
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
	if !anyComplete(accounts) {
		infoDialog("Rescan accounts", "No complete (switchable) accounts to manage.")
		return
	}
	folders, ok := pickAccountsViaBrowser(accounts, core.LoadManaged())
	// Regression guard: !ok covers cancelled/timed out/closed; len(folders) == 0
	// covers a confirmed-but-empty selection. Either way we must not persist an
	// empty managed set — SetManaged(nil) writes an authoritative-empty registry
	// that hides every profile from the tray menu.
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
