package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/miou1107/multi-claude-switcher/platform"
)

var activeProfileMu sync.Mutex

// activeProfilePath is where the last account MCS activated is recorded. A var so
// tests can redirect it to a temp dir (same pattern as managed.go and names.go).
var activeProfilePath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "multi-claude-switcher-active.json")
	}
	return filepath.Join(home, ".multi-claude-switcher", "active.json")
}

type activeProfileFile struct {
	Active string `json:"active"`
}

// SaveActiveProfile records the profile identity MCS just switched the user to.
//
// This exists because "which account is the user on" cannot be answered by
// looking at the machine once more than one account is open. Every running
// profile looks alike from the outside, so MCS used to answer with whichever the
// process list named first. That guess decides which account a switch leaves
// closed, and an arbitrary answer means an arbitrary account stays shut. MCS put
// the user on an account by its own action, so it can simply remember doing so.
//
// The identity, not the path: on the Store build every profile lives in the same
// slot directory, so a path does not name an account there.
func SaveActiveProfile(identity string) error {
	activeProfileMu.Lock()
	defer activeProfileMu.Unlock()
	data, err := json.MarshalIndent(activeProfileFile{Active: identity}, "", "  ")
	if err != nil {
		return err
	}
	return writeRegistryFile(activeProfilePath(), data)
}

// LoadActiveProfile returns the last recorded profile identity, or "" when there
// is none.
//
// Every failure reads as "": the record is a convenience that saves MCS from
// guessing, and its callers all have a fallback. Refusing to work because a
// cache is damaged would be worse than the guess it replaced.
func LoadActiveProfile() string {
	activeProfileMu.Lock()
	defer activeProfileMu.Unlock()
	data, err := os.ReadFile(activeProfilePath())
	if err != nil {
		return ""
	}
	var f activeProfileFile
	if err := json.Unmarshal(data, &f); err != nil {
		return ""
	}
	return f.Active
}

// SourceProfilePath returns the profile a switch to targetPath should treat as
// the account being left: the one whose sessions flow into the target when auto
// sync is on, and the one deliberately left closed afterwards.
//
// Shared by every host on purpose. This was three identical copies, and it is the
// answer to "which account is the user on" — the question a switch is least able
// to get wrong, since getting it wrong shuts an account the user wanted.
func SourceProfilePath(plat platform.Platform, targetPath string, profiles []*platform.ProfileInfo) string {
	if running, err := plat.DetectRunningProfiles(); err == nil {
		cur := CurrentProfilePath(running, lastActivatedPath(profiles))
		if cur != "" && !platform.SamePath(cur, targetPath) {
			return cur
		}
		// The user is on the target already (they re-picked the account they are
		// on). Any OTHER open account is still a better answer than an idle one:
		// with auto sync on the source's sessions flow into the target, and an
		// account the user does not have open is history they did not ask for.
		for _, p := range running {
			if !platform.SamePath(p, targetPath) {
				return p
			}
		}
	}
	// Nothing running, or the only thing running is the target itself. Fall back
	// to the first other profile that has sessions to offer.
	for _, p := range profiles {
		if !platform.SamePath(p.Path, targetPath) && p.HasSessionsDir {
			return p.Path
		}
	}
	if len(profiles) > 0 {
		return profiles[0].Path
	}
	return filepath.Join(plat.AppSupportDir(), "Claude")
}

// lastActivatedPath resolves the recorded profile identity to its directory, or
// "" when there is no record or it names a profile that no longer exists. The
// identity is resolved through the profile list rather than joined onto the data
// root, because on the Store build the two are not the same thing.
func lastActivatedPath(profiles []*platform.ProfileInfo) string {
	identity := LoadActiveProfile()
	if identity == "" {
		return ""
	}
	for _, p := range profiles {
		if p.Name == identity {
			return p.Path
		}
	}
	return ""
}

// CurrentProfilePath returns the profile the user is on, given everything that is
// running and the path of the last account MCS activated.
//
// The record wins while that account is still open. It goes stale the moment the
// user closes it or opens something else by hand, so a record naming a profile
// that is not running is discarded rather than trusted; what is running is the
// evidence, and the record only ever chooses between several running profiles.
func CurrentProfilePath(running []string, lastActivatedPath string) string {
	if len(running) == 0 {
		return ""
	}
	if lastActivatedPath != "" {
		for _, p := range running {
			if platform.SamePath(p, lastActivatedPath) {
				return p
			}
		}
	}
	return running[0]
}
