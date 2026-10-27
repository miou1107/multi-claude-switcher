//go:build !windows

package main

// claudelink_other.go: only Windows hands claude:// links to the switcher (see
// claudelink_windows.go).

func runClaudeLinkCommand(args []string) bool { return false }
