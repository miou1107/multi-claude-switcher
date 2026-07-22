package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

// appZipPrefix / appZipSuffix bracket the release asset that holds the packaged
// app (e.g. "Multi-Claude-Switcher_0.6.1_macos.zip"). The version is in the
// middle so we match by prefix+suffix rather than an exact name. This is the
// only published download — the self-updater extracts the binary from it.
// appZipSuffix is OS-specific and defined in update_platform_*.go
// (e.g. "_macos.zip", "_windows.zip").
const appZipPrefix = "Multi-Claude-Switcher_"

// findAppZip returns the download URL of the packaged-app asset in a release.
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

	url, ok := findAppZip(assets)
	if !ok {
		log.Printf("Release %s has no packaged-app asset (%s…%s); cannot auto-update", tag, appZipPrefix, appZipSuffix)
		if !auto {
			notify("Update unavailable", "The release has no downloadable app for this platform.")
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

// applyUpdate downloads the packaged-app zip, extracts the tray binary from
// inside it, atomically swaps that in for the currently running executable, then
// relaunches and quits the old process. Only the executable is replaced (not the
// whole bundle), so Info.plist / icon changes ship with a fresh install rather
// than a self-update — acceptable, and it keeps the swap a single atomic rename.
func applyUpdate(url string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}

	// Download + unzip in a scratch dir (cleaned up regardless of outcome).
	work, err := os.MkdirTemp("", "mcs-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	zipPath := filepath.Join(work, "update.zip")
	if err := core.DownloadTo(url, zipPath); err != nil {
		return err
	}
	extractDir := filepath.Join(work, "extract")
	if err := extractUpdateArchive(zipPath, extractDir); err != nil {
		return fmt.Errorf("extracting update archive: %w", err)
	}
	newBin, err := findTrayBinary(extractDir)
	if err != nil {
		return err
	}

	// Copy the extracted binary next to the current one so the final swap is a
	// same-filesystem atomic rename (the scratch dir is likely a different fs).
	tmp := exe + ".new"
	if err := copyExecutable(newBin, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	// Strip any quarantine so Gatekeeper doesn't block the relaunch (macOS only).
	stripQuarantine(tmp)

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
		// `open -n <bundle> --args <flag>` forwards the flag into the new app's argv.
		cmd = exec.Command("open", "-n", bundle, "--args", relaunchSkipInstanceCheckFlag)
	} else {
		args := append([]string{}, os.Args[1:]...)
		if !hasSkipInstanceFlag(args) {
			args = append(args, relaunchSkipInstanceCheckFlag)
		}
		cmd = exec.Command(exe, args...)
		detachRelaunch(cmd) // detach so it outlives us
	}
	if err := cmd.Start(); err != nil {
		// The new binary is already swapped in; only the auto-relaunch failed.
		return fmt.Errorf("update installed but relaunch failed (please reopen the app to use it): %w", err)
	}
	notify("Updated", "Restarting on the new version.")
	systray.Quit()
	return nil
}

// copyExecutable copies src to dst (0755), truncating dst. Used to move the
// extracted binary from the scratch dir onto the app's filesystem before the
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
