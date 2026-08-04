//go:build !darwin && !windows

package clip

import "errors"

// Set reports that there is no clipboard here rather than pretending to write
// to one: its caller decides not to open a browser on this error.
func Set(string) error { return errors.New("clipboard not supported on this platform") }
