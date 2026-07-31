//go:build windows

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeProfileDir creates dir with a config.json holding marker, standing in for
// a Claude data dir so we can assert which profile's data ends up in the slot.
func writeProfileDir(t *testing.T, dir, marker string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readMarker(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("reading marker in %s: %v", dir, err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// TestMSIXLifecycle walks the full create-then-switch flow on a temp roaming dir:
// start with a bare slot, add a new profile (parks the original, blanks the slot),
// let the new one accrue data, then switch back and forth verifying the right
// data lands in the slot each time and no data is lost.
func TestMSIXLifecycle(t *testing.T) {
	roaming := t.TempDir()
	slot := msixSlotDir(roaming)

	// Initial state: the bare slot holds account A; no state file yet.
	writeProfileDir(t, slot, "A")
	if got := readMSIXStateIn(roaming).Current; got != msixDefaultName {
		t.Fatalf("default current = %q, want %q", got, msixDefaultName)
	}

	// Add a new profile "Work": A is parked as .mcs-profiles\Claude, slot is empty.
	if err := msixParkForNewIn(roaming, "Work"); err != nil {
		t.Fatalf("park for new: %v", err)
	}
	if got := readMSIXStateIn(roaming).Current; got != "Work" {
		t.Fatalf("current after new = %q, want Work", got)
	}
	if exists(slot) {
		t.Fatal("slot should be absent after creating a fresh profile")
	}
	parkedA := filepath.Join(msixContainerDir(roaming), "Claude")
	if !exists(parkedA) || readMarker(t, parkedA) != "A" {
		t.Fatal("original account A was not parked intact")
	}

	// Work signs in: fresh slot data "B".
	writeProfileDir(t, slot, "B")

	// Switch back to the original "Claude": slot must become A, Work parked as B.
	if err := msixSwapToIn(roaming, "Claude"); err != nil {
		t.Fatalf("swap to Claude: %v", err)
	}
	if got := readMSIXStateIn(roaming).Current; got != "Claude" {
		t.Fatalf("current = %q, want Claude", got)
	}
	if readMarker(t, slot) != "A" {
		t.Fatalf("slot after switch = %q, want A", readMarker(t, slot))
	}
	parkedWork := filepath.Join(msixContainerDir(roaming), "Work")
	if !exists(parkedWork) || readMarker(t, parkedWork) != "B" {
		t.Fatal("Work (B) was not parked intact")
	}

	// Switch to Work again: slot back to B.
	if err := msixSwapToIn(roaming, "Work"); err != nil {
		t.Fatalf("swap to Work: %v", err)
	}
	if readMarker(t, slot) != "B" {
		t.Fatalf("slot after second switch = %q, want B", readMarker(t, slot))
	}
}

// TestMSIXSwapToMissingKeepsSlot ensures a switch to a non-existent profile fails
// without moving (or losing) the current slot.
func TestMSIXSwapToMissingKeepsSlot(t *testing.T) {
	roaming := t.TempDir()
	slot := msixSlotDir(roaming)
	writeProfileDir(t, slot, "A")

	if err := msixSwapToIn(roaming, "Ghost"); err == nil {
		t.Fatal("expected error switching to a non-existent profile")
	}
	if !exists(slot) || readMarker(t, slot) != "A" {
		t.Fatal("slot must be untouched after a failed switch")
	}
}

// TestMSIXSwapToCurrentIsNoop ensures switching to the already-active profile does
// nothing and errors on nothing.
func TestMSIXSwapToCurrentIsNoop(t *testing.T) {
	roaming := t.TempDir()
	writeProfileDir(t, msixSlotDir(roaming), "A")
	if err := msixSwapToIn(roaming, msixDefaultName); err != nil {
		t.Fatalf("swap to current should be a no-op, got %v", err)
	}
	if readMarker(t, msixSlotDir(roaming)) != "A" {
		t.Fatal("slot changed on a no-op switch")
	}
}

func TestMSIXValidateName(t *testing.T) {
	roaming := t.TempDir()
	writeProfileDir(t, msixSlotDir(roaming), "A") // current defaults to "Claude"
	writeProfileDir(t, filepath.Join(msixContainerDir(roaming), "Work"), "B")

	bad := map[string]string{
		"empty":        "",
		"reserved":     "Claude",
		"existing":     "Work",
		"path sep":     `a\b`,
		"colon":        "a:b",
		"leading dot":  ".hidden",
		"current name": "claude", // case-insensitive match of the reserved/current
	}
	for label, name := range bad {
		if err := msixValidateNameIn(roaming, name); err == nil {
			t.Errorf("%s: expected %q to be rejected", label, name)
		}
	}
	if err := msixValidateNameIn(roaming, "Personal"); err != nil {
		t.Errorf("valid name rejected: %v", err)
	}
}

// TestMSIXMigration verifies the first-login migration copies the new account's
// previously saved sessions from the parked source profile into the fresh slot.
func TestMSIXMigration(t *testing.T) {
	roaming := t.TempDir()
	uuidA := "11111111-1111-4111-8111-111111111111"

	// Parked source profile "AcctB" holds account A's old sessions under its bucket.
	fromBucket := filepath.Join(msixContainerDir(roaming), "AcctB", "claude-code-sessions", uuidA)
	if err := os.MkdirAll(filepath.Join(fromBucket, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fromBucket, "s1.json"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fromBucket, "sub", "s2.json"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Fresh slot, now signed into account A (config.json advertises its UUID).
	if err := os.MkdirAll(msixSlotDir(roaming), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(msixSlotDir(roaming), "config.json"),
		[]byte(`{"lastKnownAccountUuid":"`+uuidA+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeMSIXStateIn(roaming, msixState{Current: "AcctA", PendingMigrateFrom: "AcctB"}); err != nil {
		t.Fatal(err)
	}

	copied, done := msixAttemptMigrationIn(roaming)
	if !done {
		t.Fatal("migration should be done once the account is signed in")
	}
	if copied != 2 {
		t.Fatalf("copied = %d, want 2", copied)
	}
	dst := filepath.Join(msixSlotDir(roaming), "claude-code-sessions", uuidA)
	if !exists(filepath.Join(dst, "s1.json")) || !exists(filepath.Join(dst, "sub", "s2.json")) {
		t.Fatal("migrated session files are missing from the slot")
	}
	if readMSIXStateIn(roaming).PendingMigrateFrom != "" {
		t.Fatal("pending-migration flag should be cleared after migrating")
	}
}

// TestMSIXMigrationWaitsForSignIn ensures the migration does not fire (and the
// flag is kept) until the fresh account is actually signed in.
func TestMSIXMigrationWaitsForSignIn(t *testing.T) {
	roaming := t.TempDir()
	if err := os.MkdirAll(msixSlotDir(roaming), 0o755); err != nil { // no config.json yet
		t.Fatal(err)
	}
	if err := writeMSIXStateIn(roaming, msixState{Current: "AcctA", PendingMigrateFrom: "AcctB"}); err != nil {
		t.Fatal(err)
	}
	if _, done := msixAttemptMigrationIn(roaming); done {
		t.Fatal("migration must wait until the account is signed in")
	}
	if readMSIXStateIn(roaming).PendingMigrateFrom == "" {
		t.Fatal("pending flag should remain until the migration runs")
	}
}

// TestMSIXParkForNewQueuesMigration verifies creating a profile queues a migration
// from the profile it parked.
func TestMSIXParkForNewQueuesMigration(t *testing.T) {
	roaming := t.TempDir()
	writeProfileDir(t, msixSlotDir(roaming), "A") // current defaults to "Claude"
	if err := msixParkForNewIn(roaming, "Work"); err != nil {
		t.Fatalf("park for new: %v", err)
	}
	if got := readMSIXStateIn(roaming).PendingMigrateFrom; got != "Claude" {
		t.Fatalf("PendingMigrateFrom = %q, want Claude", got)
	}
}

func TestMSIXStateRoundTrip(t *testing.T) {
	roaming := t.TempDir()
	if err := writeMSIXStateIn(roaming, msixState{Current: "Personal"}); err != nil {
		t.Fatal(err)
	}
	if got := readMSIXStateIn(roaming).Current; got != "Personal" {
		t.Fatalf("round-trip current = %q, want Personal", got)
	}
}

// TestMSIXCreateProfileIdentityIsNotTheDirectoryName pins the rule the whole
// identity model rests on: on this build a profile's name and its directory name
// are different things. Anything deriving the identity from the path gets "Claude"
// for every profile and addresses one that FindProfiles never reports.
func TestMSIXCreateProfileIdentityIsNotTheDirectoryName(t *testing.T) {
	roaming := t.TempDir()
	slot := filepath.Join(roaming, "Claude")
	if err := os.MkdirAll(slot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := msixParkForNewIn(roaming, "Work"); err != nil {
		t.Fatalf("park: %v", err)
	}
	if got := readMSIXStateIn(roaming).Current; got != "Work" {
		t.Fatalf("state.json current = %q, want %q", got, "Work")
	}
	if filepath.Base(slot) == "Work" {
		t.Fatal("this test is meaningless if the slot is named after the profile")
	}
	// And the resolver maps the identity back to the slot, not to a parked dir.
	if got := msixProfilePath(roaming, "Work"); got != slot {
		t.Fatalf("msixProfilePath(%q) = %q, want the slot %q", "Work", got, slot)
	}
}

func TestMSIXParkTrimsTheName(t *testing.T) {
	roaming := t.TempDir()
	if err := os.MkdirAll(filepath.Join(roaming, "Claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := msixParkForNewIn(roaming, "  Work  "); err != nil {
		t.Fatalf("park: %v", err)
	}
	if got := readMSIXStateIn(roaming).Current; got != "Work" {
		t.Fatalf("state.json current = %q, want it trimmed", got)
	}
}

func TestMSIXProfilePathResolvesAParkedProfile(t *testing.T) {
	roaming := t.TempDir()
	if err := writeMSIXStateIn(roaming, msixState{Current: "Work"}); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(msixContainerDir(roaming), "Personal")
	if got := msixProfilePath(roaming, "Personal"); got != want {
		t.Fatalf("msixProfilePath = %q, want %q", got, want)
	}
}

// TestMSIXFindProfilesListsTheSlotProfileWhenTheSlotIsAbsent covers exactly the
// state msixParkForNewIn leaves behind: state.json names a profile and its
// directory does not exist yet. Dropping it there is what made a just-created
// profile vanish from the account list before the user could sign in.
func TestMSIXFindProfilesListsTheSlotProfileWhenTheSlotIsAbsent(t *testing.T) {
	roaming := t.TempDir()
	if err := writeMSIXStateIn(roaming, msixState{Current: "Work"}); err != nil {
		t.Fatal(err)
	}
	w := &WindowsPlatform{}
	got, err := w.msixFindProfilesIn(roaming)
	if err != nil {
		t.Fatalf("msixFindProfilesIn: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want the current profile listed, got %+v", got)
	}
	if got[0].Name != "Work" {
		t.Fatalf("name = %q, want the state.json name", got[0].Name)
	}
	if got[0].Exists {
		t.Fatal("a profile with no directory must report Exists false")
	}
	if !got[0].Managed {
		t.Fatal("Store profiles are MCS-managed, so it must stay listed with no data")
	}
}

// The other side of that rule. readMSIXStateIn returns Current="Claude" when there
// is no state.json, so listing an absent slot unconditionally invents a profile on
// any machine where MCS has not run yet — shown in the account list, marked as
// awaiting a sign-in the user can never complete, for a folder that does not exist.
func TestMSIXFindProfilesInventsNothingWhenMCSHasNeverRunHere(t *testing.T) {
	roaming := t.TempDir() // no state.json, no slot directory
	w := &WindowsPlatform{}
	got, err := w.msixFindProfilesIn(roaming)
	if err != nil {
		t.Fatalf("msixFindProfilesIn: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want no profiles before MCS has recorded any state, got %+v", got[0])
	}
}

// The failure this whole mechanism exists for. Parking the live profile works,
// then Claude gets relaunched and recreates its data directory in the slot, and
// the rename that should move the target in lands on an existing directory —
// which Windows refuses with a bare "Access is denied". Both the activation and
// its rollback hit it, so the user is left with their profile parked under a
// name they never chose and an empty slot.
func TestMSIXActivateMovesARecreatedSlotAside(t *testing.T) {
	roaming := t.TempDir()
	slot := msixSlotDir(roaming)
	source := filepath.Join(msixContainerDir(roaming), "Work")
	writeProfileDir(t, slot, "recreated-by-claude")
	writeProfileDir(t, source, "work-data")

	if err := msixActivate(roaming, source); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if got := readMarker(t, slot); got != "work-data" {
		t.Errorf("slot holds %q, want the activated profile", got)
	}
	if exists(source) {
		t.Error("the source should have been moved, not copied")
	}
	stray := filepath.Join(roaming, msixStrayPrefix+"1")
	if !exists(stray) {
		t.Fatalf("what was in the slot must be kept, not deleted; nothing at %s", stray)
	}
	if got := readMarker(t, stray); got != "recreated-by-claude" {
		t.Errorf("stray holds %q, want what Claude had recreated", got)
	}
}

func TestMSIXActivateIntoAFreeSlotLeavesNoStray(t *testing.T) {
	roaming := t.TempDir()
	source := filepath.Join(msixContainerDir(roaming), "Work")
	writeProfileDir(t, source, "work-data")

	if err := msixActivate(roaming, source); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if got := readMarker(t, msixSlotDir(roaming)); got != "work-data" {
		t.Errorf("slot holds %q", got)
	}
	if exists(filepath.Join(roaming, msixStrayPrefix+"1")) {
		t.Error("nothing was in the way, so no stray should have been created")
	}
}

// A second stray must not clobber the first: each one is somebody's data.
func TestMSIXStrayDirNumbersPastWhatIsAlreadyThere(t *testing.T) {
	roaming := t.TempDir()
	if err := os.MkdirAll(filepath.Join(roaming, msixStrayPrefix+"1"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := msixStrayDir(roaming)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(roaming, msixStrayPrefix+"2"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Strays live in <roaming> rather than the profile container precisely so they
// stay out of the account list. Put one where a careless fix would put it and
// the user gets a profile called ".mcs-stray-1" they never made.
func TestMSIXStrayIsNotListedAsAProfile(t *testing.T) {
	roaming := t.TempDir()
	writeProfileDir(t, msixSlotDir(roaming), "live")
	if err := writeMSIXStateIn(roaming, msixState{Current: "Claude"}); err != nil {
		t.Fatal(err)
	}
	writeProfileDir(t, filepath.Join(roaming, msixStrayPrefix+"1"), "moved-aside")

	w := &WindowsPlatform{}
	got, err := w.msixFindProfilesIn(roaming)
	if err != nil {
		t.Fatalf("msixFindProfilesIn: %v", err)
	}
	for _, p := range got {
		if strings.HasPrefix(p.Name, msixStrayPrefix) {
			t.Fatalf("a stray was listed as the profile %q", p.Name)
		}
	}
	if len(got) != 1 || got[0].Name != "Claude" {
		t.Fatalf("want just the slot profile, got %+v", got)
	}
}
