package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupAndRestore(t *testing.T) {
	tempDir := t.TempDir()
	profileDir := filepath.Join(tempDir, "TestProfile")
	sessionsDir := filepath.Join(profileDir, "claude-code-sessions", "uuid1")

	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatalf("failed to create dummy sessions dir: %v", err)
	}

	testFile := filepath.Join(sessionsDir, "local_test.json")
	if err := os.WriteFile(testFile, []byte(`{"test": true}`), 0644); err != nil {
		t.Fatalf("failed to create dummy session file: %v", err)
	}

	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	backupPath, err := bm.CreateBackup(profileDir)
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	// Verify backup file exists
	backupTestFile := filepath.Join(backupPath, "claude-code-sessions", "uuid1", "local_test.json")
	if _, err := os.Stat(backupTestFile); err != nil {
		t.Errorf("expected backup file at %s, but not found", backupTestFile)
	}

	// Test Restore
	restoreTarget := filepath.Join(tempDir, "RestoredProfile")
	if err := bm.RestoreBackup(backupPath, restoreTarget); err != nil {
		t.Fatalf("RestoreBackup failed: %v", err)
	}

	restoredFile := filepath.Join(restoreTarget, "claude-code-sessions", "uuid1", "local_test.json")
	if _, err := os.Stat(restoredFile); err != nil {
		t.Errorf("expected restored file at %s, but not found", restoredFile)
	}
}

// TestRestoreInvalidBackupPreservesTarget verifies that restoring from an
// invalid backup does not destroy the existing target sessions.
func TestRestoreInvalidBackupPreservesTarget(t *testing.T) {
	tempDir := t.TempDir()

	// Existing target with real data.
	target := filepath.Join(tempDir, "Target")
	targetSessions := filepath.Join(target, "claude-code-sessions", "uuid1")
	if err := os.MkdirAll(targetSessions, 0755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(targetSessions, "local_keep.json")
	if err := os.WriteFile(existing, []byte(`{"keep":true}`), 0644); err != nil {
		t.Fatal(err)
	}

	bm := NewBackupManager(filepath.Join(tempDir, "backups"))

	// Backup path without a claude-code-sessions dir -> invalid.
	badBackup := filepath.Join(tempDir, "not-a-real-backup")
	if err := os.MkdirAll(badBackup, 0755); err != nil {
		t.Fatal(err)
	}

	if err := bm.RestoreBackup(badBackup, target); err == nil {
		t.Fatal("expected RestoreBackup to fail on invalid backup")
	}

	// The pre-existing target data must still be intact.
	if _, err := os.Stat(existing); err != nil {
		t.Errorf("restore from an invalid backup destroyed existing target data: %v", err)
	}
}

// TestRestoreBacksUpCurrentTargetBeforeSwap verifies that a SUCCESSFUL restore
// is itself reversible: before overwriting the target, RestoreBackup snapshots
// the current target into the backup root, so restoring the wrong backup is not
// a one-way loss of the data that was there.
func TestRestoreBacksUpCurrentTargetBeforeSwap(t *testing.T) {
	tempDir := t.TempDir()
	backupRoot := filepath.Join(tempDir, "backups")
	bm := NewBackupManager(backupRoot)

	// A valid backup holding the "new" content we will restore.
	src := filepath.Join(tempDir, "SrcProfile")
	srcSessions := filepath.Join(src, "claude-code-sessions", "uuid1")
	if err := os.MkdirAll(srcSessions, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcSessions, "local_a.json"), []byte(`{"v":"new"}`), 0644); err != nil {
		t.Fatal(err)
	}
	backupPath, err := bm.CreateBackup(src)
	if err != nil {
		t.Fatal(err)
	}

	// Target already holds different, "old" content that must remain recoverable.
	target := filepath.Join(tempDir, "Target")
	targetSessions := filepath.Join(target, "claude-code-sessions", "uuid1")
	if err := os.MkdirAll(targetSessions, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetSessions, "local_a.json"), []byte(`{"v":"old"}`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := bm.RestoreBackup(backupPath, target); err != nil {
		t.Fatalf("RestoreBackup failed: %v", err)
	}

	// Target was overwritten with the restored "new" content.
	got, _ := os.ReadFile(filepath.Join(targetSessions, "local_a.json"))
	if string(got) != `{"v":"new"}` {
		t.Fatalf("restore did not apply: target content = %q", got)
	}

	// A backup of the pre-restore target ("old") must now exist in the backup
	// root — i.e. the restore did not irreversibly discard the previous data.
	backups, err := bm.ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	foundOld := false
	for _, b := range backups {
		if b == backupPath {
			continue // this is the "new" source backup, not the pre-restore one
		}
		data, rerr := os.ReadFile(filepath.Join(b, "claude-code-sessions", "uuid1", "local_a.json"))
		if rerr == nil && string(data) == `{"v":"old"}` {
			foundOld = true
		}
	}
	if !foundOld {
		t.Error("successful restore left no recoverable backup of the pre-restore target data")
	}
}

// TestRestoreStagingFailurePreservesTarget exercises the atomic-restore path:
// with a VALID backup, if staging the restore fails, the existing target must
// be left untouched (the fix stages into a temp dir before swapping).
func TestRestoreStagingFailurePreservesTarget(t *testing.T) {
	tempDir := t.TempDir()

	// A valid backup with real content.
	src := filepath.Join(tempDir, "SrcProfile")
	srcSessions := filepath.Join(src, "claude-code-sessions", "uuid1")
	if err := os.MkdirAll(srcSessions, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcSessions, "local_b.json"), []byte(`{"b":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	backupPath, err := bm.CreateBackup(src)
	if err != nil {
		t.Fatal(err)
	}

	// Target profile with precious data, inside a directory we make read-only
	// so staging the ".restoring" dir fails.
	target := filepath.Join(tempDir, "Target")
	targetSessions := filepath.Join(target, "claude-code-sessions", "uuid1")
	if err := os.MkdirAll(targetSessions, 0755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(targetSessions, "local_keep.json")
	if err := os.WriteFile(keep, []byte(`{"keep":true}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Block writes into the profile dir so staging the ".restoring" copy fails.
	// POSIX mode bits are ignored for access control on Windows, so denyDirWrites
	// uses an OS-appropriate mechanism (chmod on Unix, an icacls deny ACE on
	// Windows) and restores access on cleanup so t.TempDir removal succeeds.
	denyDirWrites(t, target)

	if err := bm.RestoreBackup(backupPath, target); err == nil {
		t.Fatal("expected RestoreBackup to fail when staging cannot be written")
	}

	// Original target data must survive intact.
	got, readErr := os.ReadFile(keep)
	if readErr != nil {
		t.Fatalf("staging failure destroyed target data: %v", readErr)
	}
	if string(got) != `{"keep":true}` {
		t.Errorf("target content changed: %q", got)
	}
}

// TestCopyFileNeverLeavesATruncatedTarget is the regression test for a copy that
// is interrupted partway.
//
// copyFile used to write straight into the destination through os.Create, so a
// process that died mid-copy left a truncated file there. That is worse than a
// failed copy: the truncated file's mtime is the moment it was written, so it is
// NEWER than its source, and sync keeps whichever copy is newer (core/sync.go).
// The next sync would therefore treat the truncated file as the current version of
// that conversation and keep it. An interruption became permanent damage.
//
// Quit clicked during a sync is the reachable version of this — MCS terminates
// itself mid-copy — but a crash or a power cut does the same thing.
func TestCopyFileNeverLeavesATruncatedTarget(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.json")
	dst := filepath.Join(dir, "dst.json")
	if err := os.WriteFile(src, []byte(`{"new":"much longer content"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte(`{"old":1}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Force the final swap to fail, which stands in for the process dying at the
	// last possible moment. Everything before it has already been written.
	orig := copyRename
	copyRename = func(from, to string) error { return os.ErrPermission }
	t.Cleanup(func() { copyRename = orig })

	if err := copyFile(src, dst); err == nil {
		t.Fatal("want the failure reported")
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("the destination must still be there: %v", err)
	}
	if string(got) != `{"old":1}` {
		t.Fatalf("destination = %q, want its original content untouched", got)
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), copyTmpSuffix) {
				t.Fatalf("staging file %q left behind", e.Name())
			}
		}
	}
}

// TestCopyFileStagingNameIsInvisibleToSync guards the choice of staging suffix. A
// process killed mid-copy leaves the staging file on disk, inside a session
// bucket. Sync walks buckets and copies *.json, so a staging file named *.json
// would be synced into the other profile as if it were a conversation.
func TestCopyFileStagingNameIsInvisibleToSync(t *testing.T) {
	if strings.HasSuffix(copyTmpSuffix, ".json") {
		t.Fatalf("copyTmpSuffix = %q, which sync would pick up as a session file", copyTmpSuffix)
	}
}

// TestBackupIfHasDataReusesAnIdenticalSnapshot is the fix for backups growing
// without bound.
//
// Every automatic safety backup used to make a full copy of the profile's session
// tree, and the panel takes one on every Sync click. A profile with ~900 session
// files, clicked a few dozen times, is how one machine reached 1.6 GB across 65
// snapshots that were nearly all identical to each other.
//
// A backup exists to protect the state that is about to be overwritten. When that
// state has not changed since the last snapshot, the last snapshot already protects
// it, and copying it again buys nothing. Nothing is deleted here: the reuse just
// stops a new copy being made.
func TestBackupIfHasDataReusesAnIdenticalSnapshot(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "Claude_Work")
	bucket := filepath.Join(profile, "claude-code-sessions", "uuid")
	if err := os.MkdirAll(bucket, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucket, "local_1.json"), []byte(`{"a":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	bm := NewBackupManager(filepath.Join(dir, "backups"))

	first, err := bm.BackupIfHasData(profile)
	if err != nil || first == "" {
		t.Fatalf("first backup: %q %v", first, err)
	}
	second, err := bm.BackupIfHasData(profile)
	if err != nil {
		t.Fatalf("second backup: %v", err)
	}
	if second != first {
		t.Fatalf("nothing changed, so the existing snapshot should be reused: got %q, want %q", second, first)
	}
	if got, _ := bm.ListBackups(); len(got) != 1 {
		t.Fatalf("want 1 snapshot on disk, got %d: %v", len(got), got)
	}

	// Once the profile really changes, a new snapshot must be taken: the old one
	// no longer protects what is about to be overwritten.
	if err := os.WriteFile(filepath.Join(bucket, "local_2.json"), []byte(`{"b":2}`), 0644); err != nil {
		t.Fatal(err)
	}
	third, err := bm.BackupIfHasData(profile)
	if err != nil {
		t.Fatalf("third backup: %v", err)
	}
	if third == first {
		t.Fatal("the profile changed, so a fresh snapshot is required")
	}
	if got, _ := bm.ListBackups(); len(got) != 2 {
		t.Fatalf("want 2 snapshots, got %d: %v", len(got), got)
	}
}

// TestCreateBackupAlwaysCopies: reuse belongs to the automatic safety net only. An
// explicit "back up now" from the user or the CLI must always produce a snapshot,
// because the user asked for one.
func TestCreateBackupAlwaysCopies(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "Claude_Work")
	bucket := filepath.Join(profile, "claude-code-sessions", "uuid")
	if err := os.MkdirAll(bucket, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucket, "local_1.json"), []byte(`{"a":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	bm := NewBackupManager(filepath.Join(dir, "backups"))

	first, err := bm.CreateBackup(profile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := bm.CreateBackup(profile)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatalf("both backups landed on %q", first)
	}
	if got, _ := bm.ListBackups(); len(got) != 2 {
		t.Fatalf("an explicit backup always copies, got %d: %v", len(got), got)
	}
}

// TestCreateBackupInTheSameSecondDoesNotMergeIntoTheLastOne guards a snapshot's
// most basic promise: that it is a copy of one moment.
//
// The directory name is timestamped to the second, and copyDir merges into an
// existing directory rather than replacing it. So two backups of one profile inside
// the same second used to land on the same path and blend together, leaving files
// that had been deleted in between still present alongside the newer ones. Two
// syncs a second apart is an ordinary thing to do, and the result was a backup that
// matched no state the profile was ever in.
func TestCreateBackupInTheSameSecondDoesNotMergeIntoTheLastOne(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "Claude_Work")
	bucket := filepath.Join(profile, "claude-code-sessions", "uuid")
	if err := os.MkdirAll(bucket, 0755); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(bucket, "deleted_later.json")
	if err := os.WriteFile(gone, []byte(`{"a":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	bm := NewBackupManager(filepath.Join(dir, "backups"))

	if _, err := bm.CreateBackup(profile); err != nil {
		t.Fatal(err)
	}
	// The user deletes a conversation, then something triggers a second backup in
	// the same second.
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	second, err := bm.CreateBackup(profile)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(second, "claude-code-sessions", "uuid", "deleted_later.json")); !os.IsNotExist(err) {
		t.Fatalf("the second snapshot inherited a file that no longer exists, stat err = %v", err)
	}
}

// TestCopyFilePreservesTheSourceMode guards against a copy widening permissions.
//
// Claude Desktop writes session files 0600 — only the user can read them. Staging
// through a fresh os.Create gives the new file the process default (0644 under a
// typical umask), and the rename carries that onto the destination, so every synced
// conversation would come out group- and world-readable. A sync must not quietly
// relax the permissions on the user's chat history.
func TestCopyFilePreservesTheSourceMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.json")
	dst := filepath.Join(dir, "dst.json")
	if err := os.WriteFile(src, []byte(`{"a":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(src, 0600); err != nil { // WriteFile is subject to umask
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0600 {
		t.Fatalf("copied mode = %04o, want 0600 from the source", got)
	}
}

// TestBackupFingerprintSeesNonJSONChanges: the session tree is not all *.json.
// Claude keeps extensionless "deleted_<uuid>" markers next to the conversations —
// that is how it records a deletion — and .DS_Store appears on macOS. A fingerprint
// that only looked at *.json would call a tree with a new deletion marker unchanged,
// so the automatic backup would reuse a snapshot taken before the deletion and the
// state actually about to be overwritten would never be captured.
func TestBackupFingerprintSeesNonJSONChanges(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "Claude_Work")
	bucket := filepath.Join(profile, "claude-code-sessions", "uuid")
	if err := os.MkdirAll(bucket, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucket, "local_1.json"), []byte(`{"a":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	bm := NewBackupManager(filepath.Join(dir, "backups"))
	first, err := bm.BackupIfHasData(profile)
	if err != nil {
		t.Fatal(err)
	}

	// Claude marks a conversation deleted. No .json file changed.
	if err := os.WriteFile(filepath.Join(bucket, "deleted_7c041d98"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	second, err := bm.BackupIfHasData(profile)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("a new deletion marker changes the tree, so the old snapshot no longer protects it")
	}
}

// TestNewestBackupForOrdersCountersNumerically: collision counters have to sort in
// the order they were created. As plain strings "-10" sorts before "-2", so the
// newest snapshot of a busy second would not be found and reuse would silently stop
// working.
func TestNewestBackupForOrdersCountersNumerically(t *testing.T) {
	root := t.TempDir()
	bm := NewBackupManager(root)
	names := []string{
		"Claude_Work_20260730_120000",
		"Claude_Work_20260730_120000-2",
		"Claude_Work_20260730_120000-10",
	}
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(root, n), 0755); err != nil {
			t.Fatal(err)
		}
	}
	got := filepath.Base(bm.newestBackupFor("Claude_Work"))
	if got != "Claude_Work_20260730_120000-10" {
		t.Fatalf("newest = %q, want the -10 snapshot", got)
	}
}

// TestNewestBackupForDoesNotMatchAnotherProfilesSnapshots is the important one:
// "Claude" and "Claude_Work" is MCS's own default naming, so this hits almost
// every user.
//
// Matching on the prefix "Claude_" also matches "Claude_Work_20260730_120000",
// and comparing what follows as a timestamp puts "Work_…" above any real
// timestamp because "W" > "2". So the newest snapshot of "Claude" was reported as
// one belonging to "Claude_Work", its fingerprint never matched, and a full copy
// was taken every single time — reinstating exactly the unbounded growth the reuse
// was added to stop, silently.
func TestNewestBackupForDoesNotMatchAnotherProfilesSnapshots(t *testing.T) {
	root := t.TempDir()
	bm := NewBackupManager(root)
	for _, n := range []string{
		"Claude_20260730_120000",
		"Claude_Work_20260730_130000", // a different profile, later in the day
	} {
		if err := os.MkdirAll(filepath.Join(root, n), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if got := filepath.Base(bm.newestBackupFor("Claude")); got != "Claude_20260730_120000" {
		t.Fatalf("newest for Claude = %q, want its own snapshot", got)
	}
	if got := filepath.Base(bm.newestBackupFor("Claude_Work")); got != "Claude_Work_20260730_130000" {
		t.Fatalf("newest for Claude_Work = %q", got)
	}
}

// TestNewestBackupForHandlesDashesInProfileNames: profile names are user-chosen and
// may contain dashes, so "work-2" is a legal name. Taking the last dash as a
// same-second counter would read the profile name as part of the counter.
func TestNewestBackupForHandlesDashesInProfileNames(t *testing.T) {
	root := t.TempDir()
	bm := NewBackupManager(root)
	for _, n := range []string{
		"Claude_work-2_20260730_120000",
		"Claude_work-2_20260730_120000-2",
	} {
		if err := os.MkdirAll(filepath.Join(root, n), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if got := filepath.Base(bm.newestBackupFor("Claude_work-2")); got != "Claude_work-2_20260730_120000-2" {
		t.Fatalf("newest = %q, want the -2 snapshot", got)
	}
}

// TestBackupReuseSurvivesFinderDroppingDSStoreInTheSnapshot: opening the backups
// folder on macOS makes Finder write .DS_Store into the directories it shows. That
// is never Claude's data, but it does change the snapshot's tree, so counting it
// would make the snapshot stop matching the profile and reuse would never fire
// again for that profile.
func TestBackupReuseSurvivesFinderDroppingDSStoreInTheSnapshot(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "Claude_Work")
	bucket := filepath.Join(profile, "claude-code-sessions", "uuid")
	if err := os.MkdirAll(bucket, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucket, "local_1.json"), []byte(`{"a":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	bm := NewBackupManager(filepath.Join(dir, "backups"))
	first, err := bm.BackupIfHasData(profile)
	if err != nil {
		t.Fatal(err)
	}
	// The user browses the backups folder.
	if err := os.WriteFile(filepath.Join(first, "claude-code-sessions", ".DS_Store"), []byte("junk"), 0644); err != nil {
		t.Fatal(err)
	}
	second, err := bm.BackupIfHasData(profile)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatal("Finder's metadata is not a change to the conversations; reuse must still apply")
	}
}
