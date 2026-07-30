// Renders the panel UI with placeholder accounts so docs/assets/panel.png can
// be regenerated whenever the interface changes. The README screenshot is
// produced from the shipped renderer rather than photographed, so it cannot
// drift from the real UI and never contains anyone's real account names.
//
// Usage:
//
//	go run ./scripts/gen-screenshot panel.html
//
// then capture it at 2x with headless Edge or Chrome:
//
//	msedge --headless --disable-gpu --hide-scrollbars \
//	  --force-device-scale-factor=2 --screenshot=docs/assets/panel.png \
//	  --window-size=400,347 file:///absolute/path/to/panel.html
//
// The window height is the content height, not the panel's real 540px: the
// live panel leaves empty space below the buttons, which only wastes room in a
// README. Add ~72px per extra account if you change the list below.
package main

import (
	"fmt"
	"os"

	"github.com/miou1107/multi-claude-switcher/internal/panelui"
)

// Placeholder accounts, chosen to show the two situations the README leads
// with (a work account alongside a personal one) plus a third to make the
// plan pills visibly different.
var accounts = []panelui.ProfileVM{
	{Folder: "Claude", Name: "Work", Plan: "Team", Current: true, SignedIn: true},
	{Folder: "Claude_Personal", Name: "Personal", Plan: "Max 20×", SignedIn: true},
	{Folder: "Claude_Side", Name: "Side project", Plan: "Pro", SignedIn: true},
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: gen-screenshot <output.html>")
		os.Exit(2)
	}
	if err := os.WriteFile(os.Args[1], []byte(panelui.RenderList(accounts, false, "")), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
