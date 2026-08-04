//go:build darwin

// Package clip puts text on the system clipboard, and waits for it to land.
//
// Waiting is the point, and the reason this is not a one-liner at each call
// site. Its caller opens a browser next; a browser that arrives first leaves the
// user pasting whatever they copied last into a public issue — content the
// program never saw and could not have masked.
package clip

import (
	"os/exec"
	"strings"
)

// Set writes text to the clipboard, returning only once it is there.
func Set(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
