package core

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// RemoveProfile takes an account off the switcher by moving its profile
// directory into the archive root, and returns where it landed.
//
// Nothing is deleted. The directory moves untouched, which is also why no backup
// is taken: a merge snapshots the profile it keeps because a merge writes into
// it, and this writes nothing.
//
// Order is resolve, refuse, move, then update state. The registries are written
// only once the folder has really left the scan path: a folder unmanaged while
// still in place is hidden from the panel and back on the next Rescan, so
// "removed" would not stay removed.
func RemoveProfile(plat platform.Platform, identity string) (string, error) {
	name := DisplayName(identity)

	// Platform refusals first, and the path they resolve to. This reads only, so a
	// refusal below still leaves the disk exactly as it was found.
	path, err := plat.PrepareRemove(identity)
	if err != nil {
		return "", err
	}
	if fi, statErr := os.Stat(path); statErr != nil || !fi.IsDir() {
		return "", fmt.Errorf("%s is no longer there. Run Rescan", name)
	}

	// Renaming a directory Claude has open is the one way this can corrupt data,
	// and POSIX rename will do it without complaint. Ask what is running rather
	// than consulting active.json: that record is empty on any machine where MCS
	// has not switched anything, which is exactly where a user who launched Claude
	// themselves would need the guard. Asking here, last before the rename, also
	// catches the panel that was drawn while Claude was closed.
	running, err := plat.DetectRunningProfiles()
	if err != nil {
		// Not knowing whether Claude holds the directory is not permission to
		// rename it, and a removal is never urgent enough to guess.
		return "", fmt.Errorf("could not check whether Claude is running, so %s was left alone: %w", name, err)
	}
	for _, r := range running {
		if platform.SamePath(r, path) {
			return "", fmt.Errorf("Claude is open on %s. Switch to another account or quit Claude, then remove it", name)
		}
	}

	dest, err := ArchiveProfile(identity, path, plat.ArchiveDir())
	if err != nil {
		// Nothing moved and no registry has been touched, so the account is still
		// listed and a retry is safe.
		return "", err
	}

	// From here the folder is gone from the scan path, so every registry naming it
	// is now wrong. Keep going past a failure: stopping at one would leave the
	// others describing a profile that no longer exists, which is worse than the
	// failure being reported. Every failure is returned, not merely logged, because
	// a display name left behind is silently inherited by any later profile that
	// reuses the identity and the user is the only one who can notice.
	var errs []error
	if err := RemoveManaged(identity); err != nil {
		errs = append(errs, fmt.Errorf("the managed list still lists it: %w", err))
	}
	if err := SetProfileName(identity, ""); err != nil {
		errs = append(errs, fmt.Errorf("its display name is still recorded, and a later profile reusing the name %q would inherit it: %w", identity, err))
	}
	// Pending entries are pruned only on sign-in, and a removed profile never
	// appears in FindProfiles again, so an entry left here would render a sign-in
	// prompt the user could never clear.
	if err := RemovePending(identity); err != nil {
		errs = append(errs, fmt.Errorf("its pending sign-in entry is still recorded: %w", err))
	}
	// A stale active.json resolves to "" through lastActivatedPath and is harmless,
	// but it is one more record naming something that is gone.
	if LoadActiveProfile() == identity {
		if err := SaveActiveProfile(""); err != nil {
			errs = append(errs, fmt.Errorf("it is still recorded as the last account used: %w", err))
		}
	}
	if len(errs) > 0 {
		joined := fmt.Errorf("%s was removed, but %w", name, errors.Join(errs...))
		log.Printf("remove: %v", joined)
		return dest, joined
	}
	return dest, nil
}
