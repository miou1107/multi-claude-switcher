package main

import (
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/miou1107/multi-claude-switcher/core"
)

// isInsideAppBundle reports whether exePath is the executable of a macOS .app
// bundle, returning the path to the bundle (the "…/Foo.app" directory). Used to
// decide whether to relaunch via LaunchServices (`open`) instead of exec'ing the
// bare binary.
func isInsideAppBundle(exePath string) (string, bool) {
	const marker = ".app/Contents/MacOS/"
	i := strings.Index(exePath, marker)
	if i < 0 {
		return "", false
	}
	return exePath[:i+len(".app")], true
}

// appZipPrefix / appZipSuffix bracket the release asset the updater keys on (e.g.
// "Multi-Claude-Switcher_0.6.1_macos.zip" on macOS, the setup.exe on Windows).
// The version sits in the middle, so we match by prefix+suffix rather than an
// exact name. appZipSuffix is OS-specific and defined in update_platform_*.go.
const appZipPrefix = "Multi-Claude-Switcher_"

// findAppZip returns the download URL of the platform's release asset.
func findAppZip(assets map[string]string) (string, bool) {
	for name, url := range assets {
		if strings.HasPrefix(name, appZipPrefix) && strings.HasSuffix(name, appZipSuffix) {
			return url, true
		}
	}
	return "", false
}

// updating single-flights the check/apply pipeline. Overlapping runs (a manual
// "Check for Updates…" landing during the startup/6h auto check, or a rapid
// double-click) would otherwise both write exe+".new" and could swap in a
// corrupt binary with no rollback. A second run bails immediately.
var updating sync.Mutex

// startUpdateChecker checks for a newer release at startup (after a short delay)
// and then periodically. `auto` runs are quiet on "already up to date".
func startUpdateChecker() {
	go func() {
		time.Sleep(8 * time.Second) // let the menu settle first
		checkForUpdate(true)
		for range time.Tick(6 * time.Hour) {
			checkForUpdate(true)
		}
	}()
}

// checkForUpdate looks for a newer release and, if found, hands it to the
// platform-specific installUpdate. When auto is false (manual "Check for
// Updates") it also reports the "up to date" and error cases to the user.
func checkForUpdate(auto bool) {
	if !updating.TryLock() {
		log.Printf("Update already in progress; skipping this check")
		if !auto {
			notify("Update in progress", "An update is already running.")
		}
		return
	}
	defer updating.Unlock()

	tag, assets, err := core.LatestRelease()
	if err != nil {
		log.Printf("Update check failed: %v", err)
		if !auto {
			notify("Update check failed", err.Error())
		}
		return
	}
	if !core.IsNewer(tag, core.Version) {
		log.Printf("Up to date (current v%s, latest %s)", core.Version, tag)
		if !auto {
			notify("Up to date", "You're on the latest version (v"+core.Version+").")
		}
		return
	}

	url, ok := findAppZip(assets)
	if !ok {
		log.Printf("Release %s has no downloadable asset for this platform (%s…%s); cannot update", tag, appZipPrefix, appZipSuffix)
		if !auto {
			notify("Update unavailable", "The release has no downloadable app for this platform.")
		}
		return
	}

	if err := installUpdate(url, tag, auto); err != nil {
		log.Printf("Update failed: %v", err)
		notify("Update failed", err.Error())
	}
	// On success installUpdate either relaunches and quits (macOS) or opens the
	// download page (Windows); there is nothing more to do here.
}

// copyExecutable copies src to dst (0755), truncating dst. Used by the macOS
// self-updater to move the extracted binary onto the app's filesystem before the
// atomic swap.
func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// Force the exec bit unconditionally: O_CREATE|0755 is umask-masked and won't
	// reset the mode of a pre-existing stale dst, so a plain OpenFile is not a
	// guarantee the swapped-in binary is runnable.
	return os.Chmod(dst, 0755)
}
