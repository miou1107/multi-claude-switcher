//go:build darwin

package main

/*
#include "menubar.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miou1107/multi-claude-switcher/core"
)

const (
	appZipPrefix = "Multi-Claude-Switcher_"
	appZipSuffix = "_macos.zip"
)

// updating single-flights the check/apply pipeline so a manual "Check for
// updates" and the periodic auto-check can't install over each other.
var updating sync.Mutex

// startUpdateChecker checks for a newer release shortly after launch and then
// every 6 hours, installing silently when one is found.
func startUpdateChecker() {
	go func() {
		time.Sleep(8 * time.Second)
		autoUpdate()
		for range time.Tick(6 * time.Hour) {
			autoUpdate()
		}
	}()
}

// autoUpdate is the quiet periodic path: install a newer release if present, do
// nothing otherwise.
func autoUpdate() {
	if !updating.TryLock() {
		return
	}
	defer updating.Unlock()
	tag, assets, err := core.LatestRelease()
	if err != nil || !core.IsNewer(tag, core.Version) {
		return
	}
	if url, ok := findAppZip(assets); ok {
		_ = installUpdate(url, tag)
	}
}

// manualCheckAndInstall is the Settings "Check for updates…" path: it reports the
// result in the panel status banner and installs when a newer release exists.
func manualCheckAndInstall() {
	if !updating.TryLock() {
		setBusyStatus(false, "An update is already in progress.")
		reloadPanel()
		return
	}
	defer updating.Unlock()

	tag, assets, err := core.LatestRelease()
	switch {
	case err != nil:
		setBusyStatus(false, "Update check failed. Try again later.")
		reloadPanel()
		return
	case !core.IsNewer(tag, core.Version):
		setBusyStatus(false, "✓ You're on the latest version (v"+core.Version+").")
		reloadPanel()
		return
	}
	url, ok := findAppZip(assets)
	if !ok {
		setBusyStatus(false, "Update "+tag+" is available, but has no download for this platform.")
		reloadPanel()
		return
	}
	setBusyStatus(true, "Downloading "+tag+"…")
	reloadPanel()
	if err := installUpdate(url, tag); err != nil {
		setBusyStatus(false, "Update failed: "+err.Error())
		reloadPanel()
	}
	// On success installUpdate relaunches and terminates this process.
}

// findAppZip returns the download URL of the macOS release asset.
func findAppZip(assets map[string]string) (string, bool) {
	for name, url := range assets {
		if strings.HasPrefix(name, appZipPrefix) && strings.HasSuffix(name, appZipSuffix) {
			return url, true
		}
	}
	return "", false
}

// installUpdate downloads the packaged-app zip, extracts the mcs-menubar binary,
// atomically swaps it in for the running executable, and relaunches the bundle.
func installUpdate(url, tag string) error {
	notify("Updating…", "Downloading "+tag)

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}

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
	if err := exec.Command("ditto", "-x", "-k", zipPath, extractDir).Run(); err != nil {
		return fmt.Errorf("extracting update archive: %w", err)
	}
	newBin, err := findMenubarBinary(extractDir)
	if err != nil {
		return err
	}

	tmp := exe + ".new"
	if err := copyExecutable(newBin, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", tmp).Run()

	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(old, exe)
		os.Remove(tmp)
		return err
	}
	_ = os.Remove(old)

	// Relaunch through LaunchServices so the bundle's LSUIElement is honored.
	if bundle, ok := isInsideAppBundle(exe); ok {
		_ = exec.Command("open", "-n", bundle).Start()
	} else {
		cmd := exec.Command(exe)
		_ = cmd.Start()
	}
	notify("Updated", "Restarting on "+tag+".")
	C.TerminateApp()
	return nil
}

func isInsideAppBundle(exePath string) (string, bool) {
	const marker = ".app/Contents/MacOS/"
	i := strings.Index(exePath, marker)
	if i < 0 {
		return "", false
	}
	return exePath[:i+len(".app")], true
}

var errFound = errors.New("found")

// findMenubarBinary locates the mcs-menubar executable inside an extracted .app.
func findMenubarBinary(root string) (string, error) {
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == "mcs-menubar" &&
			strings.Contains(path, filepath.Join("Contents", "MacOS")+string(filepath.Separator)) {
			found = path
			return errFound
		}
		return nil
	})
	if found != "" {
		return found, nil
	}
	if err != nil && !errors.Is(err, errFound) {
		return "", err
	}
	return "", fmt.Errorf("update archive did not contain the app binary")
}

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
	return os.Chmod(dst, 0755)
}

// notify shows a native macOS notification (best-effort).
func notify(title, text string) {
	script := "display notification " + strconv.Quote(text) + " with title " + strconv.Quote(title)
	_ = exec.Command("osascript", "-e", script).Start()
}
