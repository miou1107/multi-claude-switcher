//go:build !windows

package main

// panel_other.go is the stub for platforms that do not host the WebView2
// panel — macOS uses cmd/mcs-menubar (NSPopover) and Linux/other are not
// shipped. runPanel should never be called on these platforms; if it is, the
// caller has misdispatched.

import "os"

func runPanel() {
	os.Exit(2)
}
