# Design Spec — Seamless Multi-Account Switching & Sync for Claude Desktop

- Date: 2026-07-22
- Status: Design finalized, pending implementation (Phase 0 probe first)
- Continues: prior manual migration handbook for Claude Desktop dual accounts
- Adversarial review: one pass by gpt-5.5 (codex); key feedback incorporated (see "Design History")

## 1. Problem & Use Case

On a single machine, a user has multiple Claude Desktop app accounts (e.g., a company Team account + a personal Max subscription). When one account runs out of quota mid-work, the user needs to switch to the other account to keep developing. But each account's conversations, memory, skills, and state live in a separate profile, so switching breaks the flow: it forces log out / log in, and the sidebar/history is gone, requiring the user to re-establish context.

**Goals** — when the user switches accounts after hitting a quota limit:

1. No log out / log in (switch lands in an already-authenticated state)
2. Conversations, memory, skills, and state stay consistent (the other side looks the same)
3. One click to do it

**Primary walkthrough**: develop on the company Team account → quota runs out → click tray "switch to personal Max" → no login dance, conversations & memory all present → keep developing.

## 2. Scope

- **Platform**: Claude Desktop app. First release targets **macOS only**; Windows is backlog (architecture leaves room for it).
- **Sync scope**: between account profiles on the **same machine**. **Local only — no cloud, no third-party service login.**
- **Business model**: free, open source, published on GitHub.
- **Non-goals (v1)**:
  - Cross-machine sync (symlinks are same-machine only; cross-machine needs cloud, deferred)
  - Company data-leak governance (user explicitly excluded it for the personal use case)
  - Real-time dual-instance sync (Claude Desktop does not live-refresh the sidebar — see below)

## 3. Machine Reality (macOS inspection, 2026-07-22)

- Claude Desktop is an **Electron app**. A "profile" = launching with a distinct `--user-data-dir`. Known profile dirs: `~/Library/Application Support/Claude` (Profile 1, company Team) and `~/Library/Application Support/Claude_Profile2` (personal Max). Both can run at once (user-verified).
- **Login/identity** lives inside each profile's user-data-dir: `Cookies`, `buddy-tokens.json`, `config.json`, `Local State`, `ant-did`, plus browser storage (`IndexedDB`, `Local Storage`, `Session Storage`, `Preferences`, `Network Persistent State`, `Service Worker`).
- **Desktop conversation index** (the sidebar source) lives in `<user-data-dir>/claude-code-sessions/<account-UUID>/*.json`, one UUID bucket per account.
- `~/.claude/` (skills, memory, commands, CLAUDE.md, conversation JSONL under `projects/`) is **already globally shared** and independent of the Desktop profile.
- **Confirmed limitation** (user's prior testing): the Desktop app reads the sidebar conversation list **only at launch**; it does **not live-refresh** (conversations added by another process are invisible until the app restarts).

## 4. Core Design Decisions

### 4.1 Sync model: "Safe Switch" default, symlink demoted to experimental

**Default = Safe Switch.** On account switch, the tool performs, in order:

1. Detect and close the currently running Claude profile (if any)
2. Take a **timestamped snapshot backup** of the target profile's `claude-code-sessions`
3. **Sync the index** — update the **target** profile's conversation index to match the source, including the source's newest conversations; the invariant is "both sides have identical content"; detect two-sided changes, and on conflict prompt the user (never silently merge)
4. Launch the target profile via `--user-data-dir`

> Why not default to "always-identical" symlinks: a symlink bets that Claude Desktop's private on-disk format is a stable interface. An app update that rebuilds the folder, an atomic write via temp-file rename, or temp cleanup can replace the symlink with a real directory → both sides silently diverge and the user won't notice immediately. Treating the Desktop index as a "corruptible, rebuildable cache" is more robust.
>
> **Key: Safe Switch is still one click for the user.** Because switching accounts already requires relaunching the app (Desktop doesn't live-refresh), "close → sync → open the other" feels just as seamless to the user, while adding backups and safety.

**Experimental option = Live Symlink Mode.** Clearly labeled high-risk, off by default. Precondition: only one Claude process may use the shared bucket at a time; the tray enforces a process check and refuses to launch otherwise.

### 4.2 Shared vs. isolated boundary

| Data | Handling | Reason |
|---|---|---|
| `claude-code-sessions/<UUID>/` (Desktop conversation index) | **Synced** (safe switch) / optional symlink | Sidebar source; the main thing to sync |
| `~/.claude/` (skills, memory, commands, CLAUDE.md, conversation bodies) | **Already shared, untouched** | Globally shared, account-independent |
| Login/identity: `Cookies`, `buddy-tokens.json`, `config.json`, `Local State`, `ant-did` | **Isolated, never synced** | Sharing these = same account = switching does nothing |
| Browser storage: `IndexedDB`, `Local/Session Storage`, `Preferences`, `Network Persistent State`, `Service Worker` | **Isolated, never synced** (treated as identity-bound until the probe proves otherwise) | Identity/account state may hide here (mixed state) |
| Caches: `Cache`, `Code Cache`, `GPUCache`, `Crashpad`, etc. | **Isolated** | Per-profile artifacts; sharing only cross-pollutes |

### 4.3 Safety mechanisms (across all operations)

- **Timestamped snapshot backup before every sync**; all operations are rollback-able
- **Treat the Desktop index as a corruptible cache**: any operation can be rolled back, rebuilt, or diffed
- **Process lock**: if two profiles are detected using the same shared bucket, block writes or warn and disable sharing (enforced in symlink mode)
- **Profile health check** (before every switch): symlink still present, target writable, UUID as expected, a recent backup exists, no other Claude process is using the same shared index

## 5. Architecture

```
multi-claude-switcher (Go project, cross-platform structure)
├─ core/            Shared logic: switch, sync/reconcile, diff, backup/restore, health check
│                   (no OS specifics; everything goes through the platform interface)
├─ platform/
│   ├─ platform.go  Interface: resolve profile paths, launch (--user-data-dir), create/remove
│   │               links, detect/kill process, read UUID buckets
│   ├─ darwin.go    macOS implementation (now)
│   └─ windows.go   Windows implementation (backlog; interface reserved; defaults to
│                   close-then-sync, never symlink)
└─ ui/
    ├─ tray/        Menu-bar quick actions (Go systray, cross-platform):
    │               current profile, switch, sync status, open settings, quit
    └─ settings/    Settings window (Wails, one Go codebase, cross-platform):
                    profile list, two-sided diff, conflict resolution, backup/restore,
                    auto/manual toggle, dangerous operations
```

- **Language: Go** (one cross-platform core, single binary; used on macOS now, reused on Windows later)
- **Platform differences confined behind the `platform/` interface**: Windows later fills in `windows.go`; core and UI stay unchanged
- **UI split in two**: the tray holds few actions; diff / conflict / backup-restore go in the settings window (too much for a tray menu)
- **The real Windows difficulty is the sync mechanism, not the GUI** (MSIX sandbox may not honor symlinks/junctions, `--user-data-dir` may be swallowed, admin/developer mode may be required, file locks are stricter). The architecture can prepare for it, but viability still requires testing on Windows.

## 6. Implementation Phases

### Phase 0: Probe (first; answers the make-or-break unknowns)

No production code — use manual steps / quick scripts to answer the questions below. **Always back up first, prefer a throwaway temporary profile pointing at a copy of the data (never touch the two real accounts directly), delete test data afterward, leave no junk.**

**Test/dev environment requirement (mandatory)**: testing and development MUST run in a **standalone environment** — a terminal Claude Code CLI, **or Antigravity (or any non-Claude-Desktop IDE)**. **Do NOT run inside the Claude Desktop app's Code tab.** Reason: the probe closes/reopens Claude Desktop and moves its profile folders; if the dev session is hosted inside the Desktop app, it would interrupt itself and could corrupt in-use data. Note: the Claude CLI shares `~/.claude/` with the Desktop app, but the probe only touches Desktop profile folders (`~/Library/Application Support/Claude*`), not `~/.claude/`, so CLI/Antigravity are safe.

Validation checklist (incl. codex adversarial suggestions):

1. Does Claude Desktop accept an arbitrary `--user-data-dir` at launch, and still after an app update?
2. Is `claude-code-sessions/<UUID>` the **sole** source for the sidebar (or does it also read IndexedDB / Local Storage / Preferences)?
3. After a symlink/index-sync, does the other profile's app **show** the shared conversations in the sidebar? (← the make-or-break question)
4. When the UUID doesn't match the logged-in account, does Claude Desktop ignore, rebuild, error, or read normally?
5. Does the app **rebuild** `claude-code-sessions` on launch, logout, update, or crash recovery? (the nastiest symlink death)
6. Does a macOS symlink survive an app update?
7. Is the sidebar rendered from local files, or does it require login to show anything?
8. For the same conversation under two accounts, is it in the same UUID bucket or separate UUIDs?
9. On quota-exhaust switch continuing the same conversation, does the server accept the same session context, or does it only look continuous in local UI?
10. (Windows, backlog) Can MSIX pass `--user-data-dir` and read external junctions/symlinks?

**Output**: a clear verdict on whether symlink / safe-switch is viable, deciding Phase 1's direction.

### Phase 1: Go core + Safe Switch (usable CLI first)

- `core/` + `platform/darwin.go`: switch, index sync, backup/restore, diff, health check, process detection
- Ship a usable CLI first (`switch`, `sync`, `status`, `restore`) so the user can dogfood
- Safe Switch by default; symlink behind a hidden experimental flag

### Phase 2: GUI

- Tray (Go systray) + settings window (Wails)
- Auto/manual toggle, diff view, backup/restore, launch-at-login, code signing/notarization

## 7. Open Unknowns (to close in Phase 0)

- All 10 Phase 0 checklist items
- If symlink is not viable → go fully with safe switch (close-then-sync) and drop symlink mode
- The actual UUID-bucket identity-binding behavior (items 4 & 8) affects whether "sync" copies a specific bucket or needs a neutral shared UUID

## 8. Design History (key reversal)

- **Original approach**: symlink "always-identical" as the default.
- **Reversal** (this spec, adopting the codex adversarial review): switch to "Safe Switch (close-then-sync + backup) as the default; symlink demoted to an experimental option." Rationale: symlinks bet on the app's private format staying a stable interface and break silently on updates, whereas Safe Switch is still one click for the user, so the feel is unchanged while safety improves. User approved the reversal.
- **Unchanged**: Go core + platform adapter layer, macOS first / Windows backlog, free OSS.

## 9. Repo & Naming

- Repo: `github.com/miou1107/multi-claude-switcher` (public); local at `/Users/vincentkao/SourceCode/multi-cloude` (standalone git repo, `main`)
- Product name: `multi-claude-switcher`
