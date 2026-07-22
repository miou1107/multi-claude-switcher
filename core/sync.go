package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/miou1107/multi-claude-switcher/platform"
)

type SyncReport struct {
	CopiedCount  int      `json:"copied_count"`
	SkippedCount int      `json:"skipped_count"`
	CopiedFiles  []string `json:"copied_files"`
}

// SyncSessions copies session JSON files from sourceProfile to targetProfile.
// It maps sessions between matching OrgUUID folders across AccountUUIDs if needed.
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

		// Check if target file exists
		if dstInfo, err := os.Stat(targetPath); err == nil {
			// If source is newer or different size, overwrite; else skip
			if info.ModTime().After(dstInfo.ModTime()) {
				if err := copyFile(path, targetPath); err == nil {
					report.CopiedCount++
					report.CopiedFiles = append(report.CopiedFiles, relPath)
				}
			} else {
				report.SkippedCount++
			}
		} else {
			// File does not exist in target, copy it
			if err := copyFile(path, targetPath); err == nil {
				report.CopiedCount++
				report.CopiedFiles = append(report.CopiedFiles, relPath)
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error during sync walk: %w", err)
	}

	return report, nil
}
