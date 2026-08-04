package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
//
// This rule was briefly removed on the theory that a newer mtime meant a more
// degraded record, from reading 26 differing files in which the newer copy
// carried "transcriptUnavailable". That reading was wrong twice over. The files
// were in an orphan bucket sync never reads (only each profile's own
// lastKnownAccountUuid bucket is synced), and the flag is not damage: Claude Code
// reclaims old transcripts on a retention policy, so the flag is Claude Desktop
// honestly recording that the body behind a record is gone.
//
// On the buckets sync actually compares, 13 files differed, and every one where
// completedTurns differed had the higher count on the newer-mtime side. Under
// this rule the two profiles converge completely; under "never overwrite" all 13
// became permanent conflicts that no amount of syncing could clear.
func TestSyncOverwritesWhenSourceNewer(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "uuid1")
	writeAccountConfig(t, dst, "uuid1")

	old := time.Now().Add(-1 * time.Hour)
	newer := time.Now()

	// The shape seen in the real data: the newer side has more of the
	// conversation behind it.
	rel := filepath.Join("uuid1", "org1", "local_y.json")
	writeSessionFile(t, src, rel, `{"v":"y","completedTurns":315}`, newer)
	dstPath := writeSessionFile(t, dst, rel, `{"v":"y","completedTurns":277}`, old)

	report, err := SyncSessions(src, dst)
	if err != nil {
		t.Fatalf("SyncSessions failed: %v", err)
	}
	if report.CopiedCount != 1 {
		t.Errorf("expected 1 copied, got %d", report.CopiedCount)
	}
	got, _ := os.ReadFile(dstPath)
	if string(got) != `{"v":"y","completedTurns":315}` {
		t.Errorf("expected the target updated to the newer version, got %q", got)
	}
}

// TestSyncDanglingSymlinkNeitherEscapesNorAbortsTheRun pins two things at once.
//
// The existence check used os.Stat and treated every failure as "the target does
// not have this file". os.Stat follows symlinks, so a dangling one failed, and
// copyFile then wrote through it with os.Create, landing the data at the link's
// target, outside the sessions bucket. The first fix for that used Lstat but
// returned an error from the walk, which was worse in a different way: one junk
// entry aborted the entire run, so the hundreds of healthy conversations beside
// it could never sync and the caller got a nil report.
func TestSyncDanglingSymlinkNeitherEscapesNorAbortsTheRun(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "uuid1")
	writeAccountConfig(t, dst, "uuid1")

	// One poisoned path, plus healthy files either side of it alphabetically so
	// the walk has to get past the bad one to reach them.
	writeSessionFile(t, src, filepath.Join("uuid1", "local_a.json"), `{"v":"a"}`, time.Now())
	writeSessionFile(t, src, filepath.Join("uuid1", "local_z.json"), `{"v":"z"}`, time.Now())
	poisoned := filepath.Join("uuid1", "local_m.json")
	writeSessionFile(t, src, poisoned, `{"v":"m"}`, time.Now())

	escapee := filepath.Join(tempDir, "escaped.json")
	link := filepath.Join(platformSessions(dst), poisoned)
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(escapee, link); err != nil {
		t.Skipf("symlinks unavailable on this filesystem: %v", err)
	}

	report, err := SyncSessions(src, dst)
	if err != nil {
		t.Fatalf("one bad entry must not fail the whole run: %v", err)
	}
	if report == nil {
		t.Fatal("report must be returned so the caller can say what did happen")
	}
	if _, statErr := os.Stat(escapee); statErr == nil {
		t.Fatalf("sync wrote through the symlink to %s, escaping the sessions directory", escapee)
	}
	// The healthy files either side must have made it.
	if report.CopiedCount != 2 {
		t.Errorf("want the 2 healthy files copied, got CopiedCount=%d", report.CopiedCount)
	}
	for _, name := range []string{"local_a.json", "local_z.json"} {
		if _, err := os.Stat(filepath.Join(platformSessions(dst), "uuid1", name)); err != nil {
			t.Errorf("%s did not make it across: %v", name, err)
		}
	}
	// And the bad one is reported rather than silently dropped.
	if len(report.SkipErrors) != 1 {
		t.Fatalf("want 1 skip error, got %v", report.SkipErrors)
	}
	if !strings.Contains(report.SkipErrors[0], "local_m.json") {
		t.Errorf("skip error must name the file: %q", report.SkipErrors[0])
	}
}

// TestSyncBidirectionalConvergesAndReportsNothingUnresolved covers the ordinary
// case: one side is newer, so the two-way sync makes both agree. The first leg
// may report a clash on its way there, and reporting that as the outcome would
// warn about a problem that no longer exists by the time the call returns.
func TestSyncBidirectionalConvergesAndReportsNothingUnresolved(t *testing.T) {
	tempDir := t.TempDir()
	a := filepath.Join(tempDir, "A")
	b := filepath.Join(tempDir, "B")
	writeAccountConfig(t, a, "uuid1")
	writeAccountConfig(t, b, "uuid1")

	// B is the newer side, which is the order that makes leg one report a clash.
	rel := filepath.Join("uuid1", "local_x.json")
	writeSessionFile(t, a, rel, `{"completedTurns":277}`, time.Now().Add(-time.Hour))
	writeSessionFile(t, b, rel, `{"completedTurns":315}`, time.Now())

	aToB, bToA, err := SyncBidirectional(a, b)
	if err != nil {
		t.Fatalf("SyncBidirectional failed: %v", err)
	}
	if got := UnresolvedConflicts(aToB, bToA); len(got) != 0 {
		t.Errorf("the two sides converged, so nothing is unresolved; got %v", got)
	}
	// Both ends now hold the newer version.
	for _, p := range []string{a, b} {
		got, err := os.ReadFile(filepath.Join(platformSessions(p), rel))
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if string(got) != `{"completedTurns":315}` {
			t.Errorf("%s did not converge on the newer version, got %q", p, got)
		}
	}
}

// TestSyncBidirectionalReportsATrulyUnresolvedClash covers the one shape that
// survives both legs: equal mtimes, differing content. Neither side can claim to
// be newer, so nothing is overwritten and the caller has to be told.
func TestSyncBidirectionalReportsATrulyUnresolvedClash(t *testing.T) {
	tempDir := t.TempDir()
	a := filepath.Join(tempDir, "A")
	b := filepath.Join(tempDir, "B")
	writeAccountConfig(t, a, "uuid1")
	writeAccountConfig(t, b, "uuid1")

	same := time.Now().Truncate(time.Second)
	rel := filepath.Join("uuid1", "local_clash.json")
	writeSessionFile(t, a, rel, `{"v":"A"}`, same)
	writeSessionFile(t, b, rel, `{"v":"B"}`, same)

	aToB, bToA, err := SyncBidirectional(a, b)
	if err != nil {
		t.Fatalf("SyncBidirectional failed: %v", err)
	}
	got := UnresolvedConflicts(aToB, bToA)
	if len(got) != 1 || !strings.Contains(got[0], "local_clash.json") {
		t.Fatalf("want the clash reported as unresolved, got %v", got)
	}
	// And neither side was touched.
	gotA, _ := os.ReadFile(filepath.Join(platformSessions(a), rel))
	gotB, _ := os.ReadFile(filepath.Join(platformSessions(b), rel))
	if string(gotA) != `{"v":"A"}` || string(gotB) != `{"v":"B"}` {
		t.Errorf("an unresolvable clash must leave both copies alone, got A=%q B=%q", gotA, gotB)
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

// writeAccountConfigWithOrg writes a config.json naming both the account and the
// organization the profile is working in, which is the pair that decides where
// Claude Desktop reads conversations from.
func writeAccountConfigWithOrg(t *testing.T, profile, accountUUID, orgUUID string) {
	t.Helper()
	if err := os.MkdirAll(profile, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"lastKnownAccountUuid":%q,"dxt:allowlistLastUpdated:%s":"2026-08-04T01:14:05.939Z"}`,
		accountUUID, orgUUID)
	if err := os.WriteFile(filepath.Join(profile, "config.json"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestSyncSessionsRebucketsTheOrganisationToo is the whole reason importing into a
// Team account looked impossible. Sync rewrote the account segment of the path and
// left the organization segment naming the SOURCE's organization, so every
// conversation it copied landed in a folder the target account never opens: on
// disk, correct, and invisible. The conversations have to arrive in the
// organization the target is actually signed in to.
func TestSyncSessionsRebucketsTheOrganisationToo(t *testing.T) {
	tempDir := t.TempDir()
	const (
		srcAccount = "src-account"
		dstAccount = "dst-account"
		srcOrg     = "personal-org"
		dstOrg     = "company-org"
	)

	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfigWithOrg(t, src, srcAccount, srcOrg)
	writeAccountConfigWithOrg(t, dst, dstAccount, dstOrg)
	writeSessionFile(t, src, filepath.Join(srcAccount, srcOrg, "local_a.json"), `{"v":"work"}`, time.Now())

	report, err := SyncSessions(src, dst)
	if err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	if report.CopiedCount != 1 {
		t.Fatalf("copied %d, want 1 (skips %d, conflicts %d, errors %v)",
			report.CopiedCount, report.SkippedCount, report.ConflictCount, report.SkipErrors)
	}

	want := filepath.Join(platformSessions(dst), dstAccount, dstOrg, "local_a.json")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("conversation is not where the target reads them (%s): %v", want, err)
	}
	// And specifically NOT under the source's organization, which is where it used
	// to land and be ignored forever.
	stale := filepath.Join(platformSessions(dst), dstAccount, srcOrg, "local_a.json")
	if _, err := os.Stat(stale); err == nil {
		t.Errorf("conversation was also filed under the source's organization (%s), where the target never looks", stale)
	}
}

// TestSyncSessionsKeepsThePathWhenTheOrganisationIsUnknown: a profile MCS cannot
// read an organization out of must still sync. Falling back to the old
// path-preserving copy is no worse than what shipped, and refusing would break
// syncing for anyone whose config.json does not carry the stamp.
func TestSyncSessionsKeepsThePathWhenTheOrganisationIsUnknown(t *testing.T) {
	tempDir := t.TempDir()
	const (
		srcAccount = "src-account"
		dstAccount = "dst-account"
		srcOrg     = "personal-org"
	)

	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, srcAccount) // no organization stamp on either side
	writeAccountConfig(t, dst, dstAccount)
	writeSessionFile(t, src, filepath.Join(srcAccount, srcOrg, "local_a.json"), `{"v":"work"}`, time.Now())

	if _, err := SyncSessions(src, dst); err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	want := filepath.Join(platformSessions(dst), dstAccount, srcOrg, "local_a.json")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected the unchanged path-preserving copy at %s: %v", want, err)
	}
}

// TestUnresolvedConflictsMatchAcrossOrganisations: the two legs of a bidirectional
// sync write into opposite accounts, so the same conversation is named by a
// different organization folder in each leg's report. Matching the reports as raw
// paths therefore found nothing in common, and the one warning auto sync has —
// "both copies were kept, go and look" — was never printed. Auto sync runs
// unattended; the log is the only channel it has.
func TestUnresolvedConflictsMatchAcrossOrganisations(t *testing.T) {
	aToB := &SyncReport{Conflicts: []string{filepath.Join("b-org", "local_x.json")}}
	bToA := &SyncReport{Conflicts: []string{filepath.Join("a-org", "local_x.json")}}

	got := UnresolvedConflicts(aToB, bToA)
	if len(got) != 1 {
		t.Fatalf("got %q, want the one conversation both legs could not converge", got)
	}
}

// TestUnresolvedConflictsIgnoresUnrelatedConversations guards the other direction:
// matching on something too loose would report every conflict as unresolved.
func TestUnresolvedConflictsIgnoresUnrelatedConversations(t *testing.T) {
	aToB := &SyncReport{Conflicts: []string{filepath.Join("b-org", "local_x.json")}}
	bToA := &SyncReport{Conflicts: []string{filepath.Join("a-org", "local_y.json")}}

	if got := UnresolvedConflicts(aToB, bToA); len(got) != 0 {
		t.Fatalf("got %q, want nothing: each side resolved the other's clash", got)
	}
}

// TestSyncSessionsSkipsWhatTheOldBugLeftBehind. Every install that ran the old
// sync has the target's conversations sitting in the SOURCE profile under the
// target's own organization folder, because that is where the broken re-bucketing
// put them. Copying those back would push stale versions — including conversations
// the target has since deleted — into the folder the target actually reads.
func TestSyncSessionsSkipsWhatTheOldBugLeftBehind(t *testing.T) {
	tempDir := t.TempDir()
	const (
		srcAccount = "src-account"
		dstAccount = "dst-account"
		srcOrg     = "personal-org"
		dstOrg     = "company-org"
	)

	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfigWithOrg(t, src, srcAccount, srcOrg)
	writeAccountConfigWithOrg(t, dst, dstAccount, dstOrg)
	writeSessionFile(t, src, filepath.Join(srcAccount, srcOrg, "local_mine.json"), `{"v":"mine"}`, time.Now())
	// Left in the source by the old sync: a copy of the TARGET's own conversation,
	// filed under the target's organization.
	writeSessionFile(t, src, filepath.Join(srcAccount, dstOrg, "local_stale.json"), `{"v":"stale"}`, time.Now())

	if _, err := SyncSessions(src, dst); err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	mine := filepath.Join(platformSessions(dst), dstAccount, dstOrg, "local_mine.json")
	if _, err := os.Stat(mine); err != nil {
		t.Errorf("the source's own conversation must arrive: %v", err)
	}
	stale := filepath.Join(platformSessions(dst), dstAccount, dstOrg, "local_stale.json")
	if _, err := os.Stat(stale); err == nil {
		t.Error("a leftover copy of the target's own conversation was pushed back into the folder the target reads")
	}
}

// TestSyncSessionsDoesNotRemapWhenTheStampIsStale: the organization stamp is a
// heuristic on Claude Desktop's private format, and it goes stale when the user
// switches organization inside the app without relaunching. A stamp naming an
// organization the source has no conversations under is not evidence, so the paths
// are left alone rather than half-rewritten.
func TestSyncSessionsDoesNotRemapWhenTheStampIsStale(t *testing.T) {
	tempDir := t.TempDir()
	const (
		srcAccount = "src-account"
		dstAccount = "dst-account"
		realOrg    = "the-org-actually-in-use"
		staleOrg   = "the-org-the-stamp-still-names"
		dstOrg     = "company-org"
	)

	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfigWithOrg(t, src, srcAccount, staleOrg)
	writeAccountConfigWithOrg(t, dst, dstAccount, dstOrg)
	writeSessionFile(t, src, filepath.Join(srcAccount, realOrg, "local_a.json"), `{"v":"work"}`, time.Now())

	if _, err := SyncSessions(src, dst); err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	want := filepath.Join(platformSessions(dst), dstAccount, realOrg, "local_a.json")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected the unchanged path-preserving copy at %s: %v", want, err)
	}
}

// TestSyncSessionsReportsWhereFilesLanded: the report drives the merge screen and
// the sync log, so it has to name where a conversation ended up, not where it came
// from.
func TestSyncSessionsReportsWhereFilesLanded(t *testing.T) {
	tempDir := t.TempDir()
	const (
		srcAccount = "src-account"
		dstAccount = "dst-account"
		srcOrg     = "personal-org"
		dstOrg     = "company-org"
	)

	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfigWithOrg(t, src, srcAccount, srcOrg)
	writeAccountConfigWithOrg(t, dst, dstAccount, dstOrg)
	writeSessionFile(t, src, filepath.Join(srcAccount, srcOrg, "local_a.json"), `{"v":"work"}`, time.Now())

	report, err := SyncSessions(src, dst)
	if err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	want := filepath.Join(dstOrg, "local_a.json")
	if len(report.CopiedFiles) != 1 || report.CopiedFiles[0] != want {
		t.Errorf("CopiedFiles = %q, want [%q]", report.CopiedFiles, want)
	}
}

// TestSyncSessionsLeavesPathsAloneWhenBothSidesShareAnOrganisation: two profiles
// signed into the same organization need no rewriting, and rewriting anyway would
// be a no-op that only risks getting the path wrong.
func TestSyncSessionsLeavesPathsAloneWhenBothSidesShareAnOrganisation(t *testing.T) {
	tempDir := t.TempDir()
	const (
		srcAccount = "src-account"
		dstAccount = "dst-account"
		sharedOrg  = "one-org"
	)

	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfigWithOrg(t, src, srcAccount, sharedOrg)
	writeAccountConfigWithOrg(t, dst, dstAccount, sharedOrg)
	writeSessionFile(t, src, filepath.Join(srcAccount, sharedOrg, "local_a.json"), `{"v":"work"}`, time.Now())

	if _, err := SyncSessions(src, dst); err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	want := filepath.Join(platformSessions(dst), dstAccount, sharedOrg, "local_a.json")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected the conversation at %s: %v", want, err)
	}
}
