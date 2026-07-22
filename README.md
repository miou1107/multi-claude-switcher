# multi-claude-switcher

Seamless Multi-Account Switching & Sync for Claude Desktop (macOS).

## 📌 Features

- **Safe Switch**: Switch between multiple Claude Desktop profiles (`~/Library/Application Support/Claude*`) without re-authenticating or losing conversation sidebar history.
- **Automated Backup**: Automatic timestamped snapshots of `claude-code-sessions` before every switch/sync. If the backup fails, the switch aborts rather than overwriting unprotected data.
- **Conflict-safe Sync**: When both profiles changed the same session, the newer target copy is preserved and reported as a conflict instead of being silently overwritten.
- **Probe Validation Tool**: Includes `scripts/probe/probe_runner.py` for inspecting profiles and validating local session synchronization.

> **Known limitation**: sync reliably surfaces conversation buckets that already
> exist on *both* profiles. Whether a bucket that exists only in the source
> profile shows up in the target app is not yet verified on-device and needs a
> real end-to-end test before you rely on it.

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
```

### Launch System Tray App

```bash
./bin/mcs-tray
```
Appears as a menu bar item (`☁️ Claude`) on macOS for 1-click profile switching and backups.

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
