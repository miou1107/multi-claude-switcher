package core

import "testing"

func TestIsClaudeLink(t *testing.T) {
	for link, want := range map[string]bool{
		"claude://login/google-auth?code=x&hop_nonce=y": true,
		"CLAUDE://claude.ai/magic-link#token":           true,
		"claude:":                                       true,
		"https://claude.ai/login":                       false,
		"--user-data-dir=C:\\elsewhere":                 false,
		"":                                              false,
		"claudex://login":                               false,
	} {
		if got := IsClaudeLink(link); got != want {
			t.Errorf("IsClaudeLink(%q) = %v, want %v", link, got, want)
		}
	}
}

func TestClaudeLinkTarget(t *testing.T) {
	const (
		def  = `C:\Users\someone\AppData\Roaming\Claude`
		work = `C:\Users\someone\AppData\Roaming\Claude_Work`
		home = `C:\Users\someone\AppData\Roaming\Claude_Home`
	)
	cases := []struct {
		name          string
		running       []string
		lastActivated string
		want          string
	}{
		{"the account in use", []string{def, work}, work, work},
		{"the only open account", []string{work}, def, work},
		// The record names an account that is not open: what is open wins.
		{"record is stale", []string{home}, work, home},
		{"record is stale, several open", []string{home, def}, work, home},
		// Nothing open: the account the user was last on, and Claude's own
		// default when there is no record ("" means no --user-data-dir).
		{"nothing open, last account", nil, work, work},
		{"nothing open, no record", nil, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClaudeLinkTarget(c.running, c.lastActivated); got != c.want {
				t.Errorf("ClaudeLinkTarget(%q, %q) = %q, want %q", c.running, c.lastActivated, got, c.want)
			}
		})
	}
}

func TestClaudeLinkForLog(t *testing.T) {
	// A sign-in link carries the code that signs the account in, so the log
	// keeps which kind of link it was and nothing after it.
	for link, want := range map[string]string{
		"claude://login/google-auth?code=SECRET&hop_nonce=n1": "claude://login/google-auth",
		"claude://claude.ai/magic-link#SECRET":                "claude://claude.ai/magic-link",
		"claude://hotkey":                                     "claude://hotkey",
	} {
		if got := ClaudeLinkForLog(link); got != want {
			t.Errorf("ClaudeLinkForLog(%q) = %q, want %q", link, got, want)
		}
	}
}
