package core

import (
	"fmt"
	"log"
	"os"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// CreateProfileRequest describes a profile to create. RecoverUUID and Sources are
// set together, and only on the recovery path: they name the orphaned account
// whose conversations should end up in the new profile, and every profile
// currently holding some of them.
//
// Sources carry their own paths, straight from the scan. Nothing here rebuilds a
// path from a folder name: on the Store build the data root is not AppSupportDir()
// and a parked profile is a level deeper still.
type CreateProfileRequest struct {
	Name        string
	RecoverUUID string
	Sources     []GhostSource
}

// CreatedProfile is what a create produced: the identity every MCS registry keys
// on, and the directory the data lives in. They are separate because they differ
// on the Store build, where the directory is the shared slot and always called
// "Claude". Never derive one from the other.
type CreatedProfile struct {
	Identity string
	DataDir  string
}

// ProfileCreator runs the create-a-profile sequence. It exists so the macOS and
// Windows hosts share one ordering rather than each growing their own.
type ProfileCreator struct {
	Plat platform.Platform
}

func NewProfileCreator(p platform.Platform) *ProfileCreator {
	return &ProfileCreator{Plat: p}
}

// Create validates the name, quits Claude, creates the profile, arranges for a
// recovered account's conversations to follow, registers the profile as awaiting
// sign-in, and opens Claude on it.
//
// The order matters: nothing is written to MCS's own state until the disk work has
// succeeded, and nothing on disk is touched until the name is known to be good. A
// recovery that cannot copy its conversations removes the profile it just made, so
// a retry starts from a clean slate rather than colliding with a half-made folder.
func (c *ProfileCreator) Create(req CreateProfileRequest) (*CreatedProfile, error) {
	clean, err := ValidateProfileName(req.Name)
	if err != nil {
		return nil, err
	}
	if req.RecoverUUID != "" && len(req.Sources) == 0 {
		return nil, fmt.Errorf("internal: a recovery needs the profiles holding the conversations")
	}

	// Claude holds its data dir open, and on the Store build the profile is created
	// by moving that very directory.
	if err := c.Plat.TerminateApp(); err != nil {
		return nil, err
	}

	// Both come back from the platform. The identity is what FindProfiles will
	// report and what every registry below keys on; the directory is where the data
	// goes. On the Store build they differ, and filepath.Base of the directory is
	// "Claude" for every profile — deriving the identity that way is the defect this
	// signature exists to prevent.
	identity, dataDir, err := c.Plat.CreateProfile(clean)
	if err != nil {
		return nil, err
	}

	// discard undoes the directory this call created. The sources a recovery reads
	// from are never written to, so throwing the new profile away loses nothing and
	// leaves the name free for a retry.
	//
	// It matters most on the recovery path. A profile left behind holds a second
	// copy of the recovered account's conversations, and the scanner adds up the
	// buckets it finds, so the ghost the user was trying to clear would come back
	// claiming twice the number of chats it has — and a retry would add a third.
	//
	// On the Store build the data directory does not exist at this point: the
	// packaged app makes it on first launch. This is a no-op there and the parked
	// state stays as it is, which is safe — state.json still names the profile, so
	// it remains discoverable and only MCS's own registries are missing it.
	discard := func() {
		if err := os.RemoveAll(dataDir); err != nil {
			log.Printf("could not clean up the half-made profile %q: %v", dataDir, err)
		}
	}

	if req.RecoverUUID != "" {
		sources := make([]platform.RecoverySource, 0, len(req.Sources))
		for _, s := range req.Sources {
			sources = append(sources, platform.RecoverySource{Folder: s.Folder, Path: s.Path, UUID: req.RecoverUUID})
		}
		if err := c.Plat.PrepareRecovery(dataDir, sources); err != nil {
			discard()
			return nil, err
		}
	}

	if err := AddPending(identity, req.RecoverUUID); err != nil {
		discard()
		return nil, fmt.Errorf("record the new profile: %w", err)
	}
	// Add to the managed list only when the user has already curated one. The panel
	// shows this profile at once through the pending registry above, so it does not
	// need a managed entry to be visible. Writing one on a first-run (unset) list
	// would turn it into a one-element list and hide every account not in it — the
	// user's existing accounts would disappear the moment they added a second. On a
	// curated list it is added so it survives once the pending entry is pruned.
	if err := AddManagedIfCurated(identity); err != nil {
		if rmErr := RemovePending(identity); rmErr != nil {
			log.Printf("could not clear the pending entry for %q after a failed create: %v", identity, rmErr)
		}
		discard()
		return nil, fmt.Errorf("update the managed list: %w", err)
	}
	// Show the name the user typed, whatever the platform chose to call the folder.
	// Without this the same profile reads as "Claude_Work" on one platform and
	// "Work" on the other. A failure here is logged, not fatal: the profile exists,
	// is registered and works, and undoing a successful creation over a cosmetic
	// detail would be worse than the symptom.
	if err := SetProfileName(identity, clean); err != nil {
		log.Printf("could not record the display name for %q: %v", identity, err)
	}

	created := &CreatedProfile{Identity: identity, DataDir: dataDir}
	if err := c.Plat.LaunchProfile(dataDir); err != nil {
		return created, fmt.Errorf("the profile is ready but Claude didn't open: %w", err)
	}
	// Claude is now open on the new profile, so this is the account the user is
	// on. Recording it here rather than leaving it to the caller keeps every path
	// that moves the user in agreement; a record naming an account they left is
	// what makes a later switch close the wrong one.
	if err := SaveActiveProfile(identity); err != nil {
		log.Printf("could not record %q as the active account: %v", identity, err)
	}
	return created, nil
}
