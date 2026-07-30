package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/miou1107/multi-claude-switcher/platform"
)

var pendingMu sync.Mutex

// pendingPath is where the pending-sign-in registry is stored. It is a var so
// tests can redirect it to a temp dir (same pattern as managed.go's managedPath).
var pendingPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "multi-claude-switcher-pending.json")
	}
	return filepath.Join(home, ".multi-claude-switcher", "pending.json")
}

// PendingProfile is a profile MCS created that is waiting for its one-time
// sign-in. It exists because a brand-new profile dir has no config.json, so
// nothing on disk distinguishes it from a stray directory until Claude has run
// in it — and the user has just been told to go and sign in to it, so it must
// stay visible in the panel until they do.
//
// Folder is the profile's identity as FindProfiles reports it, which is the same
// key managed.json and names.json use. On the Windows Store build that is the
// name from state.json, not a directory name.
type PendingProfile struct {
	Folder string `json:"folder"`
	// ExpectUUID names the account this profile was created to receive, set on
	// the recovery path. Empty on the plain add path, which accepts any account.
	ExpectUUID string `json:"expectUUID,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

type pendingFile struct {
	Pending []PendingProfile `json:"pending"`
}

// LoadPending returns the pending-sign-in entries, or nil when the file is
// absent or unreadable. Unlike LoadManaged there is no first-run distinction to
// preserve: no entries and no file mean the same thing.
func LoadPending() []PendingProfile {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	return loadPendingLocked()
}

func loadPendingLocked() []PendingProfile {
	data, err := os.ReadFile(pendingPath())
	if err != nil {
		return nil
	}
	var pf pendingFile
	if json.Unmarshal(data, &pf) != nil {
		return nil
	}
	return pf.Pending
}

func savePendingLocked(entries []PendingProfile) error {
	if entries == nil {
		entries = []PendingProfile{}
	}
	data, err := json.MarshalIndent(pendingFile{Pending: entries}, "", "  ")
	if err != nil {
		return err
	}
	return writeRegistryFile(pendingPath(), data)
}

// AddPending records a folder as awaiting sign-in, replacing any existing entry
// for the same folder so a retried create cannot leave two.
func AddPending(folder, expectUUID string) error {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	entries := loadPendingLocked()
	out := make([]PendingProfile, 0, len(entries)+1)
	for _, e := range entries {
		if e.Folder != folder {
			out = append(out, e)
		}
	}
	out = append(out, PendingProfile{
		Folder:     folder,
		ExpectUUID: expectUUID,
		CreatedAt:  time.Now().Format(time.RFC3339),
	})
	return savePendingLocked(out)
}

// RemovePending drops a folder's entry. Removing an absent folder is a no-op,
// so callers can prune unconditionally.
func RemovePending(folder string) error {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	entries := loadPendingLocked()
	out := make([]PendingProfile, 0, len(entries))
	changed := false
	for _, e := range entries {
		if e.Folder == folder {
			changed = true
			continue
		}
		out = append(out, e)
	}
	if !changed {
		return nil
	}
	return savePendingLocked(out)
}

// StalePending returns the folders whose pending entry no longer applies, which
// is exactly those that now have a live login: the sign-in happened, so the entry
// has served its purpose. Pure, so the rule is testable without a real profile
// tree; callers pass the result to RemovePending.
//
// A profile missing from profiles is deliberately NOT stale. On the Store build a
// just-created profile has no directory at all until the packaged app launches and
// makes one (msixParkForNewIn leaves the slot absent on purpose), so pruning on
// absence would discard the entry seconds after writing it, on the one platform
// this feature exists for. Sign-in is the only thing that means "finished".
func StalePending(pending []PendingProfile, profiles []*platform.ProfileInfo) []string {
	byName := map[string]*platform.ProfileInfo{}
	for _, p := range profiles {
		byName[p.Name] = p
	}
	var stale []string
	for _, e := range pending {
		p, ok := byName[e.Folder]
		if !ok {
			continue
		}
		if _, err := platform.GetProfileAccountUUID(p.Path); err == nil {
			stale = append(stale, e.Folder)
		}
	}
	return stale
}
