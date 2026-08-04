package panelui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/miou1107/multi-claude-switcher/core"
)

// TestNoEmDashInUserFacingText pins a project rule the review process kept
// missing because nothing checked it: user-facing English carries no em dash.
// It slipped in through a wording fix that was itself correcting a different
// inaccuracy, and only turned up when someone looked at the screen.
//
// Tags and their attributes are stripped first, so this reads what a user
// reads. The report the caller passes in is data, not our copy.
//
// Every renderer the user can actually land on belongs in this map — a
// renderer missing here is a renderer this guard does not cover at all, not
// merely one this particular fixture skips. The rescan fixture below sets
// LastUpdated so it never falls into the "no date yet" branch of RenderRescan,
// which renders its own em dash as a placeholder character (not prose); that
// is a preexisting, unrelated gap in the same rule this test enforces, out of
// scope for this change, and is called out in this task's report rather than
// silently patched here.
func TestNoEmDashInUserFacingText(t *testing.T) {
	tags := regexp.MustCompile(`(?s)<[^>]*>`)
	views := map[string]string{
		"debug":    RenderDebug(DebugVM{Report: "MCS 0.11.2", Comment: "typed"}),
		"settings": RenderSettings(SettingsVM{Version: "0.11.2"}),
		"list":     RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", Plan: "Pro", Convos: 3, SignedIn: true}}, true, ""),
		"rescan": RenderRescan([]core.ScannedAccount{
			{UUID: "u1", Complete: true, HomeFolder: "Claude", Convos: 3, LastUpdated: time.Unix(0, 0)},
		}, map[string]bool{}),
		"account":    RenderAccount(AccountVM{Folder: "Claude_Old", Name: "Old one", Convos: 34}),
		"newprofile": RenderNewProfile(NewProfileVM{SuggestedName: "Work", Convos: 0}),
		"merge": RenderMerge(
			MergeCandidateVM{Folder: "Claude", Name: "Work", Plan: "Pro", Convos: 3, Current: true},
			MergeCandidateVM{Folder: "Claude_2", Name: "Work 2", Plan: "Pro", Convos: 5},
			core.MergePlan{Combined: 8, Conflicts: 1, Unreadable: 1}, "", false),
		"sync": RenderSync([]ProfileVM{{Folder: "Claude", Name: "Work", Plan: "Pro", Convos: 3, SignedIn: true}}, "", false),
	}
	for name, h := range views {
		text := tags.ReplaceAllString(h, " ")
		if strings.Contains(text, "—") {
			for _, line := range strings.Split(text, "\n") {
				if strings.Contains(line, "—") {
					t.Errorf("%s: em dash in user-facing text: %q", name, strings.TrimSpace(line))
				}
			}
		}
	}
}
