package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

// writeSessionFile writes content at a bucket-relative path with a given mtime.
func writeSessionFile(t *testing.T, profile, rel, content string, mtime time.Time) string {
	t.Helper()
	p := filepath.Join(platformSessions(profile), rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return p
}

func platformSessions(profile string) string {
	return filepath.Join(profile, "claude-code-sessions")
}

// TestSyncConflictDoesNotOverwriteNewerTarget verifies that when the target
// already holds a DIFFERENT and NEWER version of a session, sync refuses to
// overwrite it and records a conflict instead of silently destroying data.
func TestSyncConflictDoesNotOverwriteNewerTarget(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")

	old := time.Now().Add(-1 * time.Hour)
	newer := time.Now()

	rel := filepath.Join("uuid1", "org1", "local_x.json")
	writeSessionFile(t, src, rel, `{"v":"source-old"}`, old)
	dstPath := writeSessionFile(t, dst, rel, `{"v":"target-new-precious"}`, newer)

	report, err := SyncSessions(src, dst)
	if err != nil {
		t.Fatalf("SyncSessions failed: %v", err)
	}
	if report.ConflictCount != 1 {
		t.Errorf("expected 1 conflict, got %d (copied=%d skipped=%d)", report.ConflictCount, report.CopiedCount, report.SkippedCount)
	}
	got, _ := os.ReadFile(dstPath)
	if string(got) != `{"v":"target-new-precious"}` {
		t.Errorf("target was overwritten on conflict: %q", got)
	}
}

// TestSyncOverwritesWhenSourceNewer verifies a genuinely newer source version
// still updates the target.
func TestSyncOverwritesWhenSourceNewer(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")

	old := time.Now().Add(-1 * time.Hour)
	newer := time.Now()

	rel := filepath.Join("uuid1", "org1", "local_y.json")
	writeSessionFile(t, src, rel, `{"v":"source-new"}`, newer)
	dstPath := writeSessionFile(t, dst, rel, `{"v":"target-old"}`, old)

	report, err := SyncSessions(src, dst)
	if err != nil {
		t.Fatalf("SyncSessions failed: %v", err)
	}
	if report.CopiedCount != 1 {
		t.Errorf("expected 1 copied, got %d", report.CopiedCount)
	}
	got, _ := os.ReadFile(dstPath)
	if string(got) != `{"v":"source-new"}` {
		t.Errorf("expected target updated to source-new, got %q", got)
	}
}

// TestSyncSkipsIdenticalContent verifies identical files are neither copied nor
// flagged as conflicts.
func TestSyncSkipsIdenticalContent(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")

	rel := filepath.Join("uuid1", "org1", "local_z.json")
	writeSessionFile(t, src, rel, `{"v":"same"}`, time.Now())
	writeSessionFile(t, dst, rel, `{"v":"same"}`, time.Now().Add(-1*time.Hour))

	report, err := SyncSessions(src, dst)
	if err != nil {
		t.Fatalf("SyncSessions failed: %v", err)
	}
	if report.CopiedCount != 0 || report.ConflictCount != 0 {
		t.Errorf("identical content should skip: copied=%d conflict=%d skipped=%d", report.CopiedCount, report.ConflictCount, report.SkippedCount)
	}
	if report.SkippedCount != 1 {
		t.Errorf("expected 1 skipped, got %d", report.SkippedCount)
	}
}
