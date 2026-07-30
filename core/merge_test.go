package core

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/miou1107/multi-claude-switcher/platform"
)

func withStubbedNames(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	orig := namesPath
	namesPath = func() string { return filepath.Join(dir, "names.json") }
	t.Cleanup(func() { namesPath = orig })
}

// mergeFixture builds two profiles signed in to the same account, each holding
// one session the other does not, and returns their paths plus the archive root.
func mergeFixture(t *testing.T, keepUUID, archiveUUID string) (keep, archive, archiveRoot string) {
	t.Helper()
	root := t.TempDir()
	keep = filepath.Join(root, "Claude_Keep")
	archive = filepath.Join(root, "Claude_Archive")
	archiveRoot = filepath.Join(root, "archive")
	for path, uuid := range map[string]string{keep: keepUUID, archive: archiveUUID} {
		bucket := filepath.Join(path, "claude-code-sessions", uuid)
		if err := os.MkdirAll(bucket, 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := `{"lastKnownAccountUuid":"` + uuid + `"}`
		if err := os.WriteFile(filepath.Join(path, "config.json"), []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
		name := "only_in_keep.json"
		if path == archive {
			name = "only_in_archive.json"
		}
		if err := os.WriteFile(filepath.Join(bucket, name), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return keep, archive, archiveRoot
}

// mergePlatform resolves the two identities back to the fixture's paths, standing
// in for the platform without pretending to be a whole OS.
type mergePlatform struct {
	*mockPlatform
	root, archiveRoot string
	profiles          []*platform.ProfileInfo
}

func newMergePlatform(t *testing.T, keep, archive, archiveRoot string) *mergePlatform {
	t.Helper()
	return &mergePlatform{
		mockPlatform: &mockPlatform{},
		root:         filepath.Dir(keep),
		archiveRoot:  archiveRoot,
		profiles: []*platform.ProfileInfo{
			{Name: filepath.Base(keep), Path: keep, Exists: true},
			{Name: filepath.Base(archive), Path: archive, Exists: true},
		},
	}
}

func (m *mergePlatform) FindProfiles() ([]*platform.ProfileInfo, error) { return m.profiles, nil }
func (m *mergePlatform) PrepareArchive(keepIdentity, archiveIdentity string) (string, string, error) {
	return filepath.Join(m.root, keepIdentity), filepath.Join(m.root, archiveIdentity), nil
}
func (m *mergePlatform) ArchiveDir() string { return m.archiveRoot }

func TestMergePreviewCountsTheUnionAndTheConflicts(t *testing.T) {
	keep, archive, _ := mergeFixture(t, "same-uuid", "same-uuid")
	// A third record both hold, with the keeper's copy newer and different. Sync
	// leaves the keeper's alone and reports a conflict, so the other copy ends up
	// only in the archive — the user has to be told before committing.
	kp := filepath.Join(keep, "claude-code-sessions", "same-uuid", "both.json")
	ap := filepath.Join(archive, "claude-code-sessions", "same-uuid", "both.json")
	if err := os.WriteFile(ap, []byte(`{"v":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kp, []byte(`{"v":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(ap, old, old); err != nil {
		t.Fatal(err)
	}

	plan, err := MergePreview(keep, archive, "same-uuid")
	if err != nil {
		t.Fatalf("MergePreview: %v", err)
	}
	// only_in_keep, only_in_archive, both → 3, not the sum of 2 + 2.
	if plan.Combined != 3 {
		t.Fatalf("Combined = %d, want the union size 3", plan.Combined)
	}
	if plan.Conflicts != 1 {
		t.Fatalf("Conflicts = %d, want 1", plan.Conflicts)
	}
}

func TestMergePreviewIdenticalCopiesAreNotConflicts(t *testing.T) {
	keep, archive, _ := mergeFixture(t, "same-uuid", "same-uuid")
	for _, dir := range []string{keep, archive} {
		p := filepath.Join(dir, "claude-code-sessions", "same-uuid", "both.json")
		if err := os.WriteFile(p, []byte(`{"v":1}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := MergePreview(keep, archive, "same-uuid")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Conflicts != 0 {
		t.Fatalf("two identical copies are not a conflict, got %d", plan.Conflicts)
	}
	if plan.Combined != 3 {
		t.Fatalf("Combined = %d, want 3", plan.Combined)
	}
}

// TestMergePreviewSurvivesAnUnreadableRecord: SyncSessions skips a file it cannot
// read and carries on, so a preview that aborted would block a merge of hundreds
// of conversations over one junk file.
func TestMergePreviewSurvivesAnUnreadableRecord(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits do not deny reads the same way on Windows")
	}
	keep, archive, _ := mergeFixture(t, "same-uuid", "same-uuid")
	// A record present on both sides, where the archive side cannot be read.
	kp := filepath.Join(keep, "claude-code-sessions", "same-uuid", "both.json")
	if err := os.WriteFile(kp, []byte(`{"v":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ap := filepath.Join(archive, "claude-code-sessions", "same-uuid", "both.json")
	if err := os.WriteFile(ap, []byte(`{"v":1}`), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ap, 0o600) }) // so t.TempDir can clean up

	plan, err := MergePreview(keep, archive, "same-uuid")
	if err != nil {
		t.Fatalf("one unreadable record must not fail the preview: %v", err)
	}
	if plan.Unreadable != 1 {
		t.Fatalf("Unreadable = %d, want 1", plan.Unreadable)
	}
	if plan.Conflicts != 0 {
		t.Fatalf("an unreadable record is not a conflict, got %d", plan.Conflicts)
	}
}

func TestMergeDuplicatesUnionsThenArchives(t *testing.T) {
	withStubbedManaged(t)
	withStubbedNames(t)
	withStubbedPending(t)
	if err := SetManaged([]string{"Claude_Keep", "Claude_Archive"}); err != nil {
		t.Fatal(err)
	}
	if err := SetProfileName("Claude_Archive", "Old Work"); err != nil {
		t.Fatal(err)
	}
	keep, archive, archiveRoot := mergeFixture(t, "same-uuid", "same-uuid")
	plat := newMergePlatform(t, keep, archive, archiveRoot)
	backupRoot := filepath.Join(t.TempDir(), "backups")

	report, err := MergeDuplicates(plat, MergeRequest{
		KeepIdentity: "Claude_Keep", ArchiveIdentity: "Claude_Archive",
		BackupRoot: backupRoot,
	})
	if err != nil {
		t.Fatalf("MergeDuplicates: %v", err)
	}
	if report == nil || report.CopiedCount != 1 {
		t.Fatalf("want 1 session copied, got %+v", report)
	}
	for _, name := range []string{"only_in_keep.json", "only_in_archive.json"} {
		p := filepath.Join(keep, "claude-code-sessions", "same-uuid", name)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("keeper is missing %s: %v", name, err)
		}
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatalf("archived profile still in place, stat err = %v", err)
	}
	if len(LoadManaged()) != 1 || LoadManaged()[0] != "Claude_Keep" {
		t.Fatalf("managed = %v, want just the keeper", LoadManaged())
	}
	// The archived profile's display name goes with it, or a later profile reusing
	// the identity would inherit a name the user never chose for it.
	if n := LoadProfileNames()["Claude_Archive"]; n != "" {
		t.Fatalf("display name for the archived profile survived: %q", n)
	}
	backups, err := NewBackupManager(backupRoot).ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) == 0 {
		t.Fatal("the keeper must be snapshotted before it is written to")
	}
}

func TestMergeDuplicatesRefusesDifferentAccounts(t *testing.T) {
	withStubbedManaged(t)
	withStubbedNames(t)
	withStubbedPending(t)
	keep, archive, archiveRoot := mergeFixture(t, "uuid-a", "uuid-b")
	plat := newMergePlatform(t, keep, archive, archiveRoot)

	if _, err := MergeDuplicates(plat, MergeRequest{
		KeepIdentity: "Claude_Keep", ArchiveIdentity: "Claude_Archive",
		BackupRoot: filepath.Join(t.TempDir(), "backups"),
	}); err == nil {
		t.Fatal("merging two different accounts must be refused")
	}
	// Nothing may have moved on either side.
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("the other profile must be left alone: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("the keeper must be left alone: %v", err)
	}
}

func TestMergeDuplicatesLeavesManagedAloneWhenArchiveFails(t *testing.T) {
	withStubbedManaged(t)
	withStubbedNames(t)
	withStubbedPending(t)
	if err := SetManaged([]string{"Claude_Keep", "Claude_Archive"}); err != nil {
		t.Fatal(err)
	}
	keep, archive, _ := mergeFixture(t, "same-uuid", "same-uuid")
	// An archive root that cannot be created: a path under a regular file.
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	plat := newMergePlatform(t, keep, archive, filepath.Join(blocker, "archive"))

	if _, err := MergeDuplicates(plat, MergeRequest{
		KeepIdentity: "Claude_Keep", ArchiveIdentity: "Claude_Archive",
		BackupRoot: filepath.Join(t.TempDir(), "backups"),
	}); err == nil {
		t.Fatal("want an error when the archive root cannot be created")
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("profile must still be in place after a failed archive: %v", err)
	}
	// Never unmanage a folder that is still on disk: the user has to keep seeing
	// the warning and be able to retry.
	if len(LoadManaged()) != 2 {
		t.Fatalf("managed = %v, want both still listed", LoadManaged())
	}
}

// TestMergePreviewMatchesWhatTheMergeDoes is the property the merge screen's
// promise rests on. The preview and the sync are separate code; if they ever
// disagree, the number shown to the user before they commit is fiction.
func TestMergePreviewMatchesWhatTheMergeDoes(t *testing.T) {
	withStubbedManaged(t)
	withStubbedNames(t)
	withStubbedPending(t)
	keep, archive, archiveRoot := mergeFixture(t, "same-uuid", "same-uuid")
	kp := filepath.Join(keep, "claude-code-sessions", "same-uuid", "both.json")
	ap := filepath.Join(archive, "claude-code-sessions", "same-uuid", "both.json")
	if err := os.WriteFile(ap, []byte(`{"v":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kp, []byte(`{"v":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(ap, old, old); err != nil {
		t.Fatal(err)
	}

	plan, err := MergePreview(keep, archive, "same-uuid")
	if err != nil {
		t.Fatal(err)
	}
	report, err := MergeDuplicates(newMergePlatform(t, keep, archive, archiveRoot), MergeRequest{
		KeepIdentity: "Claude_Keep", ArchiveIdentity: "Claude_Archive",
		BackupRoot: filepath.Join(t.TempDir(), "backups"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if report.ConflictCount != plan.Conflicts {
		t.Errorf("preview promised %d conflicts, the merge reported %d", plan.Conflicts, report.ConflictCount)
	}
	held, err := sessionFilesByRelPath(filepath.Join(keep, "claude-code-sessions", "same-uuid"))
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != plan.Combined {
		t.Errorf("preview promised %d conversations, the keeper holds %d", plan.Combined, len(held))
	}
}
