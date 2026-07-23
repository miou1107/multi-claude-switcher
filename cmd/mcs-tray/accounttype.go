package main

import (
	"log"
	"sync"

	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
)

// acctTypes caches each profile's detected account type, keyed by profile path.
// Populated at startup and after every switch/sync; read by the menu build,
// markActive, and the warning gates. Never opens LevelDB inline.
var acctTypes = struct {
	sync.Mutex
	m map[string]core.AccountType
}{m: map[string]core.AccountType{}}

func setAcctType(path string, t core.AccountType) {
	acctTypes.Lock()
	acctTypes.m[path] = t
	acctTypes.Unlock()
}

func getAcctType(path string) core.AccountType {
	acctTypes.Lock()
	defer acctTypes.Unlock()
	return acctTypes.m[path] // zero value is AccountUnknown
}

// profileTitle composes a profile menu title: name, an optional "🏢 Team" tag
// for Team accounts, and an optional "(current)" marker.
func profileTitle(name string, t core.AccountType, current bool) string {
	title := name
	if t == core.AccountTeam {
		title += "  🏢 Team"
	}
	if current {
		title += "  (current)"
	}
	return title
}

// detectAccountTypes classifies every profile (best-effort) and updates the cache.
// Errors are logged and leave the profile as Unknown.
func detectAccountTypes(profiles []*platform.ProfileInfo) {
	for _, p := range profiles {
		t, err := core.DetectAccountType(p.Path)
		if err != nil {
			log.Printf("detect account type for %s: %v", p.Name, err)
		}
		setAcctType(p.Path, t)
	}
}
