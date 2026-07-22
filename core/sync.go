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
	CopiedCount   int      `json:"copied_count"`
	SkippedCount  int      `json:"skipped_count"`
	ConflictCount int      `json:"conflict_count"`
	CopiedFiles   []string `json:"copied_files"`
	Conflicts     []string `json:"conflicts"`
}

// SyncSessions copies session JSON files from the source profile to the target
// profile, preserving each file's bucket-relative path (<UUID>/<OrgUUID>/local_*.json).
//
// Bucketing note: this copies files at their EXACT relative path; it does NOT
// remap a source-only <AccountUUID> bucket into the target account's bucket.
// Buckets that already exist on both profiles sync correctly. Whether the target
// app surfaces a bucket that exists only in the source is unverified on-device
// (Phase 0 probe Q3/Q4 open item) and must be confirmed with a real end-to-end
// test before relying on it.
//
// Conflict handling: to avoid silently destroying data, when the target already
// holds a DIFFERENT version of a file, the source only wins if it is strictly
// newer (mtime). If the target's copy is newer or same-age, the file is left
// untouched and recorded as a conflict for the caller to resolve.
func SyncSessions(srcProfilePath, dstProfilePath string) (*SyncReport, error) {
	srcSessions := platform.GetProfileSessionsDir(srcProfilePath)
	dstSessions := platform.GetProfileSessionsDir(dstProfilePath)

	if fi, err := os.Stat(srcSessions); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("source sessions directory does not exist: %s", srcSessions)
	}

	if err := os.MkdirAll(dstSessions, 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination sessions directory: %w", err)
	}

	report := &SyncReport{}

	// Walk source sessions
	err := filepath.Walk(srcSessions, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}

		relPath, err := filepath.Rel(srcSessions, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dstSessions, relPath)

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

	if err != nil {
		return nil, fmt.Errorf("error during sync walk: %w", err)
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
