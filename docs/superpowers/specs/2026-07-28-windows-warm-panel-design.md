# Windows: keep the panel warm

Status: accepted 2026-07-28. Follows
[the WebView2 panel design](2026-07-28-windows-webview2-panel-design.md).

## Problem

Clicking the tray icon takes 1.5–2 s to produce a panel. Nearly all of it is
cold start: a fresh `mcs-tray.exe --panel` process, a fresh WebView2
environment, a fresh HTML load, on every single open. Everything else about the
panel now behaves, but a menu-bar-style popup that takes two seconds does not
feel like one.

The cost is inherent to spawning a process per open. It cannot be tuned away.

## Approach

Spawn the panel **once**, when the tray starts, and keep it alive parked
off-screen. A tray click stops being "start a program" and becomes "move a
window that already exists", which is instant.

This is deliberately not the other option on the table, folding the panel into
the tray process. That would remove the second process entirely, but the two
were split precisely so the WebView2 message loop would not fight systray for
the main thread, and unpicking that is a much larger change for the same
user-visible result.

### Lifecycle

| Event | Behaviour |
| --- | --- |
| Tray starts | Spawns the panel with no anchor. It builds its WebView, loads the account list, and parks off-screen. |
| Tray icon clicked | Tray sends `SHOW x,y`. The panel refreshes its content, moves into view and takes the foreground. |
| Click outside the panel / Esc | The panel parks off-screen again. It does **not** exit. |
| Tray icon clicked while shown | The click deactivates the panel, which parks itself. The tray's reopen guard suppresses the immediate re-show, so the icon toggles. |
| Panel process dies | The tray respawns it, with backoff, so a crash loop cannot hammer the machine. |
| Quit from the panel | Panel sends `MCS_QUIT` and exits; the tray follows. |
| Quit from the tray menu | The tray kills the panel before exiting, so nothing is orphaned. |
| Tray killed outright (Task Manager, crash) | The panel's stdin closes, and it exits on that. Otherwise it would linger as an invisible orphan holding a WebView2 open with no way to reach it. |

### Protocol

Line-based over the pipes the tray already owns. The tray writes to the panel's
stdin, the panel writes to its stdout.

- tray → panel: `SHOW <x>,<y>` — come into view anchored at that screen point.
- panel → tray: `MCS_SHOWN` — now visible.
- panel → tray: `MCS_HIDDEN` — parked itself.
- panel → tray: `MCS_QUIT` — the user chose Quit; the tray should quit too.

The tray tracks shown/hidden from these messages, which is what makes the
toggle exact rather than inferred.

### Content freshness

A warm panel outlives the state it displays: accounts can be renamed, the
active profile can change, another tool can add a profile.

The refresh happens **on park, not on show**. Rebuilding the account list is
not free — it scans profile directories and reads each one's stored plan — and
an early version that refreshed before moving the window measured 650 ms from
click to panel, which defeats the point of the change. So `parkPanel` resets
the view to the account list and re-renders on the way out, while nobody is
looking, and `showPanelAt` moves the window immediately and kicks off a
background refresh to catch anything that changed since. Measured 4–30 ms from
click to panel.

Resetting the view on park has a second benefit: the panel always reopens on
the account list rather than on whatever screen the user last left it on
(Settings, Rename, a sync result).

Re-rendering also serves a third purpose. WebView2 may throttle a window that
has been off-screen for a long time; pushing fresh HTML forces a repaint
regardless.

### Standalone launch

`mcs-tray.exe --panel --anchor X,Y` still shows the panel immediately, without
waiting for a `SHOW`. This keeps the panel testable on its own, and the two
paths converge after the first show.

## Cost

**Measured: ~314 MB resident**, across the panel process and the six
`msedgewebview2.exe` processes WebView2 spawns underneath it, for the lifetime
of the tray. The estimate this design was accepted on was ~100 MB, which
counted only the Go process; the browser is the real cost and it does not shrink
while parked.

`ICoreWebView2Controller.PutIsVisible(false)` would be the lever to make a
parked panel cheaper, but go-webview2 keeps the controller unexported, so
reaching it means forking the library.

If that resident cost turns out not to be worth it, the next step down is an
idle timeout: keep the panel warm for a few minutes after it was last used and
let it exit after that. The first click after an idle period pays the full cold
start again; everything within the window stays instant.

## Testing

The panel's window behaviour cannot be unit-tested (no display in CI), so the
same harness as the previous change applies: drive a real tray with a simulated
tray-icon click, then assert on the window from outside. What must hold:

- First click after tray startup opens the panel with no perceptible delay.
- Content renders — assert on a screenshot of the window rect, not on window
  metadata. A blank panel passes every metadata check.
- Show, park, wait, show again: content is still rendered on the second show
  (guards against WebView2 throttling a long-parked window).
- The panel process count stays at one across many open/close cycles.
- Killing the panel makes the tray respawn it.
- Killing the tray outright leaves no orphan panel.
