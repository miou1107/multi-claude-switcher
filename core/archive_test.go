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

	dest, err := ArchiveProfile(profile, archiveRoot)
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
	destA, err := ArchiveProfile(first, archiveRoot)
	if err != nil {
		t.Fatal(err)
	}
	// Re-create the same folder name and archive it again in the same second, so
	// the timestamped name collides.
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}
	destB, err := ArchiveProfile(first, archiveRoot)
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
	if _, err := ArchiveProfile(filepath.Join(root, "nope"), filepath.Join(root, "archive")); err == nil {
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
	_, err := ArchiveProfile(profile, filepath.Join(root, "archive"))
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
