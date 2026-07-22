package core

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeAccountConfig writes a minimal config.json giving the profile a
// lastKnownAccountUuid, which SyncSessions reads to know the source/target
// account buckets.
func writeAccountConfig(t *testing.T, profile, accountUUID string) {
	t.Helper()
	if err := os.MkdirAll(profile, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"lastKnownAccountUuid":%q}`, accountUUID)
	if err := os.WriteFile(filepath.Join(profile, "config.json"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSyncSessions(t *testing.T) {
	tempDir := t.TempDir()
	srcProfile := filepath.Join(tempDir, "SrcProfile")
	dstProfile := filepath.Join(tempDir, "DstProfile")
	writeAccountConfig(t, srcProfile, "uuid1")
	writeAccountConfig(t, dstProfile, "uuid1")

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

// TestSyncErrorsWhenNotLoggedIn verifies sync fails clearly (rather than
// silently doing the wrong thing) when a profile has no account UUID.
func TestSyncErrorsWhenNotLoggedIn(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "uuid1")
	// dst has no config.json.
	writeSessionFile(t, src, filepath.Join("uuid1", "local_a.json"), `{"v":1}`, time.Now())

	if _, err := SyncSessions(src, dst); err == nil {
		t.Fatal("expected SyncSessions to error when the target is not logged in")
	}
}

// TestSyncNoOpWhenSourceBucketMissing verifies that when the source account has
// no local sessions, sync is a clean no-op: no error, nothing copied, and no
// empty bucket created in the target.
func TestSyncNoOpWhenSourceBucketMissing(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "src-uuid") // logged in, but no sessions under src-uuid
	writeAccountConfig(t, dst, "dst-uuid")

	report, err := SyncSessions(src, dst)
	if err != nil {
		t.Fatalf("expected no-op, got error: %v", err)
	}
	if report.CopiedCount != 0 || report.ConflictCount != 0 || report.SkippedCount != 0 {
		t.Errorf("expected empty report, got %+v", report)
	}
	if _, err := os.Stat(filepath.Join(platformSessions(dst), "dst-uuid")); err == nil {
		t.Error("no-op sync should not have created an empty target bucket")
	}
}

// TestSyncRebucketsIntoTargetAccount is the core cross-account guarantee: when
// source and target are logged into DIFFERENT accounts, the source's sessions
// must be re-homed under the TARGET account's bucket (where the app will read
// them), NOT copied under the source account's name, and foreign/orphaned
// buckets in the source must not be dragged along.
func TestSyncRebucketsIntoTargetAccount(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "CompanyProfile")
	dst := filepath.Join(tempDir, "PersonalProfile")
	writeAccountConfig(t, src, "company-uuid")
	writeAccountConfig(t, dst, "personal-uuid")

	// A real conversation under the source's OWN account bucket.
	writeSessionFile(t, src, filepath.Join("company-uuid", "ws1", "local_a.json"), `{"v":"work"}`, time.Now())
	// A stray foreign bucket in the source that must NOT be propagated.
	writeSessionFile(t, src, filepath.Join("stray-uuid", "ws1", "local_b.json"), `{"v":"stray"}`, time.Now())

	report, err := SyncSessions(src, dst)
	if err != nil {
		t.Fatalf("SyncSessions failed: %v", err)
	}

	// The conversation must appear under the TARGET account's bucket.
	want := filepath.Join(platformSessions(dst), "personal-uuid", "ws1", "local_a.json")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("session was not re-bucketed into target account bucket (%s): %v", want, err)
	}
	// It must NOT be copied under the source account name in the target.
	notWant := filepath.Join(platformSessions(dst), "company-uuid", "ws1", "local_a.json")
	if _, err := os.Stat(notWant); err == nil {
		t.Error("session was copied under the SOURCE account bucket name (re-bucketing failed)")
	}
	// The stray foreign bucket must not have been propagated at all (this is the
	// exact pollution that filled a personal profile with an unreadable company bucket).
	if _, err := os.Stat(filepath.Join(platformSessions(dst), "stray-uuid")); err == nil {
		t.Error("a foreign (non-account) bucket was propagated to the target")
	}
	if report.CopiedCount != 1 {
		t.Errorf("expected exactly 1 copied (only the account bucket), got %d", report.CopiedCount)
	}
	if report.SourceAccount != "company-uuid" || report.TargetAccount != "personal-uuid" {
		t.Errorf("report accounts wrong: src=%q dst=%q", report.SourceAccount, report.TargetAccount)
	}
}

// TestSyncConflictDoesNotOverwriteNewerTarget verifies that when the target
// already holds a DIFFERENT and NEWER version of a session, sync refuses to
// overwrite it and records a conflict instead of silently destroying data.
func TestSyncConflictDoesNotOverwriteNewerTarget(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "uuid1")
	writeAccountConfig(t, dst, "uuid1")

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
	writeAccountConfig(t, src, "uuid1")
	writeAccountConfig(t, dst, "uuid1")

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
	writeAccountConfig(t, src, "uuid1")
	writeAccountConfig(t, dst, "uuid1")

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
