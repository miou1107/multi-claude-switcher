# Design Spec — Rescan review/pick as a local web UI

- Date: 2026-07-24
- Status: Design finalized, pending implementation plan
- Supersedes the UI half of: `2026-07-24-account-rescan-design.md` (§4.4 two-step osascript)
- Related code: `cmd/mcs-tray/rescan.go`, `cmd/mcs-tray/dialog_darwin.go`

## 1. Problem

The shipped Rescan review used two `osascript` dialogs: a text "table" (`display
dialog`) then a `choose from list` picker. macOS dialogs render in a **proportional
font**, so the space-aligned 7-column table collapses into an unreadable, wrapping wall
of text (confirmed on-device by the user). `osascript` cannot render a real table,
per-row styling, or disabled rows.

**Goal:** replace the two-step text UI with a **real visual UI** — a browser page served
by an ephemeral localhost HTTP server — that shows a proper table (checkboxes, Team
badge, greyed-out unselectable ghost rows) and a Cancel/Confirm action, returning the
selected folders to the tray.

**Non-goal:** changing the scan/dedup/completeness model, the `managed.json` registry,
or any behavior outside the review/pick step. This is a UI replacement only.

## 2. Approach

A Go tray app has no GUI toolkit; the lightweight way to a real UI is a **browser page
served from an ephemeral localhost server**, closed as soon as the user acts.

### 2.1 Flow (replaces `runRescan`'s two osascript calls)

1. Rescan menu → `core.ScanAccounts(...)` (unchanged).
2. Start `http.Server` bound to `127.0.0.1:0` (OS-assigned free port). Mint a
   single-use random token.
3. `openURL("http://127.0.0.1:<port>/?t=<token>")` opens the default browser.
4. `GET /?t=<token>` serves the review page (a real `<table>`: one row per account,
   a checkbox for each **complete** account pre-checked per the managed set / first-run
   rule, a 🏢 Team badge, ghost rows greyed out and non-selectable, Cancel + Confirm
   buttons).
5. **Confirm** → the form POSTs the checked folder names to `POST /submit?t=<token>`;
   the handler sends them on a channel, responds with a "done — you can close this tab"
   page, and shuts the server down.
6. **Cancel** → POSTs a cancel marker → returns `ok=false`.
7. The tray blocks on the result channel with a **3-minute timeout**; a timeout or a
   closed tab (no submit) yields `ok=false` and no change.
8. On `ok`: `core.SetManaged(folders)` then `relaunchSelf()` (unchanged).

Cancel, timeout, tab-close, and server error all mean "no change" — identical safety to
the previous cancel semantics.

## 3. Architecture

### 3.1 Pure renderer — `cmd/mcs-tray/pickserver.go`

`renderReviewHTML(accounts []core.ScannedAccount, preselected map[string]bool) string`
— a pure function returning a **self-contained** HTML document (inline CSS, no external
assets, theme-aware via `prefers-color-scheme`). Fully unit-testable.

- One `<tr>` per account, columns: UUID (short), Status, Email, Team, Chats,
  Last updated, Note (same 7 fields, now real columns).
- Complete accounts get a `<input type="checkbox" name="folder" value="<HomeFolder>">`,
  `checked` when `preselected[HomeFolder]`.
- Ghost/incomplete rows: greyed (`.ghost` class), **no checkbox** (not selectable), note
  shows "Invalid account data".
- 🏢 Team badge on Team rows; the Team note surfaces inline.
- `HomeFolder` is the checkbox value (the manage unit); the same email/UUID across two
  folders yields two distinct rows/checkboxes.
- HTML-escape all account-derived strings (email, note) to prevent markup injection.

### 3.2 Server + orchestration — `cmd/mcs-tray/pickserver.go`

`pickAccountsViaBrowser(accounts []core.ScannedAccount, managed []string) (folders []string, ok bool)`:

1. Compute `preselected` (reuse the first-run/managed logic: `managed == nil` → all
   complete pre-checked; else only listed folders).
2. `net.Listen("tcp", "127.0.0.1:0")`; read the assigned port; mint a token via
   `crypto/rand`.
3. `http.Server` with a mux:
   - `GET /` → validate token, serve `renderReviewHTML(...)`.
   - `POST /submit` → validate token, parse `r.Form["folder"]` (or a `cancel` field),
     send `{folders, ok}` on a buffered channel, respond with a close-tab page.
   - Any request with a bad/missing token → 403.
4. `go srv.Serve(listener)`; `openURL(url)`; on `openURL` error, `notify` the URL so the
   user can paste it manually.
5. `select { case res := <-ch: ...; case <-time.After(3*time.Minute): ok=false }`, then
   `srv.Shutdown(ctx)` in all paths. Return `res.folders, res.ok`.

### 3.3 Browser opener — per-OS

`openURL(url string)` — macOS `open <url>`; Windows `rundll32 url.dll,FileProtocolHandler`
(or `cmd /c start`); Linux `xdg-open`. Build-tagged like the existing tray icon/dialog
splits; macOS is the tested path.

### 3.4 `runRescan` change

Replace the `confirmDialogMultiline(renderReviewTable(...))` + `chooseMultipleFromList(...)`
block with:

```
folders, ok := pickAccountsViaBrowser(accounts, core.LoadManaged())
if !ok { return }
if err := core.SetManaged(folders); err != nil { notify("Rescan failed", err.Error()); return }
relaunchSelf()
```

### 3.5 Dead-code removal

`renderReviewTable`, `confirmDialogMultiline`, `chooseMultipleFromList` (and their
non-darwin stubs) become unused → remove them. `pickLabel`/`selectablePick` are replaced
by the HTML renderer + a small preselect helper; remove or repurpose. `notify`,
`infoDialog`, `osaQuote`, etc. stay (still used).

## 4. Security

- Bind **127.0.0.1 only** (never 0.0.0.0) — not reachable off-box.
- **Single-use random token** (≥128-bit, `crypto/rand`) in the URL, validated on every
  request; without it any local process/page could POST to the port. Token never logged.
- Server is **single-shot**: it shuts down on first successful submit or on timeout.
- Serves only two routes; unknown paths 404, bad token 403.

## 5. Dependencies & platform

- Stdlib only: `net`, `net/http`, `crypto/rand`, `html`, `time`, `context`. No CGO, no
  new modules.
- The server + renderer are cross-platform; only `openURL` is per-OS. Windows/Linux get
  a working `openURL`; the feature is otherwise mac-first (consistent with the project).

## 6. Testing

- **Renderer (pure):** table test on `renderReviewHTML` — complete rows have a checkbox
  with the correct `value` and `checked` state; ghost rows have `.ghost` and no
  checkbox; Team badge present iff Team; email/note HTML-escaped; the doc is
  self-contained (no `http://`/`src=` external refs).
- **Preselect helper:** `managed == nil` → all complete pre-checked; non-nil (incl.
  empty) → only listed.
- **Token/route handling:** a focused httptest — `GET /` with the right token serves the
  page and a wrong/absent token gets 403; `POST /submit` with the token returns the
  parsed folders on the channel; a `cancel` submit yields `ok=false`.
- The browser-open and full server lifecycle (real port, real `open`) is the manual
  on-device smoke test.

## 7. Known limitations (accepted)

1. Opens the user's default browser (a tab), not an in-app window — the pragmatic cost of
   a real UI without a GUI toolkit.
2. If no browser is available or `open` fails, the flow falls back to printing the URL via
   a notification; the user opens it manually.
3. The 3-minute timeout abandons an un-acted review; the user just re-runs Rescan.

## 8. Design History

- The shipped two-step osascript UI was unreadable (proportional font, 7 columns wrapping)
  — the user rejected it on sight and asked for a real visual UI.
- User chose the **local-HTML-in-browser** approach (previously deferred in
  `2026-07-24-account-rescan-design.md` §5 for round-trip complexity) over a
  reformatted-text quick fix — the round-trip is solved cleanly by an ephemeral,
  token-guarded, single-shot localhost server.
