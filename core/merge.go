package core

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// MergeRequest names the two profiles to merge, by identity. Paths are
// deliberately absent: PrepareArchive can move both directories on the Store
// build, so a path chosen by the caller may be stale by the time the merge acts on
// it.
type MergeRequest struct {
	KeepIdentity    string
	ArchiveIdentity string
	// BackupRoot overrides where the keeper's pre-merge snapshot goes. Empty means
	// the default root, which is what production uses.
	BackupRoot string
}

// MergePlan is what a merge will actually do, computed before the user commits.
type MergePlan struct {
	// Combined is how many conversations the keeper will hold afterwards: the size
	// of the UNION of the two buckets, not the sum of their counts. A record both
	// profiles hold is one conversation, not two.
	Combined int
	// Conflicts counts records both profiles hold with different content where the
	// keeper's copy is the newer one. SyncSessions leaves those alone, and the merge
	// then moves the other copy out of the UI's reach, so the user has to be told
	// before committing.
	Conflicts int
	// Unreadable counts records that could not be compared. They are neither
	// combined nor counted as conflicts, because SyncSessions will not count them
	// either: it records a skip error and moves on.
	Unreadable int
	// ArchiveTo is where the profile being given up will be parked.
	ArchiveTo string
}

// MergePreview computes what a merge would do without doing any of it. The merge
// screen renders this, so the number it promises is the number the user gets.
//
// It only fails when a whole bucket cannot be walked. Per-file failures increment
// Unreadable, matching SyncSessions, which records them and carries on — a preview
// that aborted over one junk file would block a merge of hundreds of
// conversations, which is the failure mode the sync itself was fixed for.
func MergePreview(keepPath, archivePath, uuid string) (*MergePlan, error) {
	keepFiles, keepSkipped, err := sessionFilesByRelPath(filepath.Join(platform.GetProfileSessionsDir(keepPath), uuid))
	if err != nil {
		return nil, err
	}
	archiveFiles, archiveSkipped, err := sessionFilesByRelPath(filepath.Join(platform.GetProfileSessionsDir(archivePath), uuid))
	if err != nil {
		return nil, err
	}

	// The merge runs SyncSessions(archive -> keep), which files conversations under
	// the organization the KEEPER reads. The preview has to key the archive's files
	// the same way, or it compares one conversation's two versions as if they were
	// two unrelated conversations and promises a number the merge cannot deliver.
	// Same rule, same function, so the two cannot drift apart.
	remap := orgRemapper(archivePath, keepPath)
	remapped := make(map[string]string, len(archiveFiles))
	for rel, abs := range archiveFiles {
		if target, keep := remap(rel); keep {
			remapped[target] = abs
		}
	}
	archiveFiles = remapped

	plan := &MergePlan{Combined: len(keepFiles), Unreadable: keepSkipped + archiveSkipped}
	for rel, archiveAbs := range archiveFiles {
		keepAbs, both := keepFiles[rel]
		if !both {
			plan.Combined++ // only the other side has it, so it will be copied in
			continue
		}
		same, err := filesEqual(archiveAbs, keepAbs)
		if err != nil {
			plan.Unreadable++
			continue
		}
		if same {
			continue
		}
		// Different content. SyncSessions keeps whichever is newer, so this is only
		// a conflict — a version the merge will strand in the archive — when the
		// keeper's copy is the one that wins.
		ai, err1 := os.Stat(archiveAbs)
		ki, err2 := os.Stat(keepAbs)
		if err1 != nil || err2 != nil {
			plan.Unreadable++
			continue
		}
		if !ai.ModTime().After(ki.ModTime()) {
			plan.Conflicts++
		}
	}
	return plan, nil
}

// sessionFilesByRelPath maps each .json session file under bucket to its full
// path, keyed by path relative to bucket, and reports how many entries could not
// be read.
//
// An absent bucket is empty, not an error: a profile that has never used Code has
// no bucket, and that is a valid side of a merge.
//
// A failure on an entry INSIDE the bucket is counted and skipped, never returned.
// Returning it aborts the whole walk, which would fail the preview — and therefore
// block the merge — over one unreadable file, while the sync this preview must
// predict skips that file and carries on. Only a failure to read the bucket itself
// is fatal, because then there is nothing to count.
func sessionFilesByRelPath(bucket string) (map[string]string, int, error) {
	out := map[string]string{}
	if _, err := os.Stat(bucket); errors.Is(err, os.ErrNotExist) {
		return out, 0, nil
	}
	skipped := 0
	err := filepath.Walk(bucket, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if path == bucket {
				return err // the bucket itself is unreadable; nothing to count
			}
			skipped++
			return nil
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}
		rel, relErr := filepath.Rel(bucket, path)
		if relErr != nil {
			skipped++
			return nil
		}
		out[rel] = path
		return nil
	})
	return out, skipped, err
}

// MergeDuplicates resolves two profiles signed in to the same account: the
// conversations from the profile being given up are copied into the keeper, then
// that profile is moved out of the scan path and dropped from MCS's registries.
//
// Only one direction is copied. The profile being archived is never written to,
// which makes the archive an untouched record of what was there.
//
// Caller must have terminated Claude first. Order is resolve, verify, snapshot,
// copy, move, then update state. Nothing moves on disk until every check has
// passed, so a merge refused for holding two different accounts leaves the disk
// exactly as it found it — including on the Store build, where a later step swaps
// two directories.
func MergeDuplicates(plat platform.Platform, req MergeRequest) (*SyncReport, error) {
	// Merging a profile into itself would pass every check below — one path, so
	// trivially the same account — then archive the profile it just "kept",
	// renaming the user's live profile out of the scan path and unmanaging it. The
	// panel only ever offers two distinct rows, but this is a core API and a stale
	// panel is exactly the case the account check exists for.
	if req.KeepIdentity == req.ArchiveIdentity {
		return nil, fmt.Errorf("%s can't be merged into itself", DisplayName(req.KeepIdentity))
	}
	keepName := DisplayName(req.KeepIdentity)
	archiveName := DisplayName(req.ArchiveIdentity)

	// Resolve identities to paths read-only, from the scan. Not PrepareArchive yet:
	// that can move directories.
	paths, err := profilePathsByIdentity(plat)
	if err != nil {
		return nil, err
	}
	keepPath, ok := paths[req.KeepIdentity]
	if !ok {
		return nil, fmt.Errorf("%s is no longer there — run Rescan", keepName)
	}
	archivePath, ok := paths[req.ArchiveIdentity]
	if !ok {
		return nil, fmt.Errorf("%s is no longer there — run Rescan", archiveName)
	}

	keepUUID, err := platform.GetProfileAccountUUID(keepPath)
	if err != nil {
		return nil, fmt.Errorf("%s has no account signed in, so there is nothing to merge into", keepName)
	}
	archiveUUID, err := platform.GetProfileAccountUUID(archivePath)
	if err != nil {
		return nil, fmt.Errorf("%s has no account signed in, so there is nothing to merge", archiveName)
	}
	if keepUUID != archiveUUID {
		// A stale panel could ask to merge rows that have changed underneath it.
		// Merging two genuinely different accounts would mix their histories.
		return nil, fmt.Errorf("%s and %s are different accounts, so they can't be merged", keepName, archiveName)
	}

	// SyncSessions does NOT snapshot anything — the backup has always been the
	// caller's job (switch.go, align.go and the CLI each take their own). Take it
	// here, and abort rather than copy unprotected.
	if _, err := NewBackupManager(req.BackupRoot).BackupIfHasData(keepPath); err != nil {
		return nil, fmt.Errorf("aborting merge: could not back up %s first: %w", keepName, err)
	}
	report, err := SyncSessions(archivePath, keepPath)
	if err != nil {
		return nil, fmt.Errorf("combine conversations: %w", err)
	}

	// Only now, with everything copied and nothing left to refuse, make the archive
	// possible. On the Store build this swaps the keeper into the slot when the
	// other profile holds it, so both paths move and the ones returned here are the
	// ones to use from this point on.
	newKeepPath, archivePath, err := plat.PrepareArchive(req.KeepIdentity, req.ArchiveIdentity)
	if err != nil {
		// Nothing has been given up: both profiles are in place and the keeper now
		// holds the union, so a retry is safe and the warning is still showing.
		return report, fmt.Errorf("could not make %s ready to archive: %w", archiveName, err)
	}
	// The two must have come back as different directories. On the Store build both
	// profiles are addressed through one slot, and PrepareArchive earns the right to
	// archive by swapping the keeper into it first; if that swap did not happen, the
	// path about to be renamed away is the profile the user chose to KEEP, holding
	// the conversations just merged into it. Refusing costs a retry. Not refusing
	// destroys exactly what the merge was for.
	if samePath(newKeepPath, archivePath) {
		return report, fmt.Errorf("refusing to archive %s: it is still the same folder as %s. Your conversations are safe in %s — run Rescan and try again",
			archiveName, keepName, keepName)
	}

	if _, err := ArchiveProfile(req.ArchiveIdentity, archivePath, plat.ArchiveDir()); err != nil {
		return report, err
	}

	// Registries last, and only now that the folder really is gone from the scan
	// path. Unmanaging a folder still in place would hide it while leaving it to
	// reappear on the next Rescan.
	// From here the folder is physically gone, so every registry entry naming it is
	// now wrong. Report the first failure but keep going: stopping at one would
	// leave the others describing a profile that no longer exists, which is worse
	// than the failure being reported.
	var registryErr error
	if err := RemoveManaged(req.ArchiveIdentity); err != nil {
		registryErr = fmt.Errorf("archived, but the managed list still lists it: %w", err)
		log.Printf("merge: %v", registryErr)
	}
	// The display name goes with the profile. Left behind, it would be inherited by
	// any later profile that happened to reuse the identity.
	if err := SetProfileName(req.ArchiveIdentity, ""); err != nil {
		log.Printf("merge: could not clear the display name for %q: %v", req.ArchiveIdentity, err)
	}
	// And its pending entry, if it somehow still has one. Pending entries are pruned
	// only on sign-in, and an archived profile never appears in FindProfiles again,
	// so an entry left here would render a sign-in prompt the user could never clear.
	if err := RemovePending(req.ArchiveIdentity); err != nil {
		log.Printf("merge: could not clear the pending entry for %q: %v", req.ArchiveIdentity, err)
	}
	return report, registryErr
}

// samePath reports whether two paths name the same directory.
//
// Compared as cleaned strings rather than with os.SameFile because callers need
// an answer about a path that may not exist yet, and case-insensitively on the
// platforms whose filesystems are: on Windows and on a default macOS volume,
// "Claude" and "claude" are one directory. A volume that really is
// case-sensitive makes this too cautious rather than not cautious enough, which
// is the right way round for a guard that only ever refuses.
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// profilePathsByIdentity maps every discovered profile's identity to its path.
// This is the only correct identity-to-path direction outside the platform
// package: the paths come from the scan rather than being rebuilt from a root.
func profilePathsByIdentity(plat platform.Platform) (map[string]string, error) {
	profiles, err := plat.FindProfiles()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(profiles))
	for _, p := range profiles {
		out[p.Name] = p.Path
	}
	return out, nil
}
