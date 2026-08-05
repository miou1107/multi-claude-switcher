package core

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
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
// machines measured. Nothing has produced a new one since, so this population
// only shrinks.

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
	Buckets   map[bucket][]bucketFile
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
// naming it is not describing this profile's reality.
func readBucketLooksRight(ps profileSessions) bool {
	_, ok := newestIn(ps.Buckets[ps.Read])
	return ok
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
		readNewest, _ := newestIn(p.Buckets[p.Read])
		for b, files := range p.Buckets {
			if b == p.Read {
				continue
			}
			// The whole bucket must be older than the last thing written to the
			// folder this profile reads. This is the guard against the worst
			// thing here: GetProfileActiveOrgUUID is a heuristic over a private
			// format, and someone who launches into one organization and
			// switches to another in-app without relaunching leaves the stamp
			// naming the wrong one. Being wrong there is supposed to cost
			// visibility and never data (platform/activeorg.go says so), but the
			// pre-0.11.2 defect put the same conversation names under both
			// organizations of one account, and copyFile preserves modification
			// times, so every file in the genuinely live folder has an
			// equal-time counterpart in the believed-read one and would qualify.
			// The live organization would empty.
			//
			// A folder that stopped receiving writes before v0.11.2 shipped is
			// unambiguously older than one in use. A folder that is not is not
			// safe to call abandoned, whichever the stamp names.
			if newest, any := newestIn(files); !any || !newest.Before(readNewest) {
				continue
			}
			for _, f := range files {
				at, ok := readable[f.Rel]
				if !ok || at.Before(f.MTime) {
					continue
				}
				out = append(out, tidyMove{Profile: p.Profile, ProfilePath: p.Path, Bucket: b, Rel: f.Rel, Scanned: f})
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
	for _, m := range moves {
		src := filepath.Join(m.ProfilePath, "claude-code-sessions", m.Bucket.Account, m.Bucket.Org, m.Rel)
		dst := filepath.Join(dest, m.Profile, m.Bucket.Account, m.Bucket.Org, m.Rel)
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

		if len(ps.Buckets) == 0 {
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
		ps.Read, ps.ReadKnown = bucket{Account: account, Org: org}, true

		if !readBucketLooksRight(ps) {
			log.Printf("[Tidy] skipping %s: %s/%s is recorded as the folder it reads, but it is empty or another folder has been written more recently, so that record cannot be trusted",
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
		out = append(out, bucketFile{Rel: filepath.ToSlash(rel), MTime: info.ModTime()})
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
	if !fi.ModTime().Equal(scanned.MTime) {
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
	for _, m := range done {
		account := filepath.Join(m.ProfilePath, "claude-code-sessions", m.Bucket.Account)
		dir := filepath.Dir(filepath.Join(account, m.Bucket.Org, m.Rel))
		for dir != account && dir != filepath.Dir(dir) {
			if seen[dir] {
				break
			}
			seen[dir] = true
			if !removeIfEmpty(dir) {
				break // still holds something, and so does everything above it
			}
			dir = filepath.Dir(dir)
		}
		if !seen[account] {
			seen[account] = true
			removeIfEmpty(account)
		}
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
