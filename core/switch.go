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

	// From here Claude Desktop is closed. On ANY outcome the target must still be
	// launched, or the user is left with Claude shut and no way back except
	// opening it by hand. Step 2 therefore reports failures rather than returning
	// out of this function, mirroring ManualAlign (see align.go).
	alignErr := s.autoAlign(srcProfilePath, dstProfilePath)

	// Step 3: launch the target profile.
	log.Printf("[Safe Switch] Launching Claude Desktop profile: %s...", dstProfilePath)
	if err := s.Platform.LaunchProfile(dstProfilePath); err != nil {
		if alignErr != nil {
			return fmt.Errorf("%w (and Claude Desktop could not be reopened: %v)", alignErr, err)
		}
		return fmt.Errorf("failed to launch target profile: %w", err)
	}
	if alignErr != nil {
		// Claude is back up, so the user is not stranded; the sync is what failed.
		return alignErr
	}

	log.Printf("[Safe Switch] Switch completed successfully!")
	return nil
}

// autoAlign performs the opt-in bidirectional session union, when auto sync is on
// and both profiles have an account. It returns an error rather than aborting the
// switch: its caller has already closed Claude Desktop and owes the user a
// relaunch whatever happens here.
func (s *Switcher) autoAlign(srcProfilePath, dstProfilePath string) error {
	if !AutoSyncOnSwitch() {
		log.Printf("[Safe Switch] Auto sync off: pure switch, no session data moved.")
		return nil
	}
	_, srcErr := platform.GetProfileAccountUUID(srcProfilePath)
	_, dstErr := platform.GetProfileAccountUUID(dstProfilePath)
	if srcErr != nil || dstErr != nil {
		log.Printf("[Safe Switch] Auto sync on, but a profile has no account yet (src: %v, dst: %v). Skipping align.", srcErr, dstErr)
		return nil
	}

	// Bidirectional align writes into BOTH profiles, so back up both.
	if _, err := s.BackupManager.BackupIfHasData(srcProfilePath); err != nil {
		return fmt.Errorf("skipped auto sync: failed to back up source profile (refusing to write without a backup): %w", err)
	}
	if _, err := s.BackupManager.BackupIfHasData(dstProfilePath); err != nil {
		return fmt.Errorf("skipped auto sync: failed to back up target profile (refusing to write without a backup): %w", err)
	}

	log.Printf("[Safe Switch] Auto sync on: unioning sessions between both accounts...")
	aToB, bToA, err := SyncBidirectional(srcProfilePath, dstProfilePath)
	if err != nil {
		return fmt.Errorf("failed to auto sync sessions: %w", err)
	}

	// Only the clashes both legs reported actually failed to converge; anything
	// one leg flagged was fixed by the other. Auto sync runs unattended with no UI
	// to report into, so the log is the only place a user can find out.
	if unresolved := UnresolvedConflicts(aToB, bToA); len(unresolved) > 0 {
		log.Printf("[Safe Switch] %d session(s) differ on both sides with the same timestamp, so both copies were kept:", len(unresolved))
		for _, c := range unresolved {
			log.Printf("[Safe Switch]   %s", c)
		}
	}
	for _, e := range append(append([]string{}, aToB.SkipErrors...), bToA.SkipErrors...) {
		log.Printf("[Safe Switch] skipped a session file: %s", e)
	}
	return nil
}
