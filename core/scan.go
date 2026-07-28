package core

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// ScannedAccount is one row of the rescan review: a deduped account with its
// completeness, identity, activity stats, and derived note.
//
// A row is one of three kinds:
//
//	Complete            an account signed in to HomeFolder; switchable now
//	SignedOut           a real profile folder nobody has signed in to yet
//	neither             a ghost: session data whose account is signed in nowhere
type ScannedAccount struct {
	UUID        string
	Complete    bool   // true = live login somewhere (switchable); false = ghost
	HomeFolder  string // folder where it is the live login ("" if ghost)
	Email       string
	Account     AccountType
	Convos      int
	LastUpdated time.Time
	Note        string

	// SignedOut marks a profile folder that Claude Desktop has run with but
	// that has never been signed in to: it has a config.json but no account
	// and no sessions. There is nothing to switch to yet, which is exactly why
	// it has to be shown. Dropping these silently is what makes a user with a
	// second profile ready and waiting see "only one account found" and have
	// no idea what to do about it.
	SignedOut bool
}

// SignedOutNote is the review note for a profile folder awaiting its one-time
// sign-in. Sign-in is per folder and permanent, so this is a single step, not
// something the user will have to repeat on every switch.
const SignedOutNote = "Not signed in yet. Select it here, then switch to it and sign in."

type bucketStat struct {
	Count       int
	LastUpdated time.Time
}

type dirScan struct {
	Folder    string
	LiveUUID  string // config.json lastKnownAccountUuid ("" if logged out)
	HasConfig bool   // config.json exists, i.e. this really is a profile folder
	Identity  AccountIdentity
	Account   AccountType
	Buckets   map[string]bucketStat // accountUUID -> stats (from claude-code-sessions/)
}

// deriveNote returns the review note for a row: incomplete rows are invalid data;
// complete Team rows warn that conversations can't be synced; complete personal
// rows have no note.
func deriveNote(complete bool, acct AccountType) string {
	if !complete {
		return "Invalid account data"
	}
	if acct == AccountTeam {
		return "Team account — conversations can't be synced"
	}
	return ""
}

// assembleAccounts turns per-dir scans into deduped review rows. One complete row
// per live-login dir; one ghost row per orphan UUID (a bucket that is no dir's
// live login anywhere), summed across dirs. A bucket whose UUID is some dir's
// live login is a stale duplicate and is folded away. Output is sorted: complete
// first by HomeFolder, then ghosts by UUID.
func assembleAccounts(scans []dirScan) []ScannedAccount {
	live := map[string]bool{}
	for _, s := range scans {
		if s.LiveUUID != "" {
			live[s.LiveUUID] = true
		}
	}
	var out []ScannedAccount
	for _, s := range scans {
		if s.LiveUUID == "" {
			continue
		}
		b := s.Buckets[s.LiveUUID] // zero if this account has no Code sessions yet
		out = append(out, ScannedAccount{
			UUID:        s.LiveUUID,
			Complete:    true,
			HomeFolder:  s.Folder,
			Email:       s.Identity.Email,
			Account:     s.Account,
			Convos:      b.Count,
			LastUpdated: b.LastUpdated,
			Note:        deriveNote(true, s.Account),
		})
	}
	ghost := map[string]*ScannedAccount{}
	for _, s := range scans {
		for uuid, b := range s.Buckets {
			if uuid == s.LiveUUID || live[uuid] {
				continue // own live bucket, or stale dup of an account live elsewhere
			}
			g := ghost[uuid]
			if g == nil {
				g = &ScannedAccount{UUID: uuid, Complete: false, Account: AccountUnknown, Note: deriveNote(false, AccountUnknown)}
				ghost[uuid] = g
			}
			g.Convos += b.Count
			if b.LastUpdated.After(g.LastUpdated) {
				g.LastUpdated = b.LastUpdated
			}
		}
	}
	// Profile folders waiting for their one-time sign-in. No account, no
	// sessions, but a real folder the user can be pointed at.
	for _, s := range scans {
		if s.LiveUUID != "" || len(s.Buckets) > 0 || !s.HasConfig {
			continue
		}
		out = append(out, ScannedAccount{
			HomeFolder: s.Folder,
			SignedOut:  true,
			Account:    AccountUnknown,
			Note:       SignedOutNote,
		})
	}
	for _, g := range ghost {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if ri, rj := rowRank(out[i]), rowRank(out[j]); ri != rj {
			return ri < rj
		}
		if out[i].Complete || out[i].SignedOut {
			return out[i].HomeFolder < out[j].HomeFolder
		}
		return out[i].UUID < out[j].UUID
	})
	return out
}

// rowRank orders the review: accounts you can switch to now, then folders
// waiting to be signed in to, then ghosts.
func rowRank(a ScannedAccount) int {
	switch {
	case a.Complete:
		return 0
	case a.SignedOut:
		return 1
	default:
		return 2
	}
}

// gatherDir reads one profile dir into a dirScan: its live-login UUID (cheap,
// config.json), its session buckets (count + newest mtime), and — only for a
// live login — the account identity and type (best-effort Local Storage reads).
func gatherDir(p *platform.ProfileInfo) dirScan {
	ds := dirScan{Folder: p.Name, Buckets: map[string]bucketStat{}}
	// config.json is what separates a Claude Desktop profile folder from any
	// other directory that happens to start with "Claude" (the Claude Code CLI
	// keeps one next door, for instance).
	if _, err := os.Stat(platform.GetProfileConfigPath(p.Path)); err == nil {
		ds.HasConfig = true
	}
	if uuid, err := platform.GetProfileAccountUUID(p.Path); err == nil {
		ds.LiveUUID = uuid
	}
	sessDir := platform.GetProfileSessionsDir(p.Path)
	if entries, err := os.ReadDir(sessDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			count, newest := countAndNewest(filepath.Join(sessDir, e.Name()))
			ds.Buckets[e.Name()] = bucketStat{Count: count, LastUpdated: newest}
		}
	}
	if ds.LiveUUID != "" {
		if id, err := readLocalStorageIdentity(p.Path); err == nil {
			ds.Identity = id
		}
		if at, err := DetectAccountType(p.Path); err == nil {
			ds.Account = at
		}
	}
	return ds
}

// countAndNewest walks a session bucket, counting *.json files and tracking the
// newest modification time.
func countAndNewest(dir string) (int, time.Time) {
	var count int
	var newest time.Time
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}
		count++
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return count, newest
}

// ScanAccounts scans the given profile dirs and returns the deduped review rows.
// Dirs that are not Claude Desktop profiles at all (no config.json, no login,
// no session bucket) are dropped.
func ScanAccounts(profiles []*platform.ProfileInfo) []ScannedAccount {
	var scans []dirScan
	for _, p := range profiles {
		ds := gatherDir(p)
		if ds.LiveUUID == "" && len(ds.Buckets) == 0 && !ds.HasConfig {
			continue
		}
		scans = append(scans, ds)
	}
	return assembleAccounts(scans)
}
