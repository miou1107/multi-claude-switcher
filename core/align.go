package core

import "fmt"

// ManualAlign copies one profile's Code sessions into another WITHOUT changing
// which account is active. It remembers the running profile, closes Claude
// Desktop (never write into a live profile), backs up the target, syncs
// source->target (re-bucketed under the target account), then relaunches the
// profile that was running so the user is left exactly where they started.
func (s *Switcher) ManualAlign(srcProfilePath, dstProfilePath string) (*SyncReport, error) {
	running, _, err := s.Platform.IsAppRunning()
	if err != nil {
		return nil, fmt.Errorf("failed to check running processes: %w", err)
	}

	// Remember which profile to reopen. If the app is running but we cannot
	// identify which profile it is, abort BEFORE closing anything — closing an
	// app we can't reopen would strand the user with Claude Desktop shut.
	var relaunch string
	if running {
		relaunch, err = s.Platform.DetectRunningProfile()
		if err != nil {
			return nil, fmt.Errorf("aborting align: Claude Desktop is running but its profile could not be identified (not closing it): %w", err)
		}
		if relaunch == "" {
			return nil, fmt.Errorf("aborting align: Claude Desktop is running but its profile could not be identified (not closing it)")
		}
		if err := s.Platform.TerminateApp(); err != nil {
			return nil, fmt.Errorf("failed to close Claude Desktop: %w", err)
		}
	}

	// From here Claude Desktop is closed. On ANY outcome we must reopen the
	// profile the user was on (if any), or they are stranded with it shut.
	report, alignErr := s.alignAfterClose(srcProfilePath, dstProfilePath)
	if relaunch != "" {
		if lerr := s.Platform.LaunchProfile(relaunch); lerr != nil && alignErr == nil {
			// The align itself succeeded; only reopening failed.
			return report, fmt.Errorf("sync done but could not reopen Claude Desktop (%s): %w", relaunch, lerr)
		}
	}
	return report, alignErr
}

// alignAfterClose backs up the target and syncs source->target. It is separated
// from ManualAlign so the caller can guarantee the user's profile is reopened
// whether or not these steps succeed.
func (s *Switcher) alignAfterClose(srcProfilePath, dstProfilePath string) (*SyncReport, error) {
	// Never overwrite the target's data without a backup.
	if _, err := s.BackupManager.BackupIfHasData(dstProfilePath); err != nil {
		return nil, fmt.Errorf("aborting align: failed to back up target profile: %w", err)
	}
	report, err := SyncSessions(srcProfilePath, dstProfilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to sync sessions: %w", err)
	}
	return report, nil
}
