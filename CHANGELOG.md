# CHANGELOG

## [0.6.1] - 2026-07-22

### Changed
- **The `.app` is now the only published download.** Releases no longer attach
  the raw `mcs` / `mcs-tray` binaries or the raw `_macos-universal.zip`; the sole
  asset is `Multi-Claude-Switcher_<version>_macos.zip` (the ready-to-run app), so
  there's no confusing "which file do I download" (`.github/workflows/release.yml`,
  README).
- **Self-update now sources the `.app` zip** instead of a standalone binary: it
  downloads the release zip, extracts the tray executable from
  `…/Contents/MacOS/mcs-tray` (via `ditto`), and atomically swaps that in
  (`cmd/mcs-tray/update.go`, new `findAppZip` / `findTrayBinary` / `copyExecutable`).
  Only the executable is replaced, not the whole bundle, so `Info.plist` / icon
  changes ship with a fresh install rather than a self-update.

### Upgrade note
- **Any install older than 0.6.1** (0.5.0 or 0.6.0) cannot auto-update to 0.6.1:
  their updater looks for the now-removed `mcs-tray-macos-universal` asset.
  Download the 0.6.1 `.app` once manually; 0.6.1+ self-updates normally from the
  zip thereafter.

## [0.6.0] - 2026-07-22

### Added
- **Automatic updates** (`core/update.go`, `cmd/mcs-tray/update.go`): the tray
  checks GitHub Releases on startup and every 6 hours, and when a newer version
  is available it downloads the universal binary, strips the download quarantine,
  atomically swaps it in for the running executable (with rollback on failure),
  and relaunches. New tray menu item **Check for Updates…** for a manual check.
  Verified end-to-end: a binary built as v0.4.9 self-updated to the real v0.5.0
  release and its hash matched the published asset. (Updates are trusted via
  HTTPS to the project's own GitHub Releases; per-binary checksum/signature
  verification is a planned follow-up.)
- **Double-clickable macOS `.app` bundle**: the tray now ships as
  `Multi-Claude Switcher.app` — a menu-bar-only agent (`LSUIElement`, no Dock
  icon) with a color app icon. Built by `scripts/package-app.sh` locally and by
  the release workflow (packaged into `Multi-Claude-Switcher_<ver>_macos.zip` via
  `ditto`). The app is unsigned (no Apple Developer account), so the first launch
  is a one-time **right-click → Open**; no Terminal required
  (`packaging/Info.plist.template`, `cmd/mcs-tray/assets/appicon-1024.png`).
- **Start at Login** (`core/loginitem.go`): a new checkable tray item installs or
  removes a per-user LaunchAgent
  (`~/Library/LaunchAgents/com.miou1107.multi-claude-switcher.plist`) so the app
  launches automatically at login. Plist writes are atomic. Enabling/disabling
  only writes/removes the plist and takes effect at the next login — it does not
  `launchctl load`/`unload` the job at runtime, which would otherwise spawn a
  duplicate instance on enable or SIGTERM the running app on disable.

### Changed
- **Self-update is bundle-aware**: when running inside a `.app`, the post-update
  relaunch goes through LaunchServices (`open -n <bundle>`) instead of exec'ing
  the raw binary, so the `LSUIElement` menu-bar-agent treatment is preserved (no
  transient Dock icon). Bare-binary runs are unchanged
  (`cmd/mcs-tray/update.go`, new `isInsideAppBundle`).

### Documentation
- **README Download section**: leads with the `.app` (double-click, first-launch
  right-click → Open) and keeps the raw binary / CLI as advanced options, with
  stable `releases/latest/download/…` links. Refreshed two stale notes: the
  resolved cross-account "known limitation" now explains how account-aware sync
  stays correct, and the tray description reflects the icon / active marker /
  auto-update instead of the old `☁️ Claude` text.
- **Design spec** `docs/superpowers/specs/2026-07-22-macos-app-bundle-design.md`.

## [0.5.0] - 2026-07-22

### Build / CI
- **GitHub Release automation** (`.github/workflows/release.yml`): pushing a
  `v*` tag builds universal (arm64 + Intel) macOS binaries with the version
  baked in via `-ldflags`, packages a zip + checksum, and publishes a GitHub
  Release with the raw binaries attached (the download source for the upcoming
  auto-updater). `core.Version` is now a `var` so the tag can be injected.

### Added
- **Active-profile marker in the tray**: the profile currently in use is shown
  with a checkmark and "(current)", updated after a switch and by a background
  poller so it stays correct even when the profile is changed outside the tray
  (`cmd/mcs-tray/main.go`, `platform.DetectRunningProfile`).
- **Custom profile display names**: rename profiles to friendlier labels via the
  new tray item **Rename a Profile…** (native dialogs); stored in
  `~/.multi-claude-switcher/names.json` (`core/names.go`). Names are used in the
  menu, the switch confirmation, and the active marker.
- **Menu bar icon** for the tray instead of the literal text "Claude": a
  swap-arrows template glyph that macOS recolors for light/dark menu bars
  (`cmd/mcs-tray/assets/icon.png`, embedded via `go:embed`).
- **Persistent logging** (`core/logging.go`): the tray and mutating CLI commands
  now append to `~/.multi-claude-switcher/logs/<component>.log` (plus stderr), so
  a background/auto-started tray leaves a durable trail. New tray menu item
  **Open Log Folder**.

### Fixed (correctness)
- **Sync is now account-aware — the switch's core actually works cross-account.**
  `SyncSessions` previously copied buckets at their verbatim path, so switching
  between two different accounts (a) dropped sessions under the *source* account's
  bucket name where the target app never looks (silent no-op), and (b) dragged
  foreign/orphaned buckets into the target, re-polluting it. It now reads the
  source profile's own account bucket and re-homes those sessions under the
  **target** profile's `lastKnownAccountUuid` bucket, copying only that one bucket
  (`core/sync.go`, new `platform.GetProfileAccountUUID`). `SyncReport` now reports
  `SourceAccount` / `TargetAccount`, surfaced by `mcs sync` and the Safe Switch log.
  Safe Switch gracefully **skips sync but still launches** when a profile has no
  logged-in account yet, so `switch` can still open a fresh profile to log into.
  Tests: `TestSyncRebucketsIntoTargetAccount`, `TestSyncErrorsWhenNotLoggedIn`,
  `TestSyncNoOpWhenSourceBucketMissing`, `TestSafeSwitchLaunchesWhenTargetNotLoggedIn`.

### Documentation
- **Phase 0 findings corrected with live-machine evidence** (`docs/superpowers/specs/2026-07-22-probe-results.md`):
  - The Code tab enumerates sessions **only** from `claude-code-sessions/<lastKnownAccountUuid>/`; copying a session bucket under any other name is a silent failure (files on disk, empty sidebar). Sync MUST re-bucket under the *target* profile's account UUID. Confirmed by a real natural experiment on two live profiles.
  - Falsified an earlier hypothesis that `config.json` `dxt:allowlist*` / Local Storage leveldb drives the list; the account-UUID bucket name is the whole gate.
  - Added a Config / Preferences sync analysis: config files are not monolithic (global prefs = whitelist-copy, per-account maps = merge-by-key, identity/auth = never sync). Bypass Permissions is a per-account opt-in in `claude_desktop_config.json`.
  - Closes the 0.4.0 "Known limitation": a source-only `<AccountUUID>` bucket **does** surface in the target app once copied under the target's account UUID (verified on-device by restoring a personal-account bucket into the personal profile).
- **Design spec** (`...-multi-claude-account-sync-design.md`): added the bucket-naming invariant to the Safe Switch steps and refined the shared/isolated boundary to field-level config sync.

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
- **Restore is reversible**: `RestoreBackup` now snapshots the current target
  before overwriting it, and aborts if that snapshot fails. Restoring the wrong
  backup is no longer a one-way loss of whatever the target held (`core/backup.go`).
- **Restore refuses to run while Claude Desktop is open**: `mcs restore`
  overwrites the live session index, so it now guards on `IsAppRunning` like
  `mcs sync` (`cmd/mcs/main.go`).
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
  end-to-end test. **(Closed in [Unreleased] — verified on-device.)**

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
