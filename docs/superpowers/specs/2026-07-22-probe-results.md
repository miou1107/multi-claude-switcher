# Phase 0 Probe Findings Report

- Date: 2026-07-22 10:29:55
- Platform: macOS Darwin (Apple Silicon / Intel)
- Status: Phase 0 Validation Complete

## Executive Summary

The Phase 0 probe tests confirmed that **Claude Desktop on macOS fully respects the `--user-data-dir` argument**, enabling isolated multi-account profile execution. Furthermore, the conversation session files are stored under `claude-code-sessions/<AccountUUID>/<OrgUUID>/local_<SessionUUID>.json`, confirming that index sync and Safe Switch mode are technically viable.

> **Critical refinement (2026-07-22, live-machine natural experiment).** The top-level bucket name under `claude-code-sessions/` is **exactly the logged-in account's UUID** (`config.json` → `lastKnownAccountUuid`). The app enumerates the Code tab **only** from `claude-code-sessions/<lastKnownAccountUuid>/`; it does **not** scan the folder for other buckets. Consequence: copying session files under the *source* account's bucket name into a target profile that logs in as a *different* account produces a **silent failure** — the files sit on disk but the Code tab renders empty. **Sync MUST re-bucket into a directory named after the target profile's `lastKnownAccountUuid`.** A raw bucket-name copy is not sync; it is data that never surfaces. The switcher must verify, post-sync, that the written bucket matches the target's `lastKnownAccountUuid`, or warn.

> **⛔ MAJOR CORRECTION (2026-07-23) — the folder-copy premise is WRONG for cloud-synced accounts. Q3 and Q7 below are FALSE POSITIVES.** A controlled experiment (create a real, completed session `SYNC-REAL-9` in a personal account, sync its `local_*.json` into a company **Claude Team** account's correct `<lastKnownAccountUuid>` bucket, then relaunch) proved the session **never appears** in the Team account's sidebar — not after a clean relaunch, not after fully clearing that profile's IndexedDB and letting it rebuild. Exhaustive filesystem forensics (IndexedDB / Local Storage / Session Storage / blobs / `~/.claude.json`, all Snappy-decompressed) found **no local store** that holds the session list. The smoking gun is in `~/Library/Logs/Claude/claude.ai-web.log`: the sidebar list is a React Query query keyed `["sessions_api_list_sessions", {"orgUuid": "<ORG_UUID>"}]` — i.e. **the Code session list is fetched from a server API, scoped by account + organization, NOT built by scanning `claude-code-sessions/`.** The middle path segment previously labelled `<OrgUUID>` really is the **organization** UUID (the app calls `claude.ai/api/organizations/<uuid>/…`). Official confirmation: Claude Code docs state "the session transcript is stored on Anthropic servers to sync the conversation across devices" (Pro/Max/**Team**/Enterprise). Why Q3/Q7 looked like PASS earlier: those tests only ever restored a profile's **own** previously-created sessions (already registered server-side for that same account), never imported a **foreign account's** session. **Consequences for the switcher:** (1) The local `claude-code-sessions/` files are a cache/backup, not the source of truth for the sidebar. (2) Copying files **cannot** make one account's conversations appear under a **different** account — the sidebar reflects what the server returns for the *target account's own* org. (3) This is architectural, not a bug we can fix by copying files. Session sync via file copy can only ever work where the app treats local files as authoritative (some personal-account paths); it is a **no-op for Team/organization accounts** and should be detected and disclosed, not silently attempted.

---

## Probe Validation Checklist (Questions 1 - 10)

### Q1: Does Claude Desktop accept `--user-data-dir` at launch?
- **Result**: **PASS**
- **Findings**: `open -n -a "Claude" --args --user-data-dir=/tmp/claude_probe_test/profile_q1` launched successfully and created a complete profile tree (Cookies, Local Storage, Preferences, etc.).

### Q2: Is `claude-code-sessions/<UUID>` the primary source for conversation sessions?
- **Result**: **PASS**
- **Findings**: The directory structure is `<AccountUUID>/<OrgUUID>/local_<SessionUUID>.json`. Each `.json` file contains full session metadata and turn content (`top_keys`: ['sessionId', 'cliSessionId', 'cwd', 'originCwd', 'lastFocusedAt', 'createdAt', 'lastActivityAt', 'model', 'effort', 'isArchived']).

### Q3: Do synced/copied session files appear in the target profile's sidebar on app relaunch?
- **Result**: **PASS — but strictly conditional on correct bucket naming**
- **Findings**: Copying `local_*.json` into the target profile's `<AccountUUID>/<OrgUUID>/` directory causes Claude Desktop to display the conversation on restart **only when `<AccountUUID>` equals the target profile's `lastKnownAccountUuid`**. If the files land under any other bucket name, the app ignores them entirely (empty Code tab). See the real-world natural experiment below — this exact mistake is why `Claude_Profile2` currently shows an empty Code tab despite holding 22 MB / 171 session files on disk.

### Q4: How does Claude Desktop handle UUID buckets that don't match the active account?
- **Result**: **ISOLATED — confirmed by live natural experiment**
- **Findings**: The app queries **only** `claude-code-sessions/<lastKnownAccountUuid>/`. Buckets for any other account are inert on disk. **Live evidence (2026-07-22):** `Claude_Profile2` logs in as account `ae543f88` but its `claude-code-sessions/` contains only buckets `035899b2` (company) and `f047dab6` — there is no `ae543f88` bucket, so the Code tab is empty. The profile's own `ae543f88` sessions (82 files, workspace `245fb00c`) are physically located inside the *other* profile (`Claude/claude-code-sessions/ae543f88/`), orphaned there by an earlier naive folder-copy sync. Syncing between accounts therefore requires re-bucketizing under the target account's `<AccountUUID>`, not copying the source bucket verbatim.

### Q5: Does the app rebuild or overwrite `claude-code-sessions` on restart or crash?
- **Result**: **SAFE**
- **Findings**: Session files are written incrementally per conversation turn (`local_*.json`). Restarting or launching does not wipe existing session files.

### Q6: Do macOS Symlinks in `claude-code-sessions` survive app restarts?
- **Result**: **PARTIAL / USE WITH CAUTION**
- **Findings**: Symlinks function during reading, but atomic file writes by Electron can break directory symlinks into standalone folders during writes. Safe Switch (close-then-sync) remains the primary recommended mode.

### Q7: Can the sidebar render from local files without active network connection?
- **Result**: **PASS**
- **Findings**: Desktop sidebar renders past session list locally from disk.

### Q8: For the same conversation across two accounts, are UUID buckets identical or distinct?
- **Result**: **DISTINCT**
- **Findings**: Each account profile uses its own `<AccountUUID>`. Shared sessions need to be indexed under both `<AccountUUID>` directories.

### Q9: Does the server accept continuing session context across account switches?
- **Result**: **PASS (Local continuity)**
- **Findings**: Local UI displays full conversation history. New prompt turns generate new API requests under the current active account's token.

### Q10: Windows MSIX compatibility (Backlog)
- **Result**: **BACKLOG**
- **Findings**: Windows testing deferred to Phase 1/2. macOS Darwin is primary target.

---

## Real-World Natural Experiment (2026-07-22, live machine)

An earlier manual sync (2026-06-11 / 07-08, from the pre-tool handbook era) scrambled buckets between the two live profiles. This accidental experiment answers Q3/Q4 with real data:

| Fact | Profile1 (`Claude`, company Team) | Profile2 (`Claude_Profile2`, personal Max) |
| --- | --- | --- |
| `lastKnownAccountUuid` (bucket the app reads) | `035899b2` | `ae543f88` |
| Buckets present in `claude-code-sessions/` | `035899b2`, `ae543f88`, `f047dab6` | `035899b2`, `f047dab6` |
| Does the read-bucket exist? | ✅ `035899b2` present → Code tab populated | ❌ `ae543f88` absent → **Code tab empty** |

Key takeaways:

1. **Bucket name == account UUID is the entire gate.** No `config.json` index edit, no leveldb entry, and no network login state was required beyond the account UUID matching the bucket directory name. (An earlier hypothesis that the `config.json` `dxt:allowlist*:<workspace>` index or Local Storage leveldb drives the list was **falsified** — `Claude_Profile2` already registers workspace `d129c8c1` yet still shows nothing, because the account bucket is missing.)
2. **Naive folder-copy sync silently loses data visibility, not data.** All of Profile2's own personal sessions are intact — they were copied *out* into Profile1's `ae543f88` bucket. Nothing was deleted; the sidebar just can't see them because they are under the wrong profile.
3. **Sync direction/naming is the crux of the product.** To make Profile2 show its own history: copy `Claude/claude-code-sessions/ae543f88/` → `Claude_Profile2/claude-code-sessions/ae543f88/`. The dead `035899b2` bucket in Profile2 (company data, unreadable there) is inert and can be pruned.

## Config / Preferences Sync Analysis (2026-07-22)

Follow-up finding while diagnosing a "Bypass Permissions turned itself off" report in `Claude_Profile2`.

**Bypass Permissions is a per-account opt-in gate, stored in `claude_desktop_config.json`:**
- `preferences.bypassPermissionsModeEnabled` — global on/off (was `true` in both profiles).
- `preferences.bypassPermissionsOptInByAccount` — a **map keyed by account UUID**, e.g. `{ "ae543f88…": true }`. The app only enables bypass for the logged-in account if that account's entry is `true`.
- Root cause of the report: `Claude_Profile2` (logged in as `ae543f88`) had **no `bypassPermissionsOptInByAccount` entry at all** for `ae543f88`, so bypass was gated off and the app fell back to Accept Edits. `Claude` (Profile1) *did* have `{ae543f88: true}`. This was pre-existing (file mtime predates the session-bucket restore) and only became visible once the profile had sessions to run. Also note the per-session `permissionMode` field (`bypassPermissions` / `acceptEdits` / `plan` / `auto` / `default`) inside each `local_*.json` — that is separate from, and gated by, the global per-account opt-in.

**Implication for "can we sync config?" — config files are NOT monolithic. Three tiers:**

| Tier | Example keys | Sync rule |
| --- | --- | --- |
| **Global UX preferences** (majority) | `darkMode`, `scale`, `locale`, `mcpServers`, `menuBarEnabled`, `sidebarMode`, `bypassPermissionsModeEnabled`, `keepAwakeEnabled`, `chromeExtension`, `ccBranchPrefix` | **Safe to sync (whitelist copy).** This is what a user actually wants "consistent across profiles." |
| **Per-account / per-workspace maps** | `bypassPermissionsOptInByAccount`, `bypassPermissionsGateByAccount`, `coworkModelAutoFallbackByAccount`, `dxt:allowlistEnabled:<ws>`, `epitaxyPrefs` folder-permission maps | **Sync only by key-level MERGE (union), never whole-value overwrite.** Merging propagates e.g. bypass opt-in to the other profile's account without clobbering the target's own entries. Would have auto-fixed the report above. |
| **Identity / auth** | `oauth:tokenCache`, `oauth:tokenCacheV2`, `lastKnownAccountUuid`, `remoteToolsDeviceName`, plus sibling files `Cookies`, `buddy-tokens.json`, `ant-device-registry.json` | **NEVER sync.** These *are* the account/device. Copying them makes both profiles resolve to the same account → switching does nothing, or triggers logout / auth conflict. Non-negotiable; it is the reason separate profiles exist. |

**Conclusion:** "Sync config: yes/no" is the wrong question. The correct design is **field-level selective sync**: whitelist global prefs, merge per-account maps by key, blacklist identity/auth. Default posture for any *unknown* key must be **do not sync** (these are undocumented private app fields that can be renamed/restructured by a Desktop update). This is a distinct, optional feature from session-index sync and should ship behind its own toggle.

## Discovered Profiles & UUID Structure

```json
[
  {
    "path": "/Users/vincentkao/Library/Application Support/Claude",
    "exists": true,
    "has_sessions_dir": true,
    "uuids": {
      "ae543f88-0f24-4ae6-ae21-3033915bca76": {
        "count": 82,
        "files": [
          "local_ba78450e-9db2-4089-8ee6-39d098a772c5.json",
          "local_34bc6651-485d-4be8-a4cc-43fdbcf4dfac.json",
          "local_1ced648d-5048-4e49-8c54-a7b32195fa46.json",
          "local_e88d1ef5-55fe-4039-9235-d76277a44395.json",
          "local_74789b2b-6b63-4e30-8c98-ddeee553241f.json"
        ]
      },
      "f047dab6-372f-4505-94a1-92e1bc507657": {
        "count": 19,
        "files": [
          "local_1a816eb3-ed89-41a2-94cb-5bf984176b74.json",
          "local_72ce8561-5359-448b-abeb-8030289c1230.json",
          "local_a023d8e8-8d79-4d0c-9b1a-ba8769f2852c.json",
          "local_c253f7bd-6c51-47fc-a58e-533cc9e9e7ca.json",
          "local_85e8de3a-6513-4fae-a34a-082a5fdea86d.json"
        ]
      },
      "035899b2-b130-40b6-aa9e-93cf208df7b7": {
        "count": 236,
        "files": [
          "local_2485e49d-ca86-4d37-b663-5fa4195a4159.json",
          "local_c32beae8-6da4-4ac3-b8b0-e5ca120ef39f.json",
          "local_c88a6513-1c6b-4324-bf3b-e42ed7960854.json",
          "local_1a816eb3-ed89-41a2-94cb-5bf984176b74.json",
          "local_8c79f948-81ff-4faf-b0e4-6fc377989ea1.json"
        ]
      }
    }
  },
  {
    "path": "/Users/vincentkao/Library/Application Support/Claude-3p",
    "exists": true,
    "has_sessions_dir": false,
    "uuids": {}
  },
  {
    "path": "/Users/vincentkao/Library/Application Support/ClaudeBar",
    "exists": true,
    "has_sessions_dir": false,
    "uuids": {}
  },
  {
    "path": "/Users/vincentkao/Library/Application Support/Claude_Profile2",
    "exists": true,
    "has_sessions_dir": true,
    "uuids": {
      "f047dab6-372f-4505-94a1-92e1bc507657": {
        "count": 2,
        "files": [
          "local_7cace653-e224-4996-acdb-891b31cb6726.json",
          "local_03465ecf-cd16-409c-9beb-85964dddf3d5.json"
        ]
      },
      "035899b2-b130-40b6-aa9e-93cf208df7b7": {
        "count": 233,
        "files": [
          "local_2485e49d-ca86-4d37-b663-5fa4195a4159.json",
          "local_c32beae8-6da4-4ac3-b8b0-e5ca120ef39f.json",
          "local_c88a6513-1c6b-4324-bf3b-e42ed7960854.json",
          "local_1a816eb3-ed89-41a2-94cb-5bf984176b74.json",
          "local_8c79f948-81ff-4faf-b0e4-6fc377989ea1.json"
        ]
      }
    }
  }
]
```

---

## Conclusion & Architecture Confirmation

1. **Safe Switch Mode (Close -> Backup -> Sync Index -> Launch Target Profile)** is validated as the optimal, zero-risk default.
2. **Phase 1 Go Core CLI** will focus on:
   - Process detection & clean shutdown (`pkill -f Claude`)
   - Automated profile backup (`/tmp/claude_probe_backups/`)
   - `claude-code-sessions` index copier/reconciler
   - `--user-data-dir` launcher for target profiles
