package main

import "testing"

const (
	testDefault = `C:\Users\u\AppData\Roaming\Claude`
	testWork    = `C:\Users\u\AppData\Roaming\ClaudeWork`
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
func TestUpdateRelaunchTarget(t *testing.T) {
	before := claudeSnapshot{profiles: []string{testWork}, version: "2.42304.0"}
	afterUpdate := claudeSnapshot{profiles: []string{testDefault}, version: "2.42304.2"}

	cases := []struct {
		name     string
		prev     claudeSnapshot
		cur      claudeSnapshot
		recorded string
		want     string
	}{
		{"updater reopened on the default account", before, afterUpdate, testWork, testWork},
		{"same version: the user opened the default account themselves",
			before, claudeSnapshot{profiles: []string{testDefault}, version: "2.42304.0"}, testWork, ""},
		{"user chose the default account in MCS", before, afterUpdate, testDefault, ""},
		{"no record of what the user chose", before, afterUpdate, "", ""},
		{"recorded account was not the one open before the update",
			claudeSnapshot{profiles: []string{testDefault}, version: "2.42304.0"}, afterUpdate, testWork, ""},
		{"updater kept the right account", before, claudeSnapshot{profiles: []string{testWork}, version: "2.42304.2"}, testWork, ""},
		{"work account is open next to the default one",
			before, claudeSnapshot{profiles: []string{testDefault, testWork}, version: "2.42304.2"}, testWork, ""},
		{"nothing seen before", claudeSnapshot{}, afterUpdate, testWork, ""},
		{"nothing running now", before, claudeSnapshot{}, testWork, ""},
	}
	for _, c := range cases {
		if got := updateRelaunchTarget(c.prev, c.cur, testDefault, c.recorded); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
