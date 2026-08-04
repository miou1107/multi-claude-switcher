package core

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/miou1107/multi-claude-switcher/platform"
)

type Switcher struct {
	Platform      platform.Platform
	BackupManager *BackupManager

	// relaunchMu guards owedRelaunch.
	relaunchMu sync.Mutex
	// owedRelaunch is every profile Claude Desktop must be reopened on. It is set
	// for exactly as long as MCS has Claude closed on the user's behalf and has
	// not yet reopened it. See PendingRelaunch.
	//
	// A set rather than one path: closing Claude Desktop closes every profile at
	// once, and the user may well have had several accounts open. Owing only the
	// one MCS detected is how a bystander account used to disappear on a switch.
	owedRelaunch []string
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

// notePendingRelaunch records that Claude Desktop is about to be closed and must
// be reopened on each of profilePaths. Called before terminating, so the debt
// exists for the whole window in which Claude is shut. Empty paths and repeats
// are dropped, so callers can pass "the target plus whatever was running" without
// giving the user two windows on one account.
func (s *Switcher) notePendingRelaunch(profilePaths ...string) {
	s.relaunchMu.Lock()
	defer s.relaunchMu.Unlock()
	s.owedRelaunch = nil
	for _, p := range profilePaths {
		if p == "" || containsPath(s.owedRelaunch, p) {
			continue
		}
		s.owedRelaunch = append(s.owedRelaunch, p)
	}
}

// PendingRelaunch reports the profiles Claude Desktop still owes being reopened
// on, or nil when nothing is owed.
//
// This exists because MCS can be told to quit while it has Claude closed. A switch
// or a sync shuts Claude, does its work, and reopens it; if MCS exits in between,
// the goroutine doing the work dies with it and Claude is never reopened. The user
// is left with no Claude and no MCS. A host's quit handler reads this so it can put
// Claude back before going away.
func (s *Switcher) PendingRelaunch() []string {
	s.relaunchMu.Lock()
	defer s.relaunchMu.Unlock()
	return append([]string(nil), s.owedRelaunch...)
}

// ClaimPendingRelaunch returns the owed profiles and clears them in one step, so
// each relaunch happens exactly once. Both MCS's own operation and a quit handler
// race for it; whichever arrives first reopens Claude and the other finds nothing
// to do, rather than the user ending up with two windows.
func (s *Switcher) ClaimPendingRelaunch() []string {
	s.relaunchMu.Lock()
	defer s.relaunchMu.Unlock()
	p := s.owedRelaunch
	s.owedRelaunch = nil
	return p
}

// SafeSwitch closes the running app, optionally aligns sessions, then launches
// the target. Data is moved ONLY when auto sync is ON and both profiles are
// logged in: then it backs up BOTH profiles (bidirectional align writes both)
// and unions their sessions. With auto sync OFF (default) the switch moves no
// data at all — a pure account switch.
func (s *Switcher) SafeSwitch(srcProfilePath, dstProfilePath string) error {
	log.Printf("[Safe Switch] Starting switch from %s to %s...", srcProfilePath, dstProfilePath)

	// Step 0: the target has to be real before anything else happens. Closing the
	// app the user is working in is the most disruptive thing MCS does, so it comes
	// after every check that can be made cheaply — not before. This used to be
	// missing, and a mistyped or stale folder name killed a running Claude and then
	// failed anyway, which is the worst of both outcomes.
	//
	// The source is deliberately not checked here: it only feeds the optional align,
	// which reports its own failure, and a switch away from a profile that has since
	// been removed is still a switch worth completing.
	if fi, err := os.Stat(dstProfilePath); err != nil || !fi.IsDir() {
		return fmt.Errorf("can't switch to %s: no profile folder there", filepath.Base(dstProfilePath))
	}

	// Step 1: close any running Claude Desktop (never write into a live profile).
	running, procs, err := s.Platform.IsAppRunning()
	if err != nil {
		return fmt.Errorf("failed to check running processes: %w", err)
	}
	if running {
		log.Printf("[Safe Switch] Terminating %d running Claude process(es)...", len(procs))
		// The target is what a switch owes the user, whether or not the align
		// below succeeds. So is any OTHER account that happened to be open:
		// terminating closes every profile, and a bystander account that MCS
		// closed and never reopened simply vanishes from under the user. The
		// source is the one profile deliberately left shut — putting it back is
		// what "switch" means not to do.
		//
		// Recorded before closing so a quit mid-switch can honour it (see
		// PendingRelaunch).
		s.notePendingRelaunch(append([]string{dstProfilePath}, s.bystanders(srcProfilePath, dstProfilePath)...)...)
		if err := s.Platform.TerminateApp(); err != nil {
			s.ClaimPendingRelaunch() // nothing was closed, so nothing is owed
			return fmt.Errorf("failed to terminate Claude process: %w", err)
		}
	}

	// From here Claude Desktop is closed. On ANY outcome the target must still be
	// launched, or the user is left with Claude shut and no way back except
	// opening it by hand. Step 2 therefore reports failures rather than returning
	// out of this function, mirroring ManualAlign (see align.go).
	alignErr := s.autoAlign(srcProfilePath, dstProfilePath)

	// Step 3: launch the target profile, and put back any other account that was
	// open. Claim first: a quit handler racing this may already have reopened
	// them, and launching twice gives the user two windows.
	log.Printf("[Safe Switch] Launching Claude Desktop profile: %s...", dstProfilePath)
	owed := s.ClaimPendingRelaunch()
	if len(owed) == 0 && running {
		log.Printf("[Safe Switch] Claude was already reopened elsewhere; not launching again.")
		return alignErr
	}
	if len(owed) == 0 {
		// Nothing was running, so nothing was owed, but the switch still has to
		// open its target.
		owed = []string{dstProfilePath}
	}
	primaryErr, othersErr := s.launchAll(owed, dstProfilePath)
	if primaryErr != nil {
		if alignErr != nil {
			return fmt.Errorf("%w (and Claude Desktop could not be reopened: %v)", alignErr, primaryErr)
		}
		return fmt.Errorf("failed to launch target profile: %w", primaryErr)
	}
	if othersErr != nil {
		// The switch itself worked: the target is open and this is the account the
		// user asked for. Returning this would make every caller announce a failed
		// switch and skip marking the new account as current, which is a worse lie
		// than a log line — the switch DID happen.
		log.Printf("[Safe Switch] Switched, but an account that was open could not be reopened: %v", othersErr)
	}
	if alignErr != nil {
		// Claude is back up, so the user is not stranded; the sync is what failed.
		return alignErr
	}

	log.Printf("[Safe Switch] Switch completed successfully!")
	return nil
}

// bystanders returns the profiles Claude Desktop is running on other than the
// switch's source and target: accounts the user has open that this switch is not
// about, and that terminating will close as collateral.
//
// KNOWN LIMITATION: srcProfilePath is the one profile deliberately left closed, so
// which account that is now matters more than it used to. The GUI hosts derive it
// from DetectRunningProfile, which reports whichever running profile the process
// list happened to name first. With two accounts open and a switch to a third, the
// account left closed is therefore arbitrary. Every account still comes back
// except one, which is strictly better than the previous behaviour (only one came
// back at all), but picking it deterministically needs a record of which account
// the user last activated rather than a guess from process order.
//
// A failure to enumerate is not fatal. The switch still owes its target, and
// reopening one account is a far better outcome than refusing to switch.
func (s *Switcher) bystanders(srcProfilePath, dstProfilePath string) []string {
	running, err := s.Platform.DetectRunningProfiles()
	if err != nil {
		log.Printf("[Safe Switch] Could not list running profiles (%v); only the target will be reopened.", err)
		return nil
	}
	var out []string
	for _, p := range running {
		if p == "" || platform.SamePath(p, srcProfilePath) || platform.SamePath(p, dstProfilePath) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// containsPath reports whether paths already names the same directory as p. A
// linear scan rather than a map because the set is the number of accounts the
// user has open, and because the comparison is SamePath rather than equality.
func containsPath(paths []string, p string) bool {
	for _, existing := range paths {
		if platform.SamePath(existing, p) {
			return true
		}
	}
	return false
}

// launchAll opens every owed profile and reports the two kinds of failure
// separately, because they mean different things to the user.
//
// primary is the profile the operation exists to open (a switch's target), or ""
// when every profile counts the same (an align, which only puts back what it
// closed). primaryErr is the operation failing. othersErr is the operation
// succeeding while the user is left an account short — reporting that as the
// operation failing would tell them their switch did not happen when it did.
//
// Profiles are named by path rather than DisplayName(filepath.Base(path)): on the
// Store build the directory name is not the account's identity (every profile
// lives in a slot directory called "Claude"), so a name derived from the path
// would blame the wrong account.
func (s *Switcher) launchAll(paths []string, primary string) (primaryErr, othersErr error) {
	var others []string
	for _, p := range paths {
		if err := s.Platform.LaunchProfile(p); err != nil {
			if platform.SamePath(p, primary) {
				primaryErr = err
				continue
			}
			others = append(others, fmt.Sprintf("%s (%v)", p, err))
		}
	}
	if len(others) > 0 {
		othersErr = fmt.Errorf("could not reopen %s", strings.Join(others, "; "))
	}
	return primaryErr, othersErr
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
