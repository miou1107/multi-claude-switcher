package main

import (
	"strings"
	"testing"
)

const (
	testDefault = `C:\Users\u\AppData\Roaming\Claude`
	testWork    = `C:\Users\u\AppData\Roaming\ClaudeWork`
	testOther   = `C:\Users\u\AppData\Roaming\ClaudeOther`
	oldMain     = `"C:\Users\u\AppData\Local\AnthropicClaude\app-2.42304.0\claude.exe" --user-data-dir=C:\Users\u\AppData\Roaming\ClaudeWork`
	newMain     = `"C:\Users\u\AppData\Local\AnthropicClaude\app-2.42304.2\claude.exe" `
	newHelper   = `"C:\Users\u\AppData\Local\AnthropicClaude\app-2.42304.2\claude.exe" --type=renderer --user-data-dir="C:\Users\u\AppData\Roaming\Claude"`
)

func TestClaudeVersionInCmdLines(t *testing.T) {
	cases := []struct {
		lines []string
		want  string
	}{
		{[]string{oldMain}, "2.42304.0"},
		// The helper comes first in the list but is not the app's own process.
		{[]string{newHelper, newMain}, "2.42304.2"},
		{[]string{`C:\Users\u\AppData\Local\AnthropicClaude\APP-1.2.3\Claude.exe`}, "1.2.3"},
		{[]string{`"C:\Program Files\WindowsApps\Claude_1.0\app\claude.exe"`}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := claudeVersionInCmdLines(c.lines); got != c.want {
			t.Errorf("claudeVersionInCmdLines(%q) = %q, want %q", c.lines, got, c.want)
		}
	}
}

// The case Vin hit three times between 2026-10-15 and 2026-10-19: Claude was on
// the work account, updated itself, and its updater reopened it with no
// --user-data-dir, which lands on the default profile.
//
// Every step that is declined names its reason, so the log says why the user
// was left on the default account instead of saying nothing, which is what the
// macOS guard did the first night it missed a relaunch. Steps where nothing
// happened (same version, same accounts) say nothing: the watcher looks every
// 20 seconds and would otherwise fill the log.
func TestUpdateRelaunchTarget(t *testing.T) {
	before := claudeSnapshot{profiles: []string{testWork}, version: "2.42304.0"}
	afterUpdate := claudeSnapshot{profiles: []string{testDefault}, version: "2.42304.2"}

	cases := []struct {
		name       string
		prev       claudeSnapshot
		cur        claudeSnapshot
		recorded   string
		want       string
		wantReason string // "" means the step must be silent
	}{
		{"updater reopened on the default account", before, afterUpdate, testWork, testWork, ""},
		{"same version: the user opened the default account themselves",
			before, claudeSnapshot{profiles: []string{testDefault}, version: "2.42304.0"}, testWork, "", "same version"},
		{"user chose the default account in MCS", before, afterUpdate, testDefault, "", "chose the default account"},
		{"no record of what the user chose", before, afterUpdate, "", "", "no record"},
		{"recorded account was not the one open before the update",
			claudeSnapshot{profiles: []string{testDefault}, version: "2.42304.0"}, afterUpdate, testWork, "", "was not open before"},
		{"updater kept the right account", before, claudeSnapshot{profiles: []string{testWork}, version: "2.42304.2"}, testWork, "", "kept"},
		{"work account is open next to the default one",
			before, claudeSnapshot{profiles: []string{testDefault, testWork}, version: "2.42304.2"}, testWork, "", "not on the default account alone"},
		{"version could not be read from either side",
			claudeSnapshot{profiles: []string{testWork}}, claudeSnapshot{profiles: []string{testDefault}}, testWork, "", "version could not be read"},
		{"updater reopened on a third account, not the default one",
			before, claudeSnapshot{profiles: []string{testOther}, version: "2.42304.2"}, testWork, "", "not on the default account alone"},
		{"updater reopened on the default account next to a third one",
			before, claudeSnapshot{profiles: []string{testDefault, testOther}, version: "2.42304.2"}, testWork, "", "not on the default account alone"},
		{"version became unreadable while the work account stayed open",
			before, claudeSnapshot{profiles: []string{testWork}}, testWork, "", "could not be read on one side"},
		{"user closed the work window at the same version",
			claudeSnapshot{profiles: []string{testWork, testDefault}, version: "2.42304.0"},
			claudeSnapshot{profiles: []string{testDefault}, version: "2.42304.0"}, testWork, "", "not updated"},
		{"two instances listed in the other order",
			claudeSnapshot{profiles: []string{testWork, testDefault}, version: "2.42304.0"},
			claudeSnapshot{profiles: []string{testDefault, testWork}, version: "2.42304.0"}, testWork, "", ""},
		{"nothing seen before", claudeSnapshot{}, afterUpdate, testWork, "", ""},
		{"nothing running now", before, claudeSnapshot{}, testWork, "", ""},
		{"nothing changed", before, before, testWork, "", ""},
		{"user opened a second window at the same version",
			before, claudeSnapshot{profiles: []string{testWork, testDefault}, version: "2.42304.0"}, testWork, "", ""},
	}
	if got, reason := updateRelaunchTarget(before, afterUpdate, "", testWork); got != "" || reason != "" {
		t.Errorf("no default path: got %q / %q, want silence", got, reason)
	}
	for _, c := range cases {
		got, reason := updateRelaunchTarget(c.prev, c.cur, testDefault, c.recorded)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
		switch {
		case c.wantReason == "" && reason != "":
			t.Errorf("%s: expected silence, got reason %q", c.name, reason)
		case c.wantReason != "" && !strings.Contains(reason, c.wantReason):
			t.Errorf("%s: reason %q does not mention %q", c.name, reason, c.wantReason)
		}
		if got != "" && reason != "" {
			t.Errorf("%s: a restore comes with no reason to decline, got %q", c.name, reason)
		}
	}
}
