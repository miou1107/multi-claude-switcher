package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// Conversations live at
// claude-code-sessions/<accountUUID>/<orgUUID>/, and Claude Desktop opens
// exactly one of those folders: the account it is signed in as, under the
// organization it is working in. Sync before v0.11.2 rewrote the account
// segment and left the organization segment naming the source's, so everything
// it wrote landed somewhere no account looks.
//
// The fix that followed re-copied those conversations to the right place, which
// makes the strays duplicates as well as invisible. 564 files and 36 MB on the
// machines measured.
//
// The population mostly shrinks but is not closed: orgRemapper still copies
// buckets under a third organization verbatim, and copies everything unchanged
// when either side's organization cannot be read (core/sync.go). So this runs at
// every launch rather than once, and finding nothing has to stay cheap.

// bucket names one <account>/<organization> folder inside one profile.
type bucket struct {
	Account string
	Org     string
}

// bucketFile is one file inside a bucket, addressed the way a counterpart in
// another bucket would be: by its path relative to the bucket root.
type bucketFile struct {
	Rel   string
	MTime time.Time
	Size  int64
}

// profileSessions is everything the decision needs to know about one profile.
//
// ReadKnown false means the profile's own read bucket could not be determined:
// no account signed in, an unreadable config.json, or no organization stamp.
// Such a profile contributes nothing in either direction. It offers no
// candidates, because "unknown" must never be read as "reads nothing", which
// would make every bucket it owns a candidate. And it offers no counterparts,
// because a file in a bucket that may or may not be the read one is not proof
// that anything is readable.
type profileSessions struct {
	Profile string
	// Path, not the name, is what every move is built from. On the Windows
	// Store build two entries can carry the same display name (the live slot
	// and a container directory, after a swap whose state write failed), and
	// resolving a name back to a path would then plan against one and execute
	// against the other.
	Path      string
	Read      bucket
	ReadKnown bool
	// SignedInOrgs is every organization this profile has an allowlist stamp
	// for, i.e. every one it has been signed into. Membership is a fact read
	// from config.json; which of them is ACTIVE is a heuristic. The difference
	// is the whole safety argument, see tidyCandidates.
	SignedInOrgs map[string]bool
	Buckets      map[bucket][]bucketFile
}

// tidyMove is one file to relocate.
type tidyMove struct {
	Profile     string // for logs and for the destination path
	ProfilePath string // where the file actually is; never re-derived from Profile
	Bucket      bucket
	Rel         string
	// Scanned is what the file looked like when the decision was made. The move
	// refuses if it has changed since: the scan and the moves are separated by
	// however long the whole scan took, and a sync or a switch writing in that
	// window would otherwise have its work moved away.
	Scanned bucketFile
	// ScannedRead is the bucket the profile at ProfilePath was reading when the
	// scan ran. Re-checked before the profile's moves are executed: on the
	// Windows Store build a switch RENAMES another profile into the same path,
	// and the file there is then that profile's own byte-and-time identical
	// copy, which the per-file check cannot tell apart.
	ScannedRead bucket
}

// newestIn returns the most recent modification time in a bucket, and false for
// an empty one.
func newestIn(files []bucketFile) (time.Time, bool) {
	var newest time.Time
	for _, f := range files {
		if f.MTime.After(newest) {
			newest = f.MTime
		}
	}
	return newest, len(files) > 0
}

// readBucketLooksRight reports whether the bucket believed to be read behaves
// like one at all: it must exist and hold something. A profile in use has
// conversations in the folder Claude opens, and an empty one means the record
// naming it is not describing this profile.
func readBucketLooksRight(ps profileSessions) bool {
	_, ok := newestIn(ps.Buckets[ps.Read])
	return ok
}

// safeToMoveFrom reports whether a bucket is one this profile cannot possibly
// be reading right now.
//
// This is the guard against the worst outcome available here, and it is the
// second attempt. The first compared how recently each folder had been written
// and was wrong: in the very scenario it was written for, the folder the
// heuristic wrongly names is the one the last launch wrote to, which is the
// causal reason its stamp is newest. Shifting the reproduction by one second
// brought the data loss straight back. A second heuristic drawn from the same
// signal cannot correct the first.
//
// What works is that the two segments of a bucket are not equally certain:
//
//   - The ACCOUNT is read straight out of config.json's lastKnownAccountUuid.
//     It is a recorded fact. A bucket for any other account cannot be the one
//     Claude is reading, whatever else is true.
//
//   - The ORGANIZATION is read from a side effect: GetProfileActiveOrgUUID
//     takes the newest allowlist stamp. Review argued that an organization
//     switched into in-app would carry no stamp until the next launch, which
//     would make the live folder look abandoned. That was measured rather than
//     argued: switching organization inside Claude Desktop rewrote the new
//     organization's stamp within a second, and the profile that was not
//     switched did not move, as a control. So the live organization always
//     carries the newest stamp, and MEMBERSHIP is a fact: an organization with
//     NO stamp has never been opened here and cannot be the one being read.
//
//     That measurement is one version on one machine, of a format nobody
//     documents. It is the reason this rule is safe today, and the reason the
//     rule is written as "never opened" rather than "not the newest stamp":
//     the weaker claim survives the behaviour changing.
//
// So: any bucket under another account is safe, and a bucket under this
// profile's own account is safe only if this profile has never been signed into
// that organization.
//
// That is not a compromise, it is a description of the defect. The pre-0.11.2
// sync copied conversations in under the SOURCE profile's organization, which
// the target had typically never joined, so the folders it created are exactly
// the ones with no stamp.
//
// The cost is real and was weighed: an organization this profile HAS been in,
// holding conversations it no longer reads, is left alone. On the machine
// measured that is 122 of 564 files. The alternative is a rule that cannot tell
// that folder from the one the user is working in.
func safeToMoveFrom(ps profileSessions, b bucket) bool {
	if b.Account != ps.Read.Account {
		return true
	}
	return !ps.SignedInOrgs[b.Org]
}

// tidyCandidates returns the files that may be moved out of the buckets no
// account reads.
//
// A file qualifies only when some profile's READ bucket holds the same relative
// path with a modification time no older than this file's. Both halves matter:
//
//   - Counterparts are looked for across every profile, not just the file's
//     own. The defect refiled a conversation under a different account, so its
//     readable copy is usually somewhere else entirely.
//   - "No older" is the whole safety margin. The measurement behind this found
//     425 files byte-identical and 138 differing only in fields like
//     lastFocusedAt, with the readable copy newer in every diverging case.
//     That is an observation, not a guarantee. A file whose only readable
//     counterpart is older is the one case where moving loses something.
//
// Anything that fails either half stays where it is. Moving it would not make
// it readable, only harder for a later version to find.
func tidyCandidates(profiles []profileSessions) []tidyMove {
	// The newest modification time each relative path has in any read bucket.
	// Newest is the most favourable reading, and being generous here is safe:
	// a newer readable copy is a stronger reason to let the stray go.
	readable := map[string]time.Time{}
	for _, p := range profiles {
		if !p.ReadKnown {
			continue
		}
		for _, f := range p.Buckets[p.Read] {
			if at, seen := readable[f.Rel]; !seen || f.MTime.After(at) {
				readable[f.Rel] = f.MTime
			}
		}
	}

	var out []tidyMove
	for _, p := range profiles {
		if !p.ReadKnown {
			continue
		}
		for b, files := range p.Buckets {
			if b == p.Read || !safeToMoveFrom(p, b) {
				continue
			}
			for _, f := range files {
				at, ok := readable[f.Rel]
				if !ok || at.Before(f.MTime) {
					continue
				}
				out = append(out, tidyMove{Profile: p.Profile, ProfilePath: p.Path, Bucket: b, Rel: f.Rel, Scanned: f, ScannedRead: p.Read})
			}
		}
	}
	sortTidyMoves(out)
	return out
}

// TidyMisfiled moves the conversations the pre-0.11.2 sync filed where no
// account can read them into the backup folder, preserving
// <profile>/<account>/<organization>/ so putting them back is a move in the
// other direction.
//
// It reports nothing and cannot fail its caller. This is housekeeping, and
// housekeeping must never be the thing that breaks: the recover is here because
// a panic would otherwise take down whatever goroutine the host started this
// on.
func TidyMisfiled(profiles []*platform.ProfileInfo, backupRoot string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Tidy] panicked and was contained, nothing further was moved: %v", r)
		}
	}()

	scanned := scanForTidy(profiles)
	moves := tidyCandidates(scanned)
	if len(moves) == 0 {
		log.Printf("[Tidy] nothing to tidy: no conversations are filed where no account can read them")
		return
	}

	dest := filepath.Join(backupRoot, tidiedDirName(time.Now()))
	moved, skipped, consecutive := 0, 0, 0
	var done []tidyMove
	occupied := map[string]bool{}
	for _, m := range moves {
		// Once per profile, before touching it. A per-file check cannot catch a
		// profile being substituted underneath: the file at that path is then
		// another profile's copy, identical in size and time because copyFile
		// preserves both, which is how the duplicates came to exist at all.
		if _, checked := occupied[m.ProfilePath]; !checked {
			occupied[m.ProfilePath] = stillOccupiedBy(m.ProfilePath, m.ScannedRead)
			if !occupied[m.ProfilePath] {
				log.Printf("[Tidy] %s is no longer the profile that was examined, skipping everything planned for it", m.Profile)
			}
		}
		if !occupied[m.ProfilePath] {
			skipped++
			continue
		}
		src := filepath.Join(m.ProfilePath, "claude-code-sessions", m.Bucket.Account, m.Bucket.Org, m.Rel)
		// Keyed by the path, not the display name. Two Windows Store entries
		// can share a name, and both were fed by the same defective sync, so
		// their strays collide on exactly the paths they have in common: one
		// profile's copy would fail to move on every run, forever.
		dst := filepath.Join(dest, destDirFor(m), m.Bucket.Account, m.Bucket.Org, m.Rel)
		if err := moveFileInto(src, dst, m.Scanned); err != nil {
			log.Printf("[Tidy] could not move %s: %v", m.Rel, err)
			skipped++
			consecutive++
			if consecutive >= tidyGiveUpAfter {
				// Something systemic: a redirected AppData making every rename
				// a cross-device error, a permission wall, a tree that has just
				// been moved. Carrying on would put one line per file in the
				// log at every launch, forever, for a run that cannot work.
				log.Printf("[Tidy] %d moves in a row failed, stopping; %d moved before that", consecutive, moved)
				break
			}
			continue
		}
		consecutive = 0
		moved++
		done = append(done, m)
	}
	log.Printf("[Tidy] moved %d conversations no account could read into %s; %d could not be moved", moved, dest, skipped)
	removeEmptiedBuckets(done)
}

// stillOccupiedBy reports whether the profile living at a path is still the one
// the scan examined, judged by the account and organization it reads.
//
// The Windows Store build has one shared slot directory, and a switch renames
// the current profile out and the target in. A tidy that scanned the slot and
// then executed against it would be operating on a different profile's data,
// and every per-file check would pass, because the files it planned to move are
// byte-and-time identical to the ones that arrived.
func stillOccupiedBy(profilePath string, scanned bucket) bool {
	account, err := platform.GetProfileAccountUUID(profilePath)
	if err != nil || account != scanned.Account {
		return false
	}
	org, err := platform.GetProfileActiveOrgUUID(profilePath)
	return err == nil && org == scanned.Org
}

// destDirFor names a profile's folder inside a tidied run. The display name is
// what a person reads, with a short digest of the path behind it so two
// profiles sharing a name cannot share a destination.
func destDirFor(m tidyMove) string {
	sum := sha256.Sum256([]byte(m.ProfilePath))
	return m.Profile + "-" + hex.EncodeToString(sum[:])[:8]
}

// tidiedDirName is where a run puts what it moves. Not a name parseBackupName
// accepts, so the backup pruner never considers it, the same way it never
// considers a folder somebody made by hand.
func tidiedDirName(at time.Time) string {
	return "tidied-" + at.Format("20060102")
}

// scanForTidy reads each profile's buckets and works out which one it reads.
func scanForTidy(profiles []*platform.ProfileInfo) []profileSessions {
	var out []profileSessions
	for _, p := range profiles {
		ps := profileSessions{Profile: p.Name, Path: p.Path, Buckets: map[bucket][]bucketFile{}}

		sessions := platform.GetProfileSessionsDir(p.Path)
		accounts, err := os.ReadDir(sessions)
		if err != nil {
			// A profile with no session tree is the ordinary case for one that
			// has never been signed in to, and says nothing worth logging.
			if !os.IsNotExist(err) {
				log.Printf("[Tidy] could not read %s: %v", sessions, err)
			}
			out = append(out, ps)
			continue
		}
		for _, a := range accounts {
			if !a.IsDir() {
				continue
			}
			orgs, err := os.ReadDir(filepath.Join(sessions, a.Name()))
			if err != nil {
				log.Printf("[Tidy] could not read %s: %v", filepath.Join(sessions, a.Name()), err)
				continue
			}
			for _, o := range orgs {
				if !o.IsDir() {
					continue
				}
				b := bucket{Account: a.Name(), Org: o.Name()}
				ps.Buckets[b] = readBucketFiles(filepath.Join(sessions, a.Name(), o.Name()))
			}
		}

		holdsSomething := false
		for _, files := range ps.Buckets {
			if len(files) > 0 {
				holdsSomething = true
				break
			}
		}
		if !holdsSomething {
			out = append(out, ps) // nothing here either way
			continue
		}

		account, accErr := platform.GetProfileAccountUUID(p.Path)
		org, orgErr := platform.GetProfileActiveOrgUUID(p.Path)
		if accErr != nil || orgErr != nil || account == "" || org == "" {
			// Fails closed. See profileSessions.ReadKnown. Logged only when the
			// profile actually holds conversations, or a directory that merely
			// starts with "Claude" produces this line at every launch forever.
			log.Printf("[Tidy] skipping %s, cannot tell which folder it reads (account: %v, organization: %v)", p.Name, accErr, orgErr)
			out = append(out, ps)
			continue
		}
		orgs, orgsErr := platform.GetProfileSignedInOrgs(p.Path)
		if orgsErr != nil {
			// Without the membership set every organization looks unvisited,
			// which is the permissive reading. Fails closed instead.
			log.Printf("[Tidy] skipping %s, cannot tell which organizations it has been signed into: %v", p.Name, orgsErr)
			out = append(out, ps)
			continue
		}
		ps.Read, ps.ReadKnown, ps.SignedInOrgs = bucket{Account: account, Org: org}, true, orgs

		if !readBucketLooksRight(ps) {
			log.Printf("[Tidy] skipping %s: %s/%s is recorded as the folder it reads but holds nothing, so that record cannot be trusted",
				p.Name, account, org)
			ps.ReadKnown = false
		}
		out = append(out, ps)
	}
	return out
}

func readBucketFiles(dir string) []bucketFile {
	var out []bucketFile
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || isOSMetadataFile(info.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil
		}
		out = append(out, bucketFile{Rel: filepath.ToSlash(rel), MTime: info.ModTime(), Size: info.Size()})
		return nil
	})
	return out
}

// moveFileInto relocates one file, refusing both to overwrite anything at the
// destination and to move a source that has changed since it was examined.
//
// Refusing to overwrite matters on a second run in one day, which lands in the
// same tidied-<date> folder: whatever is there came from the earlier run.
//
// Refusing a changed source is what closes the window between deciding and
// acting. The decision is made from a scan of every profile, which takes as long
// as it takes, and a sync or a switch writing into that bucket meanwhile would
// otherwise have a live file moved out from under it on the strength of a
// judgement made about a different file.
func moveFileInto(src, dst string, scanned bucketFile) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is no longer a regular file", src)
	}
	if !fi.ModTime().Equal(scanned.MTime) || fi.Size() != scanned.Size {
		// Size as well as time. copyFile deliberately restores the source's
		// modification time so sync's newest-wins comparison works, so a
		// concurrent sync can replace the contents while leaving a time this
		// would accept.
		return fmt.Errorf("%s changed since it was examined", src)
	}
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s already exists", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

// removeEmptiedBuckets deletes the directories this run emptied, and only
// those. It is given the moves that SUCCEEDED, not the ones that were planned:
// a bucket whose every move failed has not been emptied by this run, and
// removing it because something else emptied it meanwhile is not this code's
// decision to make.
//
// It climbs from the file's own directory up to the account level, so a bucket
// holding only projects/x/s1.json loses projects/x, then projects, then the
// organization and the account. Stopping at the bucket level, as an earlier
// version did, left every nested bucket behind entirely.
func removeEmptiedBuckets(done []tidyMove) {
	seen := map[string]bool{}
	accounts := map[string]bool{}
	for _, m := range done {
		account := filepath.Join(m.ProfilePath, "claude-code-sessions", m.Bucket.Account)
		dir := filepath.Dir(filepath.Join(account, m.Bucket.Org, m.Rel))
		for dir != account && strings.HasPrefix(dir, account+string(filepath.Separator)) {
			if seen[dir] {
				break
			}
			if !removeIfEmpty(dir) {
				// Still holds something. NOT marked seen: a later move can
				// empty the last sibling, and marking it here left the parent,
				// the bucket and the account behind as empty directories.
				break
			}
			seen[dir] = true
			dir = filepath.Dir(dir)
		}
		accounts[account] = true
	}
	// Swept after every climb rather than during: a profile with strays in two
	// organizations of one account reached the account level while the second
	// organization was still there, and left the emptied account behind.
	for account := range accounts {
		removeIfEmpty(account)
	}
}

// removeIfEmpty removes a directory holding nothing but the operating system's
// own leftovers, and reports whether it went.
func removeIfEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !isOSMetadataFile(e.Name()) {
			return false // still holds something
		}
	}
	// Only the operating system's own leftovers. Those came with the folder
	// rather than from the user, so they go with it.
	for _, e := range entries {
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
	if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
		log.Printf("[Tidy] could not remove the emptied folder %s: %v", dir, err)
		return false
	}
	return true
}

// tidyGiveUpAfter bounds how many consecutive failures a run tolerates before
// concluding the problem is systemic rather than per-file.
const tidyGiveUpAfter = 10

// sortTidyMoves gives a run a deterministic order, so a log reads the same way
// twice and a test can compare without sorting at the call site.
func sortTidyMoves(m []tidyMove) {
	sort.Slice(m, func(i, j int) bool {
		if m[i].Profile != m[j].Profile {
			return m[i].Profile < m[j].Profile
		}
		if m[i].Bucket != m[j].Bucket {
			if m[i].Bucket.Account != m[j].Bucket.Account {
				return m[i].Bucket.Account < m[j].Bucket.Account
			}
			return m[i].Bucket.Org < m[j].Bucket.Org
		}
		return m[i].Rel < m[j].Rel
	})
}

// StartTidyMisfiled runs the tidy on a goroutine and returns immediately.
//
// Both hosts call this and nothing else, so there is one line to keep in step
// rather than a sequence: finding the profiles, choosing the backup root and
// deciding to run in the background are all decided here, not twice.
//
// It is not attached to a switch, a sync or a backup. It has nothing to do with
// any of them, and those are the moments the user is waiting on. After the
// first run there is nothing left to find and the cost is one walk of each
// profile's session tree.
func StartTidyMisfiled(plat platform.Platform) {
	go func() {
		profiles, err := plat.FindProfiles()
		if err != nil {
			log.Printf("[Tidy] could not list profiles, nothing was tidied: %v", err)
			return
		}
		TidyMisfiled(profiles, NewBackupManager("").BackupRootDir)
	}()
}
