# Design: Manual "Align" + Auto Sync-on-Switch

- **Date:** 2026-07-22
- **Status:** Approved (design), pending implementation
- **Target version:** 0.7.0 (minor — new user-facing feature)
- **Builds on:** `2026-07-22-multi-claude-account-sync-design.md` (Safe Switch + `SyncSessions`) and `2026-07-22-probe-results.md` (bucket-naming invariant)

## 1. Problem

The tool can switch accounts and, today, always runs a **one-way** session sync
(source → target) as part of every switch. Two gaps surfaced in use:

1. **No way to reconcile without switching.** The user sometimes wants to push
   one account's sessions into another *without* changing which account is
   active — e.g. "make my personal-account sessions also show up under the
   company account, but keep me where I am."
2. **No user control over whether switching moves data at all.** The always-on
   one-way sync is invisible and not opt-out. The user wants an explicit
   on/off, and wants the "keep both sides identical" behavior to be a deliberate
   choice, not a silent side effect.

## 2. What actually gets synced (scope reality)

Grounded in on-device inspection (2026-07-22), only **local, account-bucketed
session files** can be moved between profiles. Everything else is either already
shared or physically unmovable:

| Data | This feature | Why |
|---|---|---|
| `claude-code-sessions/<accountUUID>/…/local_*.json` (Code tab) | ✅ **Phase 1** | Flat `.json`, account-bucketed, **proven** to display after re-bucketing (natural experiment) |
| `local-agent-mode-sessions/<accountUUID>/…` (Agent/Cowork workspaces: `.jsonl` transcripts, `audit.jsonl`, uploaded files, generated artifacts) | ⏳ **Phase 2, gated** | Real work, account-bucketed too, but a *subtree* (not flat `.json`) and **display-after-re-bucketing is unverified** |
| Regular chat (left sidebar) | ❌ never | Server-side, per account; only local trace is `IndexedDB/https_claude.ai` identity-bound web storage |
| `~/.claude` (memory, skills, CLAUDE.md) | ❌ untouched | Already globally shared, account-independent |
| `config.json` identity/auth, Cookies, tokens | ❌ never | Copying = both profiles become the same account = switching does nothing |
| `claude-code` / `claude-code-vm` runtime & caches | ❌ never | Version-keyed artifacts, huge, regenerable |

**Cross-account merge is intended (user-confirmed, self-use).** After an align,
one account's conversations appear under the other account. The user has
explicitly accepted this for personal use; it is exactly the "history follows
you across accounts" behavior. Because it is a privacy consideration for the
general public, the *automatic* variant defaults **off** (see §4.2).

## 3. Sync mechanics (shared by both features)

Reuse the existing, tested `core.SyncSessions(src, dst)`:

- Reads the **source** profile's own account bucket and copies those sessions
  into the **target** profile, re-homed under the **target's** account UUID
  (the bucket-naming invariant — the app only reads
  `claude-code-sessions/<lastKnownAccountUuid>/`).
- **Additive, non-destructive:** session filenames are globally unique
  (`local_<sessionUUID>.json`), so a copy adds entries and never overwrites the
  target's own. When the same path exists with different content, newer-wins by
  mtime; an older source is left as a reported conflict, never a silent
  overwrite.

**Directional vs bidirectional:**

- **One direction** (`A → B`) = one `SyncSessions(A, B)` call.
- **Bidirectional union** (`A ⇄ B`) = `SyncSessions(A, B)` then
  `SyncSessions(B, A)`. Order matters: after the first call the target holds the
  union, so the second call carries the union back and both sides converge to
  `A ∪ B`. Idempotent (identical files are skipped).

## 4. The two features

### 4.1 Manual "Align" (directional, one-shot)

A tray action that copies one profile's sessions into another **without
switching the active account** — when it finishes, the user is left on the same
account they started on.

**Flow when the user picks "From X → To Y":**

1. If Claude Desktop is running, show a native confirm dialog stating it will be
   **closed, synced, and reopened on the same account you're on now**. Cancel =
   abort, do nothing.
2. Remember which profile is currently running (call it `R`).
3. Close Claude Desktop (`TerminateApp`, verified — never write into a live
   profile).
4. **Back up the target `Y`** before writing (mandatory; abort on backup
   failure, matching Safe Switch).
5. `SyncSessions(X, Y)`.
6. Relaunch `R` (the profile that was running), so the user lands back where
   they were. If nothing was running in step 2, launch nothing.
7. Notify: copied / skipped / conflict counts.

**UI:** a `Sync sessions` submenu listing each ordered pair by display name
(`From Company → To Personal`, `From Personal → To Company`). For the common
two-profile case that is exactly two items. For N profiles it is the N·(N−1)
ordered pairs (acceptable at small N).

**Note:** manual align is *not* a switch — it never leaves you on a different
account. It only moves data and returns you to `R`.

### 4.2 Auto Sync-on-Switch (toggle, default OFF)

A checkable tray item (same pattern as **Start at Login**) that changes what a
**switch** does:

- **OFF (default):** switching is a **pure account switch — no session data is
  moved at all.** Each account keeps only its own sessions.
- **ON:** switching performs a **bidirectional union** during the switch, so
  both accounts converge to identical history over normal use.

**Why the switch window is the safe automatic moment:** at switch time the tool
already closes the running app *before* launching the target, so for that window
**both** profiles are closed and safe to write. That is what makes an automatic,
bidirectional align safe without any background file-watching daemon.

**Revised `SafeSwitch` (from source `A` running → target `B`):**

1. Close `A` (if running).
2. **Back up only the profiles that will be written.** ON writes into both `A`
   and `B` (bidirectional), so back up both. OFF writes nothing, so no backup is
   taken — a pure switch touches no session data. A backup failure on a profile
   about to be written aborts the switch.
3. **If auto sync ON and both accounts are logged in:** `SyncSessions(A, B)`
   then `SyncSessions(B, A)` (bidirectional union). **If OFF:** skip sync
   entirely — no session files move.
4. Launch `B`.

**Behavior change (deliberate):** today's switch always does a one-way sync.
After this change the default (toggle OFF) switch moves **no** data. This is the
user's explicit choice: "开关关 = 纯切换, 什么都不动." The old one-way-on-every-switch
behavior is retired in favor of the explicit toggle.

**Default OFF rationale:** this is public OSS; auto-merging one account's
conversations into another is a privacy decision each user must opt into. The
author enables it for personal use.

**Enable-time warning (with "don't remind me again"):** turning the toggle
**ON** (OFF → ON) first shows a native confirm dialog explaining the
consequence — *every switch will bidirectionally sync, so both accounts'
conversations merge* — and enabling only proceeds if confirmed. Turning it OFF
needs no warning.

macOS `display dialog` (our only dialog mechanism, via `osascript`) has **no
real checkbox control**, so the "don't remind me again" affordance is
implemented as a **third button** rather than a checkbox — identical outcome:

> ⚠️ With this on, **every account switch bidirectionally syncs** — both
> accounts' conversations will merge. Enable?
> `[Cancel] [Enable] [Enable, don't ask again]`

- **Cancel** → leave the toggle OFF.
- **Enable** → turn ON; warn again next time it is enabled.
- **Enable, don't ask again** → turn ON and set `autoSyncWarningDismissed`, so
  future enables skip the warning.

The dismiss flag only suppresses the warning; it never changes sync behavior.

### 4.3 Settings storage

A small `core/settings.go` persisting
`{ "autoSyncOnSwitch": bool, "autoSyncWarningDismissed": bool }` to
`~/.multi-claude-switcher/settings.json` (same directory and JSON pattern as
`names.json`). API: `AutoSyncOnSwitch() bool` / `SetAutoSyncOnSwitch(bool)
error`, and `AutoSyncWarningDismissed() bool` / `SetAutoSyncWarningDismissed(bool)
error` (atomic temp+rename write). The tray checkbox reflects and toggles the
first; `SafeSwitch` reads the first; the enable-time warning (§4.2) reads and
sets the second.

## 5. Safety (unchanged invariants, reaffirmed)

- **Never write into a live profile.** Every write path (manual align, auto
  align) first verifies Claude Desktop is terminated.
- **Backup before every write.** Manual align backs up the target; auto sync
  switch backs up every profile it will write to; a backup failure aborts.
- **Additive, newer-wins, conflicts reported** — never a silent overwrite
  (existing `SyncSessions` behavior).
- **No background daemon, no live symlink** — the only automatic moment is the
  switch, when both profiles are already closed.

## 6. Phase 2: Agent Mode (gated, not in this release)

Adding `local-agent-mode-sessions` to both features requires, in order:

1. **Display-verification probe** (standalone env, back up first, use throwaway
   copies, restore after, leave no junk): copy an agent-mode workspace subtree
   into another profile re-homed under that profile's account UUID, relaunch
   Claude Desktop, and confirm the Agent/Cowork view actually lists it. If it
   does **not** display, agent-mode sync is not viable and stops here.
2. **Subtree-copy engine** (only if the probe passes): unlike the flat `.json`
   Code-session copier, agent-mode is a nested workspace tree (`.jsonl`
   transcripts, `audit.jsonl`, uploaded files, generated artifacts). Needs a
   recursive copy with the same additive/newer-wins/backup guarantees, plus a
   check for any absolute paths inside the tree (`.claude/projects/…`) that
   might not survive relocation.

Until both are done, both features operate on Code sessions only.

## 7. Testing

- **`core/settings.go`:** both flags (`autoSyncOnSwitch`,
  `autoSyncWarningDismissed`) round-trip; default false when the file is
  absent; atomic write; one flag's change does not clobber the other (stub the
  settings dir to a temp `HOME`).
- **Enable-warning gating:** the pure decision "should the warning show?" is a
  small testable helper (show when turning ON and not dismissed; never when
  turning OFF); the `osascript` dialog itself is exercised in manual on-device
  verification.
- **Bidirectional union (`core`):** two temp profiles with distinct account
  buckets → after `SyncSessions(A,B)` + `SyncSessions(B,A)`, both buckets equal
  `A ∪ B`; identical files skipped (no duplicate work); a newer target file wins
  and is reported as conflict, not overwritten.
- **`SafeSwitch` toggle branch:** with auto sync OFF, no session files move and
  no backup of the source is taken beyond existing behavior; with ON (both
  logged in), both profiles end with the union. Skip-sync-but-still-launch when a
  profile has no account UUID is preserved.
- **Manual align returns to origin:** the relaunched profile equals the one that
  was running (`R`), not the sync target.
- **Manual on-device verification** (never inside the Claude Desktop Code tab):
  run a manual align between the two real profiles, confirm the target account
  shows the source's Code sessions after relaunch and that the active account is
  unchanged; toggle auto sync on, switch both ways, confirm convergence.

## 8. Out of scope

- Agent-mode sync (Phase 2, gated on §6).
- Regular-chat sync (server-side; impossible locally).
- Config/preferences field-level sync (separate future feature; see
  probe-results "Config / Preferences Sync Analysis").
- Background/live continuous sync and symlink mode (rejected: unsafe while an app
  is open).
- Windows.
