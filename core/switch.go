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

// SafeSwitch closes the running app, optionally aligns sessions, then launches
// the target. Data is moved ONLY when auto sync is ON and both profiles are
// logged in: then it backs up BOTH profiles (bidirectional align writes both)
// and unions their sessions. With auto sync OFF (default) the switch moves no
// data at all — a pure account switch.
func (s *Switcher) SafeSwitch(srcProfilePath, dstProfilePath string) error {
	log.Printf("[Safe Switch] Starting switch from %s to %s...", srcProfilePath, dstProfilePath)

	// Step 1: close any running Claude Desktop (never write into a live profile).
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

	// Step 2: align only when the user opted in AND both profiles are logged in.
	if AutoSyncOnSwitch() {
		_, srcErr := platform.GetProfileAccountUUID(srcProfilePath)
		_, dstErr := platform.GetProfileAccountUUID(dstProfilePath)
		if srcErr != nil || dstErr != nil {
			log.Printf("[Safe Switch] Auto sync on, but a profile has no account yet (src: %v, dst: %v). Skipping align.", srcErr, dstErr)
		} else {
			// Bidirectional align writes into BOTH profiles, so back up both.
			if _, err := s.BackupManager.BackupIfHasData(srcProfilePath); err != nil {
				return fmt.Errorf("aborting switch: failed to back up source profile (refusing to overwrite without a backup): %w", err)
			}
			if _, err := s.BackupManager.BackupIfHasData(dstProfilePath); err != nil {
				return fmt.Errorf("aborting switch: failed to back up target profile (refusing to overwrite without a backup): %w", err)
			}
			log.Printf("[Safe Switch] Auto sync on: unioning sessions between both accounts...")
			if err := SyncBidirectional(srcProfilePath, dstProfilePath); err != nil {
				return fmt.Errorf("failed to auto sync sessions: %w", err)
			}
		}
	} else {
		log.Printf("[Safe Switch] Auto sync off: pure switch, no session data moved.")
	}

	// Step 3: launch the target profile.
	log.Printf("[Safe Switch] Launching Claude Desktop profile: %s...", dstProfilePath)
	if err := s.Platform.LaunchProfile(dstProfilePath); err != nil {
		return fmt.Errorf("failed to launch target profile: %w", err)
	}

	log.Printf("[Safe Switch] Switch completed successfully!")
	return nil
}
