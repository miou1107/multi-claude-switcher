//go:build !windows

package main

// newProfileSupported / runNewProfileFlow are no-ops off Windows. On macOS the
// standalone build's profiles are ordinary sibling data dirs the user selects,
// so there is no MCS-managed "create a profile" step.
func newProfileSupported() bool { return false }

func runNewProfileFlow() {}
