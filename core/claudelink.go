package core

import "strings"

// claude:// links reach the switcher instead of Claude Desktop on a Windows
// machine where the user chose the switcher to open them (issue #60). Claude
// from the Microsoft Store owns the protocol there, and opens every link on its
// default account, whichever account the user is signing in to. These functions
// decide what the switcher does with a link it is handed; the handing over
// itself is platform code.

// IsClaudeLink reports whether link is a claude:// link, the only kind the
// switcher passes on. Anything else on its command line is refused rather than
// handed to Claude, where an argument that starts with -- would be read as a
// switch.
func IsClaudeLink(link string) bool {
	return len(link) >= len("claude:") && strings.EqualFold(link[:len("claude:")], "claude:")
}

// ClaudeLinkForLog is link with everything from its query or fragment on cut
// off. A sign-in link carries there the code that signs the account in, which
// must not reach a log file or a bug report.
func ClaudeLinkForLog(link string) string {
	if i := strings.IndexAny(link, "?#"); i >= 0 {
		return link[:i]
	}
	return link
}

// ClaudeLinkTarget returns the profile a claude:// link is handed to: the
// account the user is on, decided the way a switch decides it, or the one the
// switcher last opened when Claude is not running. An empty string means
// Claude's own default, started with no --user-data-dir.
//
// One account, never all the open ones. A Google sign-in would tolerate being
// sent to all of them, since only the Claude that started it accepts it, but
// an email link or single sign-on is taken by whoever receives it. And on a
// machine with both the Store build and the claude.com build, "the default
// account" names a different Claude for each, so sending to every open account
// can start a Claude nobody asked for.
func ClaudeLinkTarget(running []string, lastActivated string) string {
	if len(running) == 0 {
		return lastActivated
	}
	return CurrentProfilePath(running, lastActivated)
}
