package main

import (
	"fmt"
	"strings"

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

// teamCell renders the Team column: Yes/No for a complete account, "?" when
// unknown (ghosts and unclassifiable accounts).
func teamCell(a core.ScannedAccount) string {
	if !a.Complete || a.Account == core.AccountUnknown {
		return "?"
	}
	if a.Account == core.AccountTeam {
		return "Yes"
	}
	return "No"
}

// renderReviewTable builds the step-1 review text: a header line plus one aligned
// row per account, columns UUID / Completeness / email / Team / Convos / Last
// updated / Note. Best-effort monospace alignment (macOS dialogs use a
// proportional font, so alignment is approximate — spec §5).
func renderReviewTable(accounts []core.ScannedAccount) string {
	type row struct{ uuid, comp, email, team, convos, updated, note string }
	rows := []row{{"UUID", "Status", "Email", "Team", "Chats", "Last updated", "Note"}}
	for _, a := range accounts {
		comp := "Complete"
		if !a.Complete {
			comp = "Incomplete"
		}
		email := a.Email
		if email == "" {
			email = "—"
		}
		rows = append(rows, row{
			short(a.UUID), comp, email, teamCell(a),
			fmt.Sprintf("%d", a.Convos), fmtDate(a), a.Note,
		})
	}
	// Column widths.
	w := make([]int, 7)
	for _, r := range rows {
		for i, c := range []string{r.uuid, r.comp, r.email, r.team, r.convos, r.updated, r.note} {
			if len(c) > w[i] {
				w[i] = len(c)
			}
		}
	}
	var b strings.Builder
	for ri, r := range rows {
		cells := []string{r.uuid, r.comp, r.email, r.team, r.convos, r.updated, r.note}
		for i, c := range cells {
			b.WriteString(c)
			if i < len(cells)-1 {
				b.WriteString(strings.Repeat(" ", w[i]-len(c)+2))
			}
		}
		b.WriteString("\n")
		if ri == 0 {
			b.WriteString("\n") // blank line under the header
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// short truncates a UUID to its first 8 chars for display.
func short(uuid string) string {
	if len(uuid) > 8 {
		return uuid[:8]
	}
	return uuid
}

// pickLabel builds a one-line selectable label for a complete account, unique via
// its short UUID.
func pickLabel(a core.ScannedAccount) string {
	name := a.Email
	if name == "" {
		name = core.DisplayName(a.HomeFolder)
	}
	tag := ""
	if a.Account == core.AccountTeam {
		tag = "  🏢 Team"
	}
	return fmt.Sprintf("%s%s  [%s]", name, tag, short(a.UUID))
}

// selectablePick returns the multi-select labels for complete accounts only, a
// label→folder map, and the labels to pre-select (currently-managed folders).
func selectablePick(accounts []core.ScannedAccount, managed []string) (labels []string, labelToFolder map[string]string, preselected []string) {
	managedSet := map[string]bool{}
	for _, m := range managed {
		managedSet[m] = true
	}
	labelToFolder = map[string]string{}
	for _, a := range accounts {
		if !a.Complete {
			continue
		}
		l := pickLabel(a)
		labels = append(labels, l)
		labelToFolder[l] = a.HomeFolder
		if managedSet[a.HomeFolder] {
			preselected = append(preselected, l)
		}
	}
	return labels, labelToFolder, preselected
}

// runRescan is the "Rescan accounts…" handler: scan → review → pick → persist →
// relaunch (the menu is static, so a rebuild is needed to reflect changes).
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
	if !confirmDialogMultiline(renderReviewTable(accounts), "Continue") {
		return // cancelled at review
	}
	labels, labelToFolder, preselected := selectablePick(accounts, core.LoadManaged())
	if len(labels) == 0 {
		infoDialog("Rescan accounts", "No complete (switchable) accounts to manage.")
		return
	}
	selected, ok := chooseMultipleFromList(labels, preselected, "Select the accounts to manage:")
	if !ok {
		return // cancelled at pick
	}
	var folders []string
	for _, l := range selected {
		if f, ok := labelToFolder[l]; ok {
			folders = append(folders, f)
		}
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
