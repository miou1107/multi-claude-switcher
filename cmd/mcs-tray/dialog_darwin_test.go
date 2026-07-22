//go:build darwin

package main

import (
	"errors"
	"testing"
)

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
