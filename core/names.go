package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var namesMu sync.Mutex

// namesPath is where user-chosen display names for profiles are stored.
func namesPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "multi-claude-switcher-names.json")
	}
	return filepath.Join(home, ".multi-claude-switcher", "names.json")
}

// LoadProfileNames returns the folder-name -> display-name map (empty if unset).
func LoadProfileNames() map[string]string {
	namesMu.Lock()
	defer namesMu.Unlock()
	return loadNamesLocked()
}

func loadNamesLocked() map[string]string {
	m := map[string]string{}
	data, err := os.ReadFile(namesPath())
	if err != nil {
		return m
	}
	_ = json.Unmarshal(data, &m)
	return m
}

// SetProfileName persists a display name for a profile folder. An empty display
// name clears the override, reverting to the folder name.
func SetProfileName(folder, display string) error {
	namesMu.Lock()
	defer namesMu.Unlock()
	m := loadNamesLocked()
	if display == "" {
		delete(m, folder)
	} else {
		m[folder] = display
	}
	if err := os.MkdirAll(filepath.Dir(namesPath()), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	// Atomic write: a crash mid-write must not corrupt the existing names file.
	tmp := namesPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, namesPath())
}

// DisplayName returns the user-chosen display name for a profile folder, or the
// folder name itself when none is set.
func DisplayName(folder string) string {
	if n, ok := LoadProfileNames()[folder]; ok && n != "" {
		return n
	}
	return folder
}
