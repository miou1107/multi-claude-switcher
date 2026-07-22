//go:build windows

package main

// appZipSuffix is the Windows release asset the updater keys on. Windows ships a
// single artifact — the setup.exe installer — which is both the human download
// and the update signal. There is no self-extracting zip: upgrades run the newer
// installer, which replaces the running exe in place (see update_install_windows.go).
const appZipSuffix = "_windows_setup.exe"
