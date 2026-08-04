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

// Set writes text to the clipboard, returning only once it is there.
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
