# Design Spec — Ghost account recovery, and one profile per account

- Date: 2026-07-30, revised the same day after adversarial review of the first plan
- Status: Design finalized. No open questions; the merge conflict question is closed
  at §5.2. Implementation plan needs the corrections listed in §10 applied.
- Overturns: limitation 6.1 of `2026-07-24-account-rescan-design.md`
  ("logged-out accounts in a shared dir are unrecoverable")
- Related code: `core/scan.go`, `core/managed.go`, `core/sync.go`, `core/backup.go`,
  `platform/windows_msix.go`, `platform/darwin.go`, `platform/windows.go`,
  `internal/panelui/render.go`, `cmd/mcs-menubar/main.go`, `cmd/mcs-tray/panel_windows.go`

## 1. Problem

Three users on the team hit the same wall. They switched accounts **inside Claude
Desktop** (sign out, sign in as somebody else) rather than through MCS. Then they ran
Rescan and found an account they could see but not use: a row labelled
`Unrecognized account` / `Invalid account data`, greyed out, no checkbox.

On-device evidence, gathered 2026-07-30 (read-only inspection, three machines):

| Machine | Build | Live login | Session buckets present | Orphans |
|---|---|---|---|---|
| Machine 1 (macOS) | standalone | `aaaaaaaa` in `Claude` | `aaaaaaaa`, `bbbbbbbb`, `ffffffff` | 2 |
| Machine 2 (Windows) | MSIX Store | `cccccccc` in slot | `cccccccc`, `bbbbbbbb` | 1 |
| Machine 3 (Windows) | MSIX Store | `bbbbbbbb` in slot | `dddddddd`, `bbbbbbbb`, `eeeeeeee` | 2 |

Neither Windows machine has ever used MCS's create-profile flow (no
`.mcs-profiles`, no `state.json`). `bbbbbbbb` appears on all three: it is a shared
account, logged in on one machine and signed out on the other two.

The scanner is behaving correctly. `config.json` holds exactly one
`lastKnownAccountUuid`, so a re-login overwrites it, while
`claude-code-sessions/<uuid>/` buckets are never cleaned. One directory therefore ends
up holding one live account and N sets of orphaned conversations. `core/scan.go`
classifies those orphans as ghosts and renders them read-only, which is honest but
useless: the user is told something is wrong and given nothing to do about it.

**Two things are wrong, and they are separate.**

1. **No way out.** `runNewProfileFlow` (`cmd/mcs-tray/profiles_windows.go:26`) already
   implements exactly the right recovery on the Store build, and it is unreachable:
   it is wired only to the legacy systray menu (`cmd/mcs-tray/main.go:111`), and since
   v0.10.0 Windows runs `onReadyWindowsPanel` (`cmd/mcs-tray/onready_windows.go:73`),
   whose right-click menu is just Quit. macOS and Windows standalone have no
   equivalent at all. **The users who need this cannot reach a feature that is already written.**
2. **A dead end presented as a defect.** "Invalid account data" reads as corruption.
   The data is intact. What is missing is a directory that claims the account.

### 1.1 Why 6.1 of the rescan spec was too pessimistic

That spec concluded logged-out accounts are unrecoverable because credentials are
single-slot. The credentials genuinely are unrecoverable, but that is not what the
user wants back. What they want back is the **conversations**, and those survive in
the bucket. Give the account its own directory, put the bucket in it, and have the
user sign in once: Claude reads `claude-code-sessions/<lastKnownAccountUuid>/` and the
history is there. The login is re-established by the user, not restored by MCS.

## 2. Goals and non-goals

**Goals**

1. A reachable way to create a new account profile on **every** platform.
2. A reachable way to recover an orphaned account, from the place the user already
   discovers the problem (Rescan).
3. Detect when two profiles hold the same account and drive the user to merge, so
   "one profile per account" holds.
4. Never delete user data.

**Non-goals**

- Restoring a login. The user signs in once per recovered account. If they no longer
  have that account's credentials, recovery is not possible and the flow is cancellable.
- Recovering accounts that never used Code. No bucket exists, so there is nothing local
  to bring back. Ordinary chat conversations live on Anthropic's servers and return on
  sign-in regardless.
- Preventing in-app account switching. Claude Desktop owns that; MCS cannot intercept
  it. This design makes the aftermath recoverable, not impossible.
- Team-account sync behaviour. Unchanged (`2026-07-23-team-account-detection-design.md`).

## 3. Data model

**Read §3.5 first.** What identifies a profile is not obvious on the Store build,
and every registry in this section keys on it.

### 3.1 `ScannedAccount` additions

```go
type ScannedAccount struct {
    // … existing fields …

    // Recoverable marks a ghost whose conversations can be brought back: its
    // bucket exists and is non-empty, so giving it its own profile and signing in
    // reunites account and history. Set only on ghost rows.
    Recoverable bool

    // Sources lists every profile that holds part of this ghost's conversations.
    // Ghost rows only. Recovery copies from all of them.
    Sources []GhostSource

    // Pending marks a profile MCS just created that is waiting to be signed in to.
    // It is a separate flag from PendingUUID because an empty PendingUUID is
    // meaningful on its own: "waiting for any account" (the add path) as opposed to
    // "not pending at all".
    Pending bool

    // PendingUUID names the account this profile is waiting for, when the profile
    // was created to finish a recovery. Empty on the add path. Only read when
    // Pending is true.
    PendingUUID string
}
```

```go
// GhostSource is one profile holding part of an orphaned account's conversations.
type GhostSource struct {
    Folder string // profile identity, for display and for keying (§3.5)
    Path   string // where the bucket actually is, for the copy
    Convos int    // that profile's share of the row's conversation count
}
```

`Recoverable` is false for a ghost with an empty bucket: there is nothing to recover,
so it stays read-only and keeps its existing note.

**`Sources` is a list, not a single folder,** because `assembleAccounts` already sums
one orphan UUID across every dir that holds a bucket for it (`core/scan.go:101`). The
same signed-out account really can have conversations in two profiles — sign in as it
in profile A, switch to B, sign in as it there too, then sign out of both. A
`SourceFolder` string would silently recover one share and leave the other behind,
under a row whose count promised both.

It carries `Path` as well as `Folder` because a folder name cannot be turned back into
a path outside the `platform` package (§3.5). The path is already in hand:
`ScanAccounts` receives `[]*platform.ProfileInfo`, which has it.

### 3.2 Row kinds after this change

`rowRank` ordering becomes: complete, pending sign-in, recoverable ghost, dead ghost.

| Kind | Live login | Buckets | Note shown |
|---|---|---|---|
| Complete | yes | any | Team warning, or duplicate marker, or blank |
| Pending sign-in | no | copied bucket or none | "Sign in to finish setting this up" |
| Recoverable ghost | no | non-empty | "Signed out in Claude Desktop. Recover to sign back in." |
| Dead ghost | no | empty | existing "Invalid account data" |

### 3.3 Pending-sign-in registry

A profile MCS has just created has no `config.json` yet, so today's scanner either
drops it or, once a bucket has been pre-copied into it, misfiles it as a ghost.

MCS records its own pending profiles in **its own directory**, not inside Claude's
data area: `~/.multi-claude-switcher/pending.json`.

```json
{ "pending": [
  { "folder": "Claude_Recovered 2026-07-29",
    "expectUUID": "bbbbbbbb-0000-4000-8000-000000000002",
    "createdAt": "2026-07-30T14:02:11+08:00" }
] }
```

`folder` is the profile identity as `FindProfiles` reports it (§3.5), which is the same
key `managed.json` and `names.json` already use. On the Store build that is the name
from `state.json`, not a directory name.

`ScanAccounts` gains this list as a parameter, so it stays a pure function over
(profiles, managed-state) and remains table-testable:

```go
func ScanAccounts(profiles []*platform.ProfileInfo, pending []PendingProfile) []ScannedAccount
```

Rules:

- **A dir named in `pending` is never dropped.** `ScanAccounts` currently discards any
  dir with no live login, no buckets, and no `config.json` (`core/scan.go:219`). A
  freshly created profile matches all three until Claude has run in it, so without this
  exception the profile the user was just told to sign in to vanishes from the panel
  before they can sign in. The pending list overrides the drop.
- A dir named in `pending` with no live login becomes a **pending sign-in** row.
- If its `expectUUID` matches a bucket in that dir, that bucket is **not** also
  emitted as a ghost. Otherwise the same conversations appear twice, once as the
  thing being recovered and once as the problem.
- An entry whose folder has a live login is stale: the sign-in happened. It is
  pruned on the next scan. **This is the only pruning rule.**

**An entry whose profile is not in `FindProfiles`' output is _not_ pruned**, which is
the opposite of what a first draft of this spec said. On the Store build the profile
MCS just created has *no directory at all* between creation and the app's next launch:
`msixParkForNewIn` renames the slot away and deliberately leaves it absent so the
packaged app makes a clean one. Pruning on absence would delete the pending entry
seconds after writing it, on the one platform this feature exists for. Sign-in is the
only event that means "done", so it is the only thing that prunes.

The cost of that is an entry which outlives a profile the user deleted by hand,
rendering a permanent "Sign in to finish setting this up" row. Accepted: it is a row
the user can act on, and the alternative silently discards the entry that makes the
new profile visible at all.

The Store build needs one supporting change for the same reason. `msixFindProfiles`
emits the active profile only when the slot directory exists (`platform/windows.go:101`),
so between creating a profile and the app starting, the account list drops the current
profile entirely — true today, before this feature. It must instead emit the profile
named by `state.json` with `Exists: false` when the slot is absent. `ProfileInfo.Exists`
is set to `true` in both `inspectProfile` implementations and read by nothing
(`platform/darwin.go:54`, `platform/windows.go:125`), so this is the field's first real
use and no consumer changes.

Writing into `~/.multi-claude-switcher/` rather than into a Claude profile keeps MCS
out of a directory whose format it does not own, and matches where `managed.json`,
`settings.json`, and `backups/` already live.

### 3.4 Duplicate detection

Duplicates are detected **in the renderer, not the scanner.** The warning lives on the
account list, which `RenderList` draws from `[]ProfileVM` built by each host's
`buildProfiles` from `FindProfiles`. That function already reads every profile's account
UUID and currently discards it (`cmd/mcs-menubar/main.go:330`), so the account list
holds everything the check needs. `ProfileVM` gains `UUID string`, and `RenderList`
groups by it.

Routing this through `ScanAccounts` instead would mean scanning on every panel open
purely to learn something the host already knows, and `ScanAccounts` reads Local Storage
for each signed-in profile, which is the expensive step the panel is deliberately built
to render around (`cmd/mcs-menubar/main.go:83`). `ScannedAccount` therefore gains no
duplicate field.

Profiles with no account signed in have an empty UUID and must not group with each
other: two profiles both awaiting sign-in are not two profiles sharing an account.

`ProfileVM` gains no `Path`. The merge handler receives two identities from the webview
and resolves them to paths with a fresh `FindProfiles` call, because a path is a
`platform` concern and, on the Store build, is only valid for the current scan (§3.5).
Resolving at action time also re-checks that both profiles still exist and still hold the
same account, which is the guard §6 requires against a stale panel.

Duplicates cannot arise from in-app account switching, which produces ghosts inside a
single directory. They arise when two directories are signed in to the same account,
which is a possible misuse of the very flow this spec adds: the user is told to sign in
as a different account and signs in as an existing one instead. Handling it is
therefore in scope for this change, not a separate concern.

### 3.5 Profile identity

**A profile's identity is `ProfileInfo.Name`. It is not derivable from its path, and a
path is not derivable from it.** Everything MCS persists keys on the identity; anything
that needs a path must be handed one.

This is not a new rule. `managed.json` already keys on `Name` (`core/managed.go:74`) and
`names.json` maps `Name` to a display name (`core/names.go:66`). What is new is stating
it, because the Store build breaks the assumption that `Name == filepath.Base(Path)` and
an earlier draft of the implementation plan relied on exactly that.

| | Identity | Data directory |
|---|---|---|
| macOS, Windows standalone | the directory name under `AppSupportDir()`, e.g. `Claude_Work` | `<AppSupportDir>/<identity>` |
| Windows MSIX, active profile | `state.json`'s `current`, e.g. `Work` | `<roaming>/Claude` — the shared slot |
| Windows MSIX, parked profile | the directory name under `.mcs-profiles`, e.g. `Work` | `<roaming>/.mcs-profiles/<identity>` |

Three consequences follow, and each one is a defect if ignored.

1. **`filepath.Base(path)` is not the identity.** On the Store build the active
   profile's path always ends in `Claude` (`platform/windows.go:102` names it from
   `state.json` instead). Keying `pending.json` on `filepath.Base` would write an entry
   for a profile called `Claude` that `FindProfiles` never reports, so the entry would
   never match, while `managed.json` gained a phantom and the real profile stayed
   invisible. Profile creation must therefore **return the identity** rather than let
   callers compute it.
2. **`filepath.Join(AppSupportDir(), identity)` is not the path.** On the Store build
   `AppSupportDir()` returns `%APPDATA%`, while the data lives under
   `%LOCALAPPDATA%\Packages\Claude_<hash>\LocalCache\Roaming`, and a parked profile sits
   one level deeper again. Reconstruction is a `platform` concern; every caller outside
   `platform` must carry the path it was given by `FindProfiles`. That is why
   `GhostSource` has a `Path` (§3.1).
3. **A profile's path changes; its identity does not.** Switching accounts on the Store
   build moves directories in and out of the slot, so today's path for a profile is not
   tomorrow's. A cached path is only valid for the current scan. An identity is valid
   until the user renames the folder.

**The user's typed name becomes the display name, not the identity.** `CreateProfile`
picks the identity that suits its platform, and MCS then records
`SetProfileName(identity, typedName)`. The user sees `Work` on both platforms even
though the folder is `Claude_Work` on one and `Work` on the other — and this incidentally
fixes today's behaviour, where a profile created on the Store build shows a clean name
and one created anywhere else shows a `Claude_`-prefixed one.

**Registry comparisons use the exact recorded string.** `managed.json` compares with `==`
(`core/managed.go:76`). MCS writes the identity into every registry from one value returned
by one call, so the strings agree. Do not add case-insensitive matching to work around a
mismatch; a mismatch means something reconstructed an identity instead of carrying it.

**Comparisons against `state.json` are the one exception, and stay case-insensitive.** The
shipped Store-build code already matches that way — `msixSwapToIn` compares its target
against `current` with `EqualFold`, and `msixValidateNameIn` rejects a new name that folds
onto `current` or onto an existing parked directory (`platform/windows_msix.go`). Two Store
profiles therefore cannot exist differing only in case, so folding cannot select the wrong
one; and switching to exact matching *would* break, because a `current` written in one case
and compared against a directory in another would resolve to a path that does not exist.
Follow the shipped convention there, and the exact-match rule everywhere else.

**One known pre-existing violation, out of scope here.** `SyncSessions` formats its
"no account signed in" errors with `DisplayName(filepath.Base(profilePath))`
(`core/sync.go:85`, `:90`), so on the Store build both ends of a failed sync are called
`Claude` whatever the profiles are named. It is error text only — nothing is keyed on it —
and fixing it means changing a shipped signature, so it is recorded rather than folded into
this change. Do not copy the pattern.

## 4. User-visible design

The panel is 400px wide and every screen lives in the same webview
(`internal/panelui/render.go`). All four screens below are panel views. No native
dialogs are added.

### 4.1 Account list (`RenderList`)

Two additions.

**A dashed "add" card at the bottom of the card list**, reading
`＋ Add another account`. It goes in the list rather than the footer: the footer
already holds Rescan, Settings, and Quit, and a fourth labelled button does not fit
in 400px without demoting Rescan to a bare icon.

**A warning block above the cards when a duplicate group exists:**

> "Work" and "Claude" are the same account. Merge them to clean this up.  `[ Merge ]`

Each card in the group also carries a small `Duplicate` pill so the user can see which
rows the warning means. The warning is not dismissible and persists until the merge
happens. Switching accounts is **not** blocked: merging requires quitting Claude, and
a merge that cannot complete must never leave the user unable to use the app.

### 4.2 Rescan (`RenderRescan`)

The recoverable-ghost row stops being read-only:

> **Signed out in Claude Desktop**
> `bbbbbbbb` · 94 chats · 2026-07-29
> Its conversations are still here. Recover to sign back in.   `[ Recover ]`

The note moves from the red "bad" style to the blue "todo" style already used for
profiles awaiting sign-in. Red implied corruption; nothing is corrupt. Red is now
reserved for the duplicate warning, which is the only state that genuinely needs
fixing.

Dead ghosts (empty bucket) keep the existing red read-only treatment.

A machine with several orphans shows several Recover buttons. Recovery is **one at a
time**: each one requires its own sign-in inside Claude Desktop, which MCS cannot
batch. Machine 3 above has two: recover one, return to Rescan, recover the next.

### 4.3 Name the profile (`RenderNewProfile`)

One screen, two entry points, differing only in copy and pre-filled name.

| | From "Add another account" | From "Recover" |
|---|---|---|
| Title | Add another account | Recover this account |
| Name field | empty, placeholder `Personal` | pre-filled `Recovered 2026-07-29`, editable |
| Body | "Claude closes, your current account is saved, and a clean Claude opens." | same |
| Second line | warning: "Sign in as a **different** account. Signing in as one you already have creates a duplicate, and MCS will ask you to merge." | instruction: "Sign in as the account ending `bbbbbbbb` (94 chats). Its conversations come back on their own." |
| Confirm | Add | Recover |

Sharing one view keeps the two paths from drifting: they run the same underlying
operation and differ only in whether a bucket is pre-copied.

Name validation, before anything is touched: non-empty after trimming; no path
separators or `..`; restricted to letters, digits, space, `-`, `_`. On failure the screen
re-renders with the reason and nothing on disk changes.

**Validation returns the cleaned name, and only the cleaned name travels on.** It is a
value, not a boolean: `ValidateProfileName(raw) (clean string, err error)`. A validator
that trims a local copy and returns only an error invites the caller to keep using the
raw string, which is what `msixValidateNameIn` does today — it trims for its checks and
`msixParkForNewIn` then writes the untrimmed argument into `state.json`
(`platform/windows_msix.go:151`, `:254`). Typing ` Work ` would create a profile whose
identity has spaces around it. Passing a pre-cleaned name avoids triggering that;
`msixParkForNewIn` should also trim, so the latent bug is closed rather than merely
avoided.

Collision checks stay with the platform, because what counts as a collision differs:
a directory under `AppSupportDir()` on standalone, versus `state.json`'s `current` or a
parked directory on the Store build. `core` runs the portable character rules first and
the platform reports its own collisions as ordinary errors, which re-render the screen
the same way.

### 4.4 Merge (`RenderMerge`)

Two cards, one per duplicate profile. **The profile currently in use is pre-selected**
to keep, because keeping it means no re-sign-in. The user may pick the other one
instead, which matters when they named it something meaningful. The unselected card is
labelled `Will be archived`.

> All 141 conversations are combined into the account you keep. The other folder is
> archived, not deleted, so you can put it back yourself.

The combined count is the union of both profiles' bucket contents for that UUID, which
requires counting both before rendering. That same walk yields the conflict count, and
when it is non-zero the screen adds the disclosure sentence from §5.2 above the buttons.
The screen is a preview of a computed plan, not a description of an intention.

## 5. Mechanism

Two underlying implementations, one user-facing behaviour.

| Platform | Create a profile | New platform code |
|---|---|---|
| Windows MSIX (Store) | park the live slot under `.mcs-profiles/<current>`, point `state.json` at the new name, leave the slot absent so the app creates a fresh one, relaunch | none; `msixParkForNewIn` + `msixAttemptMigrationIn` already do this |
| macOS, Windows standalone | create `<appSupportDir>/Claude_<name>`, then `LaunchProfile` on it | small |

Both platforms already have "launch Claude on a given data dir"
(`platform/darwin.go:186`, `platform/windows.go:333`). Windows standalone already
holds the `claude://` handler for a profile with no account so the sign-in callback
lands in the right place (`platform/windows.go:346`).

### 5.1 Create a profile (both entry points)

1. Validate the name, keeping the cleaned result (§4.3).
2. Terminate Claude. If it will not quit, abort here: nothing has changed yet.
3. Create the destination, which **returns the identity and the data directory**
   (§3.5). The caller must not compute either.
   - MSIX: `msixParkForNewIn(roaming, clean)` → identity `clean`, dir `<roaming>/Claude`
     (which does not exist yet, on purpose).
   - Standalone: `os.MkdirAll(<appSupportDir>/Claude_<clean>)` → identity
     `Claude_<clean>`, dir that path.
4. Recovery only: make the orphan's conversations available in the new profile.
   - Standalone: **copy** each source's `claude-code-sessions/<uuid>/` into the new
     profile, from every path in `Sources` (§3.1), merging. Copy, not move, so the
     sources are untouched and a failure loses nothing. Once the account is live in the
     new profile, `assembleAccounts` already folds the sources' now-stale buckets away as
     duplicates of an account live elsewhere (`core/scan.go:104`), so the user does not
     end up seeing them twice.
   - MSIX: leave the buckets where they are and let the existing migration watcher copy
     after sign-in. `msixAttemptMigrationIn` copies the bucket matching whatever UUID the
     user signs in as, which is already exactly the recovery behaviour. It reads from the
     one profile it parked, so a ghost split across several profiles recovers only that
     profile's share here; the remainder stays visible as a ghost and can be recovered on
     a later pass. Noted in §9.
5. Record the identity in `pending.json` with `expectUUID` (empty for the add path).
6. Add the identity to `managed.json` so it appears in the account list immediately, and
   record the typed name with `SetProfileName(identity, clean)` so the user sees the name
   they chose on either platform (§3.5).
7. Launch Claude on the new profile's data directory.
8. The user signs in. On the next scan the pending entry is stale and pruned, and the
   row becomes an ordinary complete account.

The MSIX watcher (`startMigrationWatcher`, 5s cadence, ~15 minutes) is unchanged. On
standalone no watcher is needed: the bucket is already in place before Claude starts.

### 5.2 Merge

`core/merge.go`, new:

```go
func MergeDuplicates(keepPath, archivePath string) (*SyncReport, error)
```

1. Read both `config.json` files and refuse unless both live logins are the **same**
   UUID. A merge is only ever a duplicate cleanup.
2. Caller terminates Claude first. Abort if it will not quit.
3. **Back up the keeper.** `BackupIfHasData(keepPath)`, aborting on failure. This
   step is explicit here because `SyncSessions` does **not** snapshot anything: the
   backup has always been the caller's job (`core/switch.go`, `core/align.go`, and
   the CLI each do their own). An earlier draft of this spec asserted the opposite
   and used that non-existent safety net as the reason not to add one.
4. `SyncSessions(archivePath, keepPath)`. One direction only: the profile being
   archived is never written to, so the archive is a byte-exact record of what was
   there.
5. **Store build only, and only when the keeper is a parked profile:** swap the keeper
   into the slot with `msixSwapToIn` before archiving. See §5.3 for why.

   **Nothing may move before step 1 has passed.** Identities are resolved to paths
   read-only, from the scan, so a merge refused for holding two different accounts leaves
   the disk exactly as it found it. This swap is the first mutating step after the copy, and
   it reports the paths as they stand afterwards — any path held from before it is stale.
6. `ArchiveProfile(archivePath)`.
7. Remove the archived identity from `managed.json`, and its entries from `names.json` and
   `pending.json`. The last one matters because pending entries are pruned only on sign-in
   (§3.3) and an archived profile never appears in `FindProfiles` again, so an entry left
   behind would render a sign-in prompt the user could never clear.

**Conflicts, and why the merge screen must be a preview.** `SyncSessions` copies what
the keeper lacks and, where both hold the same record, keeps the newer mtime. A record
the keeper already has a *newer* version of is left alone and reported as a conflict
(`core/sync.go`). Merge then moves the other profile out of the scan path, so that
version survives only inside the archive.

This is not a rare edge. Two profiles signed in to one account diverge in both
directions — the measurement that settled the newer-wins rule was exactly this shape, and
some records were newer on each side. A one-directional merge will produce conflicts on
real data.

Resolved as follows.

```go
func MergePreview(keepPath, archivePath, uuid string) (*MergePlan, error)
type MergePlan struct {
    Combined   int    // conversations the keeper will hold afterwards
    Conflicts  int    // records held on both sides with different content
    Unreadable int    // records that could not be compared, so neither side counts them
    ArchiveTo  string // where the other profile will be parked
}
```

`Combined` is the size of the **union of relative paths** across both buckets, not the
sum of the two counts, so it is the number the user will actually have afterwards. A
conflict does not reduce it: a record present on both sides is in the union once either
way. §4.4's "union of both profiles' bucket contents" was already the correct definition;
the sum would have been wrong.

`Unreadable` exists because `SyncSessions` does not fail a run over one unreadable file — it
records a skip error and carries on. A preview that aborted instead would let a single junk
file block a merge of hundreds of conversations, which is the failure mode the sync itself was
fixed for. Such a record is counted in neither `Combined` nor `Conflicts`, matching what the
sync will do, and the screen says how many there were.

The merge screen renders the plan before the user commits, and when `Conflicts > 0` says
so plainly: *"N conversations exist in both profiles and have changed since they were
last in step. The newer version is kept. The other stays in the archived folder, which
you can open from Settings."* The user can cancel.

No `conflicts/` directory is added. The archive **is** the reachable copy, it is
byte-exact, and Settings already gains a shortcut to it (§5.3). Inventing a second
location for the same bytes would give the user two places to look and MCS another
directory to explain.

Both counts come from one walk of the two buckets, which the screen has to do anyway to
show `Combined`.

### 5.3 Archive

```go
func ArchiveProfile(profilePath string) (archivedTo string, err error)
```

A same-volume directory rename, which is atomic and fast, into a location **outside
the scan path** so the archived folder never reappears in Rescan. That is what makes
"one profile per account" actually hold rather than resurface on the next scan.

| Platform | Destination |
|---|---|
| macOS, Windows standalone | `~/.multi-claude-switcher/archive/<name>-<timestamp>/` |
| Windows MSIX | `<roaming>/.mcs-archive/<name>-<timestamp>/` |

macOS and standalone archive into MCS's own directory, next to `backups/`, which is
already outside `AppSupportDir()` and so outside `FindProfiles`.

**On the Store build only a parked profile may be archived.** The active profile lives in
the shared slot, and `state.json`'s `current` names it (§3.5). Renaming the slot away
would leave `current` pointing at a directory that does not exist, which — with the
`Exists: false` change from §3.3 — renders as a profile permanently waiting to be signed
in to, and which the next switch would try to park. So `ArchiveProfile` refuses a path
equal to the slot, and merge swaps the keeper in first (§5.2 step 5) whenever the user
overrides the pre-selected keeper. The swap is `msixSwapToIn`, shipped and tested, and it
leaves `current` correct by construction.

MSIX stays inside its package container. Moving out of an MSIX virtualized container
to `%USERPROFILE%` is unverified on a real Store install, whereas renames within the
container are what the shipped code already does successfully
(`msixParkForNewIn`, `renameWithRetry`). `.mcs-archive` sits beside `.mcs-profiles`;
`msixFindProfiles` enumerates only the slot and `.mcs-profiles`, so the archive is
invisible to scanning. Revisit only if a Store machine is available to verify the
cross-container move.

Settings gains **Open archive folder** next to the existing Open backup folder.

MCS still never deletes user data. Archiving is the strongest action it takes, and it
is a rename the user can undo by hand.

## 6. Error handling

| Failure | Behaviour |
|---|---|
| Claude will not quit | Abort before touching disk. "Fully quit Claude (check the tray and Task Manager) and try again." Existing wording from `msixParkForNewIn`. |
| Invalid or colliding profile name | Re-render the name screen with the reason. Nothing created. |
| Bucket copy fails partway (recovery, standalone) | Remove the half-created profile dir, drop the pending entry, leave the source untouched, report. The source is the only copy that mattered and was never written to. |
| Park fails (MSIX) | Existing rollback: rename the parked dir back into the slot, remove the container if empty. |
| Recovery: one of several sources fails to copy | Same as a single-source failure: remove the half-created profile, drop the pending entry, report. A partial recovery would produce a profile the user believes is complete. |
| Merge: preview cannot be computed (bucket unreadable) | Do not show the merge screen. Report the failure on the account list; the warning and both profiles stay as they are. Never offer a merge whose outcome could not be computed. |
| Merge: the two profiles are not the same account | Refuse with a clear message. Guards against a stale panel acting on rows that changed under it. |
| Merge: keeper backup fails | Abort before any copy. Nothing has been touched. |
| Merge: sync fails | Abort before archiving. The keeper was snapshotted in step 3, so a partial copy is recoverable. Nothing is archived, nothing is unmanaged, the warning stays. |
| Merge: archive rename fails (files locked) | Retry, then abort. `managed.json` is left unchanged, so the user sees both profiles and the warning, exactly as before the attempt. Never unmanage a folder that is still in place. |
| Archive destination already exists | Append a counter: `<name>-<timestamp>-2`. |
| Sign-in never happens | The pending entry stays, the row keeps reading "Sign in to finish setting this up", and the user can switch to it later or delete the folder. MSIX additionally gives up its watcher after ~15 minutes, as today. |

Order of operations throughout: **verify, then copy, then move, then update state.**
Registry updates (`managed.json`, `pending.json`) happen last, so a failure never
leaves MCS's view of the world ahead of the disk.

## 7. Testing

**Scanner (pure, table-driven — extends `core/scan_test.go`)**

- Two dirs with the same live UUID produce two complete rows, both `Duplicate`.
- Three dirs, two sharing a UUID: only those two are marked.
- Ghost with a non-empty bucket is `Recoverable` with one `Sources` entry carrying both
  the folder and the path it came from.
- Ghost whose UUID has buckets in two profiles gets **two** `Sources` entries, and the
  row's count is their sum.
- Ghost with an empty bucket is not `Recoverable` and keeps its existing note.
- A pending folder with no live login produces a pending row, and its `expectUUID`
  bucket is **not** also emitted as a ghost — **anywhere**. The suppression covers
  every profile holding that UUID, not just the pending folder's own copy.

  Recovery copies rather than moves, so the source profiles still hold the same
  conversations while the user goes to sign in. Suppressing only the pending
  folder's copy would leave the account showing as a recoverable ghost sourced
  from those originals, next to the row telling the user it is already being
  recovered — and recovering it again is how one account becomes two profiles.

  Two rules keep that from turning into a way to lose an account:
  - It is scoped to the `expectUUID` the pending entry names. Other orphans in the
    same profiles stay visible and recoverable.
  - It applies only while the pending folder is still in the scanned profile list.
    A user who never signs in and deletes the folder by hand gets the ghost back
    on the next Rescan, because the entry then names nothing that exists.
- A pending folder that is empty (no config, no buckets, no login) still produces a
  pending row rather than being dropped. This is the add path immediately after
  creation, and it is the case the drop filter would silently swallow.
- A pending folder that has since been signed in to produces a complete row, and the
  entry is reported as prunable.
- **A pending folder absent from the profile list is _not_ reported as prunable**, and
  still produces a pending row. This is the Store build between creating a profile and
  the app's first launch; pruning here is the defect §3.3 exists to prevent.
- Fixtures from §1 (all three machine layouts) as regression cases, with the expected
  row sets written out.

**Profile identity (§3.5)**

- `ValidateProfileName` returns the trimmed name, and ` Work ` and `Work` produce the
  same identity, display name, and registry keys.
- Standalone: creation returns identity `Claude_<name>` and a matching path.
- Store build: creation returns identity `<name>` — the value written to `state.json` —
  while the path ends in `Claude`. A test that asserts identity equals
  `filepath.Base(path)` is asserting the bug; assert they differ.
- `msixFindProfiles` with an absent slot emits the `state.json` profile with
  `Exists: false`, so a just-created Store profile is still enumerated.
- Round trip: the identity returned by creation is the one `FindProfiles` reports, and
  the one `managed.json`, `pending.json`, and `names.json` hold.

**Profile creation**

- Name validation table: empty, whitespace only, path separators, `..`, disallowed
  characters, colliding with an existing folder, valid names with spaces and dashes.
- Standalone: folder created with the `Claude` prefix so `FindProfiles` finds it.
- Recovery: bucket copied, source unchanged, `pending.json` written with the UUID.
- Recovery from two sources: both copied into one bucket in the new profile.
- Copy failure: destination removed, pending entry absent, source unchanged. With two
  sources and the second failing, the destination is still removed — no partial recovery
  is left looking complete.

**Merge**

- `MergePreview` returns the union size, not the sum, when the two buckets overlap.
- `MergePreview` counts a record present on both sides with different content as a
  conflict, and does not count identical copies.
- `MergePreview` with one unreadable record still returns a plan, counting that record as
  unreadable rather than failing. `SyncSessions` skips such a file and carries on, so a
  preview that aborted would let one junk file block a merge of hundreds of conversations.
- Merge refused for two different accounts leaves both directories exactly where they were,
  including on the Store build where a later step would have swapped them.
- Merge clears the archived identity from `managed.json`, `names.json`, **and**
  `pending.json`.
- Two temp profiles with the same UUID and partly overlapping sessions: after merge
  the keeper holds the union, the archive folder is gone from the scan path and
  present at the archive path, byte-identical to before, and `managed.json` no longer
  lists it.
- A record the keeper holds a newer version of survives the merge unchanged in the
  keeper, and its other version is present in the archive.
- Refuses when the two profiles hold different accounts.
- Sync failure leaves both profiles in place and `managed.json` untouched.
- Archive rename failure leaves `managed.json` untouched.
- Archive destination collision appends a counter.
- Store build: `ArchiveProfile` refuses the slot path, and merging with the parked
  profile as keeper swaps it into the slot before archiving, leaving `state.json`
  naming the keeper.

**Render (extends `internal/panelui/render_test.go`)**

- `RenderList` shows the warning block and the duplicate pills only when a duplicate
  group exists, and the dashed add card always.
- `RenderRescan` shows Recover only on recoverable ghosts; dead ghosts stay read-only.
- `RenderNewProfile` pre-fills the name and shows the expected-account instruction in
  the recovery variant, and the different-account warning in the add variant.
- `RenderMerge` pre-selects the in-use profile and labels the other as to be archived.
- Folder names are escaped in every new attribute context (the v0.9.1 bug class:
  use `data-*` plus `dataset`, never inline JS string arguments).

**Manual QA**

macOS standalone can be verified locally. **Windows MSIX can only be verified on a
real Store install**, which means one of the reporters' machines or a Windows test box with
the Store build. The full path to run there: recover an orphan, confirm the
conversations arrive, then deliberately sign in as an existing account to produce a
duplicate and confirm the warning and the merge. Windows standalone needs the same
pass on a non-Store install.

## 8. Phasing

| Phase | Content |
|---|---|
| 1 | `core` and `platform`: profile identity (§3.5), pending registry, scanner changes, profile creation, archive, merge, with tests |
| 2 | Panel UI: the four screens, action handlers on both hosts, Settings archive shortcut |
| 3 | Docs and release: README FAQ rewritten around recovery, `CHANGELOG.md`, `FILELIST.md`, code review, QA on a real Store install |

Phases 1 and 2 ship together. Shipping profile creation without duplicate detection
would hand users a way to create a duplicate and no way to resolve one, which is the
same shape of dead end this spec exists to remove.

Version number: this is a feature, so `0.11.0` by convention. To be confirmed before
release rather than assumed.

## 9. Known limitations, accepted

1. **One sign-in per recovered account.** MCS restores conversations, not credentials.
   An account whose password is lost cannot be recovered.
2. **Chat-only accounts leave no trace.** An account that never used Code has no
   bucket, so it cannot be detected as a ghost or recovered. Its server-side chat
   history returns on sign-in anyway.
3. **Recovery costs disk.** The bucket is copied, not moved, so the conversations exist
   twice until the user clears the source. The stale copy is hidden from the UI, not
   reclaimed.
4. **Archives are never pruned.** Same policy as `backups/`, which is documented as
   the user's to clean up.
5. **In-app switching still creates orphans.** Prevention is not possible; this design
   only makes the result recoverable. README should tell users to add accounts through
   MCS, which lowers the rate without ever reaching zero.
6. **`FindProfiles` matches any `Claude` prefix**, so an unrelated directory such as
   `ClaudeBar` is enumerated. It is dropped by `ScanAccounts` for having no config, no
   login, and no buckets, so it is harmless today. Tightening the match to require a
   separator is a separate cleanup, noted here so it is not rediscovered as a bug.
7. **On the Store build a ghost split across profiles recovers one share at a time.**
   The migration watcher copies from the single profile it parked (§5.1 step 4), so
   conversations for that account sitting in a *third* profile stay behind. The row
   remains a recoverable ghost afterwards with the smaller count, so a second pass
   finishes the job. The standalone path copies from all sources at once and has no such
   limit.
8. **A pending entry can outlive its profile** if the user deletes the folder by hand,
   leaving a row that asks for a sign-in that can never happen. This is the accepted cost
   of pruning only on sign-in (§3.3), which the Store build requires.

## 10. Design history

- 2026-07-24 `account-rescan-design.md` §6.1 accepted logged-out accounts as
  unrecoverable, correctly for credentials and too broadly for conversations.
- 2026-07-30 three machines inspected; the reported ghost rows traced to
  in-app account switching, and `runNewProfileFlow` found to be already implemented and
  unreachable since the v0.10.0 Windows panel replaced the systray menu.
- 2026-07-30, after a second adversarial review of the revision: merge's mutating steps moved
  after its checks, so a refused merge cannot leave the Store build's directories swapped;
  `MergePreview` gained `Unreadable` and stopped aborting over one bad file; archiving now
  clears `pending.json` too; the `state.json` case-folding exception and the pre-existing
  `core/sync.go` display-name violation written down rather than left to be rediscovered.
- 2026-07-30, after adversarial review of the first implementation plan: profile identity
  given its own section (§3.5) because the plan had keyed MCS's registries on
  `filepath.Base(path)`, which is `Claude` for every active Store profile and would have
  made the feature inert on the platform it exists for; ghost sources changed from one
  folder to a list; pending pruning reduced to sign-in only; the merge conflict question
  closed with a computed preview rather than a new storage location.
- Decisions taken during design: dual entry points rather than one; panel views rather
  than native dialogs; one shared name screen rather than two; recovery one account at
  a time; archive rather than delete; a persistent warning with a one-click fix rather
  than a hard block, because a merge needs Claude quit and a blocked panel with a
  failing merge would trap the user; keep the in-use profile by default, user
  overridable.
