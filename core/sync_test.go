package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncSessions(t *testing.T) {
	tempDir := t.TempDir()
	srcProfile := filepath.Join(tempDir, "SrcProfile")
	dstProfile := filepath.Join(tempDir, "DstProfile")

	srcSessions := filepath.Join(srcProfile, "claude-code-sessions", "uuid1")
	if err := os.MkdirAll(srcSessions, 0755); err != nil {
		t.Fatalf("failed to create src sessions dir: %v", err)
	}

	sessionFile := filepath.Join(srcSessions, "local_123.json")
	if err := os.WriteFile(sessionFile, []byte(`{"session": 123}`), 0644); err != nil {
		t.Fatalf("failed to create session file: %v", err)
	}

	report, err := SyncSessions(srcProfile, dstProfile)
	if err != nil {
		t.Fatalf("SyncSessions failed: %v", err)
	}

	if report.CopiedCount != 1 {
		t.Errorf("expected CopiedCount 1, got %d", report.CopiedCount)
	}

	syncedFile := filepath.Join(dstProfile, "claude-code-sessions", "uuid1", "local_123.json")
	if _, err := os.Stat(syncedFile); err != nil {
		t.Errorf("expected synced file at %s", syncedFile)
	}
}
