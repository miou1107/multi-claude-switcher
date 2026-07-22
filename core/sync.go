package core

import (
	"bytes"
	"fmt"
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
// Conflict handling: to avoid silently destroying data, when the target already
// holds a DIFFERENT version of a file, the source only wins if it is strictly
// newer (mtime). If the target's copy is newer or same-age, the file is left
// untouched and recorded as a conflict for the caller to resolve. (After
// re-bucketing, two accounts could in principle hold different content at the
// same bucket-relative path; that resolves through this same newer-wins/conflict
// rule. In practice local_<UUID>.json names are session-scoped, so a genuine
// collision is effectively impossible.)
func SyncSessions(srcProfilePath, dstProfilePath string) (*SyncReport, error) {
	srcAccount, err := platform.GetProfileAccountUUID(srcProfilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot determine source account (needed to know which bucket to sync): %w", err)
	}
	dstAccount, err := platform.GetProfileAccountUUID(dstProfilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot determine target account (needed to know which bucket to write): %w", err)
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

		dstInfo, statErr := os.Stat(targetPath)
		if statErr != nil {
			// File does not exist in target: copy it.
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

		// Content differs. Only overwrite when the source is strictly newer;
		// otherwise the target holds equal-or-newer data we must not destroy.
		if info.ModTime().After(dstInfo.ModTime()) {
			if err := copyFile(path, targetPath); err != nil {
				return fmt.Errorf("copy %s: %w", relPath, err)
			}
			report.CopiedCount++
			report.CopiedFiles = append(report.CopiedFiles, relPath)
		} else {
			report.ConflictCount++
			report.Conflicts = append(report.Conflicts, relPath)
		}
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
