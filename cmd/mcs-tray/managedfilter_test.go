package main

import "testing"

func TestMenuIncludes(t *testing.T) {
	// First run (managed == nil): show dirs with a live login or MSIX-managed.
	if !menuIncludes(nil, "Claude", true, false) {
		t.Fatal("first-run: live login should show")
	}
	if menuIncludes(nil, "Claude-3p", false, false) {
		t.Fatal("first-run: no login, not managed → hide")
	}
	if !menuIncludes(nil, "Parked", false, true) {
		t.Fatal("first-run: MSIX-managed should show")
	}
	// Registry present: authoritative, ignores live-login.
	m := []string{"Claude"}
	if !menuIncludes(m, "Claude", false, false) {
		t.Fatal("registry: listed → show")
	}
	if menuIncludes(m, "Claude_Profile2", true, false) {
		t.Fatal("registry: not listed → hide even with live login")
	}
	// Present-but-empty registry hides everything (user unchecked all).
	if menuIncludes([]string{}, "Claude", true, false) {
		t.Fatal("empty registry → hide all")
	}
}
