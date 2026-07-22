//go:build windows

package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// appZipSuffix is the Windows release asset suffix (e.g.
// "Multi-Claude-Switcher_0.7.0_windows.zip"). The CI windows job publishes this.
const appZipSuffix = "_windows.zip"

// extractUpdateArchive unzips zipPath into destDir using the standard library
// (no external tools). It rejects entries that would escape destDir (Zip Slip).
func extractUpdateArchive(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	cleanDest := filepath.Clean(destDir)
	for _, f := range r.File {
		fp := filepath.Join(cleanDest, f.Name)
		if fp != cleanDest && !strings.HasPrefix(fp, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe path in archive: %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(fp, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			return err
		}
		if err := writeZipEntry(f, fp); err != nil {
			return err
		}
	}
	return nil
}

func writeZipEntry(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

// stripQuarantine is a no-op on Windows (no Gatekeeper quarantine bit).
func stripQuarantine(path string) {}

// findTrayBinary locates mcs-tray.exe inside an extracted archive.
func findTrayBinary(root string) (string, error) {
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.EqualFold(info.Name(), "mcs-tray.exe") {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found != "" {
		return found, nil
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("update archive did not contain mcs-tray.exe")
}
