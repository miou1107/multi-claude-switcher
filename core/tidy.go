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
	Profile   string
	Read      bucket
	ReadKnown bool
	Buckets   map[bucket][]bucketFile
}

// tidyMove is one file to relocate.
type tidyMove struct {
	Profile string
	Bucket  bucket
	Rel     string
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
			if b == p.Read {
				continue
			}
			for _, f := range files {
				at, ok := readable[f.Rel]
				if !ok || at.Before(f.MTime) {
					continue
				}
				out = append(out, tidyMove{Profile: p.Profile, Bucket: b, Rel: f.Rel})
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
	moved, skipped := 0, 0
	for _, m := range moves {
		src := filepath.Join(profilePathFor(profiles, m.Profile), "claude-code-sessions", m.Bucket.Account, m.Bucket.Org, m.Rel)
		dst := filepath.Join(dest, m.Profile, m.Bucket.Account, m.Bucket.Org, m.Rel)
		if err := moveFileInto(src, dst); err != nil {
			log.Printf("[Tidy] could not move %s: %v", m.Rel, err)
			skipped++
			continue
		}
		moved++
	}
	log.Printf("[Tidy] moved %d conversations no account could read into %s; %d could not be moved", moved, dest, skipped)
	removeEmptiedBuckets(profiles, moves)
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
		ps := profileSessions{Profile: p.Name, Buckets: map[bucket][]bucketFile{}}

		account, accErr := platform.GetProfileAccountUUID(p.Path)
		org, orgErr := platform.GetProfileActiveOrgUUID(p.Path)
		if accErr == nil && orgErr == nil && account != "" && org != "" {
			ps.Read, ps.ReadKnown = bucket{Account: account, Org: org}, true
		} else {
			// Fails closed. See profileSessions.ReadKnown.
			log.Printf("[Tidy] skipping %s, cannot tell which folder it reads (account: %v, organization: %v)", p.Name, accErr, orgErr)
			out = append(out, ps)
			continue
		}

		sessions := platform.GetProfileSessionsDir(p.Path)
		accounts, err := os.ReadDir(sessions)
		if err != nil {
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

func profilePathFor(profiles []*platform.ProfileInfo, name string) string {
	for _, p := range profiles {
		if p.Name == name {
			return p.Path
		}
	}
	return ""
}

// moveFileInto relocates one file, creating the destination's parents and
// refusing to overwrite anything already there.
//
// Refusing rather than overwriting matters on a second run in one day, which
// lands in the same tidied-<date> folder: whatever is already there came from
// the earlier run and is the copy worth keeping.
func moveFileInto(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s already exists", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

// removeEmptiedBuckets deletes the bucket directories a run emptied, and only
// those. A bucket that still holds anything is left exactly as it was.
func removeEmptiedBuckets(profiles []*platform.ProfileInfo, moves []tidyMove) {
	seen := map[string]bool{}
	for _, m := range moves {
		dir := filepath.Join(profilePathFor(profiles, m.Profile), "claude-code-sessions", m.Bucket.Account, m.Bucket.Org)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		removeIfEmpty(dir)
		removeIfEmpty(filepath.Dir(dir)) // the account folder, if that was its last organization
	}
}

func removeIfEmpty(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !isOSMetadataFile(e.Name()) {
			return // still holds something
		}
	}
	// Only the operating system's own leftovers. Those came with the folder
	// rather than from the user, so they go with it.
	for _, e := range entries {
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
	if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
		log.Printf("[Tidy] could not remove the emptied folder %s: %v", dir, err)
	}
}

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
