# Design Spec — Ghost account recovery, and one profile per account

- Date: 2026-07-30
- Status: Design finalized, pending implementation plan
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

### 3.1 `ScannedAccount` additions

```go
type ScannedAccount struct {
    // … existing fields …

    // Recoverable marks a ghost whose conversations can be brought back: its
    // bucket exists and is non-empty, so giving it its own profile and signing in
    // reunites account and history. Set only on ghost rows.
    Recoverable bool

    // SourceFolder is the profile folder whose bucket holds this ghost's
    // conversations, i.e. where a recovery copies from. Ghost rows only.
    SourceFolder string

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

`Recoverable` is false for a ghost with an empty bucket: there is nothing to recover,
so it stays read-only and keeps its existing note.

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
  { "folder": "Claude_Recovered-2026-07-29",
    "expectUUID": "bbbbbbbb-0000-4000-8000-000000000002",
    "createdAt": "2026-07-30T14:02:11+08:00" }
] }
```

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
  pruned on the next scan.
- An entry whose folder no longer exists is pruned too.

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

Duplicates cannot arise from in-app account switching, which produces ghosts inside a
single directory. They arise when two directories are signed in to the same account,
which is a possible misuse of the very flow this spec adds: the user is told to sign in
as a different account and signs in as an existing one instead. Handling it is
therefore in scope for this change, not a separate concern.

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
separators or `..`; restricted to letters, digits, space, `-`, `_`; the resulting
folder must not already exist. On failure the screen re-renders with the reason and
nothing on disk changes.

### 4.4 Merge (`RenderMerge`)

Two cards, one per duplicate profile. **The profile currently in use is pre-selected**
to keep, because keeping it means no re-sign-in. The user may pick the other one
instead, which matters when they named it something meaningful. The unselected card is
labelled `Will be archived`.

> All 141 conversations are combined into the account you keep. The other folder is
> archived, not deleted, so you can put it back yourself.

The combined count is the union of both profiles' bucket contents for that UUID, which
requires counting both before rendering.

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

1. Validate the name (§4.3).
2. Terminate Claude. If it will not quit, abort here: nothing has changed yet.
3. Create the destination.
   - MSIX: `msixParkForNewIn(roaming, name)`.
   - Standalone: `os.MkdirAll(<appSupportDir>/Claude_<name>)`.
4. Recovery only: make the orphan's conversations available in the new profile.
   - Standalone: **copy** `<source>/claude-code-sessions/<uuid>/` into the new
     profile. Copy, not move, so the source is untouched and a failure loses nothing.
     Once the account is live in the new profile, `assembleAccounts` already folds the
     source's now-stale bucket away as a duplicate of an account live elsewhere
     (`core/scan.go:104`), so the user does not end up seeing it twice.
   - MSIX: leave the bucket where it is and let the existing migration watcher copy it
     after sign-in. `msixAttemptMigrationIn` copies the bucket matching whatever UUID
     the user signs in as, which is already exactly the recovery behaviour.
5. Record the folder in `pending.json` with `expectUUID` (empty for the add path).
6. Add the folder to `managed.json` so it appears in the account list immediately.
7. Launch Claude on the new profile.
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
5. `ArchiveProfile(archivePath)`.
6. Remove the archived folder from `managed.json`.

**Open question, must be resolved before merge is implemented.** Sync is purely
additive: it copies what the keeper lacks and never replaces what it has, so a
conversation that differs on both sides stays different on both sides
(`core/sync.go`). Merge then moves the losing profile out of the scan path. So for
every clash, the losing version ends up only inside the archive, reachable by hand
and by nothing in the UI. The count shown on the merge screen would also be wrong:
it promises a combined total that a clash silently reduces.

Merge is therefore **not** ready to implement as specified. Options to decide
between: report clashes on the merge screen before the user commits and let them
cancel; copy the losing versions of clashing files into a reachable
`conflicts/<timestamp>/` outside any session bucket, so both really are available;
or refuse to merge at all while clashes exist and offer a separate resolution step.
Recovery (§5.1) has no such problem and can ship without this being settled.

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
- Ghost with a non-empty bucket is `Recoverable` with `SourceFolder` set.
- Ghost with an empty bucket is not `Recoverable` and keeps its existing note.
- A pending folder with no live login produces a pending row, and its `expectUUID`
  bucket is **not** also emitted as a ghost.
- A pending folder that is empty (no config, no buckets, no login) still produces a
  pending row rather than being dropped. This is the add path immediately after
  creation, and it is the case the drop filter would silently swallow.
- A pending folder that has since been signed in to produces a complete row, and the
  entry is reported as prunable.
- A pending folder that no longer exists is reported as prunable.
- Fixtures from §1 (all three machine layouts) as regression cases, with the expected
  row sets written out.

**Profile creation**

- Name validation table: empty, whitespace only, path separators, `..`, disallowed
  characters, colliding with an existing folder, valid names with spaces and dashes.
- Standalone: folder created with the `Claude` prefix so `FindProfiles` finds it.
- Recovery: bucket copied, source unchanged, `pending.json` written with the UUID.
- Copy failure: destination removed, pending entry absent, source unchanged.

**Merge**

- Two temp profiles with the same UUID and partly overlapping sessions: after merge
  the keeper holds the union, the archive folder is gone from the scan path and
  present at the archive path, byte-identical to before, and `managed.json` no longer
  lists it.
- Refuses when the two profiles hold different accounts.
- Sync failure leaves both profiles in place and `managed.json` untouched.
- Archive rename failure leaves `managed.json` untouched.
- Archive destination collision appends a counter.

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
| 1 | `core` and `platform`: pending registry, scanner changes, profile creation, archive, merge, with tests |
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

## 10. Design history

- 2026-07-24 `account-rescan-design.md` §6.1 accepted logged-out accounts as
  unrecoverable, correctly for credentials and too broadly for conversations.
- 2026-07-30 three machines inspected; the reported ghost rows traced to
  in-app account switching, and `runNewProfileFlow` found to be already implemented and
  unreachable since the v0.10.0 Windows panel replaced the systray menu.
- Decisions taken during design: dual entry points rather than one; panel views rather
  than native dialogs; one shared name screen rather than two; recovery one account at
  a time; archive rather than delete; a persistent warning with a one-click fix rather
  than a hard block, because a merge needs Claude quit and a blocked panel with a
  failing merge would trap the user; keep the in-use profile by default, user
  overridable.
