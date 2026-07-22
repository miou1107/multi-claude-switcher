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
	// Remove existing target sessions dir and replace with backup copy
	_ = os.RemoveAll(targetSessionsDir)
	return copyDir(backupSessionsDir, targetSessionsDir)
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

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Sync()
}
