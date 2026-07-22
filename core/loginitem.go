package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// loginItemLabel is the LaunchAgent label (and reverse-DNS bundle identifier).
const loginItemLabel = "com.miou1107.multi-claude-switcher"

// launchAgentsDir returns the per-user LaunchAgents directory. It is a var so
// tests can redirect it away from the real ~/Library/LaunchAgents.
var launchAgentsDir = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "LaunchAgents")
	}
	return filepath.Join(home, "Library", "LaunchAgents")
}

func loginItemPlistPath() string {
	return filepath.Join(launchAgentsDir(), loginItemLabel+".plist")
}

// LoginItemEnabled reports whether the start-at-login LaunchAgent is installed.
func LoginItemEnabled() bool {
	_, err := os.Stat(loginItemPlistPath())
	return err == nil
}

// EnableLoginItem installs a LaunchAgent that runs exePath at login. exePath
// should be the resolved path to the running executable (inside the .app bundle
// when packaged). The write is atomic. It deliberately does NOT `launchctl load`
// the job: the app is already running when the user enables this, RunAtLoad
// would immediately start a second instance (there is no single-instance guard),
// and launchd loads everything in ~/Library/LaunchAgents at the next login
// anyway. So the setting takes effect from the next login on.
func EnableLoginItem(exePath string) error {
	if exePath == "" {
		return fmt.Errorf("cannot enable login item: empty executable path")
	}
	path := loginItemPlistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`, xmlEscape(loginItemLabel), xmlEscape(exePath))

	// Atomic write so a crash mid-write can't leave a truncated plist that
	// launchd would reject at the next login.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(plist), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// DisableLoginItem removes the start-at-login LaunchAgent. Removing a
// non-existent item is not an error. Like EnableLoginItem it does not call
// `launchctl unload`: when the app was started by launchd at login, the running
// tray IS that job, so unloading it would SIGTERM the app the user is clicking
// in. Removing the plist stops it from launching at the next login, which is the
// intended effect.
func DisableLoginItem() error {
	path := loginItemPlistPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// xmlEscape escapes the five XML predefined entities for safe embedding in the
// plist text (paths can legitimately contain & or other reserved characters).
// The ampersand must be replaced first so it doesn't double-escape the others.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
