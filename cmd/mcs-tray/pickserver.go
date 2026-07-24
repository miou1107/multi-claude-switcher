package main

import (
	"fmt"
	"html"
	"strings"

	"github.com/miou1107/multi-claude-switcher/core"
)

// computePreselect returns the set of HomeFolders to pre-check. managed == nil
// (first run, no managed.json) → every complete account; any non-nil slice
// (including empty) → only the listed folders. Ghosts are never included.
func computePreselect(accounts []core.ScannedAccount, managed []string) map[string]bool {
	firstRun := managed == nil
	managedSet := map[string]bool{}
	for _, m := range managed {
		managedSet[m] = true
	}
	pre := map[string]bool{}
	for _, a := range accounts {
		if !a.Complete {
			continue
		}
		if firstRun || managedSet[a.HomeFolder] {
			pre[a.HomeFolder] = true
		}
	}
	return pre
}

// teamLabel renders the Team column for the HTML page.
func teamLabel(a core.ScannedAccount) string {
	if !a.Complete {
		return "—"
	}
	switch a.Account {
	case core.AccountTeam:
		return "🏢 Team"
	case core.AccountPersonal:
		return "Personal"
	default:
		return "?"
	}
}

// renderReviewHTML builds the self-contained, theme-aware review page: a real
// table with a checkbox per complete account (value = HomeFolder, checked per
// preselected), a Team label, greyed non-selectable ghost rows, and Cancel /
// Confirm buttons posting to /submit with the token in a hidden field. All
// account-derived strings are HTML-escaped.
func renderReviewHTML(accounts []core.ScannedAccount, preselected map[string]bool, token string) string {
	esc := html.EscapeString
	var rows strings.Builder
	for _, a := range accounts {
		status, cls := "Complete", ""
		if !a.Complete {
			status, cls = "Incomplete", ` class="ghost"`
		}
		email := a.Email
		if email == "" {
			email = "—"
		}
		check := ""
		if a.Complete {
			c := ""
			if preselected[a.HomeFolder] {
				c = " checked"
			}
			check = fmt.Sprintf(`<input type="checkbox" name="folder" value="%s"%s>`, esc(a.HomeFolder), c)
		}
		rows.WriteString(fmt.Sprintf(
			`<tr%s><td>%s</td><td><code>%s</code></td><td>%s</td><td>%s</td><td>%s</td><td>%d</td><td>%s</td><td>%s</td></tr>`,
			cls, check, esc(short(a.UUID)), esc(status), esc(email), esc(teamLabel(a)), a.Convos, esc(fmtDate(a)), esc(a.Note)))
	}
	return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Rescan accounts</title>
<style>
:root{color-scheme:light dark}
body{font-family:-apple-system,system-ui,sans-serif;margin:2rem;max-width:64rem}
h1{font-size:1.3rem;margin:0 0 .25rem}
.hint{opacity:.65;font-size:.9rem;margin:.25rem 0 1.25rem}
table{border-collapse:collapse;width:100%}
th,td{padding:.5rem .6rem;text-align:left;border-bottom:1px solid #8884;white-space:nowrap}
th{font-size:.72rem;text-transform:uppercase;letter-spacing:.04em;opacity:.6}
tr.ghost{opacity:.45}
td:last-child,th:last-child{white-space:normal}
code{font-family:ui-monospace,SFMono-Regular,monospace;font-size:.9em}
.actions{margin-top:1.5rem;display:flex;gap:.75rem;justify-content:flex-end}
button{font:inherit;padding:.5rem 1.3rem;border-radius:.5rem;border:1px solid #8886;background:transparent;color:inherit;cursor:pointer}
button.primary{background:#0a6cff;color:#fff;border-color:#0a6cff}
</style></head><body>
<h1>Accounts on this machine</h1>
<p class="hint">Check the accounts you want Multi-Claude Switcher to manage. Incomplete accounts (orphaned data with no login) can't be managed.</p>
<form method="post" action="/submit">
<input type="hidden" name="t" value="` + esc(token) + `">
<table><thead><tr><th></th><th>UUID</th><th>Status</th><th>Email</th><th>Team</th><th>Chats</th><th>Last updated</th><th>Note</th></tr></thead>
<tbody>` + rows.String() + `</tbody></table>
<div class="actions">
<button type="submit" name="cancel" value="1">Cancel</button>
<button type="submit" class="primary">Confirm</button>
</div></form></body></html>`
}
