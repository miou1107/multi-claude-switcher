# CHANGELOG

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
