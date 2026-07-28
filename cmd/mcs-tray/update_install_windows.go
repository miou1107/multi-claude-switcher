//go:build windows

package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"fyne.io/systray"
	"github.com/miou1107/multi-claude-switcher/core"
)

// Windows updates the same way macOS does: the check finds a newer release, the
// new version is fetched and applied, and the app comes back on it — no prompt,
// no browser, no download page. The mechanism differs because the artifacts do.
// macOS ships a zip holding a bare binary, so the update is an atomic rename of
// the executable. Windows ships an Inno Setup installer, and Windows will not
// let anything overwrite a running .exe, so the update runs that installer
// unattended and quits first to release the image.
//
// The relaunch afterwards belongs to the installer's [Run] entry (see
// packaging/windows-setup.iss); nothing here starts the new binary.

// releasesPageURL is the human download page. It is only reached when an update
// could not be applied on its own, and only for a check the user asked for.
const releasesPageURL = "https://github.com/miou1107/multi-claude-switcher/releases/latest"

// updateDirName is the scratch directory the installer is downloaded to. It is
// a fixed name rather than a random temp dir on purpose: this process exits
// while the installer is still running, so it can never clean up after itself,
// and a fixed name lets the next run clear the previous one deterministically.
const updateDirName = "mcs-update"

// installerFlags run Inno Setup completely unattended: no wizard, no progress
// window, no message boxes, no cancel button, and no machine restart.
func installerFlags() []string {
	return []string{"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NOCANCEL", "/NORESTART"}
}

// looksLikeExecutable reports whether a downloaded file starts with the "MZ"
// signature every Windows executable carries. A captive-portal login page or a
// truncated download would otherwise be handed straight to CreateProcess.
func looksLikeExecutable(header []byte) bool {
	return len(header) >= 2 && header[0] == 'M' && header[1] == 'Z'
}

// installUpdate downloads the release's setup.exe, starts it silently, and
// quits so it can replace the running executable. auto only decides what
// happens when that fails: a background check stays quiet apart from the
// failure toast, while a check the user asked for also opens the download page
// so there is somewhere to go.
func installUpdate(url, tag string, auto bool) error {
	log.Printf("Updating v%s -> %s", core.Version, tag)
	notify("Updating…", fmt.Sprintf("Downloading %s", tag))

	setup, err := downloadInstaller(url)
	if err != nil {
		return failedUpdate(auto, err)
	}

	// Start the installer, then get out of its way. Inno's own startup
	// (self-extraction, Restart Manager scan) takes seconds, while
	// systray.Quit() unwinds us through onExit in milliseconds, so mcs-tray.exe
	// is free long before the file copy begins. CloseApplications=yes in the
	// .iss is the backstop if that ever stops holding.
	cmd := exec.Command(setup, installerFlags()...)
	detachRelaunch(cmd) // outlive us: we are about to exit
	if err := cmd.Start(); err != nil {
		return failedUpdate(auto, fmt.Errorf("running %s: %w", filepath.Base(setup), err))
	}
	log.Printf("Installer %s started; quitting so it can replace the running exe", setup)

	notify("Updating…", fmt.Sprintf("Installing %s and restarting.", tag))
	systray.Quit()
	return nil
}

// failedUpdate annotates an update failure and, for a manual check only, opens
// the releases page. Background checks never steal the foreground.
func failedUpdate(auto bool, err error) error {
	if !auto {
		_ = openURL(releasesPageURL)
		return fmt.Errorf("%w — opening the download page", err)
	}
	return fmt.Errorf("%w (download it from %s)", err, releasesPageURL)
}

// downloadInstaller fetches the release's setup.exe into the scratch directory
// and returns its path, having verified it really is an executable.
func downloadInstaller(url string) (string, error) {
	return downloadInstallerTo(filepath.Join(os.TempDir(), updateDirName), url)
}

// downloadInstallerTo is downloadInstaller with the scratch directory supplied,
// so the download, the clear-first rule and the executable check can be tested
// without writing to the real temp dir.
func downloadInstallerTo(dir, url string) (string, error) {
	// Clear the previous update's installer (see updateDirName).
	if err := os.RemoveAll(dir); err != nil {
		log.Printf("could not clear %s: %v", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}

	dest := filepath.Join(dir, "Multi-Claude-Switcher_setup.exe")
	if err := core.DownloadTo(url, dest); err != nil {
		return "", err
	}

	f, err := os.Open(dest)
	if err != nil {
		return "", err
	}
	header := make([]byte, 2)
	n, _ := io.ReadFull(f, header)
	f.Close()
	if !looksLikeExecutable(header[:n]) {
		return "", fmt.Errorf("the downloaded update is not a Windows installer")
	}
	return dest, nil
}
