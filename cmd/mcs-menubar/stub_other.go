//go:build !darwin

// Package main's macOS menu-bar panel (NSStatusItem + NSPopover + WKWebView) is
// macOS-only; on other platforms the tray (cmd/mcs-tray) provides the UI. This
// stub keeps the package buildable everywhere.
package main

func main() {}
