package core

import (
	"path/filepath"
	"testing"

	"github.com/miou1107/multi-claude-switcher/platform"
)

func withStubbedPending(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	orig := pendingPath
	pendingPath = func() string { return filepath.Join(dir, "pending.json") }
	t.Cleanup(func() { pendingPath = orig })
}

func TestPendingAbsentIsEmpty(t *testing.T) {
	withStubbedPending(t)
	if got := LoadPending(); len(got) != 0 {
		t.Fatalf("want no entries, got %+v", got)
	}
}

func TestPendingAddLoadRemove(t *testing.T) {
	withStubbedPending(t)
	if err := AddPending("Claude_Work", "uuid-a"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := AddPending("Claude_Personal", ""); err != nil {
		t.Fatalf("add empty uuid: %v", err)
	}
	got := LoadPending()
	if len(got) != 2 {
		t.Fatalf("want 2, got %+v", got)
	}
	if got[0].Folder != "Claude_Work" || got[0].ExpectUUID != "uuid-a" || got[0].CreatedAt == "" {
		t.Fatalf("entry0: %+v", got[0])
	}
	if got[1].ExpectUUID != "" {
		t.Fatalf("add path must allow an empty expectUUID: %+v", got[1])
	}
	if err := RemovePending("Claude_Work"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got = LoadPending()
	if len(got) != 1 || got[0].Folder != "Claude_Personal" {
		t.Fatalf("after remove: %+v", got)
	}
}

func TestPendingAddIsIdempotentPerFolder(t *testing.T) {
	withStubbedPending(t)
	_ = AddPending("Claude_Work", "uuid-a")
	_ = AddPending("Claude_Work", "uuid-b")
	got := LoadPending()
	if len(got) != 1 || got[0].ExpectUUID != "uuid-b" {
		t.Fatalf("re-adding a folder must replace it, got %+v", got)
	}
}

func TestRemovePendingMissingIsNotAnError(t *testing.T) {
	withStubbedPending(t)
	if err := RemovePending("nope"); err != nil {
		t.Fatalf("removing an absent folder must be a no-op, got %v", err)
	}
}

func TestStalePendingOnlyWhenSignedIn(t *testing.T) {
	// signedIn has a live login, so its pending entry has served its purpose.
	// waiting has no login yet. absent is not in the profile list at all, which is
	// the Store build between creating a profile and the app's first launch —
	// spec §3.3. Only the first is stale.
	dir := t.TempDir()
	signedIn := writeProfile(t, dir, "Claude_SignedIn", "uuid-a", nil)
	waiting := writeSignedOutProfile(t, dir, "Claude_Waiting")

	pending := []PendingProfile{
		{Folder: "Claude_SignedIn", ExpectUUID: "uuid-a"},
		{Folder: "Claude_Waiting", ExpectUUID: "uuid-b"},
		{Folder: "Claude_Absent", ExpectUUID: "uuid-c"},
	}
	got := StalePending(pending, []*platform.ProfileInfo{signedIn, waiting})

	if len(got) != 1 || got[0] != "Claude_SignedIn" {
		t.Fatalf("want only the signed-in folder stale, got %v", got)
	}
}

func TestStalePendingWhenSignedInAsSomeOtherAccount(t *testing.T) {
	// The user was told to sign in as one account and signed in as another. The
	// entry has still served its purpose: the profile is real and has a login, so
	// the row must stop asking for a sign-in. Whether the account was the expected
	// one is the duplicate warning's problem, not this registry's.
	dir := t.TempDir()
	p := writeProfile(t, dir, "Claude_Recovered", "some-other-uuid", nil)

	got := StalePending(
		[]PendingProfile{{Folder: "Claude_Recovered", ExpectUUID: "the-orphan-uuid"}},
		[]*platform.ProfileInfo{p})

	if len(got) != 1 || got[0] != "Claude_Recovered" {
		t.Fatalf("a signed-in profile is no longer pending whatever it signed in as, got %v", got)
	}
}
