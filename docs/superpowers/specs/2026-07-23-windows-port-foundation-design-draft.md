# Windows Port - Foundation Design Draft (Pre-Spec)

**Date:** 2026-07-23
**Author:** Claude Code (Windows-hosted session, running inside the Claude Desktop Code tab)
**Status:** Pre-design analysis. Read-only probing done on the real Windows box. **No code written.** Awaiting Vin's decision on the profile model (see §6) before authoring the formal openspec spec.
**Companion:** `docs/superpowers/handoffs/2026-07-22-windows-port-handoff.md`. This draft supersedes that handoff's §4/§5/§7 with verified Windows facts.

---

## 1. What this draft is / isn't

- **IS:** a consolidation of verified Windows facts + a decision framework for the one make-or-break choice: how to give each Claude account an isolated profile on Windows.
- **ISN'T:** the formal spec. The required workflow tooling (openspec CLI, superpowers skills) is **not installed** on this box, and the repo is **not cloned** here. The formal spec/plan will be authored in the standalone port session that has them. This draft feeds that.
- **Not settled here:** probe 3 (the launch test) could not be run safely in this session (see §3).

## 2. Verified environment facts (Windows 10 Pro, 2026-07-23)

Machine currently runs the **MSIX / enterprise-packaged** Claude Desktop:

- PackageFullName: `Claude_1.24012.1.0_x64__pzs8sxrjxfjjc`
- PackageFamilyName: `Claude_pzs8sxrjxfjjc`
- Launch AppID (AppUserModelID): `Claude_pzs8sxrjxfjjc!Claude`
- Install location: `C:\Program Files\WindowsApps\Claude_1.24012.1.0_x64__pzs8sxrjxfjjc\` (ACL-locked; the exe cannot be invoked directly)
- **No AppExecutionAlias** (`claude.exe`) is registered by the package.

**Data path (the handoff's biggest unknown - now RESOLVED):**

- Real on-disk data dir: `C:\Users\Vin\AppData\Local\Packages\Claude_pzs8sxrjxfjjc\LocalCache\Roaming\Claude\`
- Confirmed inside it: `config.json`, `claude-code-sessions\`, `claude_desktop_config.json`, plus the usual Electron dirs.
- The live process reports `--user-data-dir=C:\Users\Vin\AppData\Roaming\Claude`, but the real files are **not** there. MSIX redirects that path to the LocalCache dir above.
- **Implication:** the port must NOT treat the cmdline `--user-data-dir` as a filesystem path on MSIX; it must resolve the LocalCache redirect.

**DetectRunningProfile feasibility:** `--user-data-dir=` IS present in `Win32_Process.CommandLine` (verified), so the parse strategy is portable in principle, subject to the redirect caveat.

## 3. Hosting hazard (decides where testing can happen)

This Claude Code session runs **inside the Claude Desktop Code tab**, on the default profile. The MCS switch flow terminates Claude Desktop; doing that here kills this session. Therefore:

- **probe 3 (launch test) and all switch / terminate / backup / align end-to-end tests must run in a standalone `claude` session (a Windows terminal), never in the Desktop Code tab.** (Matches handoff §3 and iron rule IR-693.)
- This session's safe scope: design, spec/plan authoring, code, and unit tests that do not touch the live app.

## 4. Core mechanism recap

Each account = an isolated Claude Desktop profile keyed by its own data dir. The Code tab enumerates sessions only from `claude-code-sessions\<lastKnownAccountUuid>\`. Switching = terminate all Claude, then bring up the target profile. Cross-account sync = re-bucket session files under the target account UUID. The port must reproduce "bring up the target profile" on Windows.

## 5. Three candidate profile models

macOS does: `open -n -a Claude --args --user-data-dir=<path>`. Windows has three ways to reproduce "launch with an isolated profile"; they differ sharply in feasibility.

### Option A1 - MSIX + forwarded `--user-data-dir`
Launch the packaged app via AppID activation (COM `IApplicationActivationManager::ActivateApplication`, which takes AppUserModelID + an args string) and pass `--user-data-dir=<managed path>`, relying on Electron forwarding it to Chromium.
- **Pro:** no change to what Vin has installed.
- **Risk:** feasibility unverified and doubtful. No AppExecutionAlias exists, so the clean argv-forwarding route is out; packaged-app activation does not reliably forward arbitrary Chromium switches into Electron's argv. This is exactly what probe 3 must settle.
- **Resolved by:** probe 3.

### Option A2 - MSIX + swap the default data dir (rename / junction)
Pass no args. While the app is closed, point the default data dir (`...\LocalCache\Roaming\Claude`) at the selected profile - via directory rename or a junction/symlink - then launch normally by AppID. The app always reads its default dir; MCS controls what that dir physically is.
- **Pro:** sidesteps the arg-forwarding question entirely (works even if A1 is impossible). Launch is a plain AppID activation.
- **Risk:** more invasive writes into the MSIX virtual store; junctions inside `LocalCache` may have ACL/behavior quirks; higher corruption risk if the "terminate first + verify gone" guard ever fails. Backup-before-swap is mandatory.
- **Resolved by:** a junction/rename feasibility probe in the standalone session (probe 5).

### Option B - Require the standalone (non-Store) installer
Target Anthropic's standalone Windows installer instead of the MSIX/enterprise package.
- **VERIFIED (2026-07-23, claude.com/download):** a standalone Windows setup installer is offered and is the recommended install for individual users:
  - x64: `https://claude.ai/api/desktop/win32/x64/setup/latest/redirect`
  - arm64: `https://claude.ai/api/desktop/win32/arm64/setup/latest/redirect`
  - The MSIX package is positioned as the **enterprise** deployment method (Intune/SCCM/GPO/`Add-AppxPackage`). So "use the standalone installer" is the mainstream individual path, not an exotic ask.
- **Expected (UNVERIFIED - confirm empirically, probe 4):** standalone installs per-user (Squirrel-style, likely under `%LOCALAPPDATA%\AnthropicClaude\`), data in a visible `%APPDATA%\Claude` (no virtualization), and the exe is directly invocable with `--user-data-dir` - i.e. it mirrors the macOS model.
- **Pro:** cleanest, lowest-risk, closest to the existing darwin path. Custom `--user-data-dir` almost certainly works because you invoke the exe directly.
- **Con:** Vin (and every user) must run the standalone build. For Vin specifically that means installing the standalone alongside/instead of the current MSIX that hosts his sessions.

## 6. Recommendation + decision gate

**Recommended primary target: Option B (standalone installer).** It is the officially-recommended user install, removes all MSIX uncertainty, and reuses the darwin model almost verbatim (invoke exe + `--user-data-dir`). Treat MSIX support (A1/A2) as an optional later enhancement gated on probe results, not a launch blocker.

The one thing to decide before the spec:

- **(B-only):** Ship Windows support for the standalone build only; document "install from claude.com/download, not the enterprise MSIX" as a requirement. Simplest, fastest, most robust.
- **(B primary + A2 best-effort):** Ship B, plus attempt the junction-swap so MSIX users also work without reinstalling. More engineering + testing; larger risk surface.
- **(Probe A1 first):** Before deciding, run probe 3 in a standalone session; if MSIX forwards `--user-data-dir`, A1 becomes a no-reinstall option worth keeping.

## 7. Data-path discovery design (both layouts)

`platform/windows.go` `AppSupportDir()` / `FindProfiles()` must handle:

- **Standalone:** `%APPDATA%\Claude` (default) plus MCS-managed sibling profile dirs (proposal: `%APPDATA%\Claude<Name>` to mirror darwin, or `%LOCALAPPDATA%\MultiClaudeSwitcher\profiles\<name>`).
- **MSIX (if supported):** resolve `%LOCALAPPDATA%\Packages\<PFN>\LocalCache\Roaming\Claude`, discovering `<PFN>` by globbing `Packages\Claude_*` rather than hardcoding the publisher hash `pzs8sxrjxfjjc`.

Design implication: make "where is the data dir" a small resolver with a standalone branch and an MSIX branch, chosen by detecting which package is installed.

## 8. Managed-profile location & switching (open)

On macOS MCS creates multiple `Claude<Name>` dirs under Application Support and launches each with `--user-data-dir`. Windows-standalone maps cleanly (sibling dirs + `--user-data-dir`). MSIX-A2 inverts the model (one default dir, physically swapped), which the switch/backup/align code would need a Windows-specific path for. Keep darwin behavior byte-for-byte; add a Windows sibling, do not rewrite.

## 9. Next probes (run in the standalone port session)

- **probe 3 (A1 test):** launch via `IApplicationActivationManager::ActivateApplication("Claude_pzs8sxrjxfjjc!Claude", "--user-data-dir=<newEmptyDir>", ...)` (or `explorer.exe shell:AppsFolder\Claude_pzs8sxrjxfjjc!Claude`); check whether `<newEmptyDir>` gets populated = args forwarded. Non-destructive (new empty dir) but it launches the app - get Vin's OK, close + delete afterward.
- **probe 4 (Option B ground truth):** install the standalone build (VM/throwaway ideal) and confirm its `%APPDATA%\Claude` + exe path + that `Claude.exe --user-data-dir=X` spawns an isolated profile.
- **probe 5 (A2 test):** whether a junction at `...\LocalCache\Roaming\Claude` is honored by the packaged app.

## 10. Toolchain the port session must install (this box lacks all)

- **Go** (build/test). Repo not present here -> `git clone github.com/miou1107/multi-claude-switcher`.
- **openspec CLI** + **superpowers skills** (required workflow, currently absent).
- Then follow the process: brainstorming -> this draft -> formal spec in `docs/superpowers/specs/` -> writing-plans -> subagent-driven-development -> the 3 QA skills (verification-before-completion, requesting-code-review, receiving-code-review).

## 11. Iron-rule reminders carried over

English repo output; no `Co-Authored-By`; terminate-and-verify-gone before touching profile data; back up before destructive writes; verify against real behavior not docs; commit/push only when Vin asks; keep version in sync across `core.Version` / `CHANGELOG.md` / git tag; ask before minor/major bumps (patch is fine).

---

*End of draft. Decision needed at §6 before spec authoring.*

## 12. Decision & implementation status (2026-07-23)

**Decision: ship Option B (standalone build required) for the first Windows release.**
MSIX (A1/A2) is a documented future enhancement; `platform/windows.go` `LaunchProfile`
returns a clear "install the standalone build" error on MSIX-only machines. See the
README (Windows section) and CHANGELOG.

**Implemented and passing `go build ./...` + `go test ./...` on Windows (CGO=0):**
- `platform/windows.go` (+ `unsupported.go`, `windows_test.go`)
- `core/loginitem_{windows,darwin,other}.go`
- `core/backup_perm_{windows,unix}_test.go` (cross-platform staging-failure test)
- `cmd/mcs-tray/instance_{windows,unix}.go`, `relaunch_{windows,unix}.go`
- `cmd/mcs-tray/dialog_{windows,darwin,other}.go` (tray dialogs)
- `cmd/mcs-tray/update_platform_{windows,darwin,other}.go` (self-update)
- `.github/workflows/release.yml` windows-latest job

**Still pending (cannot be done inside a Claude Desktop Code tab, per IR-693):**
- Live e2e testing of switch / terminate / launch on a real standalone Claude Desktop
  install, from a standalone terminal `claude` session.
- CI-verify the macOS tray build still compiles (the tray's darwin build needs CGO + a
  mac toolchain, unavailable on the Windows dev box; Windows build/tests are all green).
