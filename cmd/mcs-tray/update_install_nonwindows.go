//go:build !windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/getlantern/systray"
	"github.com/miou1107/multi-claude-switcher/core"
)

// installUpdate downloads the packaged-app zip, extracts the tray binary from
// inside it, atomically swaps that in for the currently running executable, then
// relaunches and quits the old process. Only the executable is replaced (not the
// whole bundle), so Info.plist / icon changes ship with a fresh install rather
// than a self-update — acceptable, and it keeps the swap a single atomic rename.
// tag is used only for user-facing messaging; auto does not change the behavior
// (macOS always installs silently).
func installUpdate(url, tag string, _ bool) error {
	log.Printf("Updating v%s -> %s", core.Version, tag)
	notify("Updating…", fmt.Sprintf("Downloading %s", tag))

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
