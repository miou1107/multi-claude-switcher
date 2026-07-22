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
