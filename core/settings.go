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
	AutoSyncOnSwitch         bool `json:"autoSyncOnSwitch"`
	AutoSyncWarningDismissed bool `json:"autoSyncWarningDismissed"`
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
	if err := os.Rename(tmp, settingsPath()); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// AutoSyncOnSwitch reports whether switching should bidirectionally sync.
func AutoSyncOnSwitch() bool {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return loadSettingsLocked().AutoSyncOnSwitch
}

// SetAutoSyncOnSwitch persists the auto sync-on-switch toggle.
func SetAutoSyncOnSwitch(v bool) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	s := loadSettingsLocked()
	s.AutoSyncOnSwitch = v
	return saveSettingsLocked(s)
}

// AutoSyncWarningDismissed reports whether the enable-time warning is suppressed.
func AutoSyncWarningDismissed() bool {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return loadSettingsLocked().AutoSyncWarningDismissed
}

// SetAutoSyncWarningDismissed persists the "don't ask again" choice.
func SetAutoSyncWarningDismissed(v bool) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	s := loadSettingsLocked()
	s.AutoSyncWarningDismissed = v
	return saveSettingsLocked(s)
}
