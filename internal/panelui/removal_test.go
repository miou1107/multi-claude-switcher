package panelui

import (
	"errors"
	"testing"
)

// TestDecideRemovalOutcomeCleanRemoval pins the branch that used to differ
// between the two hosts only by convention: a clean removal (folder moved,
// no error) shows the list with a banner and builds no RemovedVM at all.
// Reverting either host's routing to the old two-way switch (always drawing
// a "removed" screen) used to pass the whole suite regardless, because
// nothing tested the decision directly. This does.
func TestDecideRemovalOutcomeCleanRemoval(t *testing.T) {
	out := DecideRemovalOutcome("Claude_Old", "Old one", 34, "/archive/Claude_Old", nil)
	if !out.ShowList {
		t.Fatalf("a clean removal must show the list, not a result screen: %+v", out)
	}
	if out.ListStatus != "Old one removed. Nothing was deleted." {
		t.Fatalf("unexpected list status: %q", out.ListStatus)
	}
	if out.Removed != (RemovedVM{}) {
		t.Fatalf("a clean removal must not build a RemovedVM at all, got %+v", out.Removed)
	}
}

// TestDecideRemovalOutcomeMovedButComplained pins the partial-failure branch:
// the folder DID move (dest is non-empty) but something afterward could not
// be cleared. This must route to the "removed" screen with RegistryNote, not
// the "nothing was moved" failure screen — that screen would send the user
// looking for an account that has, in fact, already moved.
func TestDecideRemovalOutcomeMovedButComplained(t *testing.T) {
	out := DecideRemovalOutcome("Claude_Old", "Old one", 34, "/archive/Claude_Old",
		errors.New("write managed.json: permission denied"))
	if out.ShowList {
		t.Fatalf("a partial failure must not show the plain list banner: %+v", out)
	}
	want := RemovedVM{Folder: "Claude_Old", Name: "Old one", Convos: 34, RegistryNote: "write managed.json: permission denied"}
	if out.Removed != want {
		t.Fatalf("got %+v, want %+v", out.Removed, want)
	}
}

// TestDecideRemovalOutcomeDidNotMove pins the outright-failure branch: dest is
// empty, so nothing moved at all, and the reason goes on Err, not
// RegistryNote — the two read as different things on the result screen (see
// RenderRemoved).
func TestDecideRemovalOutcomeDidNotMove(t *testing.T) {
	out := DecideRemovalOutcome("Claude_Old", "Old one", 34, "",
		errors.New("couldn't archive Old one: Claude may still be holding its files."))
	if out.ShowList {
		t.Fatalf("a failed removal must not show the plain list banner: %+v", out)
	}
	want := RemovedVM{Folder: "Claude_Old", Name: "Old one", Convos: 34, Err: "couldn't archive Old one: Claude may still be holding its files."}
	if out.Removed != want {
		t.Fatalf("got %+v, want %+v", out.Removed, want)
	}
}

// TestDecideRemovalOutcomeDidNotMoveWithNoError pins the fallback for a
// caller that breaks core.RemoveProfile's own contract (dest == "" always
// pairing with a non-nil error) and passes ("", nil) instead. This function's
// whole purpose is to stop hosts from carrying assumptions like that
// themselves, so it must not carry the same assumption about its own
// caller: err.Error() on a nil err would panic inside a render path neither
// host recovers around, which is worse than the RenderRemoved panic already
// rejected elsewhere in this history — that one at least carried a message.
// A generic, readable Err is the fallback instead of a crash.
func TestDecideRemovalOutcomeDidNotMoveWithNoError(t *testing.T) {
	out := DecideRemovalOutcome("Claude_Old", "Old one", 34, "", nil)
	if out.ShowList {
		t.Fatalf("dest is empty, so this must not show the plain list banner: %+v", out)
	}
	if out.Removed.Err == "" {
		t.Fatal("a nil err must still produce a non-empty, readable Err, not an empty string")
	}
	want := RemovedVM{Folder: "Claude_Old", Name: "Old one", Convos: 34, Err: "The removal failed, and no reason was given. Try again."}
	if out.Removed != want {
		t.Fatalf("got %+v, want %+v", out.Removed, want)
	}
}
