//go:build darwin

// Package clip puts text on the system clipboard, and waits for it to land.
//
// Waiting is the point, and the reason this is not a one-liner at each call
// site. Its caller opens a browser next; a browser that arrives first leaves the
// user pasting whatever they copied last into a public issue — content the
// program never saw and could not have masked.
package clip

import (
	"os"
	"os/exec"
	"strings"
)

// Set writes text to the clipboard and waits for pbcopy to exit before
// returning.
//
// "Waits for pbcopy to exit" is the honest claim, not "waits for the write to
// land": Set trusts pbcopy's own exit code and does not read the clipboard
// back with pbpaste to confirm the bytes it wrote are the bytes now sitting
// there. A read-back was considered and left out — it would double the
// latency in front of the browser-open this blocks (see the package doc
// comment on why that ordering matters), for a check that only catches
// pbcopy lying about its own exit status, which is not a failure mode this
// codebase has ever observed. The failure mode that IS observed — a stripped
// LaunchServices locale making pbcopy silently discard non-ASCII input while
// still exiting 0 — is handled below by forcing the locale, not by reading
// back after the fact.
func Set(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	// A GUI-launched bundle (Dock, Spotlight, login item, updater relaunch) is
	// handed a stripped LaunchServices environment with no LANG/LC_*, so
	// pbcopy's locale is C. Under C, pbcopy silently discards any input
	// containing a non-ASCII rune -- multi-byte characters anywhere in the
	// text -- and still exits 0, so the clipboard ends up empty with no error
	// returned here. Folder names and pasted report content routinely contain
	// non-ASCII text, so this is not an edge case; force a UTF-8 locale.
	cmd.Env = append(os.Environ(), "LC_ALL=en_US.UTF-8")
	return cmd.Run()
}
