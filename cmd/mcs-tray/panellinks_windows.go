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

// panelLinkGuide renders the guide behind the notice's button.
func panelLinkGuide() string {
	return panelui.RenderLinkGuide(panelGetStatus(), platform.ClaudeLinkDisplayName())
}

const (
	// linkPickPatience is how long a press of the guide's button watches for
	// the user's pick.
	linkPickPatience = 2 * time.Minute
	// linkPickSettle is how long a pick gets to show up once Windows' window
	// has closed, before the window counts as closed without one.
	linkPickSettle = 2 * time.Second
)

var (
	// linkPickGen numbers the presses of the guide's button. A watch belongs
	// to one press, and a newer press takes over from it.
	linkPickGen atomic.Int64
	// linkChooserUp is set while Windows' window may be on screen.
	linkChooserUp atomic.Bool
)

// waitingForLinkPick reports whether Windows' window may be up. Clicking in it
// takes the focus from the panel, which would otherwise park the panel and
// lose the answer to what was picked.
func waitingForLinkPick() bool {
	return linkChooserUp.Load()
}

// askWindowsForLinkApp is the guide's button: make Windows ask, then watch for
// the user's pick.
func askWindowsForLinkApp() {
	gen := linkPickGen.Add(1)
	linkChooserUp.Store(true)
	go watchLinkPick(gen)
}

// watchLinkPick follows one press of the guide's button. It returns to the
// list once the user has picked, and lets the panel close normally again as
// soon as Windows' window is gone, picked or not.
func watchLinkPick(gen int64) {
	current := func() bool { return linkPickGen.Load() == gen }
	// Only this press's watch may stand the panel down: a newer press has
	// Windows' window up again.
	release := func() {
		if current() {
			linkChooserUp.Store(false)
		}
	}

	sawChooser := true
	if err := platform.AskWindowsForClaudeLinkApp(); err != nil {
		// Settings is the way left whatever went wrong; the log says what. The
		// watch goes on, since Windows may yet show its window late or under a
		// name not measured, but it no longer holds the panel up.
		log.Printf("could not make Windows ask which app opens claude:// links: %v", err)
		sawChooser = false
		release()
		if current() {
			panelSetStatus(panelui.LinkGuideNoWindow)
			reloadPanel()
		}
	}

	var closedAt time.Time
	for deadline := time.Now().Add(linkPickPatience); time.Now().Before(deadline) && current(); time.Sleep(500 * time.Millisecond) {
		switch platform.ClaudeLinkState() {
		case platform.ClaudeLinksViaSwitcher:
			log.Println("the user chose the switcher for claude:// links")
			showLinkPick(panelui.LinksChosenStatus, release)
			return
		case platform.ClaudeLinksOtherAppPicked:
			log.Println("the user picked another app for claude:// links")
			showLinkPick("", release)
			return
		}
		if !sawChooser {
			continue
		}
		if platform.ClaudeLinkChooserOpen() {
			closedAt = time.Time{}
			continue
		}
		if closedAt.IsZero() {
			closedAt = time.Now()
			continue
		}
		if time.Since(closedAt) >= linkPickSettle {
			log.Println("Windows' window for claude:// links closed without a pick")
			release()
			reclaimPanelFocus()
			return
		}
	}
	release()
}

// showLinkPick returns to the list with what the user picked.
func showLinkPick(status string, release func()) {
	release()
	panelSetStatus(status)
	panelState.SetView("list")
	reloadPanel()
	reclaimPanelFocus()
}

// reclaimPanelFocus gives the panel the focus back once Windows' window has
// gone, so that a click outside it closes it again. A panel that cannot get it
// back is parked rather than left on top of whatever the user went back to.
func reclaimPanelFocus() {
	hwnd := panelHWND.Load()
	if hwnd == 0 || !panelOnScreen.Load() {
		return
	}
	panelWV.Dispatch(func() { forcePanelForeground(hwnd) })
	time.Sleep(300 * time.Millisecond)
	if fg, _, _ := procGetForegroundWindow.Call(); fg != hwnd && panelOnScreen.Load() {
		log.Println("panel could not take the focus back after Windows' window; parking it")
		panelWV.Dispatch(func() { parkPanel(hwnd) })
	}
}
