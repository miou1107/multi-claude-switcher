package core

import (
	"fmt"
	"log"

	"github.com/miou1107/multi-claude-switcher/platform"
)

type Switcher struct {
	Platform      platform.Platform
	BackupManager *BackupManager
}

func NewSwitcher(p platform.Platform, bm *BackupManager) *Switcher {
	if bm == nil {
		bm = NewBackupManager("")
	}
	return &Switcher{
		Platform:      p,
		BackupManager: bm,
	}
}

// SafeSwitch performs: Close active app -> Backup target profile -> Sync session index -> Launch target profile.
func (s *Switcher) SafeSwitch(srcProfilePath, dstProfilePath string) error {
	log.Printf("[Safe Switch] Starting switch from %s to %s...", srcProfilePath, dstProfilePath)

	// Step 1: Check and terminate running Claude Desktop processes
	running, procs, err := s.Platform.IsAppRunning()
	if err != nil {
		return fmt.Errorf("failed to check running processes: %w", err)
	}
	if running {
		log.Printf("[Safe Switch] Terminating %d running Claude process(es)...", len(procs))
		if err := s.Platform.TerminateApp(); err != nil {
			return fmt.Errorf("failed to terminate Claude process: %w", err)
		}
	}

	// Step 2: Backup target profile sessions before modifying.
	// The next step overwrites the target's index, so a backup is mandatory
	// whenever the target holds data. Any backup error aborts the switch: we
	// never overwrite real data without a backup.
	log.Printf("[Safe Switch] Creating backup of target profile: %s", dstProfilePath)
	backupPath, err := s.BackupManager.BackupIfHasData(dstProfilePath)
	if err != nil {
		return fmt.Errorf("aborting switch: failed to back up target profile (refusing to overwrite without a backup): %w", err)
	}
	if backupPath == "" {
		log.Printf("[Safe Switch] Target profile has no existing sessions; nothing to back up.")
	} else {
		log.Printf("[Safe Switch] Backup created at: %s", backupPath)
	}

	// Step 3: Sync session indices from source to destination
	log.Printf("[Safe Switch] Syncing sessions from source to target...")
	report, err := SyncSessions(srcProfilePath, dstProfilePath)
	if err != nil {
		return fmt.Errorf("failed to sync sessions: %w", err)
	}
	log.Printf("[Safe Switch] Sync complete: %d copied, %d skipped, %d conflict(s).", report.CopiedCount, report.SkippedCount, report.ConflictCount)
	if report.ConflictCount > 0 {
		log.Printf("[Safe Switch Warning] %d conflict(s) left untouched (target had newer content). Review before relying on these sessions:", report.ConflictCount)
		for _, c := range report.Conflicts {
			log.Printf("    conflict: %s", c)
		}
	}

	// Step 4: Launch target profile
	log.Printf("[Safe Switch] Launching Claude Desktop profile: %s...", dstProfilePath)
	if err := s.Platform.LaunchProfile(dstProfilePath); err != nil {
		return fmt.Errorf("failed to launch target profile: %w", err)
	}

	log.Printf("[Safe Switch] Switch completed successfully!")
	return nil
}
