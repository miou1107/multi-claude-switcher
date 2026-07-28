# Rescan Web UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Rescan review/pick osascript dialogs (an unreadable proportional-font text "table") with a real visual UI: an ephemeral, token-guarded localhost HTTP server that serves an HTML table with checkboxes, opened in the user's browser, returning the chosen folders to the tray.

**Architecture:** A pure HTML renderer + a single-shot `127.0.0.1` HTTP server started when the user runs Rescan. The page (real `<table>`, checkbox per complete account, greyed non-selectable ghost rows, Cancel/Confirm) POSTs the checked folders back to `/submit`; the handler sends them on a channel and the server shuts down. `runRescan` swaps its two osascript calls for one `pickAccountsViaBrowser(...)` call.

**Tech Stack:** Go 1.22 stdlib only (`net`, `net/http`, `crypto/rand`, `html`, `time`, `context`), `github.com/getlantern/systray`. No CGO, no new modules. Module path `github.com/miou1107/multi-claude-switcher`.

## Global Constraints

- **No new dependencies; no CGO.** Native `go build ./...` + `go vet ./...` clean; `GOOS=windows GOARCH=amd64 go build ./...` clean; `go test ./...` green.
- **Server binds `127.0.0.1` only** (never `0.0.0.0`), on an OS-assigned free port (`:0`).
- **Single-use token** (16 random bytes from `crypto/rand`, hex) validated on every request; missing/wrong token → HTTP 403. Token is never logged.
- **All account-derived strings HTML-escaped** (`html.EscapeString`) in the page — email, note, UUID, folder value.
- **Checkbox value is the HomeFolder** (the manage unit); the same email/UUID across two folders is two distinct rows.
- **Cancel, timeout (3 min), tab-close, and any error all mean "no change"** — `pickAccountsViaBrowser` returns `ok=false` and `runRescan` returns without calling `SetManaged`/`relaunchSelf`.
- **Preselect rule (unchanged from the shipped feature):** `managed == nil` (first run) → every complete account pre-checked; any non-nil slice (incl. empty) → only listed folders.
- **UI strings English.** Git author must display as **Vin**; never a `Co-Authored-By` trailer.
- **Docs:** update FILELIST.md, CHANGELOG.md (`[Unreleased]`), README.md, README.zh-TW.md when the feature lands (Task 3).

---

### Task 1: HTML renderer + preselect helper (`cmd/mcs-tray/pickserver.go`)

Pure, fully-testable rendering. No server yet.

**Files:**
- Create: `cmd/mcs-tray/pickserver.go`
- Test: `cmd/mcs-tray/pickserver_test.go`

**Interfaces:**
- Consumes: `core.ScannedAccount`, `core.AccountTeam`, `core.AccountPersonal`, and the existing `short(uuid string) string` + `fmtDate(a core.ScannedAccount) string` helpers in `cmd/mcs-tray/rescan.go`.
- Produces:
  - `func computePreselect(accounts []core.ScannedAccount, managed []string) map[string]bool`
  - `func renderReviewHTML(accounts []core.ScannedAccount, preselected map[string]bool, token string) string`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"strings"
	"testing"
	"time"

	"github.com/miou1107/multi-claude-switcher/core"
)

func sampleAccounts() []core.ScannedAccount {
	return []core.ScannedAccount{
		{UUID: "11111111xxxx", Complete: true, HomeFolder: "Claude", Email: "first@example.com",
			Account: core.AccountTeam, Convos: 395, LastUpdated: time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC),
			Note: "Team account — conversations can't be synced"},
		{UUID: "22222222xxxx", Complete: true, HomeFolder: "Claude_Profile2", Email: "miou1107@gmail.com",
			Account: core.AccountPersonal, Convos: 395, LastUpdated: time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)},
		{UUID: "33333333xxxx", Complete: false, Convos: 21,
			LastUpdated: time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC), Note: "Invalid account data"},
	}
}

func TestComputePreselect(t *testing.T) {
	accts := sampleAccounts()
	// first run (nil) → all complete pre-checked, ghost excluded
	all := computePreselect(accts, nil)
	if !all["Claude"] || !all["Claude_Profile2"] || all["33333333xxxx"] || len(all) != 2 {
		t.Fatalf("first-run preselect: %#v", all)
	}
	// managed present → only listed
	some := computePreselect(accts, []string{"Claude"})
	if !some["Claude"] || some["Claude_Profile2"] || len(some) != 1 {
		t.Fatalf("managed preselect: %#v", some)
	}
	// present-but-empty → none
	none := computePreselect(accts, []string{})
	if len(none) != 0 {
		t.Fatalf("empty preselect should be none: %#v", none)
	}
}

func TestRenderReviewHTML(t *testing.T) {
	accts := sampleAccounts()
	pre := map[string]bool{"Claude": true}
	out := renderReviewHTML(accts, pre, "tok123")

	// complete rows: checkbox with folder value; Claude checked, Profile2 unchecked
	if !strings.Contains(out, `name="folder" value="Claude" checked`) {
		t.Error("Claude should have a checked checkbox")
	}
	if !strings.Contains(out, `name="folder" value="Claude_Profile2"`) ||
		strings.Contains(out, `value="Claude_Profile2" checked`) {
		t.Error("Profile2 should have an unchecked checkbox")
	}
	// ghost row: greyed, NO checkbox for it
	if !strings.Contains(out, `class="ghost"`) {
		t.Error("ghost row should be greyed")
	}
	if strings.Contains(out, `value="33333333xxxx"`) {
		t.Error("ghost must not be selectable")
	}
	// token hidden field, escaping, self-contained, Team badge, note
	for _, want := range []string{
		`name="t" value="tok123"`, "first@example.com", "🏢 Team",
		"Invalid account data", "<table", "Confirm", "Cancel",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("page missing %q", want)
		}
	}
	// no external resources
	if strings.Contains(out, "http://") || strings.Contains(out, "https://") || strings.Contains(out, "src=") {
		t.Error("page must be self-contained (no external refs)")
	}
	// note with an em-dash and apostrophe is HTML-escaped safely (no raw <script> etc. — sanity)
	if strings.Contains(out, "<script") {
		t.Error("unexpected script tag")
	}
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./cmd/mcs-tray/ -run 'TestComputePreselect|TestRenderReviewHTML' -v`
Expected: FAIL (undefined: computePreselect / renderReviewHTML).

- [ ] **Step 3: Implement `cmd/mcs-tray/pickserver.go`**

```go
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
```

- [ ] **Step 4: Run it, verify it passes**

Run: `go test ./cmd/mcs-tray/ -run 'TestComputePreselect|TestRenderReviewHTML' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/mcs-tray/pickserver.go cmd/mcs-tray/pickserver_test.go
git -c user.name="Vin" -c user.email="miou1107@gmail.com" commit -m "feat(tray): HTML renderer + preselect helper for the Rescan web UI"
```

---

### Task 2: Localhost server, orchestration, and browser opener

The single-shot server (testable via httptest), the `pickAccountsViaBrowser` orchestrator, and the per-OS `openURL`.

**Files:**
- Modify: `cmd/mcs-tray/pickserver.go` (append server code)
- Create: `cmd/mcs-tray/openurl_darwin.go`, `cmd/mcs-tray/openurl_windows.go`, `cmd/mcs-tray/openurl_other.go`
- Test: `cmd/mcs-tray/pickserver_test.go` (append httptest cases)

**Interfaces:**
- Produces:
  - `type pickResult struct { folders []string; ok bool }`
  - `func pickMux(page, token string, resCh chan<- pickResult) *http.ServeMux`
  - `func pickAccountsViaBrowser(accounts []core.ScannedAccount, managed []string) ([]string, bool)`
  - `func openURL(url string) error` (per-OS)

- [ ] **Step 1: Write the failing httptest**

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPickMux(t *testing.T) {
	resCh := make(chan pickResult, 1)
	mux := pickMux("<html>PAGE</html>", "good", resCh)

	// GET with correct token → 200 + page
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/?t=good", nil))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "PAGE") {
		t.Fatalf("GET good: code=%d body=%q", rr.Code, rr.Body.String())
	}
	// GET with wrong token → 403
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/?t=bad", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("GET bad token: code=%d", rr.Code)
	}
	// POST submit with token + two folders → resCh gets them, ok=true
	form := url.Values{"t": {"good"}, "folder": {"Claude", "Claude_Profile2"}}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, postForm("/submit", form))
	res := <-resCh
	if !res.ok || len(res.folders) != 2 || res.folders[0] != "Claude" {
		t.Fatalf("submit: %#v", res)
	}
	// POST cancel → ok=false
	mux2 := pickMux("x", "good", resCh) // fresh channel drain
	rr = httptest.NewRecorder()
	mux2.ServeHTTP(rr, postForm("/submit", url.Values{"t": {"good"}, "cancel": {"1"}}))
	if res := <-resCh; res.ok {
		t.Fatalf("cancel should be ok=false: %#v", res)
	}
	// POST with bad token → 403, nothing on channel
	rr = httptest.NewRecorder()
	mux2.ServeHTTP(rr, postForm("/submit", url.Values{"t": {"bad"}, "folder": {"X"}}))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("submit bad token: code=%d", rr.Code)
	}
}

func postForm(path string, form url.Values) *http.Request {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./cmd/mcs-tray/ -run TestPickMux -v`
Expected: FAIL (undefined: pickResult / pickMux).

- [ ] **Step 3: Append server + orchestrator to `cmd/mcs-tray/pickserver.go`**

Add these imports to the file's import block: `"context"`, `"crypto/rand"`, `"encoding/hex"`, `"net"`, `"net/http"`, `"time"`.

```go
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
```

- [ ] **Step 4: Create the per-OS `openURL`**

`cmd/mcs-tray/openurl_darwin.go`:

```go
//go:build darwin

package main

import "os/exec"

// openURL opens a URL in the default browser (non-blocking).
func openURL(url string) error { return exec.Command("open", url).Start() }
```

`cmd/mcs-tray/openurl_windows.go`:

```go
//go:build windows

package main

import "os/exec"

func openURL(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
```

`cmd/mcs-tray/openurl_other.go`:

```go
//go:build !darwin && !windows

package main

import "os/exec"

func openURL(url string) error { return exec.Command("xdg-open", url).Start() }
```

- [ ] **Step 5: Run the httptest + build all platforms**

Run: `go test ./cmd/mcs-tray/ -run TestPickMux -v && go build ./... && GOOS=windows GOARCH=amd64 go build ./...`
Expected: test PASS; both builds clean.

- [ ] **Step 6: Commit**

```bash
git add cmd/mcs-tray/pickserver.go cmd/mcs-tray/pickserver_test.go cmd/mcs-tray/openurl_darwin.go cmd/mcs-tray/openurl_windows.go cmd/mcs-tray/openurl_other.go
git -c user.name="Vin" -c user.email="miou1107@gmail.com" commit -m "feat(tray): single-shot localhost server + browser opener for Rescan web UI"
```

---

### Task 3: Wire `runRescan`, remove dead osascript code, docs

**Files:**
- Modify: `cmd/mcs-tray/rescan.go` (swap the two osascript calls; delete dead helpers)
- Modify: `cmd/mcs-tray/dialog_darwin.go`, `cmd/mcs-tray/dialog_other.go`, `cmd/mcs-tray/dialog_windows.go` (remove `confirmDialogMultiline` + `chooseMultipleFromList` and their stubs)
- Modify: `cmd/mcs-tray/rescan_test.go` (delete tests for removed helpers)
- Modify docs: `FILELIST.md`, `CHANGELOG.md`, `README.md`, `README.zh-TW.md`

- [ ] **Step 1: Rewire `runRescan` in `cmd/mcs-tray/rescan.go`**

Replace the body from the `if !confirmDialogMultiline(...)` line through the `selected, ok := chooseMultipleFromList(...)` / folder-mapping block with:

```go
	folders, ok := pickAccountsViaBrowser(accounts, core.LoadManaged())
	if !ok {
		return // cancelled, timed out, or closed
	}
	if err := core.SetManaged(folders); err != nil {
		notify("Rescan failed", err.Error())
		return
	}
	relaunchSelf()
```

The `plat.FindProfiles()` / `core.ScanAccounts` / empty-check lines above stay unchanged.

- [ ] **Step 2: Delete now-dead code**

- In `cmd/mcs-tray/rescan.go`, remove: `renderReviewTable`, `teamCell`, `pickLabel`, `selectablePick`. **Keep** `short` and `fmtDate` (used by `pickserver.go`). Remove the now-unused `"strings"` import if nothing else in the file needs it.
- In `cmd/mcs-tray/dialog_darwin.go`, remove `confirmDialogMultiline` and `chooseMultipleFromList`.
- In `cmd/mcs-tray/dialog_other.go` and `cmd/mcs-tray/dialog_windows.go`, remove the `confirmDialogMultiline` and `chooseMultipleFromList` stubs.
- In `cmd/mcs-tray/rescan_test.go`, remove `TestRenderReviewTable` and `TestSelectablePick` (and `TestSelectablePickUniqueLabelsPerFolder` if present) — they test deleted functions. Leave any test of `short`/`fmtDate` if present.

- [ ] **Step 3: Build + vet + full test (all platforms)**

Run: `go build ./... && go vet ./... && GOOS=windows GOARCH=amd64 go build ./... && go test ./...`
Expected: all clean/green. (This proves no dangling references to the removed functions on any platform.)

- [ ] **Step 4: Update docs**

- `FILELIST.md`: add `cmd/mcs-tray/pickserver.go`, `cmd/mcs-tray/pickserver_test.go`, `cmd/mcs-tray/openurl_darwin.go`, `cmd/mcs-tray/openurl_windows.go`, `cmd/mcs-tray/openurl_other.go`.
- `CHANGELOG.md` under `## [Unreleased]` → `### Changed`: "Rescan accounts now opens a real review table in your browser (checkboxes, Team badges, greyed-out unmanageable accounts) instead of the plain-text dialog, which was unreadable with many columns."
- `README.md` / `README.zh-TW.md`: update the Rescan description to mention it opens a browser page to review and choose accounts.

- [ ] **Step 5: Commit**

```bash
git add cmd/mcs-tray/rescan.go cmd/mcs-tray/rescan_test.go cmd/mcs-tray/dialog_darwin.go cmd/mcs-tray/dialog_other.go cmd/mcs-tray/dialog_windows.go FILELIST.md CHANGELOG.md README.md README.zh-TW.md
git -c user.name="Vin" -c user.email="miou1107@gmail.com" commit -m "feat(tray): use the browser web UI for Rescan; remove dead osascript picker"
```

---

## Post-plan verification

`go build ./... && go vet ./... && GOOS=windows GOARCH=amd64 go build ./... && go test ./... && go test -race ./cmd/mcs-tray/` all green, CGO-free. Then the on-device smoke: run the tray, Maintenance → Rescan accounts…, confirm a browser tab opens with a readable table (2 complete + 1 greyed ghost), pre-checked per the managed set, and that Confirm persists the checked set + rebuilds the menu while Cancel/closing the tab changes nothing.
