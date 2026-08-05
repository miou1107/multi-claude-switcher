package core

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// pruneKeep is how many snapshots survive per profile.
//
// Five, because a snapshot's value decays fast. The shipped app has no restore
// button at all (RestoreBackup is only reached from the unshipped CLI), so a
// backup is a safety net somebody opens in Finder or Explorer after a switch
// went wrong, and they find that out within minutes. Restoring a week-old
// snapshot would discard a week of conversations, so keeping one is not a
// kindness.
//
// It must never be less than 1: reusableBackup reads the newest snapshot of a
// profile to decide whether a fresh copy is needed, so pruning that away would
// silently turn reuse off.
const pruneKeep = 5

// trashDirName is where pruned snapshots wait before permanent deletion. It sits
// inside the backups root, on the same volume, so staging is a plain rename.
//
// The name is deliberately one parseBackupName rejects, which is what stops a
// staged snapshot being counted as a snapshot again.
const trashDirName = ".trash"

// trashRetention is how long a staged snapshot waits before it is deleted for
// good. Thirty days: this is the only thing MCS ever deletes permanently, and
// the point of staging is to give somebody who notices late a way back.
const trashRetention = 30 * 24 * time.Hour

// pruneOps is the filesystem underneath a prune, replaced wholesale in tests so
// no test needs a real trash directory or a real clock.
type pruneOps struct {
	// listDirs returns the directory names directly inside a path.
	listDirs func(dir string) ([]string, error)
	// modTime reports when a directory was last modified.
	modTime func(path string) (time.Time, error)
	// move relocates a directory. dst is guaranteed not to exist.
	move func(src, dst string) error
	// remove deletes a directory tree permanently.
	remove func(path string) error
	// exists reports whether a path is already taken.
	exists func(path string) bool
	// mkdirAll creates the trash directory on demand.
	mkdirAll func(path string) error
	now      func() time.Time
}

func realPruneOps() pruneOps {
	return pruneOps{
		listDirs: func(dir string) ([]string, error) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				return nil, err
			}
			var out []string
			for _, e := range entries {
				if e.IsDir() {
					out = append(out, e.Name())
				}
			}
			return out, nil
		},
		modTime: func(path string) (time.Time, error) {
			fi, err := os.Stat(path)
			if err != nil {
				return time.Time{}, err
			}
			return fi.ModTime(), nil
		},
		move:     moveDir,
		remove:   os.RemoveAll,
		exists:   func(path string) bool { _, err := os.Lstat(path); return err == nil },
		mkdirAll: func(path string) error { return os.MkdirAll(path, 0755) },
		now:      time.Now,
	}
}

// prune stages snapshots beyond the newest pruneKeep per profile, then deletes
// anything that has been staged for longer than trashRetention.
//
// It reports nothing upwards and cannot fail the caller. Every backup in this
// package aborts its operation when it fails, because the alternative is
// overwriting live data with no way back. This inverts that, and the inversion
// is safe only because pruning touches nothing but copies: failing to tidy the
// safety net costs disk, and failing the switch the user asked for costs them
// their afternoon.
//
// The recover is not decoration. A panic here would propagate out through
// CreateBackup into SafeSwitch and take down a switch that had already closed
// Claude Desktop, which is the worst moment to stop.
func (bm *BackupManager) prune() {
	bm.pruneWith(realPruneOps())
}

// pruneWith is prune with its filesystem passed in.
//
// The recover lives here rather than in prune so that a test can reach it: a
// wrapper around prune could be deleted and every test would stay green while
// the containment was gone. Driving it through the ops means the test panics
// where a real failure would.
func (bm *BackupManager) pruneWith(ops pruneOps) (staged, deleted int) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Backup] prune panicked and was contained, backups were not tidied: %v", r)
			staged, deleted = 0, 0
		}
	}()
	names, err := ops.listDirs(bm.BackupRootDir)
	if err != nil {
		log.Printf("[Backup] could not read the backups folder to tidy it: %v", err)
		return 0, 0
	}

	troubled := false
	trashRoot := filepath.Join(bm.BackupRootDir, trashDirName)
	if doomed := snapshotsToPrune(names, pruneKeep); len(doomed) > 0 {
		if err := ops.mkdirAll(trashRoot); err != nil {
			log.Printf("[Backup] could not create %s, leaving %d old snapshots in place: %v", trashRoot, len(doomed), err)
			troubled = true
		} else {
			stampedAt := ops.now()
			for _, name := range doomed {
				src := filepath.Join(bm.BackupRootDir, name)
				dst, err := freeTrashPath(trashRoot, stagedName(stampedAt, name), ops.exists)
				if err != nil {
					log.Printf("[Backup] could not find a free name in %s for %s: %v", trashRoot, name, err)
					troubled = true
					continue
				}
				if err := ops.move(src, dst); err != nil {
					// Two operations pruning at once both pick the same
					// snapshot and the loser lands here. So does a permissions
					// problem. Neither is worth failing anything over.
					log.Printf("[Backup] could not set aside old snapshot %s: %v", name, err)
					troubled = true
					continue
				}
				staged++
				log.Printf("[Backup] set aside old snapshot %s; it will be deleted after %d days", name, int(trashRetention.Hours()/24))
			}
		}
	}

	deleted = bm.emptyTrash(ops, trashRoot)
	// Only when there was genuinely nothing to do. Saying it after a failure
	// contradicts the line that just explained the failure, and whoever reads
	// the log later believes the wrong one.
	if staged == 0 && deleted == 0 && !troubled {
		log.Printf("[Backup] nothing to tidy: no profile has more than %d snapshots and nothing has been set aside long enough to delete", pruneKeep)
	}
	return staged, deleted
}

// emptyTrash deletes staged snapshots that have waited out trashRetention.
func (bm *BackupManager) emptyTrash(ops pruneOps, trashRoot string) int {
	names, err := ops.listDirs(trashRoot)
	if err != nil {
		// A trash that has never been created is the ordinary case and says
		// nothing. One that exists and cannot be read is the code path that
		// frees disk failing silently, which is worth a line.
		if !os.IsNotExist(err) {
			log.Printf("[Backup] could not read %s, nothing was deleted: %v", trashRoot, err)
		}
		return 0
	}
	cutoff := ops.now().Add(-trashRetention)
	deleted := 0
	for _, name := range names {
		stagedOn, ok := stagedTime(name)
		if !ok {
			// Not a name this code wrote. Somebody's own folder, or something
			// from a future version. Never deleted: this is the one operation
			// with no way back, and the same rule that protects a hand-made
			// directory in the backups root has to protect one here.
			continue
		}
		if stagedOn.After(cutoff) {
			continue
		}
		path := filepath.Join(trashRoot, name)
		if err := ops.remove(path); err != nil {
			log.Printf("[Backup] could not delete %s: %v", name, err)
			continue
		}
		deleted++
		log.Printf("[Backup] deleted %s, set aside on %s", name, stagedOn.Format("2006-01-02"))
	}
	return deleted
}

// snapshotsToPrune returns the directory names that should be set aside: every
// snapshot beyond the newest keep for its profile.
//
// Names parseBackupName rejects are not snapshots and are never returned. That
// is what protects a folder somebody made by hand and put their own files in.
func snapshotsToPrune(names []string, keep int) []string {
	if keep < 1 {
		keep = 1 // never prune away the snapshot reusableBackup depends on
	}

	type snap struct {
		name  string
		stamp string
		seq   int
	}
	byProfile := map[string][]snap{}
	for _, name := range names {
		profile, stamp, seq, ok := parseBackupName(name)
		if !ok {
			continue
		}
		byProfile[profile] = append(byProfile[profile], snap{name, stamp, seq})
	}

	var out []string
	for _, snaps := range byProfile {
		// Newest first. seq compares as a number: "-10" sorts before "-2" as
		// text, which would make the tenth snapshot of one second look older
		// than the second.
		sort.Slice(snaps, func(i, j int) bool {
			if snaps[i].stamp != snaps[j].stamp {
				return snaps[i].stamp > snaps[j].stamp
			}
			return snaps[i].seq > snaps[j].seq
		})
		for _, s := range snaps[keepCount(keep, len(snaps)):] {
			out = append(out, s.name)
		}
	}
	sort.Strings(out) // deterministic, so a log or a test reads the same each run
	return out
}

// keepCount is how many of a profile's snapshots survive: keep, or all of them
// when there are fewer than that. Named rather than using the builtin min,
// which this file would otherwise shadow for every other file in the package.
func keepCount(keep, have int) int {
	if keep < have {
		return keep
	}
	return have
}

// stagedName is the name a snapshot takes in the trash: the date it was staged,
// then the snapshot's own name.
//
// The date is in the name rather than read back from the filesystem because
// os.Rename does not change a directory's modification time. Reading it from
// there meant a staged snapshot still carried the mtime it had as a snapshot,
// so anything older than the retention period was already expired the moment it
// was staged and was deleted in the same run. The promised month to fetch
// something back did not exist, and the only reason it went unnoticed is that
// the machine it was written on had no snapshot older than two weeks.
//
// A name also survives being copied, and says plainly, to somebody looking at
// the folder in Finder, when the thing was set aside.
func stagedName(at time.Time, snapshot string) string {
	return at.Format("20060102") + "-" + snapshot
}

// stagedTime reads back what stagedName wrote. ok is false for any name this
// code did not write, which is what keeps somebody's own folder in the trash
// from ever being deleted.
func stagedTime(name string) (time.Time, bool) {
	date, rest, found := strings.Cut(name, "-")
	if !found || rest == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("20060102", date, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// freeTrashPath picks a name inside the trash that is not already taken.
//
// A collision is not far-fetched: two snapshots of one profile staged on the
// same day differ only by the snapshot name, and a profile removed and
// recreated can produce the same snapshot name twice.
//
// It returns an error rather than a taken path when it runs out. An earlier
// version returned the last candidate "so the move fails and is logged", which
// was safe only while the move was a bare os.Rename. Handing a caller a path
// that already holds somebody else's data is not a decision to make on the way
// out of a loop.
func freeTrashPath(trashRoot, name string, exists func(string) bool) (string, error) {
	candidate := filepath.Join(trashRoot, name)
	if !exists(candidate) {
		return candidate, nil
	}
	for n := 2; n < backupCollisionLimit; n++ {
		candidate = filepath.Join(trashRoot, fmt.Sprintf("%s-%d", name, n))
		if !exists(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("every name from %s to %s-%d is taken", name, name, backupCollisionLimit-1)
}

// moveDir relocates a directory, and does nothing else.
//
// There is deliberately no copy-then-delete fallback. One was written, on the
// grounds that a configurable backups root could later put the trash on another
// volume. It made things worse in three ways, all of them inside a switch that
// has Claude Desktop closed:
//
//   - On a failure it removed the destination, which it had not necessarily
//     created. Two hosts pruning at once could have the loser delete the
//     snapshot the winner had just staged, bypassing the retention period
//     entirely.
//   - On Windows a rename fails with a sharing violation whenever an indexer or
//     antivirus holds a handle inside the directory, which is routine. That
//     turned a fast logged failure into a full recursive copy of a
//     several-hundred-megabyte tree while the user waited.
//   - If the copy succeeded and the delete then failed for the same lock, the
//     snapshot existed twice, and the next prune would copy it again.
//
// Every caller in the shipped hosts builds NewBackupManager(""), so both ends
// are on one volume today and a rename is all that is needed. MergeRequest.
// BackupRoot is a public field and only tests set it, but it is API rather than
// an internal, so that could change; whoever changes it owns this decision, and
// the failure here will be a logged skip rather than anything destructive.
func moveDir(src, dst string) error {
	return os.Rename(src, dst)
}

// BackupUsage describes what the backups folder is holding, for the Debug info
// screen.
//
// Snapshots and staged entries are counted separately because they mean
// different things to a reader: one is protection, the other is space about to
// come back. Directories MCS did not name are counted in neither, matching what
// pruning will and will not touch, so the report does not invite anyone to
// wonder why a number does not add up.
type BackupUsage struct {
	Snapshots   int
	Bytes       int64 // the surviving snapshots only
	Staged      int
	StagedBytes int64 // what is waiting to be deleted
	Other       int   // directories MCS did not name, counted so the numbers add up
	Err         string
}

func (bm *BackupManager) Usage() BackupUsage {
	entries, err := os.ReadDir(bm.BackupRootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return BackupUsage{} // nothing has been backed up yet
		}
		return BackupUsage{Err: err.Error()}
	}
	var u BackupUsage
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == trashDirName {
			staged, _ := os.ReadDir(filepath.Join(bm.BackupRootDir, trashDirName))
			for _, s := range staged {
				if s.IsDir() {
					u.Staged++
				}
			}
			u.StagedBytes += dirBytes(filepath.Join(bm.BackupRootDir, e.Name()))
			continue
		}
		if _, _, _, ok := parseBackupName(e.Name()); !ok {
			u.Other++
			continue
		}
		u.Snapshots++
		u.Bytes += dirBytes(filepath.Join(bm.BackupRootDir, e.Name()))
	}
	return u
}

// dirBytes sums a tree, ignoring what it cannot read. A size is a nice-to-know
// on a diagnostics screen and never worth an error.
func dirBytes(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}
