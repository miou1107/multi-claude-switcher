package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// The guard must come from detection, not from a record. active.json is
// deliberately left empty here: that is the state of every machine where the
// user opened Claude themselves instead of switching with MCS, and the earlier
// draft of this guard was absent in exactly that case.
func TestRemoveProfileRefusesAProfileClaudeHasOpen(t *testing.T) {
	withStubbedNames(t)
	withStubbedManaged(t)
	withStubbedPending(t)
	withStubbedActiveProfile(t)

	root, profilePath, archiveRoot := removeFixture(t, "Claude_Live")
	plat := newRemovePlatform(t, root, archiveRoot)
	plat.running = []string{profilePath}
	if err := SetManaged([]string{"Claude_Live", "Claude_Other"}); err != nil {
		t.Fatal(err)
	}
	if a := LoadActiveProfile(); a != "" {
		t.Fatalf("precondition: active.json should be empty, got %q", a)
	}

	if _, err := RemoveProfile(plat, "Claude_Live"); err == nil {
		t.Fatal("RemoveProfile renamed a directory Claude has open")
	}
	if _, err := os.Stat(profilePath); err != nil {
		t.Fatalf("the profile directory was disturbed by a refused removal: %v", err)
	}
	managed := LoadManaged()
	if len(managed) != 2 {
		t.Fatalf("a refused removal changed the managed list: %v", managed)
	}
}

// Not knowing is not permission.
func TestRemoveProfileRefusesWhenDetectionFails(t *testing.T) {
	withStubbedNames(t)
	withStubbedManaged(t)
	withStubbedPending(t)
	withStubbedActiveProfile(t)

	root, profilePath, archiveRoot := removeFixture(t, "Claude_Unknown")
	plat := newRemovePlatform(t, root, archiveRoot)
	plat.runningErr = errors.New("process list unavailable")

	if _, err := RemoveProfile(plat, "Claude_Unknown"); err == nil {
		t.Fatal("RemoveProfile proceeded without knowing what Claude has open")
	}
	if _, err := os.Stat(profilePath); err != nil {
		t.Fatalf("the profile directory was disturbed: %v", err)
	}
}

// Removing the last account MCS switched to, with nothing running, must work.
// The earlier draft refused this and told the user to switch away from an
// account that was not open.
func TestRemoveProfileAllowsTheLastActiveWhenNothingIsRunning(t *testing.T) {
	withStubbedNames(t)
	withStubbedManaged(t)
	withStubbedPending(t)
	withStubbedActiveProfile(t)

	root, _, archiveRoot := removeFixture(t, "Claude_Closed")
	plat := newRemovePlatform(t, root, archiveRoot)
	if err := SaveActiveProfile("Claude_Closed"); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveProfile(plat, "Claude_Closed"); err != nil {
		t.Fatalf("refused a profile nothing has open: %v", err)
	}
}

// The message is asserted, not merely the failure. RemoveProfile's own os.Stat
// guard and ArchiveProfile's identical check both refuse this, so "err != nil"
// stays green with the guard this test is named for deleted, and the refusal
// would then arrive only after the running check had run and from a function
// whose wording ("nothing to archive at <path>") names a path rather than the
// account and offers no Rescan.
func TestRemoveProfileRefusesAProfileThatIsGone(t *testing.T) {
	withStubbedNames(t)
	withStubbedManaged(t)
	withStubbedPending(t)
	withStubbedActiveProfile(t)

	root := t.TempDir()
	plat := newRemovePlatform(t, root, filepath.Join(root, "archive"))
	_, err := RemoveProfile(plat, "Claude_Ghost")
	if err == nil {
		t.Fatal("RemoveProfile accepted an identity with no directory behind it")
	}
	if want := "Claude_Ghost is no longer there. Run Rescan"; err.Error() != want {
		t.Fatalf("the refusal did not come from RemoveProfile's own guard:\ngot  %q\nwant %q", err, want)
	}
}

// The registries must survive a move that fails. If they were written first, a
// locked directory would leave the folder in place AND unlisted: invisible in the
// panel, back on the next Rescan, with its display name gone.
func TestRemoveProfileKeepsRegistriesWhenTheMoveFails(t *testing.T) {
	withStubbedNames(t)
	withStubbedManaged(t)
	withStubbedPending(t)
	withStubbedActiveProfile(t)

	root, profilePath, archiveRoot := removeFixture(t, "Claude_Stuck")
	plat := newRemovePlatform(t, root, archiveRoot)
	if err := SetManaged([]string{"Claude_Stuck", "Claude_Keep"}); err != nil {
		t.Fatal(err)
	}
	if err := SetProfileName("Claude_Stuck", "Stuck one"); err != nil {
		t.Fatal(err)
	}

	origRename, origDelay := renameProfile, archiveRenameDelay
	renameProfile = func(from, to string) error { return errors.New("in use by another process") }
	archiveRenameDelay = time.Millisecond
	t.Cleanup(func() { renameProfile, archiveRenameDelay = origRename, origDelay })

	if _, err := RemoveProfile(plat, "Claude_Stuck"); err == nil {
		t.Fatal("RemoveProfile reported success on a failed move")
	}
	if _, err := os.Stat(profilePath); err != nil {
		t.Fatalf("the profile directory vanished on a failed move: %v", err)
	}
	managed := LoadManaged()
	found := false
	for _, m := range managed {
		if m == "Claude_Stuck" {
			found = true
		}
	}
	if !found {
		t.Fatal("a failed move unmanaged the profile; it is now hidden but still on disk")
	}
	if n := DisplayName("Claude_Stuck"); n != "Stuck one" {
		t.Fatalf("a failed move cleared the display name: %q", n)
	}
}

// A registry that cannot be written is reported, not swallowed. The display name
// is the one whose loss the user feels: a later profile reusing the identity
// silently inherits it.
func TestRemoveProfileReportsARegistryThatCannotBeWritten(t *testing.T) {
	withStubbedNames(t)
	withStubbedManaged(t)
	withStubbedPending(t)
	withStubbedActiveProfile(t)

	root, _, archiveRoot := removeFixture(t, "Claude_Old")
	plat := newRemovePlatform(t, root, archiveRoot)

	// names.json is written by SetProfileName's own staging file plus rename, not
	// writeRegistryFile. Put a DIRECTORY where the staging file goes: os.WriteFile
	// can never open it, so the write fails for certain, without depending on
	// filesystem permission semantics. names.json itself stays a real, readable
	// file, which matters here because the complaint has to name the account the
	// way the user knows it, and that name is read back out of this file.
	orig := namesPath
	names := filepath.Join(t.TempDir(), "names.json")
	namesPath = func() string { return names }
	t.Cleanup(func() { namesPath = orig })
	if err := SetProfileName("Claude_Old", "Old one"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(names+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}

	dest, err := RemoveProfile(plat, "Claude_Old")
	if err == nil {
		t.Fatal("a registry write failure was swallowed")
	}
	if dest == "" {
		t.Fatal("the archive path was not returned; the folder did move and the caller needs to know where")
	}
	// Which registry complained, not merely that something did. Only names.json
	// was blocked here, so any other complaint means the test is being satisfied
	// by a failure it did not arrange, and the display name is the one whose loss
	// the user actually feels.
	if !strings.Contains(err.Error(), `Its name is still recorded as "Old one"`) {
		t.Fatalf("the display name is not what was reported as unclearable: %v", err)
	}
	// It also has to be readable on the screen it lands on: a whole sentence
	// naming the account, with the joined entries on their own lines.
	if !strings.HasPrefix(err.Error(), "Old one was removed, but ") {
		t.Fatalf("the complaint does not open by saying the removal did happen: %v", err)
	}
	if !strings.Contains(err.Error(), "\n") {
		t.Fatalf("the summary and the complaint must be separate lines: %v", err)
	}
}
