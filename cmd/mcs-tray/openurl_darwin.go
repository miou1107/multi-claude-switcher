//go:build darwin

package main

import "os/exec"

// openURL opens a URL in the default browser (non-blocking).
func openURL(url string) error { return exec.Command("open", url).Start() }
