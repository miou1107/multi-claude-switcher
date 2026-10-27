//go:build windows

package main

// panellinks_windows.go is the panel's side of claude:// links (issue #60):
// the account list with the notice and the foot naming Claude, and what the
// guide's button does. Kept out of panel_windows.go, which is long enough.

import (
	"log"
	"sync/atomic"
	"time"

	"github.com/miou1107/multi-claude-switcher/internal/panelui"
	"github.com/miou1107/multi-claude-switcher/platform"
)

// panelListPage renders the account list with everything it shows.
func panelListPage() string {
	version, edition := platform.ClaudeApp()
	return panelui.RenderListPage(panelui.ListVM{
		Profiles:      panelBuildProfiles(),
		CanAddAccount: newProfileSupported(),
		Status:        panelGetStatus(),
		Links:         platform.ClaudeLinkState(),
		Claude:        panelui.ClaudeAppVM{Version: version, Edition: edition},
	})
}

// linkPickPatience is how long the panel waits for the user to pick in
// Windows' window.
const linkPickPatience = 2 * time.Minute

// linkPickUntil is when the panel stops waiting for that pick, in Unix
// nanoseconds; 0 when it is not waiting.
var linkPickUntil atomic.Int64

// waitingForLinkPick reports whether Windows' window may be up. Clicking in it
// takes the focus from the panel, which would otherwise park the panel and
// lose the answer to what was picked.
func waitingForLinkPick() bool {
	return time.Now().UnixNano() < linkPickUntil.Load()
}

// askWindowsForLinkApp is the guide's button: make Windows ask, then watch for
// the user's pick and come back to the list when there is one.
func askWindowsForLinkApp() {
	linkPickUntil.Store(time.Now().Add(linkPickPatience).UnixNano())
	go func() {
		defer linkPickUntil.Store(0)
		if err := platform.AskWindowsForClaudeLinkApp(); err != nil {
			// Settings is the way left whatever went wrong; the log says what.
			log.Printf("could not make Windows ask which app opens claude:// links: %v", err)
			panelSetStatus(panelui.LinkGuideNoWindow)
			reloadPanel()
			return
		}
		for waitingForLinkPick() {
			switch s := platform.ClaudeLinkState(); s {
			case platform.ClaudeLinksViaSwitcher:
				log.Println("the user chose the switcher for claude:// links")
				panelSetStatus(panelui.LinksChosenStatus)
				panelState.SetView("list")
				reloadPanel()
				return
			case platform.ClaudeLinksOtherAppPicked:
				log.Println("the user picked another app for claude:// links")
				panelSetStatus("")
				panelState.SetView("list")
				reloadPanel()
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		log.Println("no app was picked for claude:// links")
	}()
}
