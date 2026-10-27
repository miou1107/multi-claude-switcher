//go:build windows

package main

// claudelink_windows.go is what runs when Windows hands the switcher a
// claude:// link, once the user has chosen the switcher to open them (issue
// #60): mcs-tray.exe --open-claude-link "<link>". It is a process of its own,
// started by Windows beside the tray, which passes the link to the Claude of
// the right account and exits.

import (
	"log"

	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
)

const (
	// openClaudeLinkFlag is what the registered handler runs the switcher with.
	openClaudeLinkFlag = "--open-claude-link"
	// unregisterClaudeLinkFlag removes the handler; the uninstaller runs it.
	unregisterClaudeLinkFlag = "--unregister-claude-link"
)

// runClaudeLinkCommand handles the two command lines above, and reports
// whether args was one of them.
func runClaudeLinkCommand(args []string) bool {
	if len(args) < 2 || (args[1] != openClaudeLinkFlag && args[1] != unregisterClaudeLinkFlag) {
		return false
	}
	if closer, _, err := core.SetupLogging("mcs-link"); err == nil {
		defer closer.Close()
	}
	if args[1] == unregisterClaudeLinkFlag {
		if err := platform.UnregisterClaudeLinkHandler(); err != nil {
			log.Printf("could not remove the claude:// handler: %v", err)
		}
		return true
	}
	if len(args) < 3 {
		log.Printf("%s was given no link", openClaudeLinkFlag)
		return true
	}
	openClaudeLink(args[2])
	return true
}

// openClaudeLink passes link to the Claude of the account the user is on. See
// core.ClaudeLinkTarget for which that is.
func openClaudeLink(link string) {
	if !core.IsClaudeLink(link) {
		log.Printf("refusing to pass on %q: not a claude:// link", core.ClaudeLinkForLog(link))
		return
	}
	if platform.IsClaudeLinkChooser(link) {
		// The link the panel opened to make Windows ask: arriving here means
		// the user picked the switcher. Claude has nothing to do with it.
		log.Println("Windows now opens claude:// links with the switcher")
		return
	}
	w := platform.New().(*platform.WindowsPlatform)
	profiles, err := w.FindProfiles()
	if err != nil {
		log.Printf("could not list the profiles: %v", err)
	}
	running, err := w.DetectRunningProfiles()
	if err != nil {
		// Not knowing what runs still leaves the account the user was last on,
		// which is where a sign-in started from the switcher is.
		log.Printf("could not tell which accounts Claude has open: %v", err)
	}
	target := core.ClaudeLinkTarget(running, core.ActiveProfilePath(profiles))
	name := target
	if name == "" {
		name = "Claude's default"
	}
	if err := w.OpenClaudeLink(target, link); err != nil {
		log.Printf("could not pass %s to %s: %v", core.ClaudeLinkForLog(link), name, err)
		return
	}
	log.Printf("passed %s to %s", core.ClaudeLinkForLog(link), name)
}
