package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestPruneKeepsTheNewestFivePerProfile(t *testing.T) {
	var names []string
	for i := 1; i <= 8; i++ {
		names = append(names, fmt.Sprintf("Claude_2026080%d_120000", i))
	}
	got := snapshotsToPrune(names, 5)
	want := []string{
		"Claude_20260801_120000",
		"Claude_20260802_120000",
		"Claude_20260803_120000",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruned %v, want %v", got, want)
	}
}

// The count is per profile, not across the folder. Two accounts switched
// between each other is the ordinary case, and pooling them would leave one
// profile with almost nothing.
func TestPruneCountsEachProfileSeparately(t *testing.T) {
	var names []string
	for i := 1; i <= 6; i++ {
		names = append(names,
			fmt.Sprintf("Claude_2026080%d_120000", i),
			fmt.Sprintf("Claude_Profile2_2026080%d_120000", i))
	}
	got := snapshotsToPrune(names, 5)
	want := []string{"Claude_20260801_120000", "Claude_Profile2_20260801_120000"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruned %v, want exactly the oldest of each profile: %v", got, want)
	}
}

// "Claude" and "Claude_Work" are both names MCS itself hands out. Grouping by
// prefix would count every Claude_Work snapshot as a Claude one, so a user with
// two accounts would have the second one's history thrown away to make room.
func TestPruneGroupsByExactProfileNotPrefix(t *testing.T) {
	names := []string{
		"Claude_20260801_120000",
		"Claude_Work_20260801_120000",
		"Claude_Work_20260802_120000",
		"Claude_Work_20260803_120000",
	}
	if got := snapshotsToPrune(names, 2); len(got) != 1 || got[0] != "Claude_Work_20260801_120000" {
		t.Errorf("pruned %v, want only the oldest Claude_Work: a Claude_Work snapshot is not a Claude snapshot", got)
	}
}

// The same-second counter has to compare as a number. As text "-10" sorts
// before "-2", which makes the tenth snapshot of a second look older than the
// second one and prunes the wrong end.
func TestPruneOrdersSameSecondSnapshotsNumerically(t *testing.T) {
	names := []string{
		"Claude_20260801_120000",
		"Claude_20260801_120000-2",
		"Claude_20260801_120000-10",
	}
	got := snapshotsToPrune(names, 1)
	want := []string{"Claude_20260801_120000", "Claude_20260801_120000-2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruned %v, want %v: -10 is the newest, not the oldest", got, want)
	}
}

// The one that protects somebody's own files. The maintainer's backups folder
// holds org-cleanup-2026-08-04, a directory he made by hand containing 564
// conversation files.
func TestPruneNeverTouchesDirectoriesItDidNotName(t *testing.T) {
	names := []string{
		"org-cleanup-2026-08-04",
		"org-cleanup-2026-07-01",
		".trash",
		"notes",
		"Claude_backup_old",
		"Claude_20260801",
		"Claude_20260801_120000",
	}
	// Two hand-made directories, not one: with a single one, a bug that pools
	// every unparseable name into one group still leaves it alone, because a
	// group of one is always under the limit. Two is the smallest fixture that
	// can actually fail.
	if got := snapshotsToPrune(names, 0); len(got) != 0 {
		t.Errorf("pruned %v with only one real snapshot and keep clamped to 1: nothing here may be touched", got)
	}
	// Positive control: the same call does prune when there IS something to
	// prune, so the result above is not "the function does nothing".
	names = append(names, "Claude_20260802_120000")
	if got := snapshotsToPrune(names, 1); len(got) != 1 || got[0] != "Claude_20260801_120000" {
		t.Fatalf("pruned %v, want only Claude_20260801_120000: the test above proves nothing if this cannot prune at all", got)
	}
}

// keep must never reach 0. reusableBackup reads the newest snapshot to decide
// whether a copy is needed, so pruning it away turns reuse off silently, which
// is the growth this whole file exists to stop.
func TestPruneNeverRemovesEveryStapshotOfAProfile(t *testing.T) {
	names := []string{"Claude_20260801_120000", "Claude_20260802_120000"}
	for _, keep := range []int{0, -1} {
		got := snapshotsToPrune(names, keep)
		if len(got) != 1 || got[0] != "Claude_20260801_120000" {
			t.Errorf("keep=%d pruned %v, want the newest to survive", keep, got)
		}
	}
}

func TestPruneDoesNothingBelowTheLimit(t *testing.T) {
	names := []string{"Claude_20260801_120000", "Claude_20260802_120000"}
	if got := snapshotsToPrune(names, 5); len(got) != 0 {
		t.Errorf("pruned %v with only 2 snapshots and a limit of 5", got)
	}
}

// fakeFS is a prune's filesystem: directory names, modification times, and a
// record of what was moved and deleted.
type fakeFS struct {
	dirs     map[string][]string // dir -> child directory names
	mtimes   map[string]time.Time
	moved    [][2]string
	removed  []string
	moveErr  map[string]error
	mkdirErr error
	now      time.Time
}

func (f *fakeFS) ops() pruneOps {
	return pruneOps{
		listDirs: func(dir string) ([]string, error) {
			d, ok := f.dirs[dir]
			if !ok {
				return nil, errors.New("no such directory")
			}
			out := append([]string(nil), d...)
			sort.Strings(out)
			return out, nil
		},
		modTime: func(path string) (time.Time, error) {
			mt, ok := f.mtimes[path]
			if !ok {
				return time.Time{}, errors.New("cannot stat")
			}
			return mt, nil
		},
		move: func(src, dst string) error {
			if err := f.moveErr[filepath.Base(src)]; err != nil {
				return err
			}
			f.moved = append(f.moved, [2]string{src, dst})
			// Actually move it in the fake. Without this the fake cannot
			// model staging followed by deletion in one run, which is exactly
			// where the retention period went missing.
			parent, name := filepath.Dir(dst), filepath.Base(dst)
			f.dirs[parent] = append(f.dirs[parent], name)
			srcParent, srcName := filepath.Dir(src), filepath.Base(src)
			var kept []string
			for _, n := range f.dirs[srcParent] {
				if n != srcName {
					kept = append(kept, n)
				}
			}
			f.dirs[srcParent] = kept
			return nil
		},
		remove: func(path string) error {
			f.removed = append(f.removed, path)
			return nil
		},
		exists:   func(path string) bool { _, ok := f.mtimes[path]; return ok },
		mkdirAll: func(string) error { return f.mkdirErr },
		now:      func() time.Time { return f.now },
	}
}

func movedNames(f *fakeFS) []string {
	var out []string
	for _, m := range f.moved {
		out = append(out, filepath.Base(m[0]))
	}
	sort.Strings(out)
	return out
}

func TestPruneStagesIntoTheTrashDirectory(t *testing.T) {
	root := "/root"
	// pruneKeep + 1, so exactly one snapshot is over the limit. pruneWith uses
	// the real constant rather than taking a limit, so the fixture has to be
	// sized against it.
	var names []string
	for i := 1; i <= pruneKeep+1; i++ {
		names = append(names, fmt.Sprintf("Claude_2026080%d_120000", i))
	}
	f := &fakeFS{
		dirs:   map[string][]string{root: names},
		mtimes: map[string]time.Time{},
		now:    time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
	}
	bm := &BackupManager{BackupRootDir: root}
	staged, deleted := bm.pruneWith(f.ops())

	if staged != 1 || deleted != 0 {
		t.Fatalf("staged=%d deleted=%d, want 1 and 0", staged, deleted)
	}
	want := [2]string{
		filepath.Join(root, "Claude_20260801_120000"),
		// The staging date leads the name: it is where the retention clock is
		// read from, because a rename does not change a directory's mtime.
		filepath.Join(root, ".trash", "20260805-Claude_20260801_120000"),
	}
	if f.moved[0] != want {
		t.Errorf("moved %v, want %v", f.moved[0], want)
	}
}

// A move that fails must not stop the others, and must not fail the caller.
// The realistic cause is two operations pruning at once: the loser's rename
// finds the directory already gone.
func TestPruneCarriesOnAfterAFailedMove(t *testing.T) {
	root := "/root"
	var names []string
	for i := 1; i <= 8; i++ {
		names = append(names, fmt.Sprintf("Claude_2026080%d_120000", i))
	}
	f := &fakeFS{
		dirs:    map[string][]string{root: names},
		mtimes:  map[string]time.Time{},
		moveErr: map[string]error{"Claude_20260802_120000": errors.New("no such file or directory")},
		now:     time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
	}
	bm := &BackupManager{BackupRootDir: root}
	staged, _ := bm.pruneWith(f.ops())

	if staged != 2 {
		t.Errorf("staged=%d, want 2: one of the three failed and the other two must still go", staged)
	}
	got := movedNames(f)
	want := []string{"Claude_20260801_120000", "Claude_20260803_120000"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("moved %v, want %v", got, want)
	}
}

func TestPruneDeletesOnlyWhatHasWaitedLongEnough(t *testing.T) {
	root := "/root"
	trash := filepath.Join(root, ".trash")
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	old := stagedName(now.Add(-31*24*time.Hour), "Claude_20260601_120000")
	fresh := stagedName(now.Add(-29*24*time.Hour), "Claude_20260602_120000")
	exactly := stagedName(now.Add(-trashRetention), "Claude_20260603_120000")
	f := &fakeFS{
		dirs: map[string][]string{
			root:  {".trash"},
			trash: {old, fresh, exactly},
		},
		mtimes: map[string]time.Time{},
		now:    now,
	}
	bm := &BackupManager{BackupRootDir: root}
	_, deleted := bm.pruneWith(f.ops())

	if deleted != 2 {
		t.Fatalf("deleted=%d, want 2 (older than 30 days, and exactly 30 days)", deleted)
	}
	sort.Strings(f.removed)
	want := []string{filepath.Join(trash, old), filepath.Join(trash, exactly)}
	sort.Strings(want)
	if !reflect.DeepEqual(f.removed, want) {
		t.Errorf("removed %v, want %v: the 29-day-old one must survive", f.removed, want)
	}
}

// The trash gets the same protection the backups root gets: a directory this
// code did not name is never deleted. Somebody who parks a folder in there, or
// a leftover from a future version, keeps it.
func TestPruneLeavesTrashEntriesItDidNotName(t *testing.T) {
	root := "/root"
	trash := filepath.Join(root, ".trash")
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	old := stagedName(now.Add(-31*24*time.Hour), "Claude_20260601_120000")
	f := &fakeFS{
		dirs: map[string][]string{
			root:  {".trash"},
			trash: {"my-own-folder", "Claude_20260601_120000", old},
		},
		mtimes: map[string]time.Time{},
		now:    now,
	}
	bm := &BackupManager{BackupRootDir: root}
	_, deleted := bm.pruneWith(f.ops())

	if deleted != 1 || len(f.removed) != 1 || filepath.Base(f.removed[0]) != old {
		t.Errorf("removed %v (deleted=%d), want only %q", f.removed, deleted, old)
	}
}

// The trash directory is itself a directory inside the backups root, so it is
// offered to the retention rule along with the snapshots. Its name must not
// parse as one, or MCS would eventually put the trash inside itself.
func TestPruneNeverStagesTheTrashDirectoryItself(t *testing.T) {
	if _, _, _, ok := parseBackupName(trashDirName); ok {
		t.Fatalf("%q parses as a snapshot name, so it could be staged into itself", trashDirName)
	}
	var names []string
	for i := 1; i <= 8; i++ {
		names = append(names, fmt.Sprintf("Claude_2026080%d_120000", i))
	}
	for _, n := range snapshotsToPrune(append(names, trashDirName), 5) {
		if n == trashDirName {
			t.Error("the trash directory was selected for staging")
		}
	}
}

func TestPruneLeavesEverythingWhenTheTrashCannotBeCreated(t *testing.T) {
	root := "/root"
	var names []string
	for i := 1; i <= 8; i++ {
		names = append(names, fmt.Sprintf("Claude_2026080%d_120000", i))
	}
	f := &fakeFS{
		dirs:     map[string][]string{root: names},
		mtimes:   map[string]time.Time{},
		mkdirErr: errors.New("permission denied"),
		now:      time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
	}
	bm := &BackupManager{BackupRootDir: root}
	staged, _ := bm.pruneWith(f.ops())
	if staged != 0 || len(f.moved) != 0 {
		t.Errorf("staged=%d moved=%v, want nothing moved when the trash cannot be created", staged, f.moved)
	}
}

func TestFreeTrashPathAvoidsCollisions(t *testing.T) {
	taken := map[string]bool{
		filepath.Join("/t", "snap"):   true,
		filepath.Join("/t", "snap-2"): true,
	}
	got, err := freeTrashPath("/t", "snap", func(p string) bool { return taken[p] })
	if err != nil {
		t.Fatalf("freeTrashPath: %v", err)
	}
	if want := filepath.Join("/t", "snap-3"); got != want {
		t.Errorf("freeTrashPath = %q, want %q", got, want)
	}
}

// Out of names must be an error, not a path somebody else's data is already at.
// The caller moves a directory onto whatever it is handed.
func TestFreeTrashPathRefusesWhenEveryNameIsTaken(t *testing.T) {
	if _, err := freeTrashPath("/t", "snap", func(string) bool { return true }); err == nil {
		t.Error("freeTrashPath returned a path with every name taken")
	}
}

func TestStagedNameRoundTrips(t *testing.T) {
	at := time.Date(2026, 8, 5, 13, 45, 0, 0, time.Local)
	name := stagedName(at, "Claude_20260722_170103")
	if name != "20260805-Claude_20260722_170103" {
		t.Errorf("stagedName = %q", name)
	}
	got, ok := stagedTime(name)
	if !ok || !got.Equal(time.Date(2026, 8, 5, 0, 0, 0, 0, time.Local)) {
		t.Errorf("stagedTime(%q) = %v, %v", name, got, ok)
	}
}

// Anything this code did not write is never given an age, so it is never
// deleted. Somebody who parks a folder in the trash keeps it.
func TestStagedTimeRejectsNamesItDidNotWrite(t *testing.T) {
	for _, name := range []string{
		"my-own-folder",
		"Claude_20260722_170103", // a snapshot name, not a staged one
		"org-cleanup-2026-08-04",
		"20260805",  // date but nothing after it
		"-Claude_x", // nothing before the dash
		"notadate-Claude_x",
		"",
	} {
		if _, ok := stagedTime(name); ok {
			t.Errorf("stagedTime(%q) claimed to know when it was staged", name)
		}
	}
}

// makeSnapshot writes a snapshot directory that looks like one CreateBackup
// made, so a real BackupManager will treat it as a candidate.
func makeSnapshot(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name, "claude-code-sessions", "acct", "org")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(name), 0644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, name)
}

// makeProfile writes a profile with a session tree, enough for BackupIfHasData
// and CreateBackup to work on.
func makeProfile(t *testing.T, dir string) string {
	t.Helper()
	sessions := filepath.Join(dir, "claude-code-sessions", "acct", "org")
	if err := os.MkdirAll(sessions, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, "live.json"), []byte("live"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// End to end against a real filesystem: a backup taken when the profile is
// already over the limit tidies the excess into .trash, and the snapshot it
// just took survives.
func TestCreateBackupTidiesOldSnapshots(t *testing.T) {
	root := t.TempDir()
	profile := makeProfile(t, filepath.Join(t.TempDir(), "Claude"))
	for i := 1; i <= pruneKeep+2; i++ {
		makeSnapshot(t, root, fmt.Sprintf("Claude_2026070%d_120000", i))
	}
	// Two of them, for the reason given in
	// TestPruneNeverTouchesDirectoriesItDidNotName.
	handmade := makeSnapshot(t, root, "org-cleanup-2026-08-04")
	handmade2 := makeSnapshot(t, root, "org-cleanup-2026-07-01")

	bm := &BackupManager{BackupRootDir: root}
	fresh, err := bm.CreateBackup(profile)
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("the snapshot just taken is gone: %v", err)
	}
	for _, p := range []string{handmade, handmade2} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("a directory MCS did not name was removed: %s: %v", filepath.Base(p), err)
		}
	}
	staged, err := os.ReadDir(filepath.Join(root, trashDirName))
	if err != nil {
		t.Fatalf("no trash directory was created: %v", err)
	}
	// 7 old + 1 fresh = 8 for this profile; 5 survive, so 3 are staged.
	if len(staged) != 3 {
		var names []string
		for _, e := range staged {
			names = append(names, e.Name())
		}
		t.Errorf("staged %v, want 3", names)
	}
	// And they were moved, not copied.
	for _, e := range staged {
		if _, err := os.Stat(filepath.Join(root, e.Name())); err == nil {
			t.Errorf("%s is in the trash but also still in the backups root", e.Name())
		}
	}
}

// The restore path must not tidy. Pruning there could set aside the snapshot
// being restored, which the copy that follows is about to read.
func TestRestoreDoesNotTidy(t *testing.T) {
	root := t.TempDir()
	target := makeProfile(t, filepath.Join(t.TempDir(), "Claude"))
	// Over the limit, so any prune would visibly do something.
	var oldest string
	for i := 1; i <= pruneKeep+2; i++ {
		p := makeSnapshot(t, root, fmt.Sprintf("Claude_2026070%d_120000", i))
		if i == 1 {
			oldest = p
		}
	}

	bm := &BackupManager{BackupRootDir: root}
	if err := bm.RestoreBackup(oldest, target); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}

	// RestoreBackup takes its own pre-restore backup, so the count grows; what
	// matters is that nothing was staged.
	if entries, err := os.ReadDir(filepath.Join(root, trashDirName)); err == nil && len(entries) > 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("restore staged %v: pruning during a restore can move the snapshot being restored", names)
	}
}

// Pruning is best-effort in the strongest sense: a panic inside it must not
// escape into the switch that is holding Claude Desktop closed.
//
// It panics inside the ops, which is where a real one would come from, and goes
// through pruneWith, the function prune actually calls. An earlier version
// called a wrapper directly and asserted that the wrapper's own defer worked,
// which stayed green no matter what prune did.
func TestPruneContainsAPanic(t *testing.T) {
	ops := realPruneOps()
	ops.listDirs = func(string) ([]string, error) { panic("boom") }

	bm := &BackupManager{BackupRootDir: t.TempDir()}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a panic escaped and would have taken down the switch that called it: %v", r)
		}
	}()
	staged, deleted := bm.pruneWith(ops)
	if staged != 0 || deleted != 0 {
		t.Errorf("staged=%d deleted=%d after a panic, want 0 and 0", staged, deleted)
	}
}

// And the real entry point goes through it.
func TestPruneOnARootThatIsNotThere(t *testing.T) {
	bm := &BackupManager{BackupRootDir: filepath.Join(t.TempDir(), "nope")}
	bm.prune() // must not panic or block
}

func TestMoveDirMovesTheWholeTree(t *testing.T) {
	root := t.TempDir()
	src := makeSnapshot(t, root, "Claude_20260801_120000")
	dst := filepath.Join(root, ".trash", "Claude_20260801_120000")
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := moveDir(src, dst); err != nil {
		t.Fatalf("moveDir: %v", err)
	}
	if _, err := os.Stat(src); err == nil {
		t.Error("the source is still there: this was a copy, not a move")
	}
	got, err := os.ReadFile(filepath.Join(dst, "claude-code-sessions", "acct", "org", "a.json"))
	if err != nil {
		t.Fatalf("the tree did not arrive intact: %v", err)
	}
	if string(got) != "Claude_20260801_120000" {
		t.Errorf("file contents = %q, want the original", got)
	}
}

// A move that cannot work must surface an error rather than reporting success,
// and must leave the snapshot where it was: the caller logs and moves on, and a
// snapshot that has silently gone is worse than one that did not move.
//
// The destination's parent is a FILE, so both the rename and the copy fail.
// A merely missing parent does not work as a fixture here: copyDir creates
// parent directories, so the copy succeeds and the move is real. That is fine
// in production, where the parent is the trash directory that pruneWith has
// already created, but it makes the obvious fixture prove nothing.
func TestMoveDirReportsAFailure(t *testing.T) {
	root := t.TempDir()
	src := makeSnapshot(t, root, "Claude_20260801_120000")
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := moveDir(src, filepath.Join(blocker, "x")); err == nil {
		t.Error("moveDir reported success when the destination could not exist")
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("the source was removed even though the move failed: %v", err)
	}
}

// Reproduction for the defect this file's staging existed to prevent: a
// snapshot older than the retention period was staged and permanently deleted
// in the same prune run, so the promised month to fetch it back never existed.
//
// The cause was reading the staging time from the directory's modification
// time. os.Rename does not touch it, so a staged snapshot kept the mtime it had
// as a snapshot, which is roughly when it was created. Any snapshot older than
// the retention period was therefore already expired the instant it was staged.
//
// Nobody noticed because the machine it was written on had no snapshot older
// than two weeks.
func TestAFreshlyStagedSnapshotSurvivesItsOwnPruneRun(t *testing.T) {
	root := t.TempDir()
	profile := makeProfile(t, filepath.Join(t.TempDir(), "Claude"))

	// Old enough that the retention period has already passed, which is the
	// ordinary case for anyone who has had MCS installed for a month.
	old := time.Now().Add(-60 * 24 * time.Hour)
	for i := 1; i <= pruneKeep+3; i++ {
		p := makeSnapshot(t, root, fmt.Sprintf("Claude_2026060%d_120000", i))
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}

	bm := &BackupManager{BackupRootDir: root}
	if _, err := bm.CreateBackup(profile); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	staged, err := os.ReadDir(filepath.Join(root, trashDirName))
	if err != nil {
		t.Fatalf("nothing was staged at all: %v", err)
	}
	if len(staged) == 0 {
		t.Fatal("everything staged in this run was deleted in the same run: the retention period does not exist")
	}
	// pruneKeep+3 old snapshots plus the one CreateBackup just took, keeping
	// pruneKeep, leaves 4 staged.
	if len(staged) != 4 {
		var names []string
		for _, e := range staged {
			names = append(names, e.Name())
		}
		t.Errorf("staged %v, want all 4 to still be there", names)
	}
}

// Directories MCS did not name as snapshots are counted, and so are their
// bytes. The tidied-<date> folders the misfiled-conversation cleanup writes
// live here, and without their bytes the Debug screen under-reports the folder
// by exactly what that cleanup put in it.
func TestUsageCountsFoldersItDidNotName(t *testing.T) {
	root := t.TempDir()
	write := func(dir, name string, size int) {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, name), make([]byte, size), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("Claude_20260805_120000", "a.json", 100)
	write("tidied-20260805", "b.json", 250)
	write("org-cleanup-2026-08-04", "c.json", 50)
	write(filepath.Join(".trash", "20260701-Claude_20260601_120000"), "d.json", 30)

	u := (&BackupManager{BackupRootDir: root}).Usage()
	if u.Snapshots != 1 || u.Bytes != 100 {
		t.Errorf("snapshots=%d bytes=%d, want 1 and 100", u.Snapshots, u.Bytes)
	}
	if u.Staged != 1 || u.StagedBytes != 30 {
		t.Errorf("staged=%d stagedBytes=%d, want 1 and 30", u.Staged, u.StagedBytes)
	}
	if u.Other != 2 || u.OtherBytes != 300 {
		t.Errorf("other=%d otherBytes=%d, want 2 and 300 (the tidied folder and the hand-made one)", u.Other, u.OtherBytes)
	}
}
