package core

import (
	"os"
	"path/filepath"
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
