package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"net"
	"net/http"
	"strings"
	"time"

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

// pickResult is the outcome the page POSTs back: the checked folders and whether
// the user confirmed (vs cancelled).
type pickResult struct {
	folders []string
	ok      bool
}

// closePage is the tiny confirmation shown after the user acts.
func closePage(msg string) string {
	return `<!doctype html><meta charset="utf-8"><title>Rescan accounts</title>` +
		`<body style="font-family:-apple-system,system-ui,sans-serif;margin:3rem;text-align:center">` +
		`<p style="font-size:1.1rem">` + html.EscapeString(msg) + `</p></body>`
}

// pickMux builds the two-route handler for the review page. Every request must
// carry the exact token (query param on GET, form field on POST); otherwise 403.
// The first /submit sends exactly one pickResult on resCh (non-blocking).
func pickMux(page, token string, resCh chan<- pickResult) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") != token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	})
	mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.FormValue("t") != token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.FormValue("cancel") != "" {
			_, _ = w.Write([]byte(closePage("Cancelled — you can close this tab.")))
			select {
			case resCh <- pickResult{nil, false}:
			default:
			}
			return
		}
		folders := append([]string(nil), r.Form["folder"]...)
		_, _ = w.Write([]byte(closePage("Saved — you can close this tab.")))
		select {
		case resCh <- pickResult{folders, true}:
		default:
		}
	})
	return mux
}

// pickAccountsViaBrowser starts a single-shot 127.0.0.1 server, opens the review
// page in the browser, and returns the chosen folders. Cancel, a closed tab, a
// 3-minute timeout, or any error all return ok=false (no change).
func pickAccountsViaBrowser(accounts []core.ScannedAccount, managed []string) ([]string, bool) {
	tokBytes := make([]byte, 16)
	if _, err := rand.Read(tokBytes); err != nil {
		notify("Rescan failed", "could not generate a secure token")
		return nil, false
	}
	token := hex.EncodeToString(tokBytes)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		notify("Rescan failed", "could not start the local UI server: "+err.Error())
		return nil, false
	}
	port := ln.Addr().(*net.TCPAddr).Port

	resCh := make(chan pickResult, 1)
	page := renderReviewHTML(accounts, computePreselect(accounts, managed), token)
	srv := &http.Server{Handler: pickMux(page, token, resCh)}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/?t=%s", port, token)
	if err := openURL(url); err != nil {
		notify("Rescan", "Open this URL in your browser to choose accounts:\n"+url)
	}

	select {
	case res := <-resCh:
		return res.folders, res.ok
	case <-time.After(3 * time.Minute):
		return nil, false
	}
}
