package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateProfileAddPath(t *testing.T) {
	withStubbedManaged(t)
	withStubbedActiveProfile(t)
	withStubbedPending(t)
	withStubbedNames(t)
	root := t.TempDir()
	created := filepath.Join(root, "Claude_Personal")
	if err := os.MkdirAll(created, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &mockPlatform{running: true, createdIdentity: "Claude_Personal", createdPath: created}

	got, err := NewProfileCreator(m).Create(CreateProfileRequest{Name: "  Personal  "})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.DataDir != created || got.Identity != "Claude_Personal" {
		t.Fatalf("got %+v, want identity Claude_Personal at %q", got, created)
	}
	if !m.terminated {
		t.Fatal("Claude must be quit before its data dirs are touched")
	}
	// The platform receives the cleaned name, never the raw input.
	if m.createdName != "Personal" {
		t.Fatalf("platform got name %q, want it trimmed", m.createdName)
	}
	if len(m.preparedSources) != 0 {
		t.Fatalf("the add path must not prepare a recovery, got %+v", m.preparedSources)
	}
	if !m.launched || m.launchedPath != created {
		t.Fatalf("must launch the new profile, launched=%v path=%q", m.launched, m.launchedPath)
	}
	pending := LoadPending()
	if len(pending) != 1 || pending[0].Folder != "Claude_Personal" || pending[0].ExpectUUID != "" {
		t.Fatalf("pending = %+v", pending)
	}
	// It shows up at once through the pending registry, not by curating the managed
	// list — see TestCreateProfileFirstRunLeavesTheManagedListUnset for why.
	if managed := LoadManaged(); managed != nil {
		t.Fatalf("managed = %v, want it left unset on a first-run create", managed)
	}
	// The name the user typed becomes the display name, so both platforms show it
	// even though only one of them puts it in the folder name.
	if n := LoadProfileNames()["Claude_Personal"]; n != "Personal" {
		t.Fatalf("display name = %q, want %q", n, "Personal")
	}
}

// TestCreateProfileFirstRunLeavesTheManagedListUnset is the regression test for a
// create that hid every other account. On a never-configured (first-run) system the
// managed list is nil, and the panel falls back to showing everything usable. Adding
// the new profile to the list turned that nil into a one-element list, which flipped
// the panel to "show only what is listed" — so the user's existing, signed-in
// accounts vanished the moment they added a second one. The new profile stays visible
// through the pending registry instead, so the list must be left unset here.
func TestCreateProfileFirstRunLeavesTheManagedListUnset(t *testing.T) {
	withStubbedManaged(t) // first run: managed.json absent
	withStubbedActiveProfile(t)
	withStubbedPending(t)
	withStubbedNames(t)
	root := t.TempDir()
	created := filepath.Join(root, "Claude_Personal")
	if err := os.MkdirAll(created, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &mockPlatform{running: true, createdIdentity: "Claude_Personal", createdPath: created}

	if _, err := NewProfileCreator(m).Create(CreateProfileRequest{Name: "Personal"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if managed := LoadManaged(); managed != nil {
		t.Fatalf("a first-run create must not curate the list; got %v, which hides every account not in it", managed)
	}
	// It is still shown, through the pending registry.
	if p := LoadPending(); len(p) != 1 || p[0].Folder != "Claude_Personal" {
		t.Fatalf("the new profile must be registered pending so it shows without a managed entry: %+v", p)
	}
}

// TestCreateProfileAddsToAnAlreadyCuratedList: once the user has curated a list,
// the new profile joins it (so it survives past sign-in) without disturbing what is
// already there.
func TestCreateProfileAddsToAnAlreadyCuratedList(t *testing.T) {
	withStubbedManaged(t)
	withStubbedActiveProfile(t)
	withStubbedPending(t)
	withStubbedNames(t)
	if err := SetManaged([]string{"Claude"}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	created := filepath.Join(root, "Claude_Personal")
	if err := os.MkdirAll(created, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &mockPlatform{createdIdentity: "Claude_Personal", createdPath: created}

	if _, err := NewProfileCreator(m).Create(CreateProfileRequest{Name: "Personal"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	mg := LoadManaged()
	if len(mg) != 2 || mg[0] != "Claude" || mg[1] != "Claude_Personal" {
		t.Fatalf("managed = %v, want the existing entry kept and the new one appended", mg)
	}
}

// TestCreateProfileKeysRegistriesOnTheIdentityNotThePath is the regression test
// for the defect that made this whole feature inert on the Store build. There,
// CreateProfile returns identity "Work" while the data lives in a directory called
// "Claude" — the shared slot. Anything using filepath.Base of the path writes
// "Claude" into the registries, names a profile FindProfiles never reports, and
// leaves the real one invisible.
func TestCreateProfileKeysRegistriesOnTheIdentityNotThePath(t *testing.T) {
	withStubbedManaged(t)
	withStubbedActiveProfile(t)
	withStubbedPending(t)
	withStubbedNames(t)
	// Curate the list first, so the managed write actually happens (a first-run list
	// is deliberately left unset — see TestCreateProfileFirstRunLeavesTheManagedListUnset)
	// and this test can still check it is keyed on the identity, not the directory.
	if err := SetManaged([]string{"Other"}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	slot := filepath.Join(root, "Claude")
	if err := os.MkdirAll(slot, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &mockPlatform{createdIdentity: "Work", createdPath: slot}

	got, err := NewProfileCreator(m).Create(CreateProfileRequest{Name: "Work"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Identity != "Work" {
		t.Fatalf("identity = %q, want what the platform returned", got.Identity)
	}
	if p := LoadPending(); len(p) != 1 || p[0].Folder != "Work" {
		t.Fatalf("pending = %+v, want it keyed on the identity", p)
	}
	if mg := LoadManaged(); len(mg) != 2 || mg[1] != "Work" {
		t.Fatalf("managed = %v, want the identity appended, not the directory basename", mg)
	}
	if n := LoadProfileNames()["Work"]; n != "Work" {
		t.Fatalf("names.json = %q, want it keyed on the identity", n)
	}
}

func TestCreateProfileRecoveryPath(t *testing.T) {
	withStubbedManaged(t)
	withStubbedActiveProfile(t)
	withStubbedPending(t)
	withStubbedNames(t)
	root := t.TempDir()
	created := filepath.Join(root, "Claude_Recovered")
	if err := os.MkdirAll(created, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &mockPlatform{createdIdentity: "Claude_Recovered", createdPath: created}

	_, err := NewProfileCreator(m).Create(CreateProfileRequest{
		Name: "Recovered", RecoverUUID: "orphan-uuid",
		Sources: []GhostSource{
			{Folder: "Claude", Path: "/data/Claude", Convos: 5},
			{Folder: "Claude_Two", Path: "/data/Claude_Two", Convos: 40},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Every source is passed through, with the path the scan gave it — nothing here
	// reconstructs a path from a folder name.
	if len(m.preparedSources) != 2 {
		t.Fatalf("all sources must reach the platform, got %+v", m.preparedSources)
	}
	for _, s := range m.preparedSources {
		if s.UUID != "orphan-uuid" {
			t.Fatalf("source has the wrong account: %+v", s)
		}
	}
	if m.preparedSources[0].Path != "/data/Claude" || m.preparedSources[1].Path != "/data/Claude_Two" {
		t.Fatalf("paths must come from the scan: %+v", m.preparedSources)
	}
	if p := LoadPending(); len(p) != 1 || p[0].ExpectUUID != "orphan-uuid" {
		t.Fatalf("pending must remember which account to wait for: %+v", p)
	}
}

func TestCreateProfileRejectsBadNameBeforeTouchingAnything(t *testing.T) {
	withStubbedManaged(t)
	withStubbedActiveProfile(t)
	withStubbedPending(t)
	withStubbedNames(t)
	m := &mockPlatform{}

	if _, err := NewProfileCreator(m).Create(CreateProfileRequest{Name: "  "}); err == nil {
		t.Fatal("want an error for an empty name")
	}
	if m.terminated {
		t.Fatal("a rejected name must not quit Claude")
	}
	if m.createdName != "" {
		t.Fatal("a rejected name must not reach the platform")
	}
	if len(LoadPending()) != 0 || len(LoadManaged()) != 0 {
		t.Fatal("a rejected name must not write any state")
	}
}

func TestCreateProfileRecoveryWithNoSourcesIsRefused(t *testing.T) {
	withStubbedManaged(t)
	withStubbedActiveProfile(t)
	withStubbedPending(t)
	withStubbedNames(t)
	m := &mockPlatform{createdIdentity: "Claude_Recovered", createdPath: t.TempDir()}

	if _, err := NewProfileCreator(m).Create(CreateProfileRequest{
		Name: "Recovered", RecoverUUID: "orphan-uuid",
	}); err == nil {
		t.Fatal("a recovery with nowhere to copy from must be refused")
	}
	if m.terminated {
		t.Fatal("refuse before quitting Claude — nothing has changed yet")
	}
}

func TestCreateProfileRecoveryFailureLeavesNoState(t *testing.T) {
	withStubbedManaged(t)
	withStubbedActiveProfile(t)
	withStubbedPending(t)
	withStubbedNames(t)
	root := t.TempDir()
	created := filepath.Join(root, "Claude_Recovered")
	if err := os.MkdirAll(created, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &mockPlatform{createdIdentity: "Claude_Recovered", createdPath: created, prepareErr: os.ErrPermission}

	if _, err := NewProfileCreator(m).Create(CreateProfileRequest{
		Name: "Recovered", RecoverUUID: "orphan-uuid",
		Sources: []GhostSource{{Folder: "Claude", Path: "/data/Claude", Convos: 5}},
	}); err == nil {
		t.Fatal("want the copy failure surfaced")
	}
	if len(LoadPending()) != 0 {
		t.Fatalf("pending must not be written when the copy failed: %+v", LoadPending())
	}
	if len(LoadManaged()) != 0 {
		t.Fatalf("managed must not be written when the copy failed: %v", LoadManaged())
	}
	if m.launched {
		t.Fatal("must not launch a profile whose recovery failed")
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("the half-made profile must be cleaned up, stat err = %v", err)
	}
}

// A create that gets as far as copying a recovered account's conversations and
// then fails to register the profile must throw the new profile away.
//
// Leaving it behind puts a second copy of that account's conversations on disk.
// The scanner adds up the buckets it finds, so the ghost the user was trying to
// clear reappears reporting twice the chats it has — and every retry adds
// another copy.
func TestCreateProfileDiscardsTheNewProfileWhenItCannotBeRegistered(t *testing.T) {
	withStubbedManaged(t)
	withStubbedActiveProfile(t)
	withStubbedNames(t)

	// Point the pending registry at a path it cannot possibly create: a directory
	// whose parent is a regular file.
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	origPending := pendingPath
	pendingPath = func() string { return filepath.Join(blocker, "pending.json") }
	t.Cleanup(func() { pendingPath = origPending })

	root := t.TempDir()
	created := filepath.Join(root, "Claude_Recovered")
	// PrepareRecovery has already put the recovered conversations here.
	bucket := filepath.Join(created, "claude-code-sessions", "ghost-uuid")
	if err := os.MkdirAll(bucket, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucket, "recovered.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &mockPlatform{running: true, createdIdentity: "Claude_Recovered", createdPath: created}

	_, err := NewProfileCreator(m).Create(CreateProfileRequest{
		Name:        "Recovered",
		RecoverUUID: "ghost-uuid",
		Sources:     []GhostSource{{Folder: "Claude", Path: filepath.Join(root, "Claude"), Convos: 1}},
	})
	if err == nil {
		t.Fatal("want an error when the profile cannot be registered")
	}
	if _, statErr := os.Stat(created); !os.IsNotExist(statErr) {
		t.Fatalf("the unregistered profile was left on disk, so its conversations are now duplicated: %v", statErr)
	}
	if m.launched {
		t.Error("a profile that could not be registered must not be opened")
	}
}

// TestCreateProfileRecordsTheNewAccountAsActive: creating a profile opens Claude
// on it, so that is where the user now is. Without recording it, the next switch
// would think they were still on whatever they had before and close the wrong
// account.
func TestCreateProfileRecordsTheNewAccountAsActive(t *testing.T) {
	withStubbedActiveProfile(t)
	withStubbedManaged(t)
	withStubbedPending(t)
	withStubbedNames(t)

	m := &mockPlatform{createdIdentity: "Claude_Personal", createdPath: t.TempDir()}
	if _, err := NewProfileCreator(m).Create(CreateProfileRequest{Name: "Personal"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := LoadActiveProfile(); got != "Claude_Personal" {
		t.Errorf("active account = %q, want the profile just opened (%q)", got, "Claude_Personal")
	}
}
