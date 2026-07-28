# Windows WebView2 panel (parity with macOS)

Status: design accepted 2026-07-28. Implementation to follow on `main`.

## Goal

Bring the Windows tray UX to full **visual parity** with the macOS menu-bar
panel that shipped in v0.9.0–v0.9.1. Today the two look nothing alike:

- **macOS (`cmd/mcs-menubar`)** — NSPopover + WKWebView, styled 400×540 HTML
  panel: card list with plan badges (`Max 20×`, `🏢 Team`, `Pro`, …),
  confirmation modal before switching, in-panel Rescan / Sync / Rename /
  Settings, background self-update banner.
- **Windows (`cmd/mcs-tray`, current)** — `getlantern/systray` context menu:
  Profile submenus with `Switch / Rename…`, Sync submenu, Settings > Start at
  Login, Maintenance > Backup / Open Backups / Open Logs / Check for Updates /
  Rescan…, About, Quit. Plain text throughout.

After this change, clicking the Windows tray icon opens the same styled
panel — same cards, same badges, same confirmation modal, same in-panel
navigation — driven by the same Go renderer.

## Non-goals

- No new features. Behavior parity is v0.9.1; this spec is only about the UI
  shell.
- No refactor of `core/` or `platform/` beyond what the new panel host needs.
- MSIX (Windows Store) build submission is deliberately deferred. Same code
  path applies, but re-submitting the Store package is a separate release.

## Architecture

### Shared renderer

Today `cmd/mcs-menubar/render.go` and `cmd/mcs-menubar/update.go` carry
`//go:build darwin`. The panel HTML/CSS/JS is macOS-only by build constraint,
not by content.

- Move the renderer out of `cmd/mcs-menubar` into a new package
  `internal/panelui/` (or similar):
  - `render.go` (view functions, shell, all HTML/CSS/JS emission) —
    **no build tag**.
  - `viewmodel.go` (the `profileVM`, `settingsVM` types).
- `cmd/mcs-menubar` (darwin) becomes a thin host: NSPopover + WKWebView + JS
  bridge that calls `panelui.RenderList(...)` etc.
- `cmd/mcs-tray` (windows) becomes a thin host: WebView2 window + `Bind` JS
  bridge, calling the same `panelui` functions.

Both hosts push identical HTML into their respective webviews. Adding a new
view or badge means editing `panelui/render.go` once.

### JS bridge

The panel currently calls `window.webkit.messageHandlers.mcs.postMessage(...)`.
Rewrite the `send()` shim in `shell()` to feature-detect:

```js
function send(a, arg) {
  if (window.mcsAction) { window.mcsAction(a, arg || ''); return; }
  window.webkit.messageHandlers.mcs.postMessage({action: a, folder: arg || ''});
}
```

- macOS: unchanged path.
- Windows: `go-webview2`'s `w.Bind("mcsAction", func(action, folder string) { ... })`
  exposes a Go function as `window.mcsAction(...)` in JS. The Go side dispatches
  identically to the mac `goPanelAction`.

The action dispatcher stays per-host. Some actions are platform-specific
(quit-app path, native "open folder" call, notification API), so a shared
dispatcher would be more indirection than it's worth. Each host has its own
`goPanelAction` equivalent; both call into the same `panelui` view functions
and the same `core` operations.

### Windows panel host

New file, e.g. `cmd/mcs-tray/panel_windows.go`:

- Create a `webview2.WebView` with `w.SetSize(400, 540)`.
- Set the window to **borderless, topmost, no taskbar entry**
  (`WS_POPUP | WS_EX_TOOLWINDOW | WS_EX_TOPMOST` on the underlying HWND).
- `HTMLString` load the rendered HTML on show.
- Bind `mcsAction` for JS → Go.

### Tray icon click → panel

Start with `getlantern/systray` for the icon (already integrated); a full
replacement is only on the table if none of the three click-hook options
below land cleanly. Windows convention:

- **Left click** on tray icon → show the panel (or hide if already showing).
- **Right click** → minimal native context menu with **Show panel** and
  **Quit**. The Sync / Settings / Maintenance / About items disappear from
  the tray menu — they are all reachable inside the panel.

`getlantern/systray` does not expose a left-click callback out of the box.
Spike first (5 minutes): if there is no clean hook, options in order of
preference are:

1. Set the tray icon's menu to a single "Show panel" item — a plain left
   click opens the same menu, and its lone item runs the show. Slight
   friction vs. mac (one extra click) but zero dependencies.
2. Fork/patch `systray` to expose left-click. Small patch, vendorable.
3. Drop `systray` and drive `Shell_NotifyIcon` directly with a hidden window
   class that receives `WM_LBUTTONUP` / `WM_RBUTTONUP`. Most control, most
   code.

Choose during implementation; document the choice in a code comment.

### Panel positioning

Position the panel above the tray icon so it opens near where the user
clicked, mirroring how NSPopover attaches to the status button on mac.

- Call Win32 `Shell_NotifyIconGetRect` with the tray icon's
  `NOTIFYICONIDENTIFIER` to get the icon's screen rectangle.
- Place the panel's top-right corner at the icon's top-right.
- Fallback (rare — icon hidden in the overflow flyout or Windows returns
  no rect): bottom-right of the primary monitor's work area, above the
  taskbar, with a small margin.

Adjust so the panel never overflows the work area; nudge left/up if needed.

### Auto-close on outside click

Match NSPopover's transient behavior. On the panel window, install a
`WM_ACTIVATE` handler:

- `WA_INACTIVE` → hide the window (do not destroy; the WebView is expensive
  to recreate).
- `WA_ACTIVE` / `WA_CLICKACTIVE` → no-op.

Escape key inside the panel also hides it (JS already listens for Escape,
just wire it to a new bind `mcsAction("hidePanel")` that calls the host to
hide the window).

## Runtime dependency

WebView2 Evergreen Runtime is preinstalled on Windows 11 and on Windows 10
21H2+ via Windows Update. Assume presence:

- Installer (`packaging/windows-setup.iss`) does **not** bundle
  `MicrosoftEdgeWebview2Setup.exe`. Setup.exe stays lean (~7 MB).
- At panel-init time, if `webview2.New(...)` returns an error indicating the
  runtime is missing, show a native `MessageBox` with:
  > *"WebView2 runtime is not installed. Click Install to download it from
  > Microsoft."*
  and open `https://developer.microsoft.com/en-us/microsoft-edge/webview2/`
  (or the stable Evergreen Standalone Installer URL) in the default browser.
- MSIX build: same policy. MSIX targets modern Windows 10/11 where the
  runtime is preinstalled.

## Renderer changes required

None to the HTML/CSS itself — the mac panel already sizes and styles
correctly for a 400×540 popup with light-theme colors.

Minor changes to the JS in `panelui/render.go` shell:

- Feature-detect bridge (see JS Bridge above).
- Add a `hidePanel` action, wired on Escape when no modal is open (currently
  Escape only closes the confirm modal). Do NOT change modal-Escape behavior.

## Testing

- **Renderer**: keep the render as string-return functions. Add a small
  test that snapshots a rendered list contains the expected buttons,
  badges, and the confirm-modal HTML — one snapshot suffices for both
  hosts because they consume the same output.
- **Panel host (windows)**: the WebView2 layer cannot be unit-tested in CI
  without a Windows runner + display. Integration test on developer
  machine only:
  - Click tray icon → panel opens near icon.
  - Click a non-current account → confirm modal appears with the account
    name, buttons work, Esc cancels.
  - Click outside panel → panel hides. Click tray again → panel reopens.
  - Rescan / Rename / Sync / Settings all render as on mac.
  - Kill WebView2 runtime, restart app, click tray → native MessageBox
    with install offer.
- **Regression**: the existing macOS panel continues to work — same
  binary, same HTML. Snapshot test guards against accidental drift.

## Risk

- **No Windows machine handy for iteration.** Every WebView2, tray-click,
  and positioning bug requires a QA cycle with Vin. The spike phase (tray
  left-click hook + basic WebView2 window) is the highest-risk section.
- **Old getlantern/systray**. If none of the three tray-click options land
  cleanly, we may need a small fork. Not a blocker but adds maintenance.
- **`WM_ACTIVATE` handler via CGO or syscall**. `go-webview2` exposes the
  HWND; installing a subclass procedure on it needs `syscall` calls into
  `user32.dll` (`SetWindowLongPtrW`, `CallWindowProcW`). Standard pattern,
  no CGO required.

## Out of scope for this spec

- Dark-theme styling (the mac panel is light-theme only today; keeping
  parity means Windows also light-theme).
- Windows tray icon replacement or theming.
- Any change to the Windows Store MSIX submission cadence.
