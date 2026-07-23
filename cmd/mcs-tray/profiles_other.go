//go:build !windows

package main

// newProfileSupported / runNewProfileFlow and the multi-account helpers are
// no-ops off Windows. On macOS the standalone build's profiles are ordinary
// sibling data dirs the user selects, so there is no MCS-managed "create a
// profile" step or first-login session migration.
func newProfileSupported() bool { return false }

func newProfileMenuLabel() string { return "" }

func runNewProfileFlow() {}

func startMigrationWatcher() {}
