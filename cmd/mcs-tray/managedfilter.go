package main

// menuIncludes decides whether a profile folder appears in the tray menu.
//
// When the managed registry exists (managed != nil) it is authoritative: only
// listed folders show. When it is absent (managed == nil, first run) we fall back
// to a cheap heuristic — show any dir with a live login, plus MSIX-managed
// (parked) profiles — so behavior matches the pre-rescan build until the user
// makes an explicit choice via Rescan.
func menuIncludes(managed []string, folder string, hasLiveLogin, managedFlag bool) bool {
	if managed != nil {
		for _, m := range managed {
			if m == folder {
				return true
			}
		}
		return false
	}
	return hasLiveLogin || managedFlag
}
