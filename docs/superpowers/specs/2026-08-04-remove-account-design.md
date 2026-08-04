# Remove an account

A way to take an account off the switcher. The folder is archived, not deleted.

## Why

There is no way to remove anything from the list. A slot created by mistake, a
slot signed in to the wrong account, a slot for a subscription that ended: all
of them stay on screen forever. The only path that removes a row today is
merging two duplicates, which is unreachable unless a duplicate exists and
whose purpose is not removal.

The gap also makes a second problem worse. Adding an account and signing in to
one you already have creates a duplicate, and today the only way out is a merge
— a heavier operation than the mistake deserves, because the new slot usually
holds nothing the old one lacks.

## What it does

Archive, not delete. `ArchiveProfile` renames the profile directory into the
archive root, which is the same thing a merge already does to the profile the
user gives up. Everything stays on disk in one piece, the user can move it back
by hand, and the folder is out of the scanner's path so it does not reappear on
the next Rescan.

Nothing is deleted, ever. No `os.RemoveAll` path is added by this work.

## What it refuses

Four refusals, at three layers, because the reasons are different.

**A profile Claude has open** — `core` refuses when `DetectRunningProfiles`
reports the target's directory. Renaming a directory out from under a running
Claude is the one way this operation can corrupt data, and POSIX `rename` will
do it without complaint.

An earlier draft compared the identity against `LoadActiveProfile()` instead.
That was wrong in both directions and the reasoning is recorded here so it is
not reintroduced. `LoadActiveProfile` returns `""` for any failure *and* for a
machine where MCS has never switched anything, so a user who launched Claude
themselves got no guard at all. In the other direction, with Claude closed it
still names the last account activated, so removing a profile nothing has open
was refused with "this is the account you are using" while no Claude was
running. Same cause, opposite symptoms.

It also closes a race the identity check could not see: the panel is rendered
with Claude closed, the user then opens Claude on that account, and the button
they were already looking at is still enabled. Detection at the moment of the
action is the only thing that catches it.

A detector error refuses too. Not knowing whether Claude holds the directory is
not permission to rename it, and a removal is never urgent enough to guess.

**The Store slot occupant, and a Store install with no recorded state** — the
Windows Store build addresses the active profile through one shared directory
that `state.json` names, and renaming that away leaves `state.json` pointing at
nothing. `readMSIXStateIn` substitutes the default name when there is no file,
so "the occupant is Claude" and "MCS has never run here" come back identical;
`msixStateRecorded` is the existing helper that tells them apart, and its own
comment says why the returned value cannot. With no recorded state the
occupant is unknown, so the removal is refused rather than guessed.

**The Store pending-migration source** — after a Store profile is created,
`state.json` carries `PendingMigrateFrom` naming the parked profile whose
conversations are queued to copy in once the user signs in.
`msixAttemptMigrationIn` stats that source and, finding it gone, copies nothing
and clears the flag without a word. Removing the source before signing in would
therefore lose the migration silently, so it is refused while one is queued.

**The last account on the list** — the UI hides the button when only one
profile is listed. Removing it would leave a panel with nothing in it and no
obvious way back, and the value of allowing it is nil.

Because a profile Claude has open can never be the target, **Claude does not
have to be quit** to remove a different one. That is the user-visible payoff of
the first refusal and the reason not to relax it.

## No backup

The archive *is* the backup: the directory moves untouched, byte for byte.

A merge takes one because a merge writes into the profile it keeps. This writes
nothing, so a snapshot would only double the disk cost of an operation whose
whole safety story is that it does not modify anything.

## Order of operations

1. `PrepareRemove` — resolve the identity to a path, and the platform's own
   refusals. Read-only; it moves nothing, unlike `PrepareArchive`.
2. Refuse if there is no directory there.
3. Refuse if `DetectRunningProfiles` reports that path, or errors.
4. `ArchiveProfile` — the rename.
5. Only now, the registries: `RemoveManaged`, `SetProfileName(identity, "")`,
   `RemovePending`, and `active.json` if it names this identity.

Registries last is not a preference. A folder unmanaged while still in place is
hidden from the panel and back on the next Rescan, so "removed" would not stay
removed. The display name goes with the profile or it is inherited by any later
profile that reuses the identity — the same reasoning `MergeDuplicates` records.

The running check is step 3 rather than step 1 because it needs the resolved
path, and it is the last check before the rename so the window between deciding
and acting is as small as it can be.

A failure at step 4 leaves the disk and every registry exactly as they were, so
the account is still on the list and a retry is safe. Every failure at step 5 is
logged, joined, and returned: they do not stop the remaining cleanup, because
stopping at the first would leave the others describing a folder that no longer
exists. Returning them rather than only logging matters for the display name in
particular, since a name left behind is silently inherited by any later profile
that reuses the identity, and the user is the only one who can notice.

A crash between steps 4 and 5 leaves entries naming a folder that is gone. This
needs no repair mechanism: `BuildProfiles` walks the folders that exist and
consults `managed.json` about them, so an entry for a folder that is not there
is never rendered. The only residue that can be felt is the display name, and
that is the one the returned error is about.

## Platform layer

New method on `platform.Platform`:

```go
// PrepareRemove resolves an identity to the directory a removal would archive,
// and refuses when that directory may not be renamed away.
PrepareRemove(identity string) (path string, err error)
```

Named for what the feature does. `PrepareDelete` was the first name and it
contradicts the one rule this whole design rests on, which is that nothing is
deleted.

- `DarwinPlatform` — joins the app-support dir; every profile is its own
  directory and nothing is shared.
- `WindowsPlatform`, standalone — the same, under `%APPDATA%`.
- `WindowsPlatform`, MSIX — refuses when `msixStateRecorded` is false (the
  occupant is unknowable), when the identity is the slot occupant, and when it
  is `state.json`'s `PendingMigrateFrom`. Otherwise resolves through
  `msixProfilePath`, where parked profiles live under `.mcs-profiles`.
- `unsupportedPlatform` — the existing not-supported error.

The occupant test is extracted as `msixIsSlotOccupant(roaming, identity) bool`
and used by both `PrepareArchive` and `PrepareRemove`, so the two cannot come to
disagree about what "the occupant" means.

It stays a separate method rather than a reuse of `PrepareArchive`, which takes
a *keeper* to swap into the slot. A removal has no keeper. Passing the active
identity as a stand-in would work today and read as a swap that never happens,
which is the kind of thing that is wrong the first time someone changes it.

## Screens

**Entry point.** The pencil on each account row already opens a per-account
screen. Its title becomes **Account settings** (from "Rename account"), and a
section is added at the bottom, below a rule:

> Removing takes this account off the list. Its folder is archived, not deleted.
>
> `[ Remove this account ]` — red text, red border, full width.

Not a bin icon on the list row: it would sit against the pencil, and two small
adjacent icons is the arrangement most likely to be mis-tapped.

When the account is the one in use, the button is disabled with the reason under
it: *Switch to another account first.*

**Confirmation.** A modal through the existing `askConfirm`, with its `warn`
parameter carrying the consequence:

> **Remove test?**
> It disappears from the switcher. Its folder, with all 34 conversations, moves
> to the archive folder you can open from Settings.
>
> To use this account again you have to sign in to it again.
>
> `[ Cancel ]` `[ Remove ]` — Remove filled red.

The conversation count comes from the same `ProfileVM.Convos` the list shows, so
the number promised is the number the user was already looking at.

**Result.** A screen, not a status line on the list. A removal that reports
itself in one line at the top of a changed list is the case where the user
cannot tell whether it happened.

Success names where the folder went, by its archived name, and offers the
existing `openArchive` action so the user can confirm with their own eyes.
Failure states plainly that nothing was moved and the account is still listed,
carrying the error text `ArchiveProfile` already writes for a locked directory
and for a cross-volume archive root. Both end with a way back to the list.

## Copy

No em dash anywhere in user-facing text; `emdash_test.go` already enforces it
across the rendered HTML.

## Tests

- Archive lands in the archive root and the source directory is gone.
- Every registry naming the identity is clean afterwards: managed list, display
  name, pending entry.
- **A failed rename leaves every registry intact.** `renameProfile` is already a
  var for injection; this is the path a real filesystem will not produce on
  demand and the one where a wrong order does permanent damage.
- **Refuses a profile Claude has open, with no `active.json` present.** The
  guard must come from detection, not from a record that is empty on exactly the
  machines where nobody has used MCS to switch. A test that first calls
  `SaveActiveProfile` proves nothing about that case.
- Refuses when the detector errors, and does not touch the disk when it does.
- Refuses an identity that is no longer there.
- A registry write that fails is reported to the caller, not only logged.
- MSIX `PrepareRemove`: refuses the slot occupant, refuses when no state has
  ever been recorded, refuses the pending-migration source, and resolves a
  parked profile.
- Renderer: the button appears on the account screen, is disabled for the
  account in use, and is absent when only one profile is listed.

## Deliberately not in scope

**Deleting the files.** Anyone who wants the disk space back can empty the
archive folder in Finder or Explorer. It is the only irreversible operation in
reach, and a menu entry for it will eventually be hit by a slip.

**Sweeping orphaned registry entries at startup.** Proposed as a repair for a
crash between the rename and the registry writes, and rejected: a `managed.json`
entry for a folder that is not there is inert, because the panel walks the
folders that exist and asks the list about them rather than the reverse. Adding
a background mutation of the user's registries to tidy an entry nothing reads
buys nothing and puts a writer where there was none.

**Blocking duplicates at scan time.** Considered and rejected. Two folders on
one account each hold their own conversations, possibly under different
organizations, and the merge that combines them is reached from the list — so
hiding one would hide its conversations and put them out of reach permanently.
The existing Duplicate pill and merge banner stay as they are. Removal is what
makes a mis-signed-in slot cheap to undo, which was the underlying concern.

## Version

A new feature, so 0.13.0. The minor bump is confirmed with the maintainer before
release.
