package main

import (
	"errors"
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

func TestParseAutoSyncChoice(t *testing.T) {
	if parseAutoSyncChoice("", errors.New("cancelled")) != choiceCancel {
		t.Error("non-zero exit (cancel button) must map to choiceCancel")
	}
	if parseAutoSyncChoice("button returned:Enable\n", nil) != choiceEnable {
		t.Error("Enable button must map to choiceEnable")
	}
	if parseAutoSyncChoice("button returned:Enable, don't ask again\n", nil) != choiceEnableDontAsk {
		t.Error("don't-ask button must map to choiceEnableDontAsk")
	}
}
