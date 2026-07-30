# Multi-Claude Switcher

<img src="docs/assets/icon.png" width="88" alt="Multi-Claude Switcher icon" align="right" />

Run several Claude Desktop accounts on one machine. No signing out, no retyping
passwords, and every account keeps its own Code history.

[![Download](https://img.shields.io/github/v/release/miou1107/multi-claude-switcher?label=download&style=flat-square)](https://github.com/miou1107/multi-claude-switcher/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue?style=flat-square)](LICENSE)
&nbsp; **English** | [繁體中文](README.zh-TW.md)

---

## Why you'd want this

Claude Desktop remembers one account at a time. Two situations make that hurt:

- **Out of quota.** You hit the cap on one account, and another one still has
  room to keep working.
- **Work and personal.** A company Team project and your own side projects,
  which you would rather not pile into the same account.

Doing it by hand means signing out, signing back in, waiting, and then finding
your **Code sidebar empty**, because that history went with the account you just
left. Enough friction that most people stop bothering.

**What this fixes:**

1. **One click to switch.** Click the icon, pick an account, confirm. Claude
   Desktop reopens on it.
2. **Always signed in.** Every account keeps its own session, so you never
   retype a password.
3. **Histories stay separate.** Each account's Code conversations are left
   exactly as they were. A plain switch never touches session data at all.

<img src="docs/assets/panel.png" width="400" alt="The panel listing three accounts: Work on a Team plan marked as the current account, Personal on Max 20×, and Side project on Pro, above Rescan and Settings buttons" />

---

## Install

> ⚠️ **Before anything else**
> You need the standalone [Claude Desktop](https://claude.com/download) build.
> **The Microsoft Store version is not supported**: it keeps its data somewhere
> virtualized that cannot be swapped out, which is the mechanism this whole tool
> depends on.

**macOS**

```bash
brew install --cask miou1107/tap/multi-claude-switcher
```

Or download `Multi-Claude-Switcher_<version>_macos.zip` from the
[latest release](https://github.com/miou1107/multi-claude-switcher/releases/latest),
unzip, and drag **Multi-Claude Switcher.app** into **Applications**.

*First launch only.* The app is ad-hoc signed but not notarized, since
notarizing needs a paid Apple Developer account. macOS will ask you to confirm
once. Either right-click the app and choose **Open**, then **Open** again in the
dialog. Or, if that dialog has no **Open** button (macOS 15 Sequoia and later),
go to **System Settings → Privacy & Security**, scroll down, and click **Open
Anyway**. After that it opens normally. Terminal alternative:

```bash
xattr -dr com.apple.quarantine "/Applications/Multi-Claude Switcher.app"
```

**Windows**

Download `Multi-Claude-Switcher_<version>_windows_setup.exe` from the
[latest release](https://github.com/miou1107/multi-claude-switcher/releases/latest)
and run it. It installs for your user only, so there is no administrator prompt.
Launch **Multi-Claude Switcher** from the Start Menu.

The panel is drawn with the **WebView2 Runtime**, which ships with Windows 11 and
with Windows 10 21H2 or newer. On anything older the app points you at
Microsoft's installer.

**Before any of this**, you need the standalone
[Claude Desktop](https://claude.com/download) build. The Microsoft Store version
is not supported: it keeps its data somewhere virtualized that cannot be swapped
out, which is the mechanism the whole tool depends on.

Updates install themselves in the background, on both platforms. You install
once.

## Using it

There is no window and no Dock icon. The app is a pair-of-eyes icon in the macOS
menu bar or the Windows system tray. Click it and the panel appears.

The panel lists your accounts with their plan and marks the one you are on.
Click a different one, confirm, and Claude Desktop restarts on it. That restart
is unavoidable: Claude Desktop reads its account data once, at launch, so there
is no way to move a running instance to a different account.

**Rescan** finds accounts already on the machine, including any you have not
signed into yet. **Rename** gives a profile a name you will actually recognise.
**Settings** holds the two toggles, a manual backup, update check, shortcuts to
the log and backup folders, and Quit.

Press **Esc** or click outside to close the panel. On Windows the tray icon
toggles it, and right-clicking gives you **Quit**, which is your way out if the
panel itself will not open.

## Moving conversations between accounts

Switching and syncing are separate. **A plain switch never touches session
data.** If all you want is to hop between accounts, you can stop reading here.

If you do want conversations to follow you, there are two ways:

**Sync sessions** copies one account's Code sessions into another, without
changing which account you are on. It backs the target up first, then puts you
back where you were.

**Auto Sync on switch** is off by default. Turn it on and every switch merges
both accounts' sessions in both directions, so the two converge over time. It
warns you the first time, because the merge is not undone by switching the
toggle back off.

Either way, if both accounts hold the same conversation in different states, the
more recently updated one wins and the other is reported rather than quietly
discarded. Every write is preceded by a timestamped snapshot. If that snapshot
fails, the write does not happen.

Only the Code tab syncs, and only its conversation **list**. Ordinary chat
conversations live on Anthropic's servers, one set per account, and nothing local
can move them. The Code transcripts are kept elsewhere again, and Claude Code
clears out old ones on its own schedule — so a conversation can sit in the list
with nothing left behind it. That is Claude's own housekeeping, which this tool
neither causes nor can undo.

## Team accounts only sync outwards

You can copy Code sessions **out of** a Claude Team account. You cannot copy them
**in**. A Team account gets its conversation list from Anthropic's servers, so
files placed in its local folder are never read, and no setting changes that.

The app labels Team accounts it recognises and warns you before an action that
would try to import into one. Recognition is best-effort. An account it cannot
classify is left unlabelled rather than labelled wrongly.

[What was tested, and why it behaves this way →](docs/team-accounts.md)

## FAQ and recovery

**Why do I only see one account in the list?**
Open the panel and run **Rescan**, then tick the accounts you want to manage.
Accounts you have never signed into are listed too: tick one, switch to it, and
sign in from there.

**(Windows) The panel will not open.**
Usually WebView2 is missing. Right-click the eyes icon in the tray, choose
**Quit**, install WebView2, and start the app again.

**My conversation history is a mess. Can I get the old one back?**
Yes. Settings → **Open backup folder** holds timestamped snapshots, kept per
profile. They are never pruned automatically, so delete old ones by hand if the
folder gets large.

**Where are the logs?**
Settings → **Open log folder**, or `~/.multi-claude-switcher/logs/`.

**How do I remove it completely?**

- **macOS**: delete the app, then `~/.multi-claude-switcher/` and
  `~/Library/LaunchAgents/com.miou1107.multi-claude-switcher.plist`.
- **Windows**: uninstall from Add/Remove Programs, then delete
  `%USERPROFILE%\.multi-claude-switcher`.

Removing the tool leaves your Claude Desktop accounts and their data untouched.

---

## Contributing and license

[Building from source →](docs/building.md) ·
[CLI reference →](docs/cli.md) ·
[How it works →](docs/how-it-works.md)

`FILELIST.md` describes every file in the repository. `CHANGELOG.md` has the
version history.

Licensed [MIT](LICENSE).

**Not affiliated with Anthropic.** All this tool does is launch Claude Desktop
against different local data directories and copy session files between them. It
never touches your credentials, and never uploads anything.
