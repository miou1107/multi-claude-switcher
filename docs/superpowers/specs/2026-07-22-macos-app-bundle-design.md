# Design: macOS `.app` bundle for the tray

**Date:** 2026-07-22
**Status:** Approved (design), pending implementation
**Target version:** 0.6.0 (minor — new user-facing feature)

## Problem

The tray is shipped as a bare Mach-O executable (`mcs-tray-macos-universal`).
Installing it requires the Terminal (`chmod +x`, strip quarantine, run) and the
process dies when the Terminal closes. Non-technical users cannot install or keep
it running. We want a double-clickable `.app` that lives in `/Applications`, runs
as a menu-bar-only agent, and can start at login — without requiring a paid Apple
Developer signing certificate.

## Constraints

- **No Apple Developer account.** The `.app` is unsigned and un-notarized. macOS
  Gatekeeper will block the first launch of a downloaded app. The mitigation is
  documentation: the user does **right-click → Open** once (a single click in a
  dialog, no Terminal), after which double-click works normally. This is the best
  achievable UX on the free tier and must be stated plainly in the README.
- Must not regress the existing self-update mechanism.
- macOS only (Windows packaging is out of scope, tracked separately).

## Components

### 1. Bundle layout

```
Multi-Claude Switcher.app/
  Contents/
    Info.plist
    MacOS/
      mcs-tray            # the existing universal binary, renamed
    Resources/
      icon.icns           # app icon
```

**Info.plist keys:**

| Key | Value | Why |
|-----|-------|-----|
| `CFBundleName` / `CFBundleDisplayName` | `Multi-Claude Switcher` | shown in Finder / menu |
| `CFBundleIdentifier` | `com.miou1107.multi-claude-switcher` | stable bundle id (also used for the LaunchAgent label) |
| `CFBundleExecutable` | `mcs-tray` | the binary in `MacOS/` |
| `CFBundleIconFile` | `icon` | `Resources/icon.icns` |
| `CFBundleShortVersionString` / `CFBundleVersion` | build version | from the git tag in CI, `dev` locally |
| `CFBundlePackageType` | `APPL` | it's an application |
| `LSUIElement` | `true` | **menu-bar-only agent: no Dock icon, not in ⌘-Tab** |
| `LSMinimumSystemVersion` | `11.0` | universal (arm64 + Intel) baseline |
| `NSHighResolutionCapable` | `true` | retina menu bar rendering |

`LSUIElement=true` is the single most important key: it makes the process behave
like the existing tray (menu-bar presence only), so wrapping the binary in a
bundle does not suddenly add a Dock icon or app-switcher entry.

### 2. App icon

The existing `assets/icon.png` (44×44 monochrome template) is correct for the
menu bar but too small and too plain for an app icon. We add a **1024×1024 color
source PNG** (`cmd/mcs-tray/assets/appicon-1024.png`, a rounded-square background
with the swap-arrows glyph) and convert it to `icon.icns` at package time via
`sips` (downscale to the standard iconset sizes) + `iconutil -c icns`. The menu
bar template icon is unchanged.

### 3. "Start at Login" toggle — `core/loginitem.go`

A per-user LaunchAgent, controlled from the tray:

- Plist path: `~/Library/LaunchAgents/com.miou1107.multi-claude-switcher.plist`
- `Label` = the bundle identifier; `ProgramArguments` = the resolved path to the
  running executable (`Contents/MacOS/mcs-tray`); `RunAtLoad` = true.
- API:
  - `LoginItemEnabled() bool` — the plist exists.
  - `EnableLoginItem(exePath string) error` — write the plist (atomic temp+rename).
  - `DisableLoginItem() error` — remove the plist.
- Tray: a checkable **Start at Login** menu item that reflects and toggles state.
- **No runtime `launchctl load`/`unload`.** The setting takes effect at the next
  login. Loading at enable time would immediately start a second instance
  (RunAtLoad + no single-instance guard); unloading at disable time would SIGTERM
  the running app when it was itself started by launchd. Writing/removing the
  plist is the whole operation.

**Known edge:** the plist stores an absolute path. If the user moves the `.app`
after enabling, the login item points at the old location and must be re-toggled.
Acceptable for 0.6.0; documented. (A future refinement could use
`open -a "Multi-Claude Switcher"`, but that is flakier at login time.)

### 4. Self-update compatibility — `cmd/mcs-tray/update.go`

Self-update downloads the raw `mcs-tray-macos-universal` asset and swaps the
executable at `os.Executable()`. Inside a bundle that path is
`.../Contents/MacOS/mcs-tray`, so the atomic same-dir rename still works — **no
change to what is downloaded or how it is swapped.**

The one required change is **relaunch**: today it `exec`s the raw binary
directly, which bypasses LaunchServices and would drop the `LSUIElement`
treatment (a transient Dock icon). New behavior: if the executable path is inside
a `*.app/Contents/MacOS/` bundle, relaunch with `open -n -a "<bundle path>"`;
otherwise keep the current direct-exec path (bare-binary users are unaffected).

### 5. Packaging

- **CI** (`.github/workflows/release.yml`): after building the universal
  binaries, assemble the bundle — generate `Info.plist` with the tag version
  substituted, build `icon.icns` from the committed 1024 PNG, copy the binary in
  — then `ditto -c -k --keepParent "Multi-Claude Switcher.app" <zip>` (ditto
  preserves the bundle structure correctly) and attach the zip to the Release.
  The existing raw-binary and `.zip` assets are kept (raw binary is still the
  self-update download source).
- **Local** (`scripts/package-app.sh`): the same assembly against a locally built
  (or existing) universal binary, output to `dist/`. Single source of truth for
  the plist template and icon conversion, shared conceptually with CI.

### 6. Documentation

- **README Download section**: lead with the `.app` zip (double-click; first launch
  = right-click → Open once), keep the raw binaries as an "advanced / CLI" option.
  Update Quick Start to mention the local packaging script.
- Update `CHANGELOG.md` ([Unreleased] → rolled into 0.6.0 at release) and
  `FILELIST.md` (new files: `core/loginitem.go`, `scripts/package-app.sh`,
  `cmd/mcs-tray/assets/appicon-1024.png`, plist template).

## Testing

- **`core/loginitem.go`**: unit tests for enable → `LoginItemEnabled()` true →
  plist file exists with expected Label/ProgramArguments/RunAtLoad; disable →
  false + file gone. Use a temp `HOME` (inject the LaunchAgents dir) so tests do
  not touch the real `~/Library/LaunchAgents`.
- **Bundle-path detection** (the relaunch branch): a small pure helper
  `isInsideAppBundle(exePath) (bundlePath string, ok bool)` with table tests
  (bare path, bundle path, edge cases), so the branch is testable without actually
  relaunching.
- **Manual on-device verification** (per project rule — never test inside the
  Claude Desktop Code tab): build the `.app`, move to `/Applications`, confirm
  right-click→Open works, menu bar icon appears with no Dock icon, Start at Login
  writes/removes the plist and survives a logout/login, and a self-update from a
  lower version relaunches the app correctly as a bundle.

## Out of scope

- Code signing / notarization (no developer account).
- Windows packaging.
- Auto-update checksum/signature verification (already a separate backlog item).
- A DMG installer (a zipped `.app` is enough; DMG adds no value unsigned).
