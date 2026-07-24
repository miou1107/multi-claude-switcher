package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var managedMu sync.Mutex

// managedPath is where the user-curated managed-profile list is stored. It is a
// var so tests can redirect it to a temp dir (same pattern as names.go).
var managedPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "multi-claude-switcher-managed.json")
	}
	return filepath.Join(home, ".multi-claude-switcher", "managed.json")
}

type managedFile struct {
	Managed []string `json:"managed"`
}

// LoadManaged returns the managed folder-name list. It returns nil when the file
// is absent (the first-run signal), and a non-nil (possibly empty) slice when the
// file exists — callers distinguish "never configured" from "configured empty".
func LoadManaged() []string {
	managedMu.Lock()
	defer managedMu.Unlock()
	data, err := os.ReadFile(managedPath())
	if err != nil {
		return nil // absent/unreadable → first-run
	}
	var mf managedFile
	if json.Unmarshal(data, &mf) != nil {
		return nil
	}
	if mf.Managed == nil {
		return []string{} // present but no key → configured empty, not first-run
	}
	return mf.Managed
}

// SetManaged persists the managed folder-name list (atomic tmp+rename).
func SetManaged(folders []string) error {
	managedMu.Lock()
	defer managedMu.Unlock()
	if folders == nil {
		folders = []string{}
	}
	data, err := json.MarshalIndent(managedFile{Managed: folders}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(managedPath()), 0755); err != nil {
		return err
	}
	tmp := managedPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, managedPath()); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// IsManaged reports whether the given folder is in the persisted managed list.
// Returns false when the registry is absent (first-run); callers apply their own
// first-run fallback.
func IsManaged(folder string) bool {
	for _, m := range LoadManaged() {
		if m == folder {
			return true
		}
	}
	return false
}
