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

Two refusals, at two layers, because the reasons are different.

**The account in use** — `core` refuses when the identity matches
`core.ActiveProfile()`. On macOS and the Windows standalone build this stops a
rename out from under a running Claude. It holds whether or not Claude is
running, which a check against the running path would not: with Claude closed,
no profile is "current" and every guard based on that would open.

**The Store slot occupant** — the Windows Store build addresses the active
profile through one shared directory that `state.json` names. Renaming that
directory away leaves `state.json` pointing at nothing. The platform layer
refuses it, because the rule belongs to the platform that has it.

**The last account on the list** — the UI hides the button when only one
profile is listed. Removing it would leave a panel with nothing in it and no
obvious way back, and the value of allowing it is nil.

Because the account in use can never be the target, **Claude does not have to be
quit**. No other slot's directory is open by anything, so the rename succeeds
immediately. This is the one user-visible payoff of the first refusal, and it is
the reason not to relax it later.

## No backup

The archive *is* the backup: the directory moves untouched, byte for byte.

A merge takes one because a merge writes into the profile it keeps. This writes
nothing, so a snapshot would only double the disk cost of an operation whose
whole safety story is that it does not modify anything.

## Order of operations

1. Resolve the identity to a path, read-only.
2. Refuse per the rules above.
3. `PrepareDelete` — platform-specific, see below.
4. `ArchiveProfile` — the rename.
5. Only now, the registries: `RemoveManaged`, `SetProfileName(identity, "")`,
   `RemovePending`.

Registries last is not a preference. A folder unmanaged while still in place is
hidden from the panel and back on the next Rescan, so "removed" would not stay
removed. The display name goes with the profile or it is inherited by any later
profile that reuses the identity — the same reasoning `MergeDuplicates` records.

A failure at step 4 leaves the disk and every registry exactly as they were, so
the account is still on the list and a retry is safe. Failures at step 5 are
logged and reported but do not stop the remaining cleanup: stopping at the first
would leave the others describing a folder that no longer exists.

## Platform layer

New method on `platform.Platform`:

```go
// PrepareDelete resolves an identity to the directory to archive, and refuses
// when that directory may not be renamed away.
PrepareDelete(identity string) (path string, err error)
```

- `DarwinPlatform` — joins the app-support dir; every profile is its own
  directory and nothing is shared.
- `WindowsPlatform`, standalone — the same, under `%APPDATA%`.
- `WindowsPlatform`, MSIX — refuses when the identity is `state.json`'s
  `Current`; otherwise resolves through `msixProfilePath`, where parked profiles
  live under `.mcs-profiles`.
- `unsupportedPlatform` — the existing not-supported error.

It is a separate method rather than a reuse of `PrepareArchive`, which takes a
*keeper* to swap into the slot. A removal has no keeper. Passing the active
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
- Refuses the active identity, and does not touch the disk when it does.
- Refuses an identity that is no longer there.
- MSIX `PrepareDelete` refuses the slot occupant and resolves a parked profile.
- Renderer: the button appears on the account screen, is disabled for the
  account in use, and is absent when only one profile is listed.

## Deliberately not in scope

**Deleting the files.** Anyone who wants the disk space back can empty the
archive folder in Finder or Explorer. It is the only irreversible operation in
reach, and a menu entry for it will eventually be hit by a slip.

**Blocking duplicates at scan time.** Considered and rejected. Two folders on
one account each hold their own conversations, possibly under different
organizations, and the merge that combines them is reached from the list — so
hiding one would hide its conversations and put them out of reach permanently.
The existing Duplicate pill and merge banner stay as they are. Removal is what
makes a mis-signed-in slot cheap to undo, which was the underlying concern.

## Version

A new feature, so 0.13.0. The minor bump is confirmed with the maintainer before
release.
