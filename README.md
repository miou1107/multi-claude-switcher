# Multi-Claude Switcher

Switch between multiple Claude Desktop accounts from your menu bar or system
tray — without signing in again, and without losing your Code conversation
history. macOS and Windows.

<img src="docs/assets/icon.png" width="96" alt="Multi-Claude Switcher icon" align="right" />

[![Download](https://img.shields.io/github/v/release/miou1107/multi-claude-switcher?label=download&style=flat-square)](https://github.com/miou1107/multi-claude-switcher/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue?style=flat-square)](LICENSE)
&nbsp; **English** | [繁體中文](README.zh-TW.md)

**Install**

- **macOS** — `brew install --cask miou1107/tap/multi-claude-switcher`
  <br>or [download the app](https://github.com/miou1107/multi-claude-switcher/releases/latest) and drag it to Applications ([one-time Gatekeeper step](#macos))
- **Windows** — [download the installer](https://github.com/miou1107/multi-claude-switcher/releases/latest) and run it. Per-user, no administrator prompt.

Requires the standalone [Claude Desktop](https://claude.com/download) build. The
Microsoft Store (MSIX) version is **not supported** — it stores its data in a
virtualized location that cannot be swapped. Updates install themselves, so you
only install once.

## What it does

- **Switch accounts in one click** — no re-authenticating, no losing the
  conversation sidebar. Each account stays signed in on its own.
- **Back up before it writes** — any operation that touches session data takes a
  timestamped snapshot first, and aborts rather than overwrite unprotected data.
- **Sync Code sessions between accounts** — optional, off by default, and
  conflict-safe: if both sides changed the same session, the newer copy is kept
  and the clash is reported instead of silently overwritten.
- **Find accounts you already have** — Rescan looks for Claude accounts on the
  machine and lets you pick which ones to manage, including ones you have not
  signed into yet.

## Switching vs. syncing

These are two separate actions. **A plain switch never touches session data**
unless you turn on Auto Sync.

- **Plain switch (default).** Picking an account closes Claude Desktop and
  reopens it on that account. No session data moves; each account keeps its own
  Code history.
- **Manual sync.** Pick a direction (e.g. *Work → Personal*) to copy one
  account's Code sessions into another **without changing which account you are
  on**. It closes Claude Desktop, backs up the target, copies, and reopens the
  account you were already using.
- **Auto Sync on switch (default OFF).** Every switch merges both accounts' Code
  sessions in both directions, so the two converge over time. Turning it on
  shows a one-time warning, since it merges one account's conversations into the
  other.

Both switching and syncing close and reopen Claude Desktop — that is how a
different account gets loaded. Only the Code tab (`claude-code-sessions`) syncs;
regular chat conversations live on Anthropic's servers per account and cannot be
synced locally.

## Install

### macOS

```bash
brew install --cask miou1107/tap/multi-claude-switcher
```

Or install by hand: download `Multi-Claude-Switcher_<version>_macos.zip` from
the [latest release](https://github.com/miou1107/multi-claude-switcher/releases/latest),
unzip it, and drag **Multi-Claude Switcher.app** into **Applications**.

**First launch only — clear Gatekeeper once.** The app is ad-hoc signed but not
notarized (notarizing requires a paid Apple Developer account), so macOS asks
you to confirm the first time. Either:

- **Right-click** the app → **Open**, then **Open** in the dialog, or
- if that dialog has no **Open** button (macOS 15 Sequoia and later): open
  **System Settings → Privacy & Security**, scroll down, and click **Open
  Anyway**.

After that, just double-click. (Terminal alternative: `xattr -dr
com.apple.quarantine "/Applications/Multi-Claude Switcher.app"`.)

### Windows

Download `Multi-Claude-Switcher_<version>_windows_setup.exe` from the
[latest release](https://github.com/miou1107/multi-claude-switcher/releases/latest)
and run it. It installs per-user (no administrator prompt), adds a Start Menu
shortcut, and registers an entry in Add/Remove Programs. Then launch
**Multi-Claude Switcher** from the Start Menu.

The panel needs the **WebView2 Runtime**, already present on Windows 11 and on
Windows 10 21H2 or newer. On older systems the app shows a dialog with a link to
Microsoft's installer.

### Uninstalling

- **macOS** — delete **Multi-Claude Switcher.app**, then `~/.multi-claude-switcher/`
  and `~/Library/LaunchAgents/com.miou1107.multi-claude-switcher.plist`.
- **Windows** — uninstall from Add/Remove Programs, then delete
  `%USERPROFILE%\.multi-claude-switcher`.

Removing the app never touches your Claude Desktop accounts or their data.

## Using it

The app has no Dock icon and no window. It sits in the **macOS menu bar** (top
right) or the **Windows system tray** (bottom right — you may need the "show
hidden icons" arrow) as a pair-of-eyes icon. Click it to open the panel.

The panel lists your accounts with their subscription plan and marks the one in
use; clicking another switches to it after a confirmation. **Rescan**, **Sync**,
**Rename** and **Settings** all live inside it. Settings holds the Auto Sync and
**Start at login** toggles, a manual backup, **Check for updates**, shortcuts to
the log and backup folders, and Quit.

Click outside the panel or press **Esc** to close it. On Windows, clicking the
tray icon again also closes it, and right-clicking the icon offers **Quit** —
the one way out if the panel itself cannot open.

## Team accounts are export-only

Session sync can **export out of** a Claude Team account, but **cannot import
into** one. A Team account's Code conversation list is fetched from Anthropic's
servers, so session files copied into its local folder are ignored and never
appear — there is no setting that changes this.

The app tags a detected Team account and warns you before an action that would
try to import into it. Detection is best-effort: an account it cannot classify
is left untagged rather than mislabeled.

[Full test results and mechanism →](docs/team-accounts.md)

## Troubleshooting

**Nothing to switch between / only one account listed.** Open the panel →
**Rescan** and check the accounts you want to manage. Accounts you have not
signed into yet are listed too — select one, switch to it, then sign in.

**"Not supported" with the Microsoft Store build.** Install the standalone
Claude Desktop from [claude.com/download](https://claude.com/download) instead.

**The panel does not open on Windows.** The WebView2 Runtime is missing —
right-click the tray icon → **Quit**, install WebView2, and start the app again.

**Something went wrong and I want my sessions back.** Settings → **Open backup
folder**. Snapshots are timestamped per profile. Note that they are never
pruned, so the folder grows over time; deleting old ones by hand is safe.

**Where are the logs?** Settings → **Open log folder**, or
`~/.multi-claude-switcher/logs/`.

## Contributing

[Building from source →](docs/building.md) · [CLI reference →](docs/cli.md) ·
[How it works →](docs/how-it-works.md)

`FILELIST.md` describes every file in the repository; `CHANGELOG.md` has the
version history.

## License

[MIT](LICENSE).

Not affiliated with or endorsed by Anthropic. It launches Claude Desktop against
different data directories and copies session files between them; it never sees
your credentials.
