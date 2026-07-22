//go:build !darwin && !windows

package main

import "fmt"

// appZipSuffix has no meaningful value on unsupported OSes; self-update is a
// no-op there.
const appZipSuffix = "_unsupported.zip"

func extractUpdateArchive(zipPath, destDir string) error {
	return fmt.Errorf("self-update is not supported on this OS")
}

func stripQuarantine(path string) {}

func findTrayBinary(root string) (string, error) {
	return "", fmt.Errorf("self-update is not supported on this OS")
}
