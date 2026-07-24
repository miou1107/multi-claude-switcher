package main

import (
	"strings"
	"testing"
	"time"

	"github.com/miou1107/multi-claude-switcher/core"
)

func TestRenderReviewTable(t *testing.T) {
	accts := []core.ScannedAccount{
		{UUID: "035899b2", Complete: true, HomeFolder: "Claude", Email: "vincent@fontrip.com",
			Account: core.AccountTeam, Convos: 395, LastUpdated: time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC),
			Note: "Team account — conversations can't be synced"},
		{UUID: "f047dab6", Complete: false, Convos: 21,
			LastUpdated: time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC), Note: "Invalid account data"},
	}
	out := renderReviewTable(accts)
	for _, want := range []string{"035899b2", "vincent@fontrip.com", "Complete", "Incomplete",
		"395", "2026-07-24", "Invalid account data", "Yes"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q in:\n%s", want, out)
		}
	}
}

func TestSelectablePick(t *testing.T) {
	accts := []core.ScannedAccount{
		{UUID: "035899b2", Complete: true, HomeFolder: "Claude", Email: "vincent@fontrip.com", Account: core.AccountTeam},
		{UUID: "ae543f88", Complete: true, HomeFolder: "Claude_Profile2", Email: "b@x.com", Account: core.AccountPersonal},
		{UUID: "f047dab6", Complete: false}, // ghost — must NOT be selectable
	}
	labels, m, pre := selectablePick(accts, []string{"Claude"})
	if len(labels) != 2 {
		t.Fatalf("want 2 selectable, got %d", len(labels))
	}
	if len(pre) != 1 || m[pre[0]] != "Claude" {
		t.Fatalf("pre-select should map to managed folder Claude: pre=%v m=%v", pre, m)
	}
	for _, l := range labels {
		if strings.Contains(l, "f047dab6") {
			t.Fatal("ghost leaked into selectable labels")
		}
	}
}
