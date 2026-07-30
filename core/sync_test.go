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

// TestSyncNeverOverwritesDifferingContent is the regression test for the
// newer-wins rule, which was removed because on real data it preferred the
// damaged copy.
//
// Measured on a user's machine, 2026-07-30, for one account held by two
// profiles: 26 files differed, and in 16 of them the file with the NEWER
// mtime carried "transcriptUnavailable" and had lost its "cliSessionId",
// while the older file was intact. Claude Desktop rewrites a session record
// when it can no longer find the transcript behind it, and that rewrite moves
// the mtime forward. So a newer mtime does not mean "more recent edit", it
// means "degraded more recently", and preferring it destroyed the only good
// copy.
//
// Sync is therefore purely additive: it copies files the target does not have
// and never replaces one it does. A file present in both with differing
// content is always reported as a conflict, whichever side is newer.
func TestSyncNeverOverwritesDifferingContent(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "uuid1")
	writeAccountConfig(t, dst, "uuid1")

	old := time.Now().Add(-1 * time.Hour)
	newer := time.Now()

	// The shape that made this a bug: the newer copy is the degraded one.
	degradedButNewer := `{"v":"y","transcriptUnavailable":true}`
	intactButOlder := `{"v":"y","cliSessionId":"abc"}`

	rel := filepath.Join("uuid1", "org1", "local_y.json")
	writeSessionFile(t, src, rel, degradedButNewer, newer)
	dstPath := writeSessionFile(t, dst, rel, intactButOlder, old)

	report, err := SyncSessions(src, dst)
	if err != nil {
		t.Fatalf("SyncSessions failed: %v", err)
	}
	if report.CopiedCount != 0 {
		t.Errorf("a differing file must never be copied over, got CopiedCount=%d", report.CopiedCount)
	}
	if report.ConflictCount != 1 {
		t.Errorf("expected 1 conflict, got %d (copied=%d skipped=%d)",
			report.ConflictCount, report.CopiedCount, report.SkippedCount)
	}
	got, _ := os.ReadFile(dstPath)
	if string(got) != intactButOlder {
		t.Errorf("the target's intact copy was destroyed by a newer degraded source: %q", got)
	}
}

// TestSyncDoesNotWriteThroughADanglingSymlink pins the hole that made the
// additive rule conditional. The existence check used os.Stat and treated every
// failure as "the target does not have this file". os.Stat follows symlinks, so
// a dangling one failed, and copyFile then wrote through it with os.Create,
// landing the data at the link's target — outside the sessions bucket entirely.
// The same branch would truncate a real file whenever stat failed for a mundane
// reason such as a permission error.
func TestSyncDoesNotWriteThroughADanglingSymlink(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "uuid1")
	writeAccountConfig(t, dst, "uuid1")

	rel := filepath.Join("uuid1", "local_z.json")
	writeSessionFile(t, src, rel, `{"v":"source"}`, time.Now())

	// A dangling symlink where the target's copy would live, pointing outside
	// the profile.
	escapee := filepath.Join(tempDir, "escaped.json")
	link := filepath.Join(platformSessions(dst), rel)
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(escapee, link); err != nil {
		t.Skipf("symlinks unavailable on this filesystem: %v", err)
	}

	report, err := SyncSessions(src, dst)

	// Erroring out is fine; writing outside the bucket is not.
	if _, statErr := os.Stat(escapee); statErr == nil {
		t.Fatalf("sync wrote through the symlink to %s, escaping the sessions directory", escapee)
	}
	if err == nil && report.CopiedCount != 0 {
		t.Errorf("a path the target already occupies must not be counted as copied, got %d", report.CopiedCount)
	}
}

// TestSyncBidirectionalReportsConflicts pins that both legs' reports come back.
// Auto Sync on switch runs unattended, so a clash that is not reported here can
// never be reported at all.
func TestSyncBidirectionalReportsConflicts(t *testing.T) {
	tempDir := t.TempDir()
	a := filepath.Join(tempDir, "A")
	b := filepath.Join(tempDir, "B")
	writeAccountConfig(t, a, "uuid1")
	writeAccountConfig(t, b, "uuid1")

	// Same relative path, different contents: neither side may overwrite the
	// other, so both legs report the clash.
	rel := filepath.Join("uuid1", "local_clash.json")
	writeSessionFile(t, a, rel, `{"v":"A"}`, time.Now())
	writeSessionFile(t, b, rel, `{"v":"B"}`, time.Now().Add(-time.Hour))

	aToB, bToA, err := SyncBidirectional(a, b)
	if err != nil {
		t.Fatalf("SyncBidirectional failed: %v", err)
	}
	if aToB.ConflictCount != 1 || bToA.ConflictCount != 1 {
		t.Fatalf("both legs must report the clash, got aToB=%d bToA=%d", aToB.ConflictCount, bToA.ConflictCount)
	}
	// And neither side was changed.
	gotA, _ := os.ReadFile(filepath.Join(platformSessions(a), rel))
	gotB, _ := os.ReadFile(filepath.Join(platformSessions(b), rel))
	if string(gotA) != `{"v":"A"}` || string(gotB) != `{"v":"B"}` {
		t.Errorf("a clash must leave both copies alone, got A=%q B=%q", gotA, gotB)
	}
}

// TestSyncStillCopiesFilesTheTargetLacks pins the behaviour the additive rule
// must keep: bringing across conversations the target does not have at all is
// the entire point of sync, and removing newer-wins must not weaken it.
func TestSyncStillCopiesFilesTheTargetLacks(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "uuid1")
	writeAccountConfig(t, dst, "uuid1")

	rel := filepath.Join("uuid1", "org1", "local_only_in_source.json")
	writeSessionFile(t, src, rel, `{"v":"new conversation"}`, time.Now())

	report, err := SyncSessions(src, dst)
	if err != nil {
		t.Fatalf("SyncSessions failed: %v", err)
	}
	if report.CopiedCount != 1 {
		t.Fatalf("expected the missing file to be copied, got CopiedCount=%d", report.CopiedCount)
	}
	got, err := os.ReadFile(filepath.Join(platformSessions(dst), "uuid1", "org1", "local_only_in_source.json"))
	if err != nil {
		t.Fatalf("file was not copied into the target bucket: %v", err)
	}
	if string(got) != `{"v":"new conversation"}` {
		t.Errorf("contents = %q", got)
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

func TestSyncBidirectionalUnion(t *testing.T) {
	tempDir := t.TempDir()
	a := filepath.Join(tempDir, "A")
	b := filepath.Join(tempDir, "B")
	writeAccountConfig(t, a, "a-uuid")
	writeAccountConfig(t, b, "b-uuid")
	// Each account has one distinct session under its OWN account bucket.
	writeSessionFile(t, a, filepath.Join("a-uuid", "local_a.json"), `{"v":"A"}`, time.Now())
	writeSessionFile(t, b, filepath.Join("b-uuid", "local_b.json"), `{"v":"B"}`, time.Now())

	aToB, bToA, err := SyncBidirectional(a, b)
	if err != nil {
		t.Fatalf("SyncBidirectional failed: %v", err)
	}
	// Both legs' reports must come back, so the caller can tell the user which
	// sessions did not converge. Nothing clashes here, so both are clean.
	if aToB == nil || bToA == nil {
		t.Fatalf("both reports must be returned, got aToB=%v bToA=%v", aToB, bToA)
	}
	if aToB.ConflictCount != 0 || bToA.ConflictCount != 0 {
		t.Errorf("unexpected conflicts: aToB=%d bToA=%d", aToB.ConflictCount, bToA.ConflictCount)
	}

	// After union, BOTH accounts hold BOTH sessions, each under its own bucket.
	for _, want := range []string{
		filepath.Join(platformSessions(a), "a-uuid", "local_a.json"),
		filepath.Join(platformSessions(a), "a-uuid", "local_b.json"),
		filepath.Join(platformSessions(b), "b-uuid", "local_a.json"),
		filepath.Join(platformSessions(b), "b-uuid", "local_b.json"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected %s after bidirectional union: %v", want, err)
		}
	}
}
