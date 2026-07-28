# Windows WebView2 panel (parity with macOS)

Status: design accepted 2026-07-28. Shipped in v0.10.0; the tray-click and
panel-positioning sections were revised and reshipped in v0.10.1 (see the
"Resolved (v0.10.1)" notes below).

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

**Resolved (v0.10.1).** v0.10.0 shipped option 1 and the extra click was
visible in use, so it was replaced by a fourth option that had been missed:
`getlantern/systray` is unmaintained, and its maintained successor
**`fyne.io/systray`** already exposes the hook this spec wanted. Its
`systray_windows.go` calls `SetOnTapped`'s handler from `WM_LBUTTONUP` and
falls back to the context menu only when no handler is set, so left-click
opens the panel and right-click still surfaces a menu. That is option 2's
result without maintaining a fork. The public API is a superset of what this
repo used, so the migration was an import-path change plus `SetOnTapped`.

The panel toggles: clicking the icon while the panel is open closes it. The
click deactivates the panel (which exits it) before `WM_LBUTTONUP` reaches the
tray, so a short guard window after a panel exit suppresses the reopen that
would otherwise follow. See `shouldSpawnPanel`.

Right-click keeps a single **Quit** item rather than matching macOS's
no-menu-at-all. Rationale: when the WebView2 Runtime is absent the panel
cannot open, and the panel's own Quit is then unreachable — without the tray
item the app could only be stopped from Task Manager.

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

**Resolved (v0.10.1).** `Shell_NotifyIconGetRect` was dropped: it needs the
icon's owning window handle and id, which `systray` does not expose, and the
panel is a separate process that could not receive them anyway. Instead the
tray reads `GetCursorPos` at click time — the cursor is on the icon — and
passes it to the panel as `--anchor X,Y`. The panel resolves the work area of
the monitor containing that point (`MonitorFromRect` + `GetMonitorInfoW`,
`MonitorFromRect` rather than `MonitorFromPoint` because it takes its argument
by pointer and so needs no per-arch struct packing), centres itself
horizontally on the click, opens upward or downward depending on which half of
the monitor was clicked, and clamps to the work area. Because the work area
already excludes the taskbar, this handles a taskbar on any edge, secondary
monitors (including negative coordinates), and monitors smaller than the panel
without special cases. See `panelPlacement`, which is pure and unit-tested.

The move keeps setting the size to `panelWidth`×`panelHeight` and adds
`SWP_FRAMECHANGED`. Both matter, and an attempt to "improve" this with
`SWP_NOSIZE` during v0.10.1 development is worth recording as a trap:
`wv.SetSize` runs while the window still has a frame, so it calls
`AdjustWindowRect` and makes the window 416×579 for a 400×540 client.
`makeBorderlessTopmost` then removes the frame, at which point that slack
becomes bare client area the WebView2 control does not cover, and it paints
**black down the right and bottom edges**. Resizing back to 400×540 is what
makes client == control again, and the resulting `WM_SIZE` is what go-webview2
listens for to resize the control.

Note for anyone adding per-monitor DPI awareness later: the anchor crosses a
process boundary, so the tray and the panel must keep the *same* awareness
mode or the anchor lands in the wrong coordinate space. They are the same
binary today, and neither ships a DPI manifest, which is what makes this safe.

### First-paint flash

go-webview2 creates its window, calls `ShowWindow` on it, and only then embeds
the browser, which takes long enough that the empty window is visible at the
system default position before anything can move it. The handle is not
available until `NewWithOptions` returns, `WindowOptions` has no position or
visibility field, and `WebViewOptions.Window` (which would let a caller supply
their own window) is accepted but never read in this version.

So a goroutine started *before* the WebView is created watches for the window
by class name and parks it at −32000,−32000. It is **parked, not hidden**, and
both halves of that matter:

- Hiding is too weak. The library's own `ShowWindow`/`UpdateShowWindow`/
  `SetFocus` land right after creation and can win the race, so the window
  blinks at the default position anyway. Moving sticks, because nothing in the
  library ever moves the window (`SetSize` passes `SWP_NOMOVE`).
- Hiding is also too strong, and this one cost a QA round: **WebView2 stops
  rendering while its host window is hidden, and does not resume when the
  window is shown again.** The result is a panel that passes every check a test
  can make from outside (right process, right position, right size, visible,
  topmost) and is completely blank on screen. Verification therefore has to
  screenshot the window rect, not just query it.

The window stays visible and rendering the whole time, off where no monitor
covers it, and the panel becomes visible to the user in one step: a
`SWP_NOSIZE` move into place, followed by the foreground handover. The size is
applied earlier, while parked, so the browser has re-laid out before anything
is on screen.

The same watcher also applies `WS_EX_TOOLWINDOW` the instant the window
appears. The library creates a plain `WS_OVERLAPPEDWINDOW`, which earns a
taskbar button; leaving the style change to the main styling step meant a
button appeared and disappeared again on every open, for the ~750 ms the
browser took to embed. The shell adds buttons asynchronously, so setting the
style this early beats it.

**Verifying this class of bug.** Pixel-diffing the taskbar does not work: it is
translucent (no uniform background to key on), the running-underline blinks as
unrelated processes come and go, and the hover/active highlight moves whenever
focus changes, which opening the panel necessarily does. What does work is an
A/B capture: run with the fix disabled and with it enabled, stack the taskbar
frames from the launch window into one image, and compare. The offending button
is unmistakable across four consecutive frames in the "disabled" strip and
absent in the other.

The window class is registered per-process, so it can be found by name from
inside the panel process but not from an external test harness; verification
has to enumerate windows by owning pid.

### Auto-close on outside click

Match NSPopover's transient behavior. On the panel window, install a
`WM_ACTIVATE` handler:

- `WA_INACTIVE` → hide the window (do not destroy; the WebView is expensive
  to recreate).
- `WA_ACTIVE` / `WA_CLICKACTIVE` → no-op.

**Resolved (v0.10.1): the foreground handover.** Opening the panel on a single
tray click exposed a race the two-click route hid. Immediately after a tray
click the shell owns the foreground, so a *separate process* asking for it is
refused: the panel's `SetForegroundWindow` failed, Windows deactivated the
window, and the `WA_INACTIVE` handler read that as an outside click and
dismissed the panel before the user saw anything. With the old "Show panel"
menu item the foreground had already settled by the time the panel spawned,
which is why v0.10.0 never hit this.

Two changes, both needed:

1. The tray calls `AllowSetForegroundWindow(panelPid)` right after spawning.
   This is the supported way to hand the foreground right to another process,
   and the tray is entitled to do it because the click made it the foreground
   process.
2. The panel arms its `WA_INACTIVE` dismissal only after confirming (by
   polling `GetForegroundWindow`) that it actually reached the foreground.
   Arming on `WM_ACTIVATE` instead does not work: the window is created,
   focused and hidden before the message hook is installed, so re-showing it
   need not produce a fresh `WM_ACTIVATE` for the hook to see.

If the panel never reaches the foreground the rule stays disarmed on purpose.
A window that was never active will not receive a genuine deactivation either,
and Esc still closes the panel.

This is the cost of the two-process split. A single-process panel (the
"reuse the panel instead of respawning" item) would not have the problem at
all, and is the obvious direction if more focus bugs turn up.

**Diagnosability.** The panel is a GUI-subsystem process with no console, so a
panel that started but never appeared left only its startup banner in the log.
It now logs its window rect, visibility, foreground state and `WM_ACTIVATE`
transitions. Both bugs above were invisible without it.

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
