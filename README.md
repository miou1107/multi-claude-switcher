# multi-claude-switcher
<img width="236" height="277" alt="image" src="https://github.com/user-attachments/assets/62f863dd-9545-4a5c-ac46-66c32517f21f" />

<img width="2457" height="829" alt="image" src="https://github.com/user-attachments/assets/fa5eef07-356a-4f8a-8eba-4f82d8e9f531" />

Seamless Multi-Account Switching & Sync for Claude Desktop (macOS & Windows).

## 📌 Features

- **Safe Switch**: Switch between multiple Claude Desktop profiles (`~/Library/Application Support/Claude*`) without re-authenticating or losing conversation sidebar history.
- **Automated Backup**: Automatic timestamped snapshots of `claude-code-sessions` before every switch/sync. If the backup fails, the switch aborts rather than overwriting unprotected data.
- **Conflict-safe Sync**: When both profiles changed the same session, the newer target copy is preserved and reported as a conflict instead of being silently overwritten.
- **Probe Validation Tool**: Includes `scripts/probe/probe_runner.py` for inspecting profiles and validating local session synchronization.

## 📥 Download

[![Download latest](https://img.shields.io/github/v/release/miou1107/multi-claude-switcher?label=Download&style=for-the-badge)](https://github.com/miou1107/multi-claude-switcher/releases/latest)

Prebuilt **universal** (Apple Silicon + Intel) downloads are attached to every
release — no need to build from source.

- **[⬇︎ Menu bar app (`.app`)](https://github.com/miou1107/multi-claude-switcher/releases/latest)** — recommended: double-clickable, lives in `/Applications`, can start at login. Grab the `Multi-Claude-Switcher_<version>_macos.zip` on the latest release.
- **[⬇︎ Raw tray binary (`mcs-tray`)](https://github.com/miou1107/multi-claude-switcher/releases/latest/download/mcs-tray-macos-universal)** — advanced: run from the Terminal, no bundle.
- **[⬇︎ CLI (`mcs`)](https://github.com/miou1107/multi-claude-switcher/releases/latest/download/mcs-macos-universal)** — command-line tool.

### Install the app

1. Download `Multi-Claude-Switcher_<version>_macos.zip` from the [latest release](https://github.com/miou1107/multi-claude-switcher/releases/latest) and unzip it (double-click).
2. Drag **Multi-Claude Switcher.app** into your **Applications** folder.
3. **First launch only:** right-click the app → **Open** → **Open** in the dialog. (The app is unsigned — a paid Apple Developer certificate would remove this step — so macOS asks you to confirm the first time. After that, double-click works normally.)

A swap-arrows icon (⇄) appears in the menu bar. Click it to switch profiles; a
checkmark marks the profile in use. Enable **Start at Login** from the menu to
launch it automatically. The app **updates itself** from GitHub Releases, so you
only install once.

<details>
<summary>Prefer the raw binary (Terminal)?</summary>

```bash
# Download, make executable, clear the download quarantine, run.
curl -L -o mcs-tray \
  https://github.com/miou1107/multi-claude-switcher/releases/latest/download/mcs-tray-macos-universal
chmod +x mcs-tray
xattr -dr com.apple.quarantine mcs-tray
./mcs-tray
```

The quarantine-strip step is what lets Gatekeeper open the unsigned binary. Note
the process stops when the Terminal closes; the `.app` above does not have this
limitation.
</details>

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
│   ├── plans/                         # Implementation plans
│   │   └── 2026-07-22-phase-0-probe.md
│   └── superpowers/
│       └── specs/                     # Design specifications & probe reports
│           ├── 2026-07-22-multi-claude-account-sync-design.md
│           └── 2026-07-22-probe-results.md
├── scripts/
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

### Launch System Tray App

```bash
./bin/mcs-tray
```
Appears as a swap-arrows icon in the macOS menu bar for 1-click profile switching and backups. The icon marks the profile currently in use, and the app checks GitHub for updates and installs them automatically.

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
