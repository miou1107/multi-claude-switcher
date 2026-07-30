package core

import (
	"errors"
	"fmt"
)

// ErrRunningProfileUnknown means Claude Desktop is running but MCS cannot tell
// which profile it is on, so it refuses to close it: an app it cannot reopen
// would leave the user stranded with Claude shut.
//
// It is a sentinel rather than a bare message because the panel has to recognise
// it and say something a non-technical user can act on. This happens for a whole
// class of users — anyone who opened Claude Desktop themselves rather than
// through MCS, since profile detection works by matching the --user-data-dir
// argument MCS passes.
var ErrRunningProfileUnknown = errors.New("Claude Desktop is running but its profile could not be identified")

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
			return nil, fmt.Errorf("%w: %v", ErrRunningProfileUnknown, err)
		}
		if relaunch == "" {
			return nil, ErrRunningProfileUnknown
		}
		// Record the debt BEFORE closing, so it exists for the whole window in
		// which Claude is shut. A quit handler reads it to put Claude back if MCS
		// is told to exit mid-operation (Switcher.PendingRelaunch).
		s.notePendingRelaunch(relaunch)
		if err := s.Platform.TerminateApp(); err != nil {
			s.ClaimPendingRelaunch() // nothing was closed, so nothing is owed
			return nil, fmt.Errorf("failed to close Claude Desktop: %w", err)
		}
	}

	// From here Claude Desktop is closed. On ANY outcome we must reopen the
	// profile the user was on (if any), or they are stranded with it shut.
	report, alignErr := s.alignAfterClose(srcProfilePath, dstProfilePath)
	// Claim rather than read: if a quit handler got here first it has already
	// reopened Claude, and launching again would give the user two windows.
	if owed := s.ClaimPendingRelaunch(); owed != "" {
		if lerr := s.Platform.LaunchProfile(owed); lerr != nil && alignErr == nil {
			// The align itself succeeded; only reopening failed.
			return report, fmt.Errorf("sync done but could not reopen Claude Desktop (%s): %w", owed, lerr)
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
