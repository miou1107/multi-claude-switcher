# CHANGELOG

## [Unreleased]

### Added
- Tray now detects **Team** accounts (from the cached organization list) and tags them `🏢 Team` in the profile menu. Actions that would import Code sessions *into* a Team account — enabling Auto Sync, or a manual sync direction targeting a Team account — now warn that import is a no-op for Team accounts (they are export-only). Detection is best-effort; unrecognized accounts are left untagged rather than mislabeled.
- Rescan accounts: scan the machine for Claude accounts, review each (UUID, completeness, email, Team, conversations, last-updated), and pick which to manage. Incomplete/ghost accounts (orphaned Code sessions with no login) are shown read-only as "Invalid account data".

### Changed
- First-run menu now shows accounts with an active login (and managed profiles); a logged-out profile folder no longer appears until you run Rescan accounts.

## [0.7.9] - 2026-07-23

### Fixed
- **Tray logging was silently dropped on the GUI build.** `SetupLogging` used
  `io.MultiWriter(os.Stderr, f)`, but a `-H=windowsgui` build has no valid stderr,
  and MultiWriter aborts on the first writer's error — so nothing reached the log
  file since v0.7.5. The file is now written first and stderr errors are swallowed
  (`core/logging.go`).
- **Windows dialogs could open hidden or behind other windows.** The window-hiding
  flag also hid the WinForms dialogs, and a background tray's dialogs are not
  foreground; both are fixed (CREATE_NO_WINDOW only, plus TopMost/owner on every
  dialog) so About / Rename / Sync / new-profile prompts reliably appear in front
  (`cmd/mcs-tray/hidewindow_windows.go`, `cmd/mcs-tray/dialog_windows.go`).
- **Windows Store profile swap was fragile.** The rename that swaps the live data
  directory could fail while Claude still held its files open; the retry window is
  now ~20s, each step is logged, a failed swap cleans up and reports a clear "quit
  Claude fully" message, and no data is ever deleted (`platform/windows_msix.go`).
- **Two tray tooltips said "in Finder" (a macOS term) on every OS.** They now name
  the platform's file manager — "File Explorer" on Windows, "Finder" on macOS
  (`cmd/mcs-tray/main.go`, `cmd/mcs-tray/dialog_*.go`).

### Added
- **Windows Store build: bring the second account's saved sessions over
  automatically.** After you create your other account ("New account profile…")
  and sign into it, that account's previously saved Code sessions are copied into
  its new profile (`cmd/mcs-tray/profiles_windows.go`, `platform/windows_msix.go`).

### Documentation
- **Documented a hard limitation: Claude Team accounts are export-only.** Session
  sync can export a Team account's Code sessions OUT (Team → personal) but cannot
  import INTO a Team account (anything → Team). A Team account builds its Code
  sidebar from a server API (`sessions_api_list_sessions`, scoped to account +
  organization), so session files copied into its local folder are ignored and
  never appear, even after a clean restart or full cache wipe. Verified
  2026-07-23 on a live Team account; both READMEs now carry a top-of-page
  warning, and `docs/superpowers/specs/2026-07-22-probe-results.md` records the
  correction (the earlier "folder copy always surfaces sessions" premise was a
  false positive that only tested restoring an account's own sessions).

### Changed
- **Release CI now bumps the Homebrew tap automatically.** After the macOS build
  publishes the `_macos.zip`, a new `update-homebrew-tap` job in `release.yml`
  writes the new version and its SHA256 into
  `miou1107/homebrew-tap/Casks/multi-claude-switcher.rb` and pushes the change,
  so `brew install --cask miou1107/tap/multi-claude-switcher` tracks each
  release without a manual step. Requires a `HOMEBREW_TAP_TOKEN` repository
  secret (fine-grained PAT with Contents:read/write on the tap repo); the job
  soft-fails if the token is missing, leaving the mac/windows releases
  themselves unaffected.

## [0.7.8] - 2026-07-23

### Added
- **Windows: support the Microsoft Store / MSIX build of Claude Desktop.** The
  Store build can't be launched with a custom `--user-data-dir`, so on it MCS
  switches accounts by swapping the single live data directory in place: the
  active profile sits in `…\LocalCache\Roaming\Claude`, inactive ones are parked
  under `…\Roaming\.mcs-profiles\<name>`, and a switch renames them and relaunches
  the packaged app via its AppUserModelID. All moves are reversible same-volume
  renames (no data is deleted); a failed switch rolls back. A new **"New account
  profile…"** tray item (Store build only) saves the current account and opens a
  fresh Claude to sign into another one. The standalone build is unaffected and
  still wins when both are installed. See
  `docs/superpowers/specs/2026-07-23-windows-msix-support-design.md`,
  `platform/windows_msix.go`, `platform/windows.go`,
  `cmd/mcs-tray/profiles_windows.go`.
- **macOS: ad-hoc sign the app bundle when packaging.** `scripts/package-app.sh`
  now runs `codesign --sign -` on the `.app` before zipping. This needs no Apple
  Developer account and does not notarize the app, so a browser-downloaded copy
  still needs a one-time Gatekeeper bypass on first launch. What it buys: one
  clean whole-bundle signature with a stable identity after the universal binary
  is assembled, which keeps the self-updater's in-place binary swap
  codesign-valid. The README install steps now also cover clearing Gatekeeper on
  macOS 15 (System Settings → Privacy & Security), where the older right-click →
  Open path no longer appears.

### Changed
- **Each profile is now its own submenu.** A profile in the tray menu used to be
  a single click-to-switch item, with renaming tucked under Settings. Each
  account is now a submenu with **Switch to this profile** and **Rename…**, so an
  account's actions live together and renaming targets that account directly (no
  more "which profile?" picker). Switching is therefore one step deeper (open the
  account's submenu, then Switch). The "Rename a Profile…" entry has been removed
  from Settings. `cmd/mcs-tray/main.go`.

## [0.7.7] - 2026-07-23

### Fixed
- **Windows: PowerShell windows flashed on screen periodically.** The tray is a
  GUI process with no console of its own, so every console helper it spawned —
  the 4-second running-profile poll's `powershell`, plus `taskkill` / `tasklist`
  / `reg` — popped its own black window. Each is now launched with
  `CREATE_NO_WINDOW` (`platform/hidewindow_windows.go`,
  `core/hidewindow_windows.go`, `cmd/mcs-tray/hidewindow_windows.go`).
- **Windows: the app showed a generic Start Menu / taskbar / Explorer icon.**
  `mcs-tray.exe` carried no icon resource (`SetIcon` only themes the live tray
  glyph, not the file). A Windows icon resource generated from `icon.ico` is now
  compiled into the executable (`cmd/mcs-tray/rsrc_windows_amd64.syso`).

### Changed
- **Windows releases ship only the installer.** The `_windows.zip` is dropped, so
  `Multi-Claude-Switcher_<version>_windows_setup.exe` is the single Windows
  download. When a newer version is released the app notifies you, and a manual
  "Check for Updates" opens the download page; running the new installer upgrades
  in place. macOS keeps its silent binary-swap self-update. The self-updater is
  split into `cmd/mcs-tray/update_install_{nonwindows,windows}.go`
  (`cmd/mcs-tray/update.go`, `.github/workflows/release.yml`).

## [0.7.6] - 2026-07-23

### Added
- **Windows installer.** Releases now include
  `Multi-Claude-Switcher_<version>_windows_setup.exe`, a per-user Inno Setup
  installer (no administrator prompt) that installs the tray app, adds a Start
  Menu shortcut, and registers an uninstaller in Add/Remove Programs
  (`packaging/windows-setup.iss`, `.github/workflows/release.yml`). The
  `_windows.zip` is retained for the in-app self-updater.

## [0.7.5] - 2026-07-23

### Fixed
- **Windows tray app opened a console window and stayed attached to it.** It is
  now built with `-H=windowsgui`, so it runs from the tray with no console window
  (`.github/workflows/release.yml`).
- **Windows "Check for Updates" left a stray tray icon and showed no message.**
  The NotifyIcon-balloon approach is replaced with a proper Windows toast
  notification, which adds no extra tray icon (`cmd/mcs-tray/dialog_windows.go`
  `notify`).

### Changed
- **The Windows zip now contains only `mcs-tray.exe`.** The `mcs.exe` CLI is no
  longer bundled, matching the macOS release which ships just the app.

## [0.7.4] - 2026-07-23

### Fixed
- **Windows tray icon failed to load** ("Unable to set icon"). Startup used
  `SetTemplateIcon` with a macOS template PNG, which systray rejects on Windows.
  The icon is now set per-OS (`setTrayIcon`): a template PNG on macOS, the
  multi-resolution `icon.ico` via `SetIcon` on Windows
  (`cmd/mcs-tray/trayicon_{darwin,windows,other}.go`).

### Documentation
- Added a **Traditional Chinese README** (`README.zh-TW.md`) and an
  `English | 繁體中文` language switcher at the top of both READMEs.

## [0.7.3] - 2026-07-23

### Added
- **Windows support (in progress).** The platform layer, start-at-login, single-
  instance guard, self-update, and tray dialogs now have Windows implementations
  behind build tags, and a `windows-latest` CI job publishes a
  `Multi-Claude-Switcher_<version>_windows.zip`. Switching targets the
  **standalone** Claude Desktop build (launched with `--user-data-dir`); the
  Microsoft Store / MSIX build is detected but not yet supported for launching.
  - `platform/windows.go` — process detection (`Win32_Process`), profile
    discovery, terminate-by-PID (never the identically named Claude Code CLI),
    and standalone-exe launch.
  - `core/loginitem_windows.go` — start-at-login via the `HKCU\...\Run` key.
  - `cmd/mcs-tray/instance_windows.go` — single-instance guard via `tasklist`.
  - `cmd/mcs-tray/dialog_windows.go` — tray dialogs / notifications via PowerShell.
  - `cmd/mcs-tray/update_platform_windows.go` — self-update: `_windows.zip` asset,
    pure-Go unzip, `.exe` relaunch.

### Fixed
- `core/backup_test.go` now induces a staging-write failure in an OS-appropriate
  way (an `icacls` deny ACE on Windows, `chmod` on Unix), so the
  restore-preserves-target test passes on Windows instead of relying on POSIX
  permission bits.

## [0.7.2] - 2026-07-23

### Fixed
- **Menu-bar icon was squished.** systray forces the tray image to a 16x16
  square (`[image setSize:NSMakeSize(16, 16)]`), so the 0.7.1 template — a
  non-square 69x44 — got compressed horizontally and the eyes looked distorted.
  The template now renders on a square canvas (eyes centered with vertical
  padding), so it displays at the correct aspect
  (`scripts/gen-icons/main.go` `renderTemplate`, `cmd/mcs-tray/assets/icon.png`).

## [0.7.1] - 2026-07-23

### Changed
- **New app icon — a pair of eyes** (left large, right small, each with a
  pupil), replacing the generic swap-arrows glyph. Ships as a color app icon,
  a black menu-bar template that macOS recolors for light/dark, a
  multi-resolution Windows `.ico`, and a 512px doc image. All are generated
  from geometry by `scripts/gen-icons/main.go` (`go run scripts/gen-icons/main.go`),
  so the source of truth is code, not binaries
  (`cmd/mcs-tray/assets/{appicon-1024.png,icon.png,icon.ico}`,
  `docs/assets/icon.png`).

## [0.7.0] - 2026-07-22

### Added
- **Manual "Sync sessions" tray submenu:** copy one account's Code sessions
  into another **without switching accounts** — it closes Claude Desktop, backs
  up the target, syncs (re-bucketed under the target account), and reopens the
  account you were already on (`core/align.go` `Switcher.ManualAlign`,
  `cmd/mcs-tray/main.go`).
- **"Auto Sync on Switch" toggle (default OFF):** when on, every switch
  bidirectionally unions both accounts' Code sessions so they converge to the
  same history; safe because both profiles are closed during the switch window.
  Enabling shows a one-time warning (with an "Enable, don't ask again" option),
  since it merges one account's conversations into the other. The toggle sits at
  the top of the **Sync sessions** submenu, and while it is on the manual
  directions below it are disabled (redundant)
  (`core/settings.go`, `core/sync.go` `SyncBidirectional`,
  `cmd/mcs-tray/autosync.go`).
- **Single-instance guard:** launching a second tray while one is already running
  now shows an "already running" notice and quietly exits, so the menu bar never
  gets duplicate icons/updaters. The self-update relaunch is exempt
  (`cmd/mcs-tray/instance.go`).

### Changed
- **Switching no longer auto-syncs by default.** Previously every switch ran a
  one-way session sync; now a switch moves **no** session data unless
  "Auto Sync on Switch" is enabled. This makes cross-account conversation
  merging an explicit opt-in (`core/switch.go`).
- **Tidier tray menu:** the growing action list is grouped into **Settings** and
  **Maintenance** submenus, the version moved into a new **About** item, and only
  the frequent actions (switch, Sync sessions) stay at the top level
  (`cmd/mcs-tray/main.go`).

### Notes
- Scope is Code sessions (`claude-code-sessions`) only. Agent Mode / Cowork
  sessions (`local-agent-mode-sessions`) are not synced; that is a separate,
  display-verification-gated follow-up. Regular chat is server-side per account
  and cannot be synced locally.

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
