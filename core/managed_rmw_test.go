package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests in this file cover the read-modify-write path onto managed.json.
//
// Callers used to do that by hand — LoadManaged, append or filter, SetManaged —
// and LoadManaged returns nil both when the registry has never been written and
// when it is present but unparseable. A caller cannot tell those apart, so a
// corrupt registry read as "first run" turned an append into a replace: the
// user's whole account list became the one profile being added.

func writeManagedRaw(t *testing.T, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(managedPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedPath(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAddManagedAppendsToAnExistingList(t *testing.T) {
	withStubbedManaged(t)
	if err := SetManaged([]string{"Claude", "Claude_Work"}); err != nil {
		t.Fatal(err)
	}
	if err := AddManaged("Claude_Personal"); err != nil {
		t.Fatal(err)
	}
	got := LoadManaged()
	if len(got) != 3 || got[2] != "Claude_Personal" {
		t.Fatalf("want the two existing entries plus the new one, got %#v", got)
	}
}

func TestAddManagedOnFirstRunCreatesAOneEntryList(t *testing.T) {
	withStubbedManaged(t)
	if err := AddManaged("Claude_Work"); err != nil {
		t.Fatal(err)
	}
	got := LoadManaged()
	if len(got) != 1 || got[0] != "Claude_Work" {
		t.Fatalf("want just the added entry, got %#v", got)
	}
}

func TestAddManagedIsIdempotent(t *testing.T) {
	withStubbedManaged(t)
	if err := SetManaged([]string{"Claude"}); err != nil {
		t.Fatal(err)
	}
	if err := AddManaged("Claude"); err != nil {
		t.Fatal(err)
	}
	if got := LoadManaged(); len(got) != 1 {
		t.Fatalf("adding an entry that is already there must not duplicate it, got %#v", got)
	}
}

// The one that matters: a corrupt registry must stop the write, not be silently
// replaced. Before AddManaged existed this test's profile list ended up as
// ["Claude_New"] and every other account vanished from the panel.
func TestAddManagedRefusesToOverwriteACorruptRegistry(t *testing.T) {
	withStubbedManaged(t)
	const corrupt = `{"managed": ["Claude", "Claude_Work"` // truncated mid-write
	writeManagedRaw(t, corrupt)

	err := AddManaged("Claude_New")
	if err == nil {
		t.Fatal("want an error when the registry cannot be parsed, got nil")
	}
	if !strings.Contains(err.Error(), "managed.json") {
		t.Errorf("the error should name the file the user has to fix, got %q", err)
	}

	// And the damaged file must still be exactly as it was, so it can be repaired
	// by hand rather than having been replaced with a one-entry list.
	after, readErr := os.ReadFile(managedPath())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != corrupt {
		t.Fatalf("the registry was rewritten despite the refusal:\n%s", after)
	}
}

func TestRemoveManagedDropsOnlyTheNamedEntry(t *testing.T) {
	withStubbedManaged(t)
	if err := SetManaged([]string{"Claude", "Claude_Work", "Claude_Personal"}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveManaged("Claude_Work"); err != nil {
		t.Fatal(err)
	}
	got := LoadManaged()
	if len(got) != 2 || got[0] != "Claude" || got[1] != "Claude_Personal" {
		t.Fatalf("want the other two kept in order, got %#v", got)
	}
}

func TestRemoveManagedRefusesToOverwriteACorruptRegistry(t *testing.T) {
	withStubbedManaged(t)
	const corrupt = `{"managed": [`
	writeManagedRaw(t, corrupt)

	if err := RemoveManaged("Claude_Work"); err == nil {
		t.Fatal("want an error when the registry cannot be parsed, got nil")
	}
	after, readErr := os.ReadFile(managedPath())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != corrupt {
		t.Fatalf("the registry was rewritten despite the refusal:\n%s", after)
	}
}

// Removing something that is not there is a no-op rather than an error, so
// callers can prune unconditionally — and it must not rewrite the file, because
// a rewrite is a chance to lose data for no gain.
func TestRemoveManagedOnAnAbsentEntryChangesNothing(t *testing.T) {
	withStubbedManaged(t)
	if err := SetManaged([]string{"Claude"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(managedPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveManaged("Claude_NeverExisted"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(managedPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("removing an absent entry rewrote the registry")
	}
}
