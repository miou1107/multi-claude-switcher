//go:build windows

package core

import (
	"fmt"
	"os/exec"
	"strings"
)

// Start-at-login on Windows is a per-user registry value under the Run key.
// Windows launches each Run entry directly at logon, so there is no console
// flash (unlike a .bat/.cmd in the Startup folder) and no COM/shortcut plumbing
// (unlike a .lnk). loginRunKey/loginRunValue are vars so tests can redirect them
// to a throwaway key instead of the real Run key.
var (
	loginRunKey   = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	loginRunValue = "Multi-Claude-Switcher"
)

// LoginItemEnabled reports whether the start-at-login Run value is present.
func LoginItemEnabled() bool {
	// `reg query` exits non-zero when the value (or key) does not exist.
	cmd := exec.Command("reg", "query", loginRunKey, "/v", loginRunValue)
	hideConsole(cmd)
	return cmd.Run() == nil
}

// EnableLoginItem writes a Run value pointing at exePath so Windows launches it
// at logon. The stored path is quoted so a path containing spaces survives the
// command-line parse Windows performs on Run entries at logon. reg add with /f
// creates the key path if needed and overwrites any existing value.
func EnableLoginItem(exePath string) error {
	if exePath == "" {
		return fmt.Errorf("cannot enable login item: empty executable path")
	}
	data := `"` + exePath + `"`
	cmd := exec.Command("reg", "add", loginRunKey,
		"/v", loginRunValue, "/t", "REG_SZ", "/d", data, "/f")
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("enable login item: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DisableLoginItem removes the Run value. Removing a value that is not present
// is a no-op, mirroring the darwin implementation.
func DisableLoginItem() error {
	if !LoginItemEnabled() {
		return nil
	}
	cmd := exec.Command("reg", "delete", loginRunKey, "/v", loginRunValue, "/f")
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("disable login item: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
