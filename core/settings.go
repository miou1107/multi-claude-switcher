// core/settings.go
package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var settingsMu sync.Mutex

// settingsPath is where user settings are stored. It is a var so tests can
// redirect it to a temp dir (same pattern as loginitem.go's launchAgentsDir).
var settingsPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "multi-claude-switcher-settings.json")
	}
	return filepath.Join(home, ".multi-claude-switcher", "settings.json")
}

type settings struct {
	AutoAlignOnSwitch         bool `json:"autoAlignOnSwitch"`
	AutoAlignWarningDismissed bool `json:"autoAlignWarningDismissed"`
}

func loadSettingsLocked() settings {
	var s settings
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		return s // defaults (all false) when absent/unreadable
	}
	_ = json.Unmarshal(data, &s)
	return s
}

func saveSettingsLocked(s settings) error {
	if err := os.MkdirAll(filepath.Dir(settingsPath()), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// Atomic write: a crash mid-write must not corrupt the existing settings.
	tmp := settingsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, settingsPath())
}

// AutoAlignOnSwitch reports whether switching should bidirectionally sync.
func AutoAlignOnSwitch() bool {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return loadSettingsLocked().AutoAlignOnSwitch
}

// SetAutoAlignOnSwitch persists the auto-align-on-switch toggle.
func SetAutoAlignOnSwitch(v bool) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	s := loadSettingsLocked()
	s.AutoAlignOnSwitch = v
	return saveSettingsLocked(s)
}

// AutoAlignWarningDismissed reports whether the enable-time warning is suppressed.
func AutoAlignWarningDismissed() bool {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return loadSettingsLocked().AutoAlignWarningDismissed
}

// SetAutoAlignWarningDismissed persists the "don't ask again" choice.
func SetAutoAlignWarningDismissed(v bool) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	s := loadSettingsLocked()
	s.AutoAlignWarningDismissed = v
	return saveSettingsLocked(s)
}
