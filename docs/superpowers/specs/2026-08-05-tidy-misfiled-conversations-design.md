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

One consequence is real and is accepted deliberately. A bucket named by an organization
the profile still carries a stamp for is one the user was signed into and could sign into
again. Moving its files means that if they rejoin, that organization looks empty. The
conversations themselves are not lost, since they are also in the organization the
profile reads today, and the moved copies can be fetched from the backup folder. The
alternative considered was leaving those buckets alone, which would have removed the
surprise entirely at the cost of about 4 MB of the 36 MB. The maintainer chose to move
them.

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
  touches. A user switching organization mid-run would move a bucket from unread to read
  underneath the walk; the per-file moves that had already happened are recoverable from
  the backup folder, and the rest are skipped when their destination check fails.
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

## Version

A fix to data left behind by an old defect, with no visible change. A patch bump.
