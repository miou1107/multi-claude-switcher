# multi-claude-switcher

**English** | [繁體中文](README.zh-TW.md)

<img src="docs/assets/icon.png" width="120" alt="Multi-Claude Switcher icon" />

<img width="236" height="277" alt="image" src="https://github.com/user-attachments/assets/62f863dd-9545-4a5c-ac46-66c32517f21f" />

<img width="2457" height="829" alt="image" src="https://github.com/user-attachments/assets/fa5eef07-356a-4f8a-8eba-4f82d8e9f531" />

Seamless Multi-Account Switching & Sync for Claude Desktop (macOS & Windows).

## 📌 Features

- **Safe Switch**: Switch between multiple Claude Desktop profiles (`~/Library/Application Support/Claude*`) without re-authenticating or losing conversation sidebar history.
- **Automated Backup**: Automatic timestamped snapshots of `claude-code-sessions` before any operation that writes sessions — a manual align, `mcs sync`, or a switch with Auto Sync on. A plain switch (the default, Auto Sync off) never touches session data, so it takes no backup. If the backup fails, the write aborts rather than overwriting unprotected data.
- **Conflict-safe Sync**: When both profiles changed the same session, the newer target copy is preserved and reported as a conflict instead of being silently overwritten.
- **Probe Validation Tool**: Includes `scripts/probe/probe_runner.py` for inspecting profiles and validating local session synchronization.

## 🔄 Syncing sessions between accounts

Switching accounts and syncing sessions are two separate actions — a plain
switch never touches session data unless you turn on auto sync.

- **Plain switch (default):** clicking a profile in the menu just closes
  Claude Desktop and reopens it on that profile. No session data moves. Each
  account keeps only its own Code conversation history.
- **Manual align — "Sync sessions" submenu:** pick a direction (e.g. `From
  Company → To Personal`) to copy one account's Code sessions into another
  **without switching which account you're on**. It closes Claude Desktop,
  backs up the target account, copies the sessions over, and reopens the
  account you were already using.
- **"Auto Sync on Switch" toggle (default OFF):** turn this on and every
  switch bidirectionally unions both accounts' Code sessions, so the two
  accounts converge to the same conversation history over time. Because
  turning it on merges one account's conversations into the other, enabling it
  shows a one-time warning dialog (with an "Enable, don't ask again" option to
  skip the warning on future enables).

> **Scope:** only the Code tab (`claude-code-sessions`) syncs. Regular chat
> conversations are stored server-side per account and can't be synced
> locally. Agent Mode / Cowork sessions are not covered yet.

## 📥 Download

[![Download latest](https://img.shields.io/github/v/release/miou1107/multi-claude-switcher?label=Download%20app&style=for-the-badge)](https://github.com/miou1107/multi-claude-switcher/releases/latest)

On the [latest release](https://github.com/miou1107/multi-claude-switcher/releases/latest), download the zip for your platform:

> **macOS — `Multi-Claude-Switcher_<version>_macos.zip`** — the ready-to-run
> **Multi-Claude Switcher.app** (a universal macOS application, Apple Silicon +
> Intel). Unzip and run it, nothing to build or compile.
>
> **Windows — `Multi-Claude-Switcher_<version>_windows_setup.exe`** — the
> installer (per-user, no administrator prompt); run it and launch from the Start Menu.

### macOS

1. Download the `Multi-Claude-Switcher_<version>_macos.zip` above and **unzip** it (double-click the zip). You get **Multi-Claude Switcher.app**.
2. Drag **Multi-Claude Switcher.app** into your **Applications** folder.
3. **First launch only:** right-click the app → **Open**, then **Open** in the dialog. (The app is unsigned — a paid Apple Developer certificate would remove this step — so macOS asks you to confirm the first time. After that, just double-click.)

The app runs in the **menu bar** (top-right), shown as a pair-of-eyes icon —
it has no Dock icon. Click it to switch profiles; a checkmark marks the profile
in use. Enable **Start at Login** from the menu to launch it automatically. The
app **updates itself** from GitHub Releases, so you only install once.

### Windows

1. Download **`Multi-Claude-Switcher_<version>_windows_setup.exe`** above and run
   it. It is a per-user install (no administrator prompt) that installs the app,
   adds a Start Menu shortcut, and registers an entry in Add/Remove Programs.
2. Launch **Multi-Claude Switcher** from the Start Menu. It appears as a
   pair-of-eyes icon in the system tray (bottom-right; you may need the "show
   hidden icons" arrow). Click it to switch profiles; a checkmark marks the one in
   use. Enable **Start at Login** from the menu to launch it automatically. When a
   new version is released it notifies you; use **Check for Updates** to open the
   download page, then run the new installer to upgrade (it installs over the old
   version).

> **Requires the standalone Claude Desktop build.** Install Claude Desktop from
> [claude.com/download](https://claude.com/download) (the regular per-user
> installer). The **Microsoft Store / MSIX** build is **not supported yet**: it
> stores its data in a virtualized location and cannot be relaunched with a custom
> profile directory, which is how switching works. If you have the Store version,
> replace it with the standalone build to use the switcher.

> **How sync stays correct**: the Code tab only lists conversations from the
> bucket named after the profile's own logged-in account. Sync reads the source
> profile's account bucket and re-homes those sessions under the *target*
> profile's account bucket, so cross-account switches surface correctly (verified
> on-device) rather than silently dropping sessions in a bucket the target app
> never reads.

## 📁 Repository Structure

```
multi-claude-switcher/
├── docs/
│   ├── assets/
│   │   └── icon.png                   # App icon for README / docs
│   ├── plans/                         # Implementation plans
│   │   └── 2026-07-22-phase-0-probe.md
│   └── superpowers/
│       └── specs/                     # Design specifications & probe reports
│           ├── 2026-07-22-multi-claude-account-sync-design.md
│           └── 2026-07-22-probe-results.md
├── scripts/
│   ├── gen-icons/
│   │   └── main.go                    # Generates all icon assets from geometry
│   └── probe/
│       └── probe_runner.py            # Phase 0 probe suite tool
├── CHANGELOG.md                       # Project version history
├── FILELIST.md                        # List of project files
└── README.md                          # Project documentation
```

## 🚀 Quick Start

### Build Binaries

```bash
# Build CLI tool
go build -o bin/mcs ./cmd/mcs

# Build System Tray GUI app
go build -o bin/mcs-tray ./cmd/mcs-tray

# Package a double-clickable macOS .app (universal, into dist/)
./scripts/package-app.sh 0.6.0
```

On **Windows** (PowerShell), build the tray + CLI — pure Go, no CGO / C toolchain:

```powershell
go build -o bin/mcs-tray.exe ./cmd/mcs-tray
go build -o bin/mcs.exe ./cmd/mcs
```

### Launch System Tray App

```bash
./bin/mcs-tray
```
Appears as a pair-of-eyes icon in the macOS menu bar for 1-click profile switching and backups. The icon marks the profile currently in use, and the app checks GitHub for updates and installs them automatically.

### CLI Commands

Check detected profiles and running processes:

```bash
./bin/mcs status
```

Backup session indices for all profiles:

```bash
./bin/mcs backup
```

Sync session files from source profile to target profile:

```bash
./bin/mcs sync Claude Claude_Profile2
```

Perform Safe Switch (close active app -> backup -> sync -> launch target profile):

```bash
./bin/mcs switch Claude Claude_Profile2
```

Restore session indices from a backup snapshot:

```bash
./bin/mcs restore ~/.multi-claude-switcher/backups/Claude_20260722_103206 Claude
```

## 📜 License

MIT License.
