package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// The tests here exercise PrepareArchive as a step that MOVES directories, which
// is what it does on the Windows Store build and what no other test reproduces.
//
// There, both profiles are reached through one shared slot directory. Only the
// occupant of the slot is live, so archiving the other one first requires
// swapping the keeper in. Everything downstream — which folder gets renamed into
// the archive, whether the keeper survives — depends on that swap having
// happened, and the merge cannot see it directly: all it gets back is two paths.

// The failure this guards against. If the swap silently does not happen,
// PrepareArchive hands back the slot as BOTH the keeper and the profile to
// archive. Archiving it would rename away the profile the user chose to keep,
// which by that point holds the conversations from both accounts.
func TestMergeRefusesWhenPrepareArchiveHandsBackOneFolderForBoth(t *testing.T) {
	withStubbedNames(t)
	withStubbedManaged(t)
	withStubbedPending(t)
	keep, archive, archiveRoot := mergeFixture(t, "same-uuid", "same-uuid")
	plat := newMergePlatform(t, keep, archive, archiveRoot)
	// The broken swap: one directory, described twice.
	plat.prepareArchive = func(_, _ string) (string, string, error) {
		return keep, keep, nil
	}

	_, err := MergeDuplicates(plat, MergeRequest{
		KeepIdentity:    filepath.Base(keep),
		ArchiveIdentity: filepath.Base(archive),
		BackupRoot:      filepath.Join(t.TempDir(), "backups"),
	})
	if err == nil {
		t.Fatal("want a refusal when both paths are the same folder, got nil")
	}
	if !strings.Contains(err.Error(), "Rescan") {
		t.Errorf("the message should tell the user what to do next: %v", err)
	}

	// The keeper must still be there, holding the union it was just given.
	for _, name := range []string{"only_in_keep.json", "only_in_archive.json"} {
		p := filepath.Join(keep, "claude-code-sessions", "same-uuid", name)
		if _, statErr := os.Stat(p); statErr != nil {
			t.Fatalf("the kept profile lost %s: %v", name, statErr)
		}
	}
	// And nothing may have been archived.
	if entries, _ := os.ReadDir(archiveRoot); len(entries) != 0 {
		t.Fatalf("a refused merge archived something: %v", entries)
	}
}

// slotPlatform models the Store build: one live directory (the slot) plus a
// parking area, with the profile's identity kept in a side table rather than in
// the directory name.
type slotPlatform struct {
	*mockPlatform
	t          *testing.T
	root       string
	slot       string // <root>/Claude — whichever profile is live lives here
	parkedDir  string // <root>/.parked/<identity>
	current    string // identity currently occupying the slot
	parked     string // identity currently parked
	archiveDir string
}

func (s *slotPlatform) pathFor(identity string) string {
	if identity == s.current {
		return s.slot
	}
	return filepath.Join(s.parkedDir, identity)
}

func (s *slotPlatform) FindProfiles() ([]*platform.ProfileInfo, error) {
	return []*platform.ProfileInfo{
		{Name: s.current, Path: s.slot, Exists: true},
		{Name: s.parked, Path: filepath.Join(s.parkedDir, s.parked), Exists: true},
	}, nil
}

// PrepareArchive does the real thing: if the profile to archive is the one in the
// slot, the keeper is swapped in so the archive can be renamed away without
// leaving the slot empty.
func (s *slotPlatform) PrepareArchive(keepIdentity, archiveIdentity string) (string, string, error) {
	if s.current == archiveIdentity {
		parkArchive := filepath.Join(s.parkedDir, archiveIdentity)
		keepParked := filepath.Join(s.parkedDir, keepIdentity)
		if err := os.Rename(s.slot, parkArchive); err != nil {
			return "", "", err
		}
		if err := os.Rename(keepParked, s.slot); err != nil {
			return "", "", err
		}
		s.current, s.parked = keepIdentity, archiveIdentity
	}
	return s.pathFor(keepIdentity), s.pathFor(archiveIdentity), nil
}

func (s *slotPlatform) ArchiveDir() string { return s.archiveDir }

// The Store build's happy path, end to end: the profile being given up starts out
// as the live one, and the merge has to end with the keeper live, holding both
// sides' conversations, and the other profile in the archive.
func TestMergeOnASharedSlotSwapsTheKeeperInAndArchivesTheOther(t *testing.T) {
	withStubbedNames(t)
	withStubbedManaged(t)
	withStubbedPending(t)

	root := t.TempDir()
	plat := &slotPlatform{
		mockPlatform: &mockPlatform{},
		t:            t,
		root:         root,
		slot:         filepath.Join(root, "Claude"),
		parkedDir:    filepath.Join(root, ".parked"),
		current:      "Give Up", // the profile to archive is the live one
		parked:       "Keep",
		archiveDir:   filepath.Join(root, ".archive"),
	}
	// "Give Up" is live, in the slot; "Keep" is parked. Both hold one session the
	// other does not, under the same account.
	seed := func(dir, session string) {
		bucket := filepath.Join(dir, "claude-code-sessions", "acct")
		if err := os.MkdirAll(bucket, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"lastKnownAccountUuid":"acct"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bucket, session), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	seed(plat.slot, "from_giveup.json")
	seed(filepath.Join(plat.parkedDir, "Keep"), "from_keep.json")

	if _, err := MergeDuplicates(plat, MergeRequest{
		KeepIdentity:    "Keep",
		ArchiveIdentity: "Give Up",
		BackupRoot:      filepath.Join(t.TempDir(), "backups"),
	}); err != nil {
		t.Fatalf("MergeDuplicates: %v", err)
	}

	// The keeper is now the live profile, holding both sides.
	if plat.current != "Keep" {
		t.Fatalf("the keeper should be live, slot holds %q", plat.current)
	}
	for _, name := range []string{"from_keep.json", "from_giveup.json"} {
		p := filepath.Join(plat.slot, "claude-code-sessions", "acct", name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("the live profile is missing %s: %v", name, err)
		}
	}

	// The other profile is out of the scan path and in the archive, named after
	// its identity rather than after the slot directory it used to occupy.
	if _, err := os.Stat(filepath.Join(plat.parkedDir, "Give Up")); !os.IsNotExist(err) {
		t.Errorf("the archived profile is still in the scan path: %v", err)
	}
	entries, err := os.ReadDir(plat.archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("want exactly one archive, got %v", entries)
	}
	if !strings.HasPrefix(entries[0].Name(), "Give Up-") {
		t.Errorf("archive should be named after the profile, got %q", entries[0].Name())
	}
	// Its conversation must still be in there: archiving moves, it never deletes.
	if _, err := os.Stat(filepath.Join(plat.archiveDir, entries[0].Name(), "claude-code-sessions", "acct", "from_giveup.json")); err != nil {
		t.Errorf("the archive lost its conversations: %v", err)
	}
}
