package core

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
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
	bm.pruneCatching(func() { bm.pruneWith(realPruneOps()) })
}

// pruneCatching runs the tidy-up and swallows a panic. Split out from prune so
// a test can prove the containment rather than trusting the defer is written
// correctly, which is not something reading it can establish.
func (bm *BackupManager) pruneCatching(run func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Backup] prune panicked and was contained, backups were not tidied: %v", r)
		}
	}()
	run()
}

func (bm *BackupManager) pruneWith(ops pruneOps) (staged, deleted int) {
	names, err := ops.listDirs(bm.BackupRootDir)
	if err != nil {
		log.Printf("[Backup] could not read the backups folder to tidy it: %v", err)
		return 0, 0
	}

	trashRoot := filepath.Join(bm.BackupRootDir, trashDirName)
	if doomed := snapshotsToPrune(names, pruneKeep); len(doomed) > 0 {
		if err := ops.mkdirAll(trashRoot); err != nil {
			log.Printf("[Backup] could not create %s, leaving %d old snapshots in place: %v", trashRoot, len(doomed), err)
		} else {
			for _, name := range doomed {
				src := filepath.Join(bm.BackupRootDir, name)
				dst := freeTrashPath(trashRoot, name, ops.exists)
				if err := ops.move(src, dst); err != nil {
					// Two operations pruning at once both pick the same
					// snapshot and the loser lands here. So does a permissions
					// problem. Neither is worth failing anything over.
					log.Printf("[Backup] could not set aside old snapshot %s: %v", name, err)
					continue
				}
				staged++
				log.Printf("[Backup] set aside old snapshot %s; it will be deleted after %d days", name, int(trashRetention.Hours()/24))
			}
		}
	}

	deleted = bm.emptyTrash(ops, trashRoot)
	if staged == 0 && deleted == 0 {
		log.Printf("[Backup] nothing to tidy: no profile has more than %d snapshots and nothing has been set aside long enough to delete", pruneKeep)
	}
	return staged, deleted
}

// emptyTrash deletes staged snapshots that have waited out trashRetention.
func (bm *BackupManager) emptyTrash(ops pruneOps, trashRoot string) int {
	names, err := ops.listDirs(trashRoot)
	if err != nil {
		return 0 // no trash yet, which is the ordinary case
	}
	cutoff := ops.now().Add(-trashRetention)
	deleted := 0
	for _, name := range names {
		path := filepath.Join(trashRoot, name)
		mt, err := ops.modTime(path)
		if err != nil {
			// Unreadable means unknown age, and unknown age must not be
			// treated as old. This is the one operation with no way back.
			log.Printf("[Backup] could not tell how long %s has been set aside, leaving it: %v", name, err)
			continue
		}
		if mt.After(cutoff) {
			continue
		}
		if err := ops.remove(path); err != nil {
			log.Printf("[Backup] could not delete %s: %v", name, err)
			continue
		}
		deleted++
		log.Printf("[Backup] deleted %s, set aside on %s", name, mt.Format("2006-01-02"))
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
		for _, s := range snaps[min(keep, len(snaps)):] {
			out = append(out, s.name)
		}
	}
	sort.Strings(out) // deterministic, so a log or a test reads the same each run
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// freeTrashPath picks a name inside the trash that is not already taken.
//
// A collision is not far-fetched: a profile removed and recreated can produce
// the same snapshot name twice. os.Rename onto an existing non-empty directory
// fails rather than merging, so this has to be handled rather than left to luck.
func freeTrashPath(trashRoot, name string, exists func(string) bool) string {
	candidate := filepath.Join(trashRoot, name)
	if !exists(candidate) {
		return candidate
	}
	for n := 2; n < backupCollisionLimit; n++ {
		candidate = filepath.Join(trashRoot, fmt.Sprintf("%s-%d", name, n))
		if !exists(candidate) {
			return candidate
		}
	}
	// Every name taken. Return the last one so the move fails and is logged,
	// rather than silently overwriting something.
	return candidate
}

// moveDir relocates a directory, falling back to copy-then-delete if the rename
// will not do.
//
// It does not try to work out WHY the rename failed. An earlier version matched
// the error against "cross-device", which is what Unix says and is not what
// Windows says (it says the file cannot be moved to a different disk drive), so
// the fallback it existed for would not have run on the platform most likely to
// need it. Classifying errors by their text is guessing; trying the other route
// and reporting its error instead is not. A rename that failed for a real
// reason, a locked directory or a permission, fails the copy too and that
// error is what the caller sees.
//
// The fallback is unreachable today: every caller builds NewBackupManager(""),
// so the trash is always a subdirectory of the backups root and the two ends
// are on one volume. It is here so that making the root configurable later
// cannot quietly stop pruning working, which is the failure this whole file
// exists to prevent.
//
// dst is guaranteed not to exist (see freeTrashPath), so the copy cannot merge
// into somebody else's directory.
func moveDir(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyDir(src, dst); err != nil {
		_ = os.RemoveAll(dst)
		return err
	}
	return os.RemoveAll(src)
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
	Snapshots int
	Bytes     int64
	Staged    int
	Err       string
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
			u.Bytes += dirBytes(filepath.Join(bm.BackupRootDir, e.Name()))
			continue
		}
		if _, _, _, ok := parseBackupName(e.Name()); !ok {
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
