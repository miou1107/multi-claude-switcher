//go:build windows

package main

import (
	"fmt"
	"os/exec"
)

// releasesPageURL is the human download page for the installer. On Windows the
// app is installed via a setup.exe and upgrades by running the newer installer,
// which replaces the running executable cleanly and in place. Rather than swap
// the binary underneath a live process, installUpdate surfaces the new version
// and opens this page so the user downloads and runs the new installer.
const releasesPageURL = "https://github.com/miou1107/multi-claude-switcher/releases/latest"

// lastNotifiedTag suppresses repeat "update available" toasts for the same
// version across the periodic (6h) auto-checks, so a pending update nags at most
// once per app run.
var lastNotifiedTag string

// installUpdate handles a newer release on Windows. The first parameter (the
// setup asset URL) is unused: nothing is downloaded here. A manual check opens
// the download page; an automatic (background) check only notifies, and only
// once per version, so periodic checks never steal focus or open a browser on
// their own.
func installUpdate(_, tag string, auto bool) error {
	if auto {
		if tag == lastNotifiedTag {
			return nil
		}
		lastNotifiedTag = tag
		notify("Update available",
			fmt.Sprintf("Version %s is available. Open the tray menu → Check for Updates to download it.", tag))
		return nil
	}
	notify("Update available", fmt.Sprintf("Opening the download page for %s.", tag))
	openURL(releasesPageURL)
	return nil
}

// openURL opens a URL in the default browser. rundll32's FileProtocolHandler
// launches the browser (a GUI process), so no console window appears — there is
// nothing to hide.
func openURL(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
