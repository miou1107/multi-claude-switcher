# CHANGELOG

## [Unreleased]

### Documentation
- **Phase 0 findings corrected with live-machine evidence** (`docs/superpowers/specs/2026-07-22-probe-results.md`):
  - The Code tab enumerates sessions **only** from `claude-code-sessions/<lastKnownAccountUuid>/`; copying a session bucket under any other name is a silent failure (files on disk, empty sidebar). Sync MUST re-bucket under the *target* profile's account UUID. Confirmed by a real natural experiment on two live profiles.
  - Falsified an earlier hypothesis that `config.json` `dxt:allowlist*` / Local Storage leveldb drives the list; the account-UUID bucket name is the whole gate.
  - Added a Config / Preferences sync analysis: config files are not monolithic (global prefs = whitelist-copy, per-account maps = merge-by-key, identity/auth = never sync). Bypass Permissions is a per-account opt-in in `claude_desktop_config.json`.
- **Design spec** (`...-multi-claude-account-sync-design.md`): added the bucket-naming invariant to the Safe Switch steps and refined the shared/isolated boundary to field-level config sync.

## [0.3.0] - 2026-07-22

### Added
- **macOS System Tray GUI (`mcs-tray`)**:
  - `cmd/mcs-tray/main.go`: Menu bar quick switcher using `github.com/getlantern/systray`.
  - Dynamic profile listing and 1-click Safe Switch trigger from macOS menu bar.
  - Quick backup trigger and Finder folder shortcut.

## [0.2.0] - 2026-07-22

### Added
- **Go Core & CLI Engine (`mcs`)**:
  - `platform/`: Platform abstraction interface (`platform.go`) and macOS Darwin implementation (`darwin.go`) for process control (`pkill`), profile discovery, and `--user-data-dir` launch.
  - `core/`: Backup manager (`backup.go`), session index sync engine (`sync.go`), and Safe Switch controller (`switch.go`).
  - `cmd/mcs/main.go`: Command-line tool supporting `mcs status`, `mcs sync`, `mcs switch`, `mcs backup`, and `mcs restore`.
  - Unit test suite: `core/backup_test.go` and `core/sync_test.go` (100% passing).

## [0.1.0] - 2026-07-22

### Added
- **Phase 0 Probe Suite**: `scripts/probe/probe_runner.py` for profile status inspection, session backup, and `--user-data-dir` launch validation.
- **Probe Findings Report**: `docs/superpowers/specs/2026-07-22-probe-results.md` confirming `--user-data-dir` support and Safe Switch mode feasibility on macOS.
- **Implementation Plan**: `docs/plans/2026-07-22-phase-0-probe.md` outlining probe tasks and safety verifications.
- Core documentation: `README.md` and `FILELIST.md`.
