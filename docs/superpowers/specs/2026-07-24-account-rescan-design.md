# Design Spec — Account Rescan & review-to-manage picker

- Date: 2026-07-24
- Status: Design finalized, pending implementation plan
- Continues: `2026-07-23-team-account-detection-design.md`, `2026-07-22-probe-results.md`
- Related code: `platform/*.FindProfiles`, `core/accounttype.go`, `core/names.go`,
  `core/settings.go`, `cmd/mcs-tray/main.go`

## 1. Problem & Use Case

Today the tray discovers profiles by scanning `~/Library/Application Support` for
`Claude*` directories, then the menu applies a **hardcoded filter**
(`cmd/mcs-tray/main.go:75`): a directory is shown only if it has a Code sessions dir,
or is `Managed`, or is literally named `Claude` / `Claude_Profile2`. That filter has
three problems:

1. A real logged-in account in a non-standard directory (e.g. `Claude_Work`) that has
   no Code sessions yet is **hidden**.
2. "Has a sessions dir" is a poor test of "is a real account" — it lets junk
   directories through and hides fresh logins.
3. The managed set is fixed in code; the user has **no control** over which accounts
   the switcher manages.

**Goal:** add a **"Rescan accounts…"** action that scans the machine for Claude
accounts, presents a **review table** the user can inspect (UUID, completeness, email,
Team flag, conversation count, last-updated, note), and lets the user **pick which
complete accounts to manage**. The picked set is persisted and replaces the hardcoded
filter.

**Non-goal:** recovering accounts that were logged out of a shared directory (see §6),
switching or syncing behavior changes, and a rich native window (macOS free-tier uses
`osascript`, see §5).

## 2. What is scannable, and where the signals live

Each account's data lives in a sibling `Claude*` directory. Two independent UUID
sources exist inside each directory, with very different meaning:

| Source | Path | Meaning | Survives logout? |
|---|---|---|---|
| Live login | `<dir>/config.json` → `lastKnownAccountUuid` | the account currently logged into this dir | No (single slot, overwritten on re-login) |
| Session buckets | `<dir>/claude-code-sessions/<AccountUUID>/` | every account that has used Code in this dir | **Yes** (bucketed by UUID, never cleaned) |

On-device inspection (2026-07-24, author's machine):

| Dir | Live login | Session buckets (json count) |
|---|---|---|
| `Claude` | `035899b2` | `035899b2`(395), `ae543f88`(82), `f047dab6`(19) |
| `Claude_Profile2` | `ae543f88` | `ae543f88`(395), `f047dab6`(2) |
| `Claude-3p`, `ClaudeBar` | none | none (junk dirs, skipped) |

**Human-readable identity** (`email`, `display_name`, `full_name`, `account_uuid`, org
memberships) is cached in `<dir>/Local Storage/leveldb` and is reliably extractable via
the existing goleveldb reader — but **only for the live-login account** of a dir. A
proven sample value:

```
ajs_user_traits:      {"email":"vincent@fontrip.com", "account_uuid":"035899b2-…", …}
react-query-cache-ls: {"uuid":"035899b2-…","email_address":"vincent@fontrip.com",
                       "full_name":"Fontrip Vin","display_name":"Vin","memberships":[…]}
```

Team classification reuses `core.DetectAccountType` (reads the same Local Storage org
tiers), so Team status is also only available for live-login accounts.

## 3. Data model & completeness

The **manage/switch unit is the directory** (the switcher launches
`--user-data-dir=<dir>`). The review table is **presented by account (deduped by
UUID)** for human comprehension, but a checked complete account maps to its directory,
and `managed.json` stores **directory folder names**.

### 3.1 Dedup / completeness rules

1. **Complete account** — one row per directory that has a live login. Keyed by its
   `lastKnownAccountUuid`. Identity + Team come from that dir's Local Storage. This row
   is **selectable** (checkbox).
   - If the same account UUID is the live login of two directories (rare), that is two
     complete rows (two switchable dirs) — do not collapse them.
2. **Ghost (incomplete) account** — a session bucket whose UUID is **not** the live
   login of any directory anywhere. Deduped across dirs into one row. Has only
   UUID / conversation count / last-updated; no email, no Team. This row is
   **read-only** (not selectable).
3. **Stale duplicate — folded away.** A session bucket whose UUID matches some complete
   account's live login in another dir is a stale copy of a live account, **not** a
   separate row. (Example: `Claude/ae543f88`(82) is folded away because `ae543f88` is
   live in `Claude_Profile2`.)

Applied to the sample data, this yields exactly three rows: two complete
(`035899b2`, `ae543f88`) and one ghost (`f047dab6`).

### 3.2 Per-field derivation

- **UUID** — the account UUID (bucket name / `lastKnownAccountUuid`).
- **Completeness** — `Complete` (live login) or `Incomplete` (ghost), per §3.1.
- **email** — from the live dir's Local Storage; `—` when unavailable (ghost, or read
  failure on a complete account).
- **Team** — `core.DetectAccountType` on the live dir → `Yes` (Team) / `No` (Personal)
  / `?` (Unknown or ghost).
- **Conversation count** — number of `*.json` files in that account's bucket. For a
  complete account: its live dir's bucket. For a ghost: the sum across the dirs where
  the orphan bucket appears.
- **Last updated** — newest session-json mtime in that same bucket set (e.g. `2026-07-24`).
- **Note** — derived, in this precedence:
  - Team account → `Team account — conversations can't be synced`
  - Ghost/incomplete → `Invalid account data`
  - Personal complete account → *(blank)*

  (UI strings are English to match the existing tray voice; the Chinese phrasing in the
  design dialogue maps to these.)

## 4. Architecture

Four layers, keeping the decision logic pure and testable.

### 4.1 Identity reader — `core/identity.go` (new)

Extend the Local Storage reader (sibling to `readLocalStorageOrgs`) to extract an
`AccountIdentity{UUID, Email, DisplayName, FullName}` from the cached account payloads
(`ajs_user_traits` / `react-query-cache-ls`). Reuses `decodeLocalStorageValue` and the
copy-then-open pattern. Returns a zero value + error when unreadable (never panics).

### 4.2 Scanner — `core/scan.go` (new)

`ScanAccounts(profiles []*platform.ProfileInfo) []ScannedAccount` — pure over the
platform's already-discovered dirs:

```
type ScannedAccount struct {
    UUID        string
    Complete    bool
    HomeFolder  string        // folder name where it is the live login ("" if ghost)
    Email       string
    Account     core.AccountType   // Team/Personal/Unknown
    Convos      int
    LastUpdated time.Time
    Note        string
}
```

1. For each dir read `lastKnownAccountUuid` (live login) and enumerate
   `claude-code-sessions/*` buckets (the platform already populates `UUIDBuckets`).
2. Build the live-login set across all dirs.
3. Emit one complete row per live-login dir (identity + Team from §4.1 / §2); emit one
   ghost row per orphan UUID not in the live-login set; fold stale duplicates (§3.1).
4. Fill counts, last-updated, and the derived note.

### 4.3 Managed registry — `core/managed.go` (new)

`~/.multi-claude-switcher/managed.json` = `{"managed": ["Claude", "Claude_Profile2"]}`
(a list of folder names). Mirrors `names.go` / `settings.go`: mutex-guarded, atomic
tmp+rename write. API: `LoadManaged() []string`, `SetManaged([]string) error`,
`IsManaged(folder) bool`.

**First-run seeding:** when `managed.json` is absent, treat **all complete accounts'
home folders** as managed. This preserves today's behavior (nothing silently
disappears) without writing a file until the user makes an explicit choice.

**Menu filter change:** replace the hardcoded condition at `cmd/mcs-tray/main.go:75`
with `core.IsManaged(folderName)` (falling back to the first-run seed when the file is
absent).

**Stale prune:** on rescan, a managed folder that no longer exists on disk is dropped
from `managed.json`.

### 4.4 Tray action & two-step UI — `cmd/mcs-tray/rescan.go` (new)

Add **"Rescan accounts…"** to the Maintenance submenu (next to Backup / Check for
Updates, `main.go:139-143`). Handler:

1. `FindProfiles()` → `core.ScanAccounts(...)`.
2. **Step 1 — review dialog.** An `osascript` text dialog whose body is the 7-column
   table rendered as a monospace, column-aligned block (all rows, ghosts included).
   Buttons: `Cancel`, `Continue`.
3. **Step 2 — pick dialog.** `osascript` `choose from list` with multiple selection,
   listing **only complete accounts**; currently-managed accounts are pre-selected.
   Ghost rows are omitted here (they are read-only, shown only in step 1).
4. On confirm: `SetManaged(selectedFolders)`, then `relaunchSelf()` to rebuild the
   static systray menu (the established pattern for "the profile set changed",
   `main.go:348`).

Cancel at either step makes no change.

## 5. UI & platform constraints

- macOS free-tier UI is `osascript`. It **cannot** render a true multi-column table or
  make individual `choose from list` rows non-selectable. The two-step flow works
  around both: step 1 is a read-only aligned-text preview (so 7 columns stay legible),
  step 2 lists only selectable (complete) accounts.
- **Column order (final):** `UUID, Completeness, email, Team, Convos, Last updated, Note`.
- Windows standalone shares the directory model and can reuse the scanner; its picker
  UI is a follow-up. The Windows MSIX/Store build uses a slot-swap registry
  (`.mcs-profiles\state.json`) and is out of scope for this spec.

## 6. Known limitations (accepted, documented)

1. **Logged-out accounts in a shared dir are unrecoverable.** `config.json` / cookies /
   Local Storage are single-slot; re-login overwrites them. Only accounts that occupy
   their own directory are surfaceable. A user who only ever used one `Claude` dir sees
   exactly one account.
2. **UUID count ≠ manageable accounts.** Session buckets reveal every account that used
   *Code* in a dir, but a ghost has no login/token and **cannot be switched to** — it is
   informational only, shown as `Incomplete` / `Invalid account data`.
3. **Undercount for chat-only accounts.** An account that only ever used web chat (never
   Code) leaves no session bucket, so it is not counted among historical UUIDs.
4. **Ghost identity is usually unresolvable.** A ghost UUID that is no dir's live login
   has no Local Storage identity, so email/Team show `—`/`?`.
5. **Buckets are never cleaned.** Counts can include long-dead accounts.

## 7. Testing

- **Scanner (pure):** table test over dir/bucket combinations — live-only, live+ghost,
  ghost-only, stale-duplicate folding, junk dir skipped, multi-dir same-UUID. The sample
  data in §2 is a fixture (expected: 2 complete + 1 ghost).
- **Identity reader:** unit test extraction of email/display_name/uuid from a committed
  or in-test Local Storage fixture; graceful error on unreadable/absent store.
- **Managed registry:** round-trip load/save, first-run seed (absent file → all complete
  managed), stale prune (missing folder dropped), atomic write.
- **Note derivation:** table test of the precedence in §3.2 (Team → Ghost → blank).
- **Menu filter:** `IsManaged` gates menu inclusion; absent-file fallback seeds from
  complete accounts.

## 8. Design History

- User chose **dedup by account UUID with ghost rows shown read-only** (not by-dir-only,
  not a cleanup tool) — ghosts appear in the review so the user sees incomplete data,
  but cannot be managed.
- User chose the **two-step UI** (aligned-text table preview → multi-select pick) over a
  single squished native list or a local-HTML table (rejected for the browser→app
  round-trip complexity).
- Columns finalized at 7, order `UUID` first; Team and Note columns added late; the
  last-updated column is labeled **"Last updated"**; personal accounts' note is blank.
- First-run seeding, pre-checking currently-managed accounts, and pruning missing
  managed folders were approved as defaults.
