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
//   - runningPath is the data directory Claude Desktop is running on, or "".
//   - plan looks up a profile's subscription label by path. It is a function because
//     each host caches it differently and the read is expensive.
func BuildProfiles(profiles []*platform.ProfileInfo, managed []string, runningPath string, plan func(path string) string) []ProfileVM {
	var out []ProfileVM
	for _, p := range profiles {
		uuid, uuidErr := platform.GetProfileAccountUUID(p.Path)
		signedIn := uuidErr == nil
		if !includeProfile(managed, p.Name, signedIn, p.Managed) {
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
func includeProfile(managed []string, folder string, signedIn, mcsCreated bool) bool {
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
