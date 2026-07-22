//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// appZipSuffix is the macOS release asset suffix (e.g.
// "Multi-Claude-Switcher_0.7.0_macos.zip").
const appZipSuffix = "_macos.zip"

// extractUpdateArchive unpacks a macOS .app zip using ditto (which preserves the
// bundle's symlinks and metadata).
func extractUpdateArchive(zipPath, destDir string) error {
	return exec.Command("ditto", "-x", "-k", zipPath, destDir).Run()
}

// stripQuarantine removes the Gatekeeper quarantine attribute so the relaunched
// binary is not blocked.
func stripQuarantine(path string) {
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", path).Run()
}

// errFound is a sentinel used to stop the walk early once the binary is located.
var errFound = errors.New("found")

// findTrayBinary locates the tray executable inside an extracted .app bundle
// (…/Contents/MacOS/mcs-tray).
func findTrayBinary(root string) (string, error) {
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == "mcs-tray" &&
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
