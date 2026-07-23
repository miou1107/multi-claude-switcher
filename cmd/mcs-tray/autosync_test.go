package main

import (
	"strings"
	"testing"
)

func TestShouldWarnAutoSync(t *testing.T) {
	cases := []struct{ enabling, dismissed, want bool }{
		{true, false, true},   // enabling, not dismissed -> warn
		{true, true, false},   // enabling, dismissed -> no warn
		{false, false, false}, // disabling -> never warn
		{false, true, false},
	}
	for _, c := range cases {
		if got := shouldWarnAutoSync(c.enabling, c.dismissed); got != c.want {
			t.Errorf("shouldWarnAutoSync(%v,%v)=%v want %v", c.enabling, c.dismissed, got, c.want)
		}
	}
}

func TestAutoSyncWarningMessage(t *testing.T) {
	base := autoSyncWarningMessage(nil)
	if base == "" {
		t.Fatal("base message empty")
	}
	withTeam := autoSyncWarningMessage([]string{"Company"})
	if withTeam == base {
		t.Error("expected an extra Team note when a Team profile is present")
	}
	if !strings.Contains(withTeam, "Company") || !strings.Contains(withTeam, "cannot be imported") {
		t.Errorf("Team note missing details: %q", withTeam)
	}
}
