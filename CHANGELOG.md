# CHANGELOG

## [0.4.0] - 2026-07-22

### Fixed (safety hardening)
- **Safe Switch never overwrites without a backup**: if the target profile has
  existing sessions and the pre-switch backup fails, the switch now aborts
  instead of proceeding to overwrite (`core/switch.go`).
- **Sync no longer silently destroys data**: `SyncSessions` compares content and
  only overwrites when the source is strictly newer; when the target holds a
  different, newer version it is left untouched and reported as a conflict
  (`core/sync.go`, `SyncReport.ConflictCount` / `Conflicts`).
- **Termination is verified**: `TerminateApp` now returns an error if a Claude
  Desktop process is still running after force kill, so we never sync into a
  live-writing profile (`platform/darwin.go`).
- **Atomic restore**: `RestoreBackup` stages into a temp dir and swaps in only on
  success, so a mid-restore failure no longer half-destroys the target
  (`core/backup.go`).
- **Standalone `mcs sync` is now safe**: refuses to run while Claude Desktop is
  open (avoids writing into a live-writing profile), and aborts on a genuine
  backup failure instead of silently overwriting (`cmd/mcs/main.go`).
- **`DetectRunningProfile` handles profile paths that contain spaces**: the
  default profile path is `.../Application Support/Claude`, and `ps` renders
  args space-joined without quoting; detection now matches against known profile
  paths with an argument boundary instead of splitting on spaces
  (`platform/darwin.go`). Prevents the tray from picking a truncated source path
  and failing the switch after closing Claude.
- **Copies preserve source modification time** (`os.Chtimes`), so sync's
  mtime-based conflict detection stays meaningful across repeated runs
  (`core/backup.go`).

### Changed
- **Single version source of truth** (`core/version.go`); CLI and tray import it
  (previously 0.1.0 / 0.2.0 / 0.3.0 disagreed across files).
- **Tray picks the running profile as the sync source** via the new
  `DetectRunningProfile` platform method, instead of an arbitrary other profile.
- **Tray confirms before switching**: clicking a profile now shows a native
  confirmation dialog (osascript), since the switch closes Claude Desktop; a
  mis-click no longer silently kills a running session. Switch failures surface
  as a macOS notification (`cmd/mcs-tray/main.go`).

### Known limitation
- Cross-account sync only reliably surfaces buckets that already exist on both
  profiles. Whether a source-only `<AccountUUID>` bucket appears in the target
  app is unverified on-device (Phase 0 probe open item) and needs a real
  end-to-end test.

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
