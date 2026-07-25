//go:build !darwin

package main

import "github.com/miou1107/multi-claude-switcher/core"

// runPicker is a no-op on non-macOS platforms: the native webview window is
// macOS-only, so the tray uses a different Rescan flow there. Returning OK:false
// makes Rescan a no-op rather than persisting anything.
func runPicker(_ []core.ScannedAccount, _ map[string]bool) result {
	return result{OK: false}
}
