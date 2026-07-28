//go:build !windows

package main

// Stub so main.go's runtime.GOOS == "windows" branch compiles on non-Windows;
// it is never called there.
func onReadyWindowsPanel() {}
