# Tidy the conversations the pre-0.11.2 sync misfiled

## Why

Sync before v0.11.2 rewrote a conversation's account segment and left its
organization segment naming the source's. Claude Desktop reads exactly one folder,
`claude-code-sessions/<signed-in account>/<active organization>/`, so everything that
sync wrote landed somewhere no account opens.

The files are invisible, and the fix that followed re-copied them to the right place,
so they are also duplicates. They are dead weight rather than a hazard, and they roughly
double the session-folder size. Measured on two machines: **564 files, 36 MB**, across
two profiles.

The population only shrinks. Nothing has produced a new misfiled file since v0.11.2.

## What it does

At startup, on a background goroutine, MCS moves those files into
`~/.multi-claude-switcher/backups/tidied-<YYYYMMDD>/`, preserving
`<profile>/<account>/<organization>/` so putting them back is a move in the other
direction. Nothing is deleted. There is no user interface.

## Silent, which reverses what the issue asked for

[#11](https://github.com/miou1107/multi-claude-switcher/issues/11) says a silent tidy on
upgrade is out of the question, on the grounds that never deleting a conversation is the
one thing this tool promises. The premise does not survive contact with what this
actually does: it moves, exactly like the backup pruning that already runs silently, and
every file it moves is unreadable by any account and has a readable copy elsewhere. A
confirmation would ask the user to decide something they cannot observe and have no
information about, which is a way of transferring blame rather than seeking consent.

One consequence is real and is accepted deliberately, and review showed the first draft of
this paragraph under-stated it by an order of magnitude.

The obvious case is an organization the profile still carries a stamp for: one the user
was signed into and could sign into again. Moving its files means that if they rejoin, it
looks empty. That was measured at about 4 MB of the 36 MB.

The larger case is a whole ACCOUNT. A profile signed out of account A and into account B
keeps A's history under `<acctA>/`, and if any other profile is signed into A with a
synced copy, every one of those files has an equal-time counterpart and qualifies. Signing
back into A in that profile then shows nothing. Two profiles on one account is an ordinary
setup, particularly on the Windows Store build.

The maintainer chose to move both. Review then showed that the organization half of that
choice cannot be implemented safely: there is no way to tell an organization the profile
has left from the one it is in, when the only evidence is a stamp that can name the wrong
one. So the organization half is reversed and organizations this profile has been signed
into are left alone, which costs about 122 of the 564 files measured.

The account half stands, and is safe: `lastKnownAccountUuid` is a recorded fact rather
than a guess, so a bucket under another account is provably not the one being read. What
is accepted there is unchanged: a profile signed out of account A and into B has A's
history moved, and signing back into A shows nothing until it is fetched from the backup
folder.

## What may be moved

For each profile, the read bucket is `<lastKnownAccountUuid>/<active organization>`,
where the active organization is the newest `dxt:allowlistLastUpdated:<org>` stamp in
`config.json` (see `platform.GetProfileActiveOrgUUID`). Every other bucket in that
profile is unread.

**A profile whose read bucket cannot be determined is skipped entirely.** A profile with
no account signed in, or an unreadable or unparseable `config.json`, or no organization
stamp, has no known read bucket, and treating "unknown" as "reads nothing" would make
every one of its buckets a candidate. That is the single most dangerous mistake available
here, so it fails closed.

**A bucket is a candidate only if this profile cannot possibly be reading it.** This is
the guard against the worst outcome available here, and it is the second attempt. The first
compared how recently each folder had been written, and review reproduced the data loss
through it: in the very scenario it was written for, the folder the heuristic wrongly names
is the folder the last launch wrote to, which is the causal reason its stamp is newest.
Shifting the reproduction by one second brought the loss straight back. A second heuristic
drawn from the same signal cannot correct the first.

What works is that the two segments of a bucket are not equally certain.

The **account** is read straight out of `config.json`'s `lastKnownAccountUuid`. It is a
recorded fact, so a bucket under any other account cannot be the one Claude is reading.

The **organization** is a guess: `GetProfileActiveOrgUUID` takes the newest allowlist
stamp, and someone who switches organization in-app without relaunching leaves that stamp
naming the previous one. But MEMBERSHIP is not a guess. A stamp exists because this profile
was signed into that organization, so an organization with no stamp is one it has never
opened and cannot be reading. `platform.GetProfileSignedInOrgs` reads the whole set.

So a bucket under another account is a candidate, and a bucket under this profile's own
account is a candidate only if this profile has never been signed into that organization.

That is not a compromise, it is a description of the defect: the pre-0.11.2 sync copied
conversations in under the SOURCE profile's organization, which the target had typically
never joined, so the folders it created are exactly the ones with no stamp.

The believed-read bucket must also exist and hold something, since an empty one means the
record naming it is not describing this profile.

A file in an unread bucket is moved only when both hold:

1. **A counterpart exists.** Some profile's read bucket contains a file at the same path
   relative to its bucket root. All profiles are considered, not just the file's own:
   this is the same conversation refiled under a different account, which is the whole
   shape of the defect.
2. **The counterpart is not older.** Its modification time is greater than or equal to
   this file's.

The second condition is not decoration. The measurement behind this feature found 425
files byte-identical and 138 differing only in fields like `lastFocusedAt`, with the
readable copy newer in every diverging case. Every one is an observation, not a
guarantee; a file whose only readable counterpart is older is the one case where moving
loses something, so it stays.

Files that fail either condition are left where they are and counted in the log. Moving
them would not make them readable, only harder to find if a later version learns to
recover them.

Empty directories left behind by a move are removed, and only those: a directory that
still holds anything is left alone.

## When it runs

Once, on a goroutine, after the host has started. It is not attached to a switch, a sync
or a backup, because it has nothing to do with any of them and those are the moments when
the user is waiting.

After the first run there is nothing to find, and the cost is one walk of each profile's
session tree. Measured here: 531 files across two profiles.

## Failure policy

Best-effort, like the backup pruning it sits beside, and for the same reason: this is
housekeeping, and housekeeping must never be the thing that breaks. Every failure is
logged and skipped, a panic is recovered, and nothing is reported upwards.

A file that cannot be moved stays where it was. A destination that already exists is
never overwritten; the move is skipped and logged.

## Interaction with the backup pruning

`tidied-<YYYYMMDD>` is not a name `parseBackupName` accepts, so the pruner never considers
it, exactly as it never considers a folder somebody made by hand. Verified, and pinned by
a test rather than left to the reader.

## Errors and edge cases

- **Claude Desktop running.** It reads and writes the read bucket, which this never
  touches. The window between deciding and acting is closed at the file level: every move
  carries what the scan saw, and refuses a source whose modification time has changed
  since. A sync or a switch writing into a bucket during the scan therefore has its work
  left alone rather than moved away on the strength of a judgement about a different file.
- **Profile identity is the path, never the display name.** On the Windows Store build two
  entries can carry the same name (the live slot and a container directory, after a swap
  whose state write failed). Resolving a name back to a path would plan against one and
  act on the other, and `platform/windows_msix.go` records that the inverse mapping does
  not exist. Every move carries its own path, and the destination folder carries a short
  digest of that path behind the display name, or two same-named profiles would collide
  on exactly the paths they have in common and one of them would fail to move forever.
- **A systemic failure stops the run.** Ten consecutive failures ends it. A redirected
  `AppData` making every rename a cross-device error would otherwise put one line per
  file, 564 of them, in the log at every launch forever.
- **A merge or removal in flight** relocates whole profiles. Each affected move fails with
  ENOENT, is logged, and is skipped.
- **A second run on the same day** finds `tidied-<date>` already there and adds to it. A
  file whose destination already exists is skipped rather than overwritten.
- **The whole tree is unreadable.** Logged once, nothing moved.

## Tests

The decision is a pure function over a description of the buckets, so the rule is tested
without a filesystem:

- a file with a counterpart in another profile's read bucket is moved
- a file with no counterpart anywhere is left
- a file whose only counterpart is older is left
- a file whose counterpart has the same modification time is moved
- a file in a read bucket is never a candidate
- a profile with no signed-in account contributes no candidates AND is not itself
  treated as reading nothing
- a profile with an unreadable organization stamp is skipped
- a profile whose believed-read bucket holds nothing is skipped, through the real scan as
  well as the pure function
- an organization this profile has been signed into is left alone, a bucket under another
  account is not, and the whole wrong-organization scenario is reproduced end to end,
  including the extra file that defeated the first attempt at this guard
- two profiles carrying the same display name do not have a move executed against the
  wrong one
- a source whose modification time OR size changed after the scan is not moved: `copyFile`
  restores the source's modification time on purpose, so a concurrent sync can replace the
  contents while leaving a time a time-only check would accept
- ten consecutive failures end a run without losing anything
- the Debug screen's byte count includes the folders this feature writes
- a bucket whose moves all failed is not removed
- nested directories are cleaned all the way up to the account level
- the same conversation misfiled in two profiles is handled once per profile

Plus, against a real filesystem: the directory structure is preserved, an existing
destination is not overwritten, emptied directories are removed and non-empty ones are
not, and `tidied-<date>` survives a prune.

Each test is verified by breaking the code it guards and confirming it fails, and each
mutation is confirmed to compile first: a mutation that does not build is indistinguishable
from a useless test.

## Deliberately not in scope

**A user interface.** No button, no screen, no confirmation. An earlier draft had a
Settings entry and a screen listing every bucket with its counts and sizes. None of it
attached to a decision the user could make, and this repository already records that
lesson in the remove-account design's appendix: the result screen was ceremony, and the
confirmation said one thing three times in the implementation's vocabulary.

**Deleting anything.** Files move to the backup folder and stay there. They are not staged
into `.trash`, so the 30-day deletion never applies to them.

**Touching `~/.claude/projects/`.** Transcripts are shared and separate.

**A test of `StartTidyMisfiled`.** It resolves the real machine's profiles and the real
backup root, so calling it from a test moves the conversations of whoever runs `go test`.
One was written and removed; the only reason it did no harm is that the machine it ran on
had nothing left to tidy. It also asserted nothing, since the whole body is a goroutine
launch. `TidyMisfiled` is driven directly against a temporary directory instead.

## Version

A fix to data left behind by an old defect, with no visible change. A patch bump.
