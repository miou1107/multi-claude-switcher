//go:build windows

package platform

import (
	"os"
	"path/filepath"
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

func TestMSIXStateRoundTrip(t *testing.T) {
	roaming := t.TempDir()
	if err := writeMSIXStateIn(roaming, msixState{Current: "Personal"}); err != nil {
		t.Fatal(err)
	}
	if got := readMSIXStateIn(roaming).Current; got != "Personal" {
		t.Fatalf("round-trip current = %q, want Personal", got)
	}
}
