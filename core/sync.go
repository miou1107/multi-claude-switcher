package core

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/miou1107/multi-claude-switcher/platform"
)

type SyncReport struct {
	SourceAccount string   `json:"source_account"`
	TargetAccount string   `json:"target_account"`
	CopiedCount   int      `json:"copied_count"`
	SkippedCount  int      `json:"skipped_count"`
	ConflictCount int      `json:"conflict_count"`
	CopiedFiles   []string `json:"copied_files"`
	Conflicts     []string `json:"conflicts"`

	// SkipErrors names files that could not be examined or copied, with the
	// reason. One unreadable file must not stop the rest: a profile holds
	// hundreds of conversations, and aborting the walk over a single junk entry
	// would block every other one from ever syncing.
	SkipErrors []string `json:"skip_errors,omitempty"`
}

// noteSkipError records a per-file failure so the walk can carry on.
func (r *SyncReport) noteSkipError(relPath string, err error) {
	r.SkipErrors = append(r.SkipErrors, fmt.Sprintf("%s: %v", relPath, err))
}

// SyncSessions makes the target account's conversation history include the
// source account's conversations.
//
// Account re-bucketing (the whole point): Claude Desktop's Code tab reads ONLY
// from claude-code-sessions/<lastKnownAccountUuid>/. So sync reads the SOURCE
// profile's own account bucket and writes those sessions into the TARGET
// profile's own account bucket, renaming the top-level bucket from the source
// account UUID to the target account UUID. This is what makes history follow you
// across accounts. A verbatim path-preserving copy (the previous behavior) would
// drop the sessions under the source account's bucket name, where the target app
// never looks (silent failure) — and would drag along any foreign/orphaned
// buckets, re-polluting the target. We copy ONLY the source account bucket.
//
// Conflict handling: a file the target does not have is copied. When both sides
// hold the same path with different contents, the one with the newer mtime wins;
// if the target's copy is newer, or the two mtimes are equal, the target is left
// alone and the clash is recorded as a conflict.
//
// mtime is a good proxy for "which version is current" in this particular data
// model, and that is measured rather than assumed. On a machine with two live
// profiles of the same account, all 384 identically-contented files also had
// identical mtimes, with no exceptions, because Claude Desktop advances the mtime
// on every rewrite and copyFile preserves it. Of the 13 files that did differ,
// every one where completedTurns differed had the higher turn count on the
// newer-mtime side.
//
// Do not replace this with a rule that reads the JSON. These records are Claude
// Desktop's private format, and a field-based rule fails in both directions:
// silently, if a field is renamed, and destructively, if the field is not
// monotonic. completedTurns is not monotonic — it falls when a user undoes turns,
// and it is incomparable across worktree branches — so "more turns wins" would
// undo a user's own deletion.
//
// In particular, transcriptUnavailable is NOT a damage signal. Claude Code
// reclaims old transcripts on a retention policy, and the flag is Claude Desktop
// honestly recording that the body behind a record is gone. On the machine
// measured above, 123 of 397 records pointed at transcripts that no longer
// existed and a further 113 were already flagged, so roughly six records in ten
// have no body behind them. That share only rises. An earlier version of this
// function treated the flag as corruption and refused to overwrite anything at
// all, which turned every ordinary version advance into a permanent false
// conflict.
func SyncSessions(srcProfilePath, dstProfilePath string) (*SyncReport, error) {
	// Sessions are stored per account, so both ends need one. A profile that
	// has never been signed in to has no bucket to read from or write to, and
	// saying that plainly is far more use than the missing config key.
	srcAccount, err := platform.GetProfileAccountUUID(srcProfilePath)
	if err != nil {
		return nil, fmt.Errorf("%s has no account signed in yet, so it has no sessions to copy. Switch to it and sign in first",
			DisplayName(filepath.Base(srcProfilePath)))
	}
	dstAccount, err := platform.GetProfileAccountUUID(dstProfilePath)
	if err != nil {
		return nil, fmt.Errorf("%s has no account signed in yet, so there is nowhere to copy sessions to. Switch to it and sign in first",
			DisplayName(filepath.Base(dstProfilePath)))
	}

	report := &SyncReport{SourceAccount: srcAccount, TargetAccount: dstAccount}

	// Only the source's OWN account bucket is synced; foreign/orphaned buckets
	// are deliberately left behind so we never re-pollute the target.
	srcBucket := filepath.Join(platform.GetProfileSessionsDir(srcProfilePath), srcAccount)
	if fi, statErr := os.Stat(srcBucket); statErr != nil || !fi.IsDir() {
		// Nothing to sync (source account has no local sessions yet).
		return report, nil
	}

	// Re-bucket: everything under the source account bucket lands under the
	// target account bucket.
	dstBucket := filepath.Join(platform.GetProfileSessionsDir(dstProfilePath), dstAccount)
	if err := os.MkdirAll(dstBucket, 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination account bucket: %w", err)
	}

	walkErr := filepath.Walk(srcBucket, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}

		relPath, err := filepath.Rel(srcBucket, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dstBucket, relPath)

		// Lstat, and only ErrNotExist counts as absent. copyFile truncates through
		// os.Create, so treating every stat failure as "not there" would destroy
		// the target's copy on a permission or I/O error. Lstat rather than Stat so
		// a dangling symlink is seen for what it is instead of being followed and
		// written past, which would land the data outside the sessions directory.
		//
		// A failure here is recorded and the walk continues. Returning an error
		// would abort the whole run, and a profile holds hundreds of conversations:
		// one junk entry must not stop all the others from ever syncing.
		dstInfo, statErr := os.Lstat(targetPath)
		if statErr != nil {
			if !errors.Is(statErr, fs.ErrNotExist) {
				report.noteSkipError(relPath, statErr)
				return nil
			}
			// Absent from the target: copy it.
			if err := copyFile(path, targetPath); err != nil {
				report.noteSkipError(relPath, err)
				return nil
			}
			report.CopiedCount++
			report.CopiedFiles = append(report.CopiedFiles, relPath)
			return nil
		}
		if !dstInfo.Mode().IsRegular() {
			// A symlink, directory or device where a session record belongs. Not
			// something to compare, and certainly not something to write through.
			report.noteSkipError(relPath, fmt.Errorf("target is not a regular file (%s)", dstInfo.Mode()))
			return nil
		}

		// Target already has this file. Compare content before touching it.
		same, cmpErr := filesEqual(path, targetPath)
		if cmpErr != nil {
			report.noteSkipError(relPath, cmpErr)
			return nil
		}
		if same {
			report.SkippedCount++
			return nil
		}

		// Content differs: the newer mtime wins. See the function comment for why
		// mtime and not the record's own fields.
		if info.ModTime().After(dstInfo.ModTime()) {
			if err := copyFile(path, targetPath); err != nil {
				report.noteSkipError(relPath, err)
				return nil
			}
			report.CopiedCount++
			report.CopiedFiles = append(report.CopiedFiles, relPath)
			return nil
		}
		// The target is newer, or the two are the same age and still differ. Either
		// way this side has nothing better to offer, so the target is left alone.
		report.ConflictCount++
		report.Conflicts = append(report.Conflicts, relPath)
		return nil
	})

	if walkErr != nil {
		return nil, fmt.Errorf("error during sync walk: %w", walkErr)
	}

	return report, nil
}

// filesEqual reports whether two files have identical contents.
func filesEqual(a, b string) (bool, error) {
	fa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	if fa.Size() != fb.Size() {
		return false, nil
	}
	ba, err := os.ReadFile(a)
	if err != nil {
		return false, err
	}
	bb, err := os.ReadFile(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ba, bb), nil
}

// SyncBidirectional makes both profiles' Code sessions converge to the union of
// the two. It syncs source->target first (so the target then holds the union),
// then target->source, leaving both accounts with A ∪ B. SyncSessions is
// additive and skips identical files, so this is safe and idempotent. Both
// profiles must be logged in (SyncSessions errors otherwise).
//
// It returns both legs' reports. Note that a clash reported by the first leg is
// usually resolved by the second: if B's copy was newer, leg one leaves it alone
// and reports a conflict, then leg two copies it back over A. Treating leg one's
// count as the outcome would report a problem that no longer exists. Use
// UnresolvedConflicts for what actually failed to converge.
func SyncBidirectional(profileA, profileB string) (aToB, bToA *SyncReport, err error) {
	aToB, err = SyncSessions(profileA, profileB)
	if err != nil {
		return nil, nil, err
	}
	bToA, err = SyncSessions(profileB, profileA)
	if err != nil {
		return aToB, nil, err
	}
	return aToB, bToA, nil
}

// UnresolvedConflicts returns the paths a two-way sync could not make agree: the
// ones both legs reported. Anything only one leg flagged was fixed by the other.
//
// With the newer-mtime-wins rule the only way to be in both lists is for the two
// copies to differ while carrying the same mtime, which nothing observed in
// practice does — Claude Desktop advances the mtime on every rewrite and copyFile
// preserves it. So this is normally empty, and when it is not, something genuinely
// odd has happened and is worth a log line.
func UnresolvedConflicts(aToB, bToA *SyncReport) []string {
	if aToB == nil || bToA == nil {
		return nil
	}
	inB := make(map[string]bool, len(bToA.Conflicts))
	for _, c := range bToA.Conflicts {
		inB[c] = true
	}
	var out []string
	for _, c := range aToB.Conflicts {
		if inB[c] {
			out = append(out, c)
		}
	}
	return out
}
