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
	// Each of these is written as a whole sentence, and says what the user has to
	// do about it, because errors.Join puts them on the screen one per line and
	// each one is read on its own. Three of the four need nothing done, and saying
	// so is the point: the panel lists the folders that exist and asks the
	// registries about those, so an entry naming a folder that is gone is never
	// drawn. The display name is the exception, and the only one worth an action.
	//
	// "Account", not "profile": a profile is the folder, and the user was never
	// shown that word anywhere else in the app.
	var errs []error
	if err := RemoveManaged(identity); err != nil {
		errs = append(errs, fmt.Errorf("The switcher's own account list still mentions it. Nothing needs doing: the panel only shows accounts whose folder is still there. (%w)", err))
	}
	if err := SetProfileName(identity, ""); err != nil {
		errs = append(errs, fmt.Errorf("Its name is still recorded as %q. If you sign in to this account again later it will come back under that name, which you can change with Rename. (%w)", name, err))
	}
	// Pending entries are pruned only on sign-in, so one left here would outlive
	// every folder it could describe.
	if err := RemovePending(identity); err != nil {
		errs = append(errs, fmt.Errorf("Its \"waiting to sign in\" note is still recorded. Nothing needs doing: that note is only ever shown against an account whose folder is still there. (%w)", err))
	}
	// A stale active.json resolves to "" through lastActivatedPath and is harmless,
	// but it is one more record naming something that is gone.
	if LoadActiveProfile() == identity {
		if err := SaveActiveProfile(""); err != nil {
			errs = append(errs, fmt.Errorf("It is still recorded as the account you used last. Nothing needs doing: switching to any account replaces that. (%w)", err))
		}
	}
	if len(errs) > 0 {
		// The summary is its own line rather than a "..., but" clause running into
		// the first complaint: errors.Join separates its entries with newlines, and
		// the screen draws one line per entry.
		joined := fmt.Errorf("%s was removed, but some of what the switcher had recorded about it could not be cleared.\n%w", name, errors.Join(errs...))
		log.Printf("remove: %v", joined)
		return dest, joined
	}
	return dest, nil
}
