# Prune old backup snapshots

## Why

`~/.multi-claude-switcher/backups/` grows without limit. Measured on the maintainer's
machine on 2026-08-05: **94 snapshot directories, 2.2 GB, accumulated in two weeks.**
Nothing has ever removed one.

Snapshots are created by `BackupIfHasData` before every switch (once for the source
profile and once for the destination), before every sync, before every merge and before
every restore, and by `CreateBackup` for the explicit "Back up all accounts" button.

Two facts shape the design, and both were measured rather than assumed:

**The existing reuse mitigation works.** `reusableBackup` returns the newest snapshot
instead of copying when the profile is byte-for-byte unchanged. A first hypothesis, that
`copyFile` loses sub-second precision and so the nanosecond fingerprint can never match,
was tested and disproved: across 263 files in one snapshot, `st_mtime_ns` was identical
to the live profile in every case. Of the 94 snapshots, 65 (1.60 GB) predate the
fingerprint feature and could never have been reused; only 29 (0.57 GB) came after it,
and only 3 of those duplicate their predecessor, which is what the explicit backup button
is documented to do. So most of the 2.2 GB is a one-off historical backlog, not an
ongoing leak. That does not remove the need for a bound: 29 snapshots in six days is
still unbounded growth.

**The shipped product cannot restore a backup.** The panel offers "Back up all accounts"
and "Open backup folder". `RestoreBackup` is called only from `cmd/mcs`, which is not
built by the release workflow. Backups are therefore a safety net a user reaches through
Finder or Explorer, and their value decays quickly: a snapshot from a week ago, restored,
would discard a week of conversations. This is why the retention rule counts recent
snapshots rather than trying to preserve history.

## What it does

After a backup is successfully created, snapshots beyond the newest 5 for that profile
are moved into `~/.multi-claude-switcher/backups/.trash/`. Anything already in `.trash/`
for more than 30 days is deleted permanently.

Nothing about this is visible in the panel.

## Retention rule

Group snapshot directories by the profile they belong to, using the existing
`parseBackupName`, which parses `<profile>_<YYYYMMDD>_<HHMMSS>` with an optional `-<n>`
same-second counter, from the right, because profile names may contain both underscores
and dashes.

Only directories `parseBackupName` accepts are candidates. **Anything else is never
touched.** This is load-bearing rather than tidy: the maintainer's backups folder holds a
hand-made directory, `org-cleanup-2026-08-04`, containing 564 conversation files he moved
there himself, and it must survive.

Within each profile group, order newest-first by (timestamp, sequence), keep the first 5,
and move the rest. Sequence must compare numerically: `-10` sorts before `-2` as text.

Keeping at least 1 guarantees the newest snapshot per profile survives, which is what
`reusableBackup` reads, so pruning cannot break reuse.

`manual-snapshots/` and `archive/` are siblings of `backups/` and are out of scope. They
exist because a user asked for them.

## The two stages, and why not the operating system's trash

Stage one moves a snapshot into `backups/.trash/<YYYYMMDD>-<name>/`. Stage two deletes it
thirty days later. `.trash/` is not a snapshot name `parseBackupName` accepts, so a staged
snapshot can never be considered for retention again.

**The staging date is in the name, not read from the filesystem.** The first implementation
took it from the directory's modification time, which review showed does not work: `os.Rename`
does not change a directory's mtime, so a staged snapshot still carried the mtime it had as a
snapshot. Anything older than the retention period was therefore already expired the instant
it was staged and was deleted in the same run, which removed the entire point of staging. It
went unnoticed because the machine this was written on had no snapshot older than two weeks;
any user who had installed MCS a month earlier would have lost their oldest snapshots outright
on the first switch after upgrading.

The name also decides what stage two may touch. An entry whose name this code did not write
has no staging date and is never deleted, which gives the trash the same protection the
backups root has: a folder somebody put there themselves stays.

The operating system's trash was the first choice and was rejected on review.

On Windows, `SHFileOperationW` with `FOF_ALLOWUNDO` **silently deletes permanently**
rather than recycling when the Recycle Bin is disabled by policy, when the item exceeds
the bin's quota, or when the path exceeds `MAX_PATH`. A `GetDriveType == DRIVE_FIXED`
guard covers none of the first two. An operation whose safe path and whose destructive
path are the same call, distinguished only by conditions it does not report, is not a
safe place to send a user's only copy of anything.

On macOS the equivalent, `os.Rename` into `~/.Trash`, is sound: it was tested, including
that a missing `~/.Trash` fails with ENOENT and leaves the source intact rather than
renaming the snapshot onto the trash path itself. But it produces no "Put Back" metadata
without cgo, and `core` and `platform` contain no cgo at all today, which is why the
Windows tray builds with `CGO_ENABLED=0` and needs no C toolchain.

A directory inside `backups/` is a plain `os.Rename` on one volume, identical on both
platforms, with no API that can reinterpret it as a permanent delete. The panel already
has "Open backup folder", so a user who wants something back can reach it.

Neither trash frees disk space on its own. Stage two is what actually frees it, and it is
the first thing this tool has ever deleted permanently, which is why it is time-based and
generous rather than immediate.

## Where it runs

At the end of a successful backup, in `BackupIfHasData` and `CreateBackup`.

It must not run inside `RestoreBackup`'s pre-restore backup. `RestoreBackup` backs up the
target profile before overwriting it, and if that backup triggered a prune, the snapshot
being restored could be moved out from under the copy that is about to read it. The prune
entry point therefore takes an explicit "skip" path that `RestoreBackup` uses, rather than
relying on the ordering happening to be safe.

## Failure policy

Pruning is best-effort and reports nothing upwards. A snapshot that cannot be moved is
logged and skipped, and the rest still run. The function recovers from a panic and logs
it.

The reasoning is asymmetric cost: failing to tidy the safety net is a minor problem, and
failing the switch, sync or merge the user actually asked for is a major one. This
inverts the rule everywhere else in `core`, where a backup failure aborts the operation,
and that inversion is correct only because pruning never puts live data at risk.

## Errors and edge cases

- **Cross-volume move: not handled, deliberately.** A copy-then-delete fallback was written
  for a future configurable root and then removed on review, because it made three things
  worse inside a switch that has Claude Desktop closed. It removed a destination it had not
  necessarily created, so two hosts pruning at once could have the loser delete what the
  winner had just staged. On Windows a rename fails with a sharing violation whenever an
  indexer holds a handle inside the directory, which turned a fast logged failure into a full
  recursive copy of a several-hundred-megabyte tree while the user waited. And if the copy
  succeeded while the delete then failed, the snapshot existed twice and the next prune would
  copy it again. A rename is all that is needed while the trash is a subdirectory of the root.
- **Name collision in `.trash/`.** Suffixed `-2`, `-3` and so on. When every name is taken it
  returns an error rather than a taken path: handing a caller a path that already holds
  somebody else's data is not a decision to make on the way out of a loop.
- **`.trash/` missing.** Created on demand. One that exists but cannot be read is logged,
  since that is the path that frees disk failing silently.
- **Concurrency.** Two operations pruning at once may both select the same snapshot; the
  second `os.Rename` fails with ENOENT, which is logged and skipped like any other failure.

## Observability

`log.Printf` on every move and every permanent delete, with the snapshot name and its
size, and one line when a prune finds nothing to do. Silent means undiagnosable, and this
is the one part of MCS that removes data.

The Debug info screen gains one line: `Backups: <n> snapshots, <size>` plus, when
non-empty, `<n> awaiting deletion, <size>`. The two sizes are reported apart, or a reader
attributes bytes already on their way out to the snapshots being kept. That screen exists to be pasted into an issue, and
disk usage is the first thing anyone would ask about.

## Tests

The rule is tested through an injected move function, so no test touches a real trash.

- keeps exactly the newest 5 per profile
- profiles group exactly, not by prefix: `Claude_Work_20260101_000000` is not a `Claude`
  snapshot
- same-second sequence numbers order numerically, not as text
- directories `parseBackupName` rejects are never passed to the move function, with
  `org-cleanup-2026-08-04` as a named case
- a move failure on one snapshot does not stop the others and does not fail the caller
- a profile with 5 or fewer snapshots produces no moves
- `.trash/` entries older than 30 days are deleted; younger ones are not
- a snapshot staged in this very run survives it, which is the property the first
  implementation silently did not have
- a `.trash/` entry whose name this code did not write is never deleted
- `RestoreBackup`'s pre-restore backup does not prune
- a panic inside the prune does not escape, driven through the real entry point rather
  than through a wrapper whose own defer proves nothing

Each test is verified by breaking the code it guards and confirming it fails.

## Deliberately not in scope

**Hardlink snapshots.** Making `copyDir` hardlink unchanged files instead of copying them
would make snapshots nearly free, which attacks the cause rather than the symptom. It
depends on Claude Desktop replacing session files wholesale rather than editing them in
place: with hardlinks, an in-place edit would silently rewrite every snapshot that shares
the file, destroying the backups instead of bounding them.

The existing comment on `sessionsFingerprint` asserts wholesale replacement without
evidence. A first attempt to establish it by comparing birth time with modification time
was abandoned when a positive control showed the metric cannot distinguish the two cases
on APFS. A direct measurement was then started: 532 files were recorded by inode, and of
the 4 rewritten during the observation window, all 4 had a new inode, which supports
wholesale replacement. Four is not enough to bet every backup on, and the change also
touches restore and the fingerprint's meaning. Recorded as its own issue with the evidence
so far.

**Removing the explicit backup button's always-copy behaviour.** It copies on purpose:
the user pressed something that says it will back up.

**A total size cap.** Considered. It bounds bytes but leaves a user unable to predict how
many steps back they can go, and the count rule already bounds growth. Revisit if a single
profile's sessions ever grow large enough that 5 snapshots is itself too much.

## Version

A bug fix in behaviour the user never sees, so a patch bump.
