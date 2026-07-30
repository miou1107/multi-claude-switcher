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
// Conflict handling: sync is purely ADDITIVE. A file the target does not have is
// copied; a file the target already has is never replaced, whatever its contents
// or timestamps. When the two sides hold different versions of the same file,
// both are kept and the clash is recorded in the report for the caller to
// surface.
//
// This used to overwrite when the source's mtime was newer. Do not reintroduce
// that: on real data the newer file is the damaged one. See the conflict branch
// in the walk below for the measurements, and
// TestSyncNeverOverwritesDifferingContent for the regression test.
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

		// Only existence matters. The target's mtime is deliberately not read;
		// see the conflict branch below for why comparing it was harmful.
		//
		// Lstat, and only ErrNotExist counts as absent. copyFile truncates
		// through os.Create, so treating every stat failure as "not there" would
		// destroy the target's copy on a permission or I/O error — the one thing
		// this function must never do. Lstat rather than Stat so a dangling
		// symlink counts as present instead of being followed and written past.
		if _, statErr := os.Lstat(targetPath); statErr != nil {
			if !errors.Is(statErr, fs.ErrNotExist) {
				return fmt.Errorf("inspect %s in the target: %w", relPath, statErr)
			}
			// Absent from the target: copy it.
			if err := copyFile(path, targetPath); err != nil {
				return fmt.Errorf("copy %s: %w", relPath, err)
			}
			report.CopiedCount++
			report.CopiedFiles = append(report.CopiedFiles, relPath)
			return nil
		}

		// Target already has this file. Compare content before touching it.
		same, cmpErr := filesEqual(path, targetPath)
		if cmpErr != nil {
			return fmt.Errorf("compare %s: %w", relPath, cmpErr)
		}
		if same {
			report.SkippedCount++
			return nil
		}

		// Content differs, so this file is never touched. Sync is purely
		// additive: it brings across conversations the target does not have and
		// never replaces one it does.
		//
		// This used to overwrite when the source's mtime was newer, on the
		// assumption that a newer mtime meant a more recent edit. On real data it
		// means the opposite. Measured on a user's machine (2026-07-30) for one
		// account held by two profiles: of 26 differing files, 16 had the NEWER
		// copy carrying "transcriptUnavailable" and missing its "cliSessionId"
		// while the older copy was intact. Claude Desktop rewrites a session
		// record when it can no longer find the transcript behind it, and that
		// rewrite moves the mtime forward — so preferring the newer file
		// systematically replaced good data with degraded data, and the only good
		// copy was gone.
		//
		// There is no reliable way to tell which side is better from here: the
		// judgement depends on Claude's own record format, which this tool does
		// not own and which changes without notice. So we do not guess. The clash
		// is reported and both copies survive.
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
// It returns both legs' reports so the caller can surface clashes. Sync never
// replaces a file the other side already has, so a conversation that differs on
// both sides stays different on both sides. Dropping that on the floor is how a
// user of Auto Sync would be told nothing at all about the sessions that did not
// converge.
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
