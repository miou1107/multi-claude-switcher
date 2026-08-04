package core

import (
	"encoding/json"
	"fmt"
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

// loadManagedLocked reads the registry and, unlike LoadManaged, says why it came
// back empty. Callers that are about to write need that distinction: a nil list
// means "never configured" to a first-run check but "I could not read your list"
// to a corrupt file, and only one of those may be replaced with a fresh list.
//
// Returns (nil, nil) only for a genuinely absent file.
func loadManagedLocked() ([]string, error) {
	data, err := os.ReadFile(managedPath())
	if os.IsNotExist(err) {
		return nil, nil // never configured
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", managedPath(), err)
	}
	var mf managedFile
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil, fmt.Errorf("%s is damaged and was left untouched. Repair or delete it: %w", managedPath(), err)
	}
	if mf.Managed == nil {
		return []string{}, nil // present but no key → configured empty, not first-run
	}
	return mf.Managed, nil
}

// LoadManaged returns the managed folder-name list. It returns nil when the file
// is absent (the first-run signal), and a non-nil (possibly empty) slice when the
// file exists — callers distinguish "never configured" from "configured empty".
//
// A file that exists but cannot be read or parsed also returns nil, which a
// caller cannot tell apart from first-run. That is acceptable for showing the
// panel, where the fallback is to show everything, and NOT acceptable for
// anything that writes the list back: use AddManaged or RemoveManaged, which
// refuse rather than replace a list they could not read.
func LoadManaged() []string {
	managedMu.Lock()
	defer managedMu.Unlock()
	folders, err := loadManagedLocked()
	if err != nil {
		return nil
	}
	return folders
}

// saveManagedLocked writes the list atomically. The staging file gets a unique
// name rather than a fixed ".tmp": the panel and the tray are separate processes
// and both write this registry, so a shared staging name lets one truncate the
// other's half-written file and rename the result into place.
func saveManagedLocked(folders []string) error {
	if folders == nil {
		folders = []string{}
	}
	data, err := json.MarshalIndent(managedFile{Managed: folders}, "", "  ")
	if err != nil {
		return err
	}
	return writeRegistryFile(managedPath(), data)
}

// SetManaged replaces the managed folder-name list with exactly what it is
// given. This is for the Rescan screen, where the list IS the user's ticks and
// replacing it is the point. To add or drop one entry, use AddManaged or
// RemoveManaged instead: they read and write under one lock, so a concurrent
// writer cannot lose an entry, and they refuse to act on a list they could not
// read.
func SetManaged(folders []string) error {
	managedMu.Lock()
	defer managedMu.Unlock()
	return saveManagedLocked(folders)
}

// AddManaged puts one identity in the managed list, leaving everything else
// alone. Adding one that is already there is a no-op.
func AddManaged(identity string) error {
	managedMu.Lock()
	defer managedMu.Unlock()
	current, err := loadManagedLocked()
	if err != nil {
		return err
	}
	for _, m := range current {
		if m == identity {
			return nil
		}
	}
	return saveManagedLocked(append(current, identity))
}

// AddManagedIfCurated adds identity to the managed list, but only when the list has
// already been curated. On a never-configured (first-run) list it does nothing.
//
// This is what a freshly created profile uses. Turning a nil (first-run) list into a
// one-element list would flip the panel from "show every usable account" to "show
// only what is listed", so the user's existing accounts would vanish the moment they
// added a second one. The new profile stays visible through the pending registry
// until it is signed in, and through being signed in after, so it needs no managed
// entry on a first-run list. On a curated list it is added, so it survives past the
// point the pending entry is pruned.
func AddManagedIfCurated(identity string) error {
	managedMu.Lock()
	defer managedMu.Unlock()
	current, err := loadManagedLocked()
	if err != nil {
		return err
	}
	if current == nil {
		return nil // first run — leave the list unset
	}
	for _, m := range current {
		if m == identity {
			return nil
		}
	}
	return saveManagedLocked(append(current, identity))
}

// RemoveManaged drops one identity from the managed list, leaving everything
// else alone. Removing one that is not there changes nothing and writes nothing.
func RemoveManaged(identity string) error {
	managedMu.Lock()
	defer managedMu.Unlock()
	current, err := loadManagedLocked()
	if err != nil {
		return err
	}
	out := make([]string, 0, len(current))
	found := false
	for _, m := range current {
		if m == identity {
			found = true
			continue
		}
		out = append(out, m)
	}
	if !found {
		return nil
	}
	return saveManagedLocked(out)
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
