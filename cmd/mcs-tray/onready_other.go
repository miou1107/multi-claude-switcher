//go:build !windows

package main

// Stubs so main.go's runtime.GOOS == "windows" branch compiles on non-Windows;
// they are never called there.
func onReadyWindowsPanel() {}

// stopPanelProcess is a no-op off Windows: only the Windows tray keeps a warm
// panel process alongside itself.
func stopPanelProcess() {}

// restoreProtocolHandler is a no-op off Windows: only Windows resolves the
// sign-in callback through a registered command line that has to be repointed.
func restoreProtocolHandler() {}
