# Windows Store / MSIX support — in-place profile-folder swap

Status: implemented (v0.7.8), pending on-device verification of the switch.

## Problem

The macOS and standalone-Windows switcher launches Claude Desktop with a
different `--user-data-dir` per profile. The **Microsoft Store / MSIX** build of
Claude Desktop can't be driven that way: it has no directly invocable `claude.exe`
on PATH (only an ACL-locked `…\WindowsApps\Claude_<ver>\app\Claude.exe`), and its
`%APPDATA%\Claude` writes are virtualized to
`%LOCALAPPDATA%\Packages\Claude_<hash>\LocalCache\Roaming\Claude`. Passing a custom
data dir to a Store-activated Electron app is not reliably supported.

Many users (including the first Windows tester) have only the Store build, where
the tray showed **zero switchable profiles**.

## Approach: swap the live data directory in place

Instead of "launch with a different data dir", switch accounts by swapping the
single live data directory. Let `<roaming>` =
`%LOCALAPPDATA%\Packages\Claude_<hash>\LocalCache\Roaming` (the real backing store
the MSIX runtime redirects `%APPDATA%\Claude` to — confirmed to hold
`claude-code-sessions`, `config.json`, etc.).

```
<roaming>\Claude                    the ACTIVE profile's data ("the slot")
<roaming>\.mcs-profiles\<name>       each INACTIVE profile, parked
<roaming>\.mcs-profiles\state.json   { "current": "<name of slot occupant>" }
```

- **Switch to X:** close Claude → rename slot `Claude` to `.mcs-profiles\<current>`
  → rename `.mcs-profiles\<X>` to `Claude` → set `current = X` → relaunch via
  AppUserModelID (`Claude_<hash>!Claude`) with
  `explorer shell:AppsFolder\<AUMID>`.
- **New account:** close Claude → park the slot → leave the slot absent (the app
  creates a fresh, signed-out data dir on launch) → `current = <newName>` →
  relaunch. The user signs into the second account; it becomes a normal profile.

All moves are **same-volume directory renames**: atomic, fast, and reversible. No
data is ever deleted. A failed activation rolls the parking back so the slot is
never left empty. State is written after the rename, so a state-write failure only
mislabels the slot (cosmetic), never loses data.

## Detection & UI

- `DetectRunningProfile` returns the slot path when the app is running (the slot's
  identity is `state.current`).
- `FindProfiles` lists the slot (named per state) + every parked dir, all marked
  `Managed` so the tray shows them even before they have session data.
- The tray adds a Windows-only **"New account profile…"** item (shown only when
  `MSIXAvailable()`), then relaunches itself so the rebuilt menu includes the new
  profile.

Standalone Windows is unaffected: `isMSIX()` returns false whenever
`findClaudeExecutable()` succeeds, so an installed standalone build always wins and
uses the original `--user-data-dir` path.

## Code map

- `platform/windows_msix.go` — discovery (package dir, roaming, AUMID), state,
  the testable move core (`msixSwapToIn`, `msixParkForNewIn`, `msixValidateNameIn`,
  taking an explicit roaming dir), and the exported `MSIXAvailable` /
  `MSIXCurrentName` / `MSIXNewProfile`.
- `platform/windows.go` — `isMSIX()` and the MSIX branches in `FindProfiles`,
  `DetectRunningProfile`, `LaunchProfile`.
- `platform/windows_msix_test.go` — full create→switch lifecycle, rollback, name
  validation, driven on a temp dir (no real Claude needed).
- `cmd/mcs-tray/profiles_windows.go` / `profiles_other.go` — the "New account
  profile…" flow and its non-Windows no-op.

## Not verified from the dev session

The core assumption — that renaming `…\LocalCache\Roaming\Claude` and relaunching
the Store app makes it load the swapped-in profile — is high-confidence (that dir
IS the app's backing store) but was not live-tested, because terminating Claude
Desktop from a session hosted inside its Code tab kills that session (IR-693).
Verified manually on-device before release: create a second profile, sign in,
switch back and forth, confirm each account's sessions return intact.

## Deferred

- Auto Sync on Switch with the swap model (default is off; plain switch works).
- MSIX detection of the account UUID for smarter labels; users can rename via the
  existing Rename action.
