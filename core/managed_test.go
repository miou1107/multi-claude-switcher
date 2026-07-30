package core

import (
	"path/filepath"
	"testing"
)

// withStubbedManaged points the managed registry at a temp file for the duration
// of a test. Extracted from the inline stubbing that used to live in each test so
// other files in the package can reuse it.
func withStubbedManaged(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	orig := managedPath
	managedPath = func() string { return filepath.Join(dir, "managed.json") }
	t.Cleanup(func() { managedPath = orig })
}

func TestManagedRegistry(t *testing.T) {
	withStubbedManaged(t)

	// Absent file → nil (first-run signal).
	if got := LoadManaged(); got != nil {
		t.Fatalf("absent file: want nil, got %#v", got)
	}
	if IsManaged("Claude") {
		t.Fatal("absent file: IsManaged should be false")
	}

	if err := SetManaged([]string{"Claude", "Claude_Profile2"}); err != nil {
		t.Fatal(err)
	}
	got := LoadManaged()
	if len(got) != 2 || got[0] != "Claude" || got[1] != "Claude_Profile2" {
		t.Fatalf("round-trip: got %#v", got)
	}
	if !IsManaged("Claude") || IsManaged("Claude-3p") {
		t.Fatal("IsManaged wrong after save")
	}

	// Present-but-empty → non-nil empty slice (distinct from absent).
	if err := SetManaged([]string{}); err != nil {
		t.Fatal(err)
	}
	if got := LoadManaged(); got == nil {
		t.Fatal("present-empty: want non-nil empty slice, got nil")
	}
}
