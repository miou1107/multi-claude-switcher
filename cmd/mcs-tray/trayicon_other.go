//go:build !darwin && !windows

package main

// setTrayIcon is a no-op on operating systems without a wired-up icon asset.
func setTrayIcon() {}
