package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/miou1107/multi-claude-switcher/platform"
)

type BackupManager struct {
	BackupRootDir string
}

func NewBackupManager(rootDir string) *BackupManager {
	if rootDir == "" {
		home, _ := os.UserHomeDir()
		rootDir = filepath.Join(home, ".multi-claude-switcher", "backups")
	}
	return &BackupManager{BackupRootDir: rootDir}
}

func (bm *BackupManager) CreateBackup(profilePath string) (string, error) {
	sessionsDir := platform.GetProfileSessionsDir(profilePath)
	if fi, err := os.Stat(sessionsDir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("sessions directory does not exist: %s", sessionsDir)
	}

	profileName := filepath.Base(profilePath)
	timestamp := time.Now().Format("20060102_150405")
	backupDir := filepath.Join(bm.BackupRootDir, fmt.Sprintf("%s_%s", profileName, timestamp))

	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup root: %w", err)
	}

	targetSessionsDir := filepath.Join(backupDir, "claude-code-sessions")
	if err := copyDir(sessionsDir, targetSessionsDir); err != nil {
		return "", fmt.Errorf("failed to copy sessions dir: %w", err)
	}

	return backupDir, nil
}

// BackupIfHasData backs up the profile only when it actually holds sessions.
// It returns ("", nil) when there is nothing to back up (no sessions dir), the
// backup path on success, and ("", err) on a genuine backup failure. Callers
// that are about to overwrite the profile MUST abort on a non-nil error so real
// data is never overwritten without a backup.
func (bm *BackupManager) BackupIfHasData(profilePath string) (string, error) {
	sessionsDir := platform.GetProfileSessionsDir(profilePath)
	if fi, err := os.Stat(sessionsDir); err != nil || !fi.IsDir() {
		return "", nil // nothing to lose
	}
	return bm.CreateBackup(profilePath)
}

func (bm *BackupManager) ListBackups() ([]string, error) {
	if _, err := os.Stat(bm.BackupRootDir); os.IsNotExist(err) {
		return nil, nil
	}
	entries, err := os.ReadDir(bm.BackupRootDir)
	if err != nil {
		return nil, err
	}
	var backups []string
	for _, entry := range entries {
		if entry.IsDir() {
			backups = append(backups, filepath.Join(bm.BackupRootDir, entry.Name()))
		}
	}
	return backups, nil
}

func (bm *BackupManager) RestoreBackup(backupPath, targetProfilePath string) error {
	backupSessionsDir := filepath.Join(backupPath, "claude-code-sessions")
	if fi, err := os.Stat(backupSessionsDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("invalid backup directory: %s", backupPath)
	}

	targetSessionsDir := platform.GetProfileSessionsDir(targetProfilePath)

	// A successful restore overwrites (and then discards) whatever the target
	// currently holds. Snapshot the current target first so the restore is
	// itself reversible — restoring the wrong backup must not be a one-way loss.
	// Abort if the snapshot fails: never discard live data without a recoverable
	// backup (same invariant as switch/sync).
	if _, err := bm.BackupIfHasData(targetProfilePath); err != nil {
		return fmt.Errorf("refusing to restore: failed to back up the current target first: %w", err)
	}

	// Stage the restore into a temp dir first. A mid-copy failure (disk full,
	// permissions) then leaves the existing target untouched instead of half
	// destroyed.
	tmpDir := targetSessionsDir + ".restoring"
	_ = os.RemoveAll(tmpDir)
	if err := copyDir(backupSessionsDir, tmpDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("failed to stage restore: %w", err)
	}

	// Move the current target aside, then swap in the fully-staged copy. If the
	// final swap fails, roll the original back into place.
	oldDir := targetSessionsDir + ".old"
	_ = os.RemoveAll(oldDir)
	if _, err := os.Stat(targetSessionsDir); err == nil {
		if err := os.Rename(targetSessionsDir, oldDir); err != nil {
			_ = os.RemoveAll(tmpDir)
			return fmt.Errorf("failed to move current target aside: %w", err)
		}
	}
	if err := os.Rename(tmpDir, targetSessionsDir); err != nil {
		_ = os.Rename(oldDir, targetSessionsDir) // best-effort rollback
		_ = os.RemoveAll(tmpDir)                 // don't leak the staged copy
		return fmt.Errorf("failed to swap in restored sessions: %w", err)
	}
	_ = os.RemoveAll(oldDir)
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}

		return copyFile(path, targetPath)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}

	// Preserve the source modification time. Sync/conflict decisions compare
	// mtimes, so a copy must not reset them to "now" (which would make every
	// copied file look newer than its source on the next comparison).
	if fi, statErr := os.Stat(src); statErr == nil {
		_ = os.Chtimes(dst, fi.ModTime(), fi.ModTime())
	}
	return nil
}
