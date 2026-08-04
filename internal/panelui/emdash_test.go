package panelui

import (
	"regexp"
	"strings"
	"testing"
)

// TestNoEmDashInUserFacingText pins a project rule the review process kept
// missing because nothing checked it: user-facing English carries no em dash.
// It slipped in through a wording fix that was itself correcting a different
// inaccuracy, and only turned up when someone looked at the screen.
//
// Tags and their attributes are stripped first, so this reads what a user
// reads. The report the caller passes in is data, not our copy.
func TestNoEmDashInUserFacingText(t *testing.T) {
	tags := regexp.MustCompile(`(?s)<[^>]*>`)
	views := map[string]string{
		"debug":    RenderDebug(DebugVM{Report: "MCS 0.11.2", Comment: "typed"}),
		"settings": RenderSettings(SettingsVM{Version: "0.11.2"}),
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
