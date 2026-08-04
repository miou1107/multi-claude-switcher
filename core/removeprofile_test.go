package core

import (
	"os"
	"path/filepath"
	"testing"
)

// removeFixture builds one profile directory holding a session file, and returns
// its path plus the archive root.
func removeFixture(t *testing.T, identity string) (root, profilePath, archiveRoot string) {
	t.Helper()
	root = t.TempDir()
	profilePath = filepath.Join(root, identity)
	archiveRoot = filepath.Join(root, "archive")
	bucket := filepath.Join(profilePath, "claude-code-sessions", "uuid-1")
	if err := os.MkdirAll(bucket, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucket, "local_x.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, profilePath, archiveRoot
}

// removePlatform resolves the identity back to the fixture's directory and
// answers what is running. Modelled on mergePlatform in merge_test.go: enough of
// a platform to exercise the order of operations, without pretending to be an OS.
type removePlatform struct {
	*mockPlatform
	root, archiveRoot string
	refuse            error
	running           []string
	runningErr        error
}

func (p *removePlatform) PrepareRemove(identity string) (string, error) {
	if p.refuse != nil {
		return "", p.refuse
	}
	return filepath.Join(p.root, identity), nil
}
func (p *removePlatform) DetectRunningProfiles() ([]string, error) {
	return p.running, p.runningErr
}
func (p *removePlatform) ArchiveDir() string { return p.archiveRoot }

func newRemovePlatform(t *testing.T, root, archiveRoot string) *removePlatform {
	t.Helper()
	return &removePlatform{mockPlatform: &mockPlatform{}, root: root, archiveRoot: archiveRoot}
}

func TestRemoveProfileArchivesAndClearsRegistries(t *testing.T) {
	withStubbedNames(t)
	withStubbedManaged(t)
	withStubbedPending(t)
	withStubbedActiveProfile(t)

	root, profilePath, archiveRoot := removeFixture(t, "Claude_Old")
	plat := newRemovePlatform(t, root, archiveRoot)

	if err := SetManaged([]string{"Claude_Old", "Claude_Keep"}); err != nil {
		t.Fatal(err)
	}
	if err := SetProfileName("Claude_Old", "Old one"); err != nil {
		t.Fatal(err)
	}
	if err := AddPending("Claude_Old", ""); err != nil {
		t.Fatal(err)
	}
	if err := SaveActiveProfile("Claude_Old"); err != nil {
		t.Fatal(err)
	}

	dest, err := RemoveProfile(plat, "Claude_Old")
	if err != nil {
		t.Fatalf("RemoveProfile: %v", err)
	}
	if _, err := os.Stat(profilePath); !os.IsNotExist(err) {
		t.Fatal("the profile directory is still in the scan path")
	}
	if _, err := os.Stat(filepath.Join(dest, "claude-code-sessions", "uuid-1", "local_x.json")); err != nil {
		t.Fatalf("the conversation did not travel with the folder: %v", err)
	}
	for _, m := range LoadManaged() {
		if m == "Claude_Old" {
			t.Fatal("managed.json still lists the removed profile")
		}
	}
	if n := DisplayName("Claude_Old"); n != "Claude_Old" {
		t.Fatalf("display name survived removal: %q", n)
	}
	for _, e := range LoadPending() {
		if e.Folder == "Claude_Old" {
			t.Fatal("pending.json still lists the removed profile")
		}
	}
	if a := LoadActiveProfile(); a == "Claude_Old" {
		t.Fatal("active.json still points at the removed profile")
	}
}
