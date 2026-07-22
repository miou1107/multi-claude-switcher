package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/getlantern/systray"
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

// trayAsset is the release asset that holds this (the tray) binary.
const trayAsset = "mcs-tray-macos-universal"

// updating single-flights the check/apply pipeline. Overlapping runs (a manual
// "Check for Updates…" landing during the startup/6h auto check, or a rapid
// double-click) would otherwise both write exe+".new" and could swap in a
// corrupt binary with no rollback. A second run bails immediately.
var updating sync.Mutex

// startUpdateChecker checks for a newer release at startup (after a short delay)
// and then periodically. When one is found it downloads and applies it
// automatically, then relaunches. `auto` runs are quiet on "already up to date".
func startUpdateChecker() {
	go func() {
		time.Sleep(8 * time.Second) // let the menu settle first
		checkForUpdate(true)
		for range time.Tick(6 * time.Hour) {
			checkForUpdate(true)
		}
	}()
}

// checkForUpdate looks for a newer release and, if found, applies it. When auto
// is false (manual "Check for Updates") it also reports the "up to date" and
// error cases to the user.
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

	url, ok := assets[trayAsset]
	if !ok {
		log.Printf("Release %s has no %q asset; cannot auto-update", tag, trayAsset)
		if !auto {
			notify("Update unavailable", "The release has no downloadable binary for this app.")
		}
		return
	}

	log.Printf("Updating v%s -> %s", core.Version, tag)
	notify("Updating…", fmt.Sprintf("Downloading %s", tag))
	if err := applyUpdate(url); err != nil {
		log.Printf("Update failed: %v", err)
		notify("Update failed", err.Error())
		return
	}
	// applyUpdate relaunches and quits on success, so we normally don't return.
}

// applyUpdate downloads the new binary, atomically swaps it in for the currently
// running executable, then relaunches and quits the old process.
func applyUpdate(url string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}

	// Download alongside the current binary so the final swap is a same-filesystem
	// atomic rename.
	tmp := exe + ".new"
	if err := core.DownloadTo(url, tmp); err != nil {
		os.Remove(tmp) // don't leave a partial download behind
		return err
	}
	if err := os.Chmod(tmp, 0755); err != nil {
		os.Remove(tmp)
		return err
	}
	// Strip the download quarantine so Gatekeeper doesn't block the relaunch.
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", tmp).Run()

	// Swap: move the current binary aside, move the new one in; roll back on
	// failure so we never leave the app without an executable.
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(old, exe) // roll back
		os.Remove(tmp)
		return err
	}
	_ = os.Remove(old)

	log.Printf("Update applied; relaunching %s", exe)
	var cmd *exec.Cmd
	if bundle, ok := isInsideAppBundle(exe); ok {
		// Relaunch through LaunchServices so the bundle's Info.plist (notably
		// LSUIElement) is honored; exec'ing the raw binary would drop the
		// menu-bar-agent treatment and flash a Dock icon. `open` detaches on its
		// own, so no Setpgid is needed here.
		cmd = exec.Command("open", "-n", bundle)
	} else {
		cmd = exec.Command(exe, os.Args[1:]...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // detach so it outlives us
	}
	if err := cmd.Start(); err != nil {
		// The new binary is already swapped in; only the auto-relaunch failed.
		return fmt.Errorf("update installed but relaunch failed (please reopen the app to use it): %w", err)
	}
	notify("Updated", "Restarting on the new version.")
	systray.Quit()
	return nil
}
