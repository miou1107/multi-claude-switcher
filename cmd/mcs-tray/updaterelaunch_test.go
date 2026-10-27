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

// Issue #56. Squirrel closes the old main process before starting the new one,
// and for a moment only the old helpers are alive. They still carry the work
// account's --user-data-dir but no readable version, so a look that lands then
// sees the work account at version "". The look after that is the default
// account at the new version, and it has to be judged against the last look
// that could read a version, or the watcher declines and the user stays put.
func TestRelaunchWatchLookDuringTheSwap(t *testing.T) {
	looks := []claudeSnapshot{
		{profiles: []string{testWork}, version: "2.42304.0"},
		{profiles: []string{testWork}}, // old helpers only
		{profiles: []string{testDefault}, version: "2.42304.2"},
	}
	var w relaunchWatch
	for i, cur := range looks {
		got, reason := w.look(cur, testDefault, testWork)
		if i < len(looks)-1 {
			if got != "" || reason != "" {
				t.Errorf("look %d: got %q / %q, want silence while the update is under way", i, got, reason)
			}
			continue
		}
		if got != testWork {
			t.Errorf("look after the swap: got %q / %q, want %q", got, reason, testWork)
		}
	}
}

// A version that stays unreadable is not a swap, which takes seconds against a
// 20-second poll. The watcher says why once and then goes quiet, instead of
// writing the same line every 20 seconds.
func TestRelaunchWatchUnreadableVersionThatStays(t *testing.T) {
	var w relaunchWatch
	w.look(claudeSnapshot{profiles: []string{testWork}, version: "2.42304.0"}, testDefault, testWork)
	var reasons []string
	for i := 0; i < 4; i++ {
		got, reason := w.look(claudeSnapshot{profiles: []string{testWork}}, testDefault, testWork)
		if got != "" {
			t.Fatalf("look %d: got target %q, want none", i, got)
		}
		if reason != "" {
			reasons = append(reasons, reason)
		}
	}
	if len(reasons) != 1 || !strings.Contains(reasons[0], "could not be read") {
		t.Errorf("reasons = %q, want exactly one saying the version could not be read", reasons)
	}
}
