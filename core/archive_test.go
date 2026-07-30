package core

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestArchiveProfileMovesItOutOfTheScanPath(t *testing.T) {
	root := t.TempDir()
	scanPath := filepath.Join(root, "appsupport")
	archiveRoot := filepath.Join(root, "archive")
	profile := filepath.Join(scanPath, "Claude_Work")
	if err := os.MkdirAll(filepath.Join(profile, "claude-code-sessions", "uuid"), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(profile, "claude-code-sessions", "uuid", "local_1.json")
	if err := os.WriteFile(marker, []byte(`{"keep":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	dest, err := ArchiveProfile("Claude_Work", profile, archiveRoot)
	if err != nil {
		t.Fatalf("ArchiveProfile: %v", err)
	}
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Fatalf("profile must be gone from the scan path, stat err = %v", err)
	}
	if !strings.HasPrefix(dest, archiveRoot) {
		t.Fatalf("dest %q must sit under the archive root %q", dest, archiveRoot)
	}
	if !strings.Contains(filepath.Base(dest), "Claude_Work") {
		t.Fatalf("archive name must keep the profile name, got %q", filepath.Base(dest))
	}
	// Nothing is deleted, so the contents must survive byte-for-byte.
	b, err := os.ReadFile(filepath.Join(dest, "claude-code-sessions", "uuid", "local_1.json"))
	if err != nil {
		t.Fatalf("archived contents missing: %v", err)
	}
	if string(b) != `{"keep":true}` {
		t.Fatalf("contents changed: %q", b)
	}
}

func TestArchiveProfileCollisionGetsACounter(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	first := filepath.Join(root, "Claude_Work")
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}
	destA, err := ArchiveProfile("Claude_Work", first, archiveRoot)
	if err != nil {
		t.Fatal(err)
	}
	// Re-create the same folder name and archive it again in the same second, so
	// the timestamped name collides.
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}
	destB, err := ArchiveProfile("Claude_Work", first, archiveRoot)
	if err != nil {
		t.Fatalf("second archive must not fail on a name collision: %v", err)
	}
	if destA == destB {
		t.Fatalf("both archives landed on %q", destA)
	}
	if _, err := os.Stat(destA); err != nil {
		t.Fatalf("the first archive must be untouched: %v", err)
	}
}

func TestArchiveProfileMissingSourceIsAnError(t *testing.T) {
	root := t.TempDir()
	if _, err := ArchiveProfile("Claude_Gone", filepath.Join(root, "nope"), filepath.Join(root, "archive")); err == nil {
		t.Fatal("want an error for a profile that is not there")
	}
}

// TestArchiveProfileGivesUpAtOnceWhenRetryingCannotHelp: the rename is retried
// because Windows can still be releasing Claude's handles. A cross-volume rename
// will never succeed, and spending 20 seconds on it before reporting "Claude may
// still be holding its files" sends the user to Task Manager over a path problem.
func TestArchiveProfileGivesUpAtOnceWhenRetryingCannotHelp(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "Claude_Work")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}

	orig := renameProfile
	renameProfile = func(from, to string) error {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EXDEV}
	}
	t.Cleanup(func() { renameProfile = orig })

	start := time.Now()
	_, err := ArchiveProfile("Claude_Work", profile, filepath.Join(root, "archive"))
	if err == nil {
		t.Fatal("want an error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("gave up after %v — an unretryable failure must not be retried", elapsed)
	}
	if strings.Contains(err.Error(), "holding its files") {
		t.Fatalf("message blames Claude for a path problem: %v", err)
	}
	if _, statErr := os.Stat(profile); statErr != nil {
		t.Fatalf("a failed archive must leave the profile in place: %v", statErr)
	}
}

// On the Store build every profile's directory is the shared slot, literally
// named "Claude". Naming the archive after the directory would file all of them
// under one name, and — worse — the failure messages would tell the user their
// profile "Claude" could not be archived when they asked to archive "Work".
func TestArchiveProfileNamesTheArchiveAfterTheIdentityNotTheDirectory(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	// The Store build's shared slot: the directory is "Claude" for every profile.
	slot := filepath.Join(root, "Claude")
	if err := os.MkdirAll(slot, 0o755); err != nil {
		t.Fatal(err)
	}

	dest, err := ArchiveProfile("Work", slot, archiveRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(dest), "Work-") {
		t.Fatalf("archive should be named after the profile, got %q", filepath.Base(dest))
	}
}

func TestArchiveProfileFailureNamesTheProfileNotItsDirectory(t *testing.T) {
	root := t.TempDir()
	slot := filepath.Join(root, "Claude")
	if err := os.MkdirAll(slot, 0o755); err != nil {
		t.Fatal(err)
	}
	orig := renameProfile
	renameProfile = func(from, to string) error {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EXDEV}
	}
	t.Cleanup(func() { renameProfile = orig })

	_, err := ArchiveProfile("Work", slot, filepath.Join(root, "archive"))
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "Work") {
		t.Errorf("the message must name the profile the user asked about: %v", err)
	}
}

// A state.json a user has edited by hand can carry an identity with a separator
// in it, which would otherwise place the archive outside the archive root.
func TestArchiveProfileKeepsAHostileIdentityInsideTheArchiveRoot(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	profile := filepath.Join(root, "Claude")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}

	dest, err := ArchiveProfile("../../escaped", profile, archiveRoot)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(archiveRoot, dest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		t.Fatalf("archive escaped its root: %q", dest)
	}
}

// The retry exists because Windows can still be releasing Claude's file handles
// when the rename is first attempted. Nothing observed it actually recover until
// this test: every other test either succeeds first time or fails unretryably.
func TestArchiveProfileSucceedsAfterATransientLock(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "Claude_Work")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}

	origDelay := archiveRenameDelay
	archiveRenameDelay = time.Millisecond
	t.Cleanup(func() { archiveRenameDelay = origDelay })

	orig := renameProfile
	attempts := 0
	renameProfile = func(from, to string) error {
		attempts++
		if attempts < 3 {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EBUSY}
		}
		return orig(from, to)
	}
	t.Cleanup(func() { renameProfile = orig })

	dest, err := ArchiveProfile("Claude_Work", profile, filepath.Join(root, "archive"))
	if err != nil {
		t.Fatalf("a lock that clears must not fail the archive: %v", err)
	}
	if attempts != 3 {
		t.Errorf("want 3 attempts, got %d", attempts)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("the profile should have landed in the archive: %v", err)
	}
}
