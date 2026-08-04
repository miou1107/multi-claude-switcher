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
	msg := autoSyncWarningMessage()
	if msg == "" {
		t.Fatal("base message empty")
	}
	// A Team account used to get an extra warning that conversations could not be
	// imported into it. That was wrong: they can, once they are filed under the
	// organization the account is signed in to. Warning about it again would talk
	// users out of a sync that works.
	if strings.Contains(msg, "cannot be imported") {
		t.Errorf("the Team import caveat is no longer true and must not be shown: %q", msg)
	}
}
