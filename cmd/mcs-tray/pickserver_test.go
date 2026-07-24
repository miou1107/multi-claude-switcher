package main

import (
	"strings"
	"testing"
	"time"

	"github.com/miou1107/multi-claude-switcher/core"
)

func sampleAccounts() []core.ScannedAccount {
	return []core.ScannedAccount{
		{UUID: "11111111xxxx", Complete: true, HomeFolder: "Claude", Email: "first@example.com",
			Account: core.AccountTeam, Convos: 395, LastUpdated: time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC),
			Note: "Team account — conversations can't be synced"},
		{UUID: "22222222xxxx", Complete: true, HomeFolder: "Claude_Profile2", Email: "second@example.com",
			Account: core.AccountPersonal, Convos: 395, LastUpdated: time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)},
		{UUID: "33333333xxxx", Complete: false, Convos: 21,
			LastUpdated: time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC), Note: "Invalid account data"},
	}
}

func TestComputePreselect(t *testing.T) {
	accts := sampleAccounts()
	// first run (nil) → all complete pre-checked, ghost excluded
	all := computePreselect(accts, nil)
	if !all["Claude"] || !all["Claude_Profile2"] || all["33333333xxxx"] || len(all) != 2 {
		t.Fatalf("first-run preselect: %#v", all)
	}
	// managed present → only listed
	some := computePreselect(accts, []string{"Claude"})
	if !some["Claude"] || some["Claude_Profile2"] || len(some) != 1 {
		t.Fatalf("managed preselect: %#v", some)
	}
	// present-but-empty → none
	none := computePreselect(accts, []string{})
	if len(none) != 0 {
		t.Fatalf("empty preselect should be none: %#v", none)
	}
}

func TestRenderReviewHTML(t *testing.T) {
	accts := sampleAccounts()
	pre := map[string]bool{"Claude": true}
	out := renderReviewHTML(accts, pre, "tok123")

	// complete rows: checkbox with folder value; Claude checked, Profile2 unchecked
	if !strings.Contains(out, `name="folder" value="Claude" checked`) {
		t.Error("Claude should have a checked checkbox")
	}
	if !strings.Contains(out, `name="folder" value="Claude_Profile2"`) ||
		strings.Contains(out, `value="Claude_Profile2" checked`) {
		t.Error("Profile2 should have an unchecked checkbox")
	}
	// ghost row: greyed, NO checkbox for it
	if !strings.Contains(out, `class="ghost"`) {
		t.Error("ghost row should be greyed")
	}
	if strings.Contains(out, `value="33333333xxxx"`) {
		t.Error("ghost must not be selectable")
	}
	// token hidden field, escaping, self-contained, Team badge, note
	for _, want := range []string{
		`name="t" value="tok123"`, "first@example.com", "🏢 Team",
		"Invalid account data", "<table", "Confirm", "Cancel",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("page missing %q", want)
		}
	}
	// no external resources
	if strings.Contains(out, "http://") || strings.Contains(out, "https://") || strings.Contains(out, "src=") {
		t.Error("page must be self-contained (no external refs)")
	}
	// note with an em-dash and apostrophe is HTML-escaped safely (no raw <script> etc. — sanity)
	if strings.Contains(out, "<script") {
		t.Error("unexpected script tag")
	}
}
