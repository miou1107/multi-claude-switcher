package panelui

import (
	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
)

// BuildProfiles turns the discovered profile directories into the view models the
// account list, the sync screen and the merge screen all render from.
//
// It lives here, shared by both hosts, because it used to live in each of them.
// They drifted: ProfileVM.SignedIn was set on Windows and not on macOS, so on macOS
// every account arrived as "not signed in", the sync screen offered no pairs at all,
// and it told the user two signed-in accounts were not signed in. Nothing caught it,
// because the renderer's tests build their own view models and never run the code
// that fills them in. One builder, one place to add a field, and a test that can
// reach it.
//
//   - managed is the user's curated folder list. A nil slice means "never
//     configured" (first run) and is not the same as an empty one, which means the
//     user chose nothing.
//   - pending is the folder names of profiles MCS has just created and is waiting
//     for the user to sign in to. They are shown regardless of the managed list, so
//     a freshly created profile is never invisible in the panel it was made from —
//     which matters on macOS, where such a profile carries no `Managed` flag.
//   - runningPath is the data directory Claude Desktop is running on, or "".
//   - plan looks up a profile's subscription label by path. It is a function because
//     each host caches it differently and the read is expensive.
func BuildProfiles(profiles []*platform.ProfileInfo, managed []string, pending []string, runningPath string, plan func(path string) string) []ProfileVM {
	pendingSet := map[string]bool{}
	for _, f := range pending {
		pendingSet[f] = true
	}
	var out []ProfileVM
	for _, p := range profiles {
		uuid, uuidErr := platform.GetProfileAccountUUID(p.Path)
		signedIn := uuidErr == nil
		if !includeProfile(managed, p.Name, signedIn, p.Managed, pendingSet[p.Name]) {
			continue
		}
		out = append(out, ProfileVM{
			Folder:   p.Name,
			Name:     core.DisplayName(p.Name),
			Current:  runningPath != "" && p.Path == runningPath,
			Plan:     plan(p.Path),
			SignedIn: signedIn,
			// Its own account's bucket only. A profile can hold orphaned buckets
			// from accounts signed out inside Claude Desktop, and those are not
			// what a sync moves.
			Convos: p.UUIDBuckets[uuid],
			// The account this profile is signed in to, empty when none. The list
			// groups by it to spot two profiles holding one account. On the error
			// path GetProfileAccountUUID returns "", which is the same empty
			// sentinel, so no special-casing of the signed-out profile is needed.
			UUID: uuid,
		})
	}
	return out
}

// includeProfile decides whether a discovered folder belongs in the panel.
//
// Once the user has curated a managed list, it is the whole answer — including
// folders with no account in them, which is how the user reaches one to sign in. On
// first run there is no list yet, so fall back to what is usable: anything signed in,
// plus anything MCS created itself, which has no account until the user signs in and
// would otherwise be invisible in the very panel it was created from.
func includeProfile(managed []string, folder string, signedIn, mcsCreated, pending bool) bool {
	if pending {
		// Just created and awaiting its one-time sign-in. It must show whether the
		// list is first-run or curated-but-not-yet-listing-it, or it is invisible in
		// the very panel it was created from.
		return true
	}
	if managed != nil {
		for _, m := range managed {
			if m == folder {
				return true
			}
		}
		return false
	}
	return signedIn || mcsCreated
}
