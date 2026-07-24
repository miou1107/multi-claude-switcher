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

	t.Run("first run (managed nil) pre-selects every complete account", func(t *testing.T) {
		labels, _, pre := selectablePick(accts, nil)
		if len(labels) != 2 {
			t.Fatalf("want 2 selectable, got %d", len(labels))
		}
		if len(pre) != 2 {
			t.Fatalf("first run must pre-select ALL complete accounts, got %d: %v", len(pre), pre)
		}
		for _, l := range labels {
			if strings.Contains(l, "f047dab6") {
				t.Fatal("ghost leaked into selectable labels")
			}
		}
	})

	t.Run("managed set (even non-nil empty) is honored as-is", func(t *testing.T) {
		labels, m, pre := selectablePick(accts, []string{"Claude"})
		if len(labels) != 2 {
			t.Fatalf("want 2 selectable, got %d", len(labels))
		}
		if len(pre) != 1 || m[pre[0]] != "Claude" {
			t.Fatalf("pre-select should map to managed folder Claude: pre=%v m=%v", pre, m)
		}
	})

	t.Run("managed empty slice (non-nil) pre-selects nothing", func(t *testing.T) {
		_, _, pre := selectablePick(accts, []string{})
		if len(pre) != 0 {
			t.Fatalf("non-nil empty managed must not pre-select all, got %d: %v", len(pre), pre)
		}
	})
}

func TestSelectablePickUniqueLabelsPerFolder(t *testing.T) {
	// Same UUID/email/account can legitimately be the live login of two
	// different profile folders; labels must stay unique per folder so
	// neither becomes unreachable via labelToFolder.
	accts := []core.ScannedAccount{
		{UUID: "035899b2", Complete: true, HomeFolder: "Claude", Email: "vincent@fontrip.com", Account: core.AccountTeam},
		{UUID: "035899b2", Complete: true, HomeFolder: "Claude_Profile2", Email: "vincent@fontrip.com", Account: core.AccountTeam},
	}
	labels, m, _ := selectablePick(accts, []string{})
	if len(labels) != 2 {
		t.Fatalf("want 2 selectable labels, got %d: %v", len(labels), labels)
	}
	if labels[0] == labels[1] {
		t.Fatalf("labels must be unique per folder, got duplicate: %q", labels[0])
	}
	if len(m) != 2 {
		t.Fatalf("label->folder map should have 2 entries (one per folder), got %d: %v", len(m), m)
	}
	folders := map[string]bool{}
	for _, l := range labels {
		folders[m[l]] = true
	}
	if !folders["Claude"] || !folders["Claude_Profile2"] {
		t.Fatalf("both folders should be reachable via distinct labels: %v", m)
	}
}
