# Design Spec — Team-account detection & import-locked warnings

- Date: 2026-07-23
- Status: Design finalized, pending implementation plan
- Continues: `2026-07-22-multi-claude-account-sync-design.md` and `2026-07-22-probe-results.md`
- Related finding: a Claude **Team** account's Code sidebar is served by an Anthropic
  server API (`sessions_api_list_sessions`, scoped by `orgUuid`), so session files
  copied *into* a Team profile's local folder never appear. Sync can export **out of**
  a Team account but cannot import **into** one. See probe-results and both READMEs.

## 1. Problem & Use Case

The switcher happily copies Code sessions in any direction, but copying **into** a
Team account is a silent no-op (the Team sidebar is server-authoritative). A user who
turns on Auto Sync, or picks a manual sync direction that targets their company Team
account, gets no feedback that the import half will do nothing. They only discover it
by noticing the conversations never show up.

**Goal:** detect whether a profile's logged-in account is a **Team** account, and warn
the user at the moment they attempt an action that would try to **import into** it.

**Non-goal:** blocking the action. Detection is best-effort/heuristic (see §6); we warn,
we never hard-block, and we never mislabel on uncertainty.

## 2. What "Team" means here, and where the signal lives

Anthropic does not expose a clean `organization_type` field locally. The distinguishing
data is the account's **organization list**, cached in plaintext (Snappy-compressed
inside a LevelDB, not the encrypted OAuth token) at:

```
<profile>/Local Storage/leveldb/
```

Each organization entry carries `name`, `rate_limit_tier`, and `billing_type`.
On-device inspection (2026-07-23, both of the author's profiles):

| Profile | Account | Organizations found (tier) | Classification |
|---|---|---|---|
| `Claude` (company) | vincent@fontrip.com | **"Fontrip" → `default_raven`**, plus two personal orgs (`default_claude_ai`, `auto_api_evaluation`) | **Team** |
| `Claude_Profile2` (personal) | fontripdata@… | "…'s Organization" → `default_claude_max_20x` only | **Personal** |

`raven` is Anthropic's internal codename for the Team/Enterprise product; `default_raven`
is the Team-seat rate-limit tier. Personal plans use `default_claude_ai` / `_pro` /
`_max` / `_max_5x` / `_max_20x`, and the individual API org uses `auto_api_evaluation`.

**Why LevelDB and not the OAuth token or `config.json`:** `config.json` only holds
`lastKnownAccountUuid` and a `safeStorage`-encrypted token (would need the macOS Keychain
key / Windows DPAPI to decrypt — fragile, prompts, cross-platform pain). The org list is
already cached unencrypted in Local Storage. Local Storage uses LevelDB's **default**
comparator (unlike IndexedDB's custom `idb_cmp1`), and goleveldb natively decompresses
the block-level Snappy — so reading it needs only goleveldb, no custom comparer, no
separate Snappy dependency.

## 3. Architecture

Two layers, split so the decision logic is a pure, fully-testable function.

### 3.1 Reader — `core/accounttype.go` (`readOrgs`)

1. **Copy then read** (the profile's Claude may be running and holds a LevelDB lock):
   copy `<profile>/Local Storage/leveldb` to a temp dir, reusing the same copy approach
   as backups, then `goleveldb.OpenFile(..., ReadOnly)` on the copy. Always remove the
   temp copy afterward.
2. Iterate all key/values, decode Chromium Local Storage value encoding (leading byte:
   `0` = UTF-16LE, `1` = Latin-1/UTF-8), and from values that contain the account
   bootstrap payload extract every organization's `{name, rate_limit_tier, billing_type}`.
3. Return `[]orgInfo`. Any I/O or parse failure returns an error (→ `Unknown`, never a
   guess).

### 3.2 Classifier — `core/accounttype.go` (`classify`)

Pure function `classify(orgs []orgInfo) AccountType`:

```
type AccountType int   // Unknown, Personal, Team

TEAM_TIERS     = {"default_raven"}                       // extensible allow-list
PERSONAL_TIERS = {"default_claude_ai", "default_claude_pro",
                  "default_claude_max", "default_claude_max_5x",
                  "default_claude_max_20x", "auto_api_evaluation"}
```

- If any org's tier ∈ `TEAM_TIERS` → **Team**.
- Else if the list is non-empty and every org's tier ∈ `PERSONAL_TIERS` → **Personal**.
- Else (empty list, or any tier in neither set) → **Unknown**, and log the unrecognized
  tier so the allow-lists can be extended.

Both sets are explicit allow-lists; the classifier never infers from an unknown tier.

### 3.3 Caching / timing — `core/accounttype.go`

- Detect **once per profile at tray startup**, and **re-detect after each switch/sync**.
- Cache the result per profile path in memory (guarded by a mutex, like `markActive`).
- The menu build and the warning checks read the cache; they never open LevelDB inline
  (avoids per-menu-open latency).

## 4. UI & warning behavior — `cmd/mcs-tray`

### 4.1 Passive label (profile submenu title)

- A profile classified **Team** shows `🏢 Team` appended to its submenu title.
- **Personal** and **Unknown** show nothing (current appearance).
- `markActive` composes the title so the Team tag and the `(current)` marker coexist,
  e.g. `Company  🏢 Team  (current)`. The Team tag is derived from the cache, so the
  active-marker refresh loop must preserve it.

### 4.2 Active-time warnings (the primary feature)

Warn only for actions that would **import into** a Team account. Pure switching, and
exporting **out of** a Team account, are correct and stay silent.

1. **Enabling Auto Sync.** The existing one-time enable warning
   (`askEnableAutoSync` in `autosync.go`) gains an extra sentence **when any detected
   profile is Team**:
   > ⚠️ "<Name>" is a Team account — Code conversations **cannot be imported into it**.
   > Auto Sync will only export *out of* it, never merge others' conversations *in*.

   The warning still respects the existing "Enable, don't ask again" dismissal.

2. **Manual sync direction targeting a Team account.** In the "Sync sessions" submenu,
   a `From A → To B` direction where **B is Team** shows a confirmation/notice on click
   (reusing the dialog helpers) explaining the import half is a no-op for B. The user
   may proceed (export/other-direction effects still apply) or cancel.

If a profile is **Unknown**, no label and no warning — we prefer a missed warning over a
false one.

## 5. Dependency & platform

- Adds `github.com/syndtr/goleveldb` — pure Go, **no CGO**, so the tray stays
  CGO-free and Windows still builds.
- Local Storage path is analogous on Windows (`%APPDATA%\Claude\Local Storage\leveldb`);
  the reader takes the path from the platform layer, so the feature is cross-platform by
  construction. (Windows verification is follow-up, consistent with the project's
  macOS-first stance.)

## 6. Known limitations (accepted, documented)

1. **Cache staleness.** The org list is a cache; a profile whose app hasn't run recently
   may be stale. Tier/billing rarely change, so impact is low.
2. **Reverse-engineered fields.** `raven` and the tier names are undocumented internals;
   a Claude update could rename them. Mitigation: unrecognized tier → `Unknown` (graceful
   under-warn), logged so the allow-list can be updated. No mislabeling.
3. **Multi-org accounts over-warn.** An account that is a member of *any* Team org is
   classified Team even if the user normally works in a personal org under it. This
   over-warns in the safe direction (warns about import into a real Team org that genuinely
   can't be imported into).
4. **New Team tier under-warns.** A Team seat on a tier codename not yet in `TEAM_TIERS`
   classifies as `Unknown` → no warning. This is the deliberate consequence of the
   pure-auto-detection decision (no manual override); the allow-list is easy to extend.

## 7. Testing

- **Classifier (pure):** table test over org combinations — Team-only, personal-only,
  mixed (Team + personal → Team), empty (→ Unknown), unknown-tier (→ Unknown). The two
  real accounts above are fixtures.
- **Value decoding:** unit test the Chromium UTF-16LE / Latin-1 value decoder.
- **Reader:** smoke test against a small committed LevelDB fixture (or a temp store built
  in-test) to prove the copy-open-extract path returns the expected orgs.
- **Warning gating:** table test that the warning fires iff the import target is Team
  (mirrors `TestShouldWarnAutoSync`).

## 8. Design History

- User chose **pure auto-detection** with no manual override; `Unknown` fallback retained
  (that is not an override, only a "don't mislabel" safety valve).
- User chose the **passive label after the account name**, then clarified the real intent:
  the "cannot import" message must fire at **action time** — when enabling Auto Sync or
  when a sync direction imports into a Team account — not merely as a static badge.
