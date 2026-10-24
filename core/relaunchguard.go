package core

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// selfRelaunchWindow is how long after Claude Desktop quit a relaunch may still
// be taken as Claude's own. Claude's updater is back within ten to fifteen
// seconds (measured 2026-10-19 to 10-22 on six relaunches); five minutes is
// the age at which Claude itself stops trusting its own marker (its reader
// logs "stale marker" past that), so the two agree on what "just now" means.
const selfRelaunchWindow = 5 * time.Minute

// selfRelaunchMarker is the file Claude Desktop writes into the profile it is
// running on just before it quits itself for an update, and reads back (and
// deletes) on the next launch to restore the window. When the relaunch lands
// on another profile, the marker stays where it was written, which is how MCS
// can tell "Claude quit itself from this account" from "the user quit it".
// Name observed in Claude Desktop 3.1623 (the constant TA in its main script).
const selfRelaunchMarker = "stealth-relaunch"

// RelaunchGuard notices when Claude Desktop reopens itself on the wrong
// account, so the host can put the user back where they were.
//
// This exists because Claude Desktop installs its updates in the background:
// it downloads the new version, waits until the user has been idle for a
// while, quits itself and relaunches. The relaunch does not carry the
// --user-data-dir flag MCS started it with, so it comes up on the default
// profile — whichever account lives in <AppSupport>/Claude — and the user,
// who had switched to another account, finds themselves back on the first one
// with nothing in MCS's own log to explain it. Measured on one machine: six
// such relaunches in a week, three of them while a non-default account was
// in use, each followed by the user switching again by hand.
//
// It is fed every time Claude Desktop starts or stops (see Observe and
// MarkGone) and only ever decides; what to do about the decision is
// RestoreAfterSelfRelaunch.
type RelaunchGuard struct {
	now func() time.Time

	// running is every profile Claude Desktop was on the last time it was seen
	// running. When Claude vanishes this is what it vanished FROM, and so what
	// a restore brings back: every account that was open, not just the
	// recorded one, because Claude's quit closed all of them at once.
	running []string
	// goneAt is when the last Claude Desktop process disappeared, or zero while
	// Claude is running.
	goneAt time.Time
	// goneByMCS records that MCS itself had Claude closed at that moment (a
	// switch, a sync, or this guard's own restore). Whatever comes back next
	// is MCS's doing and must not be second-guessed, even when the record of
	// the active account has not been updated yet.
	goneByMCS bool
}

// NewRelaunchGuard returns a guard reading the clock from now; nil means the
// wall clock.
func NewRelaunchGuard(now func() time.Time) *RelaunchGuard {
	if now == nil {
		now = time.Now
	}
	return &RelaunchGuard{now: now}
}

// Restore is the guard's verdict that Claude Desktop relaunched itself onto
// the wrong account. Reopen is every profile that was open before and must
// come back; it is the user's account to defend, so the recorded account is
// always among them.
type Restore struct {
	Reopen []string
	// Gap is how long Claude was gone, kept for the log so a reader can tell
	// an updater relaunch (seconds) from anything slower.
	Gap time.Duration
}

// MarkGone records that Claude Desktop has just quit, from the system's own
// notification rather than from a look at the process table. The table lags:
// Claude's helper processes outlive the main one, and a fast relaunch can be
// up again before a delayed look happens, so a guard that only learned of
// quits from Observe(nil, ...) could miss the whole sequence. Idempotent while
// Claude stays gone; the first call fixes the time the gap is measured from.
//
// mcsOwes reports whether MCS itself has Claude closed for an operation of its
// own; once true for a quit it stays true, since the relaunch that follows is
// then MCS's.
func (g *RelaunchGuard) MarkGone(mcsOwes bool) {
	if g.goneAt.IsZero() {
		g.goneAt = g.now()
		g.goneByMCS = mcsOwes
		return
	}
	if mcsOwes {
		g.goneByMCS = true
	}
}

// Observe takes what Claude Desktop is running on now and returns a Restore
// when the sequence it has just completed is a self-relaunch onto the wrong
// account, or nil when there is nothing to do.
//
// running is every profile Claude Desktop is on at this moment (empty when it
// is not running). recorded is the path of the account MCS last put the user
// on, or "" when there is no record. mcsOwes reports whether MCS itself has
// Claude closed for an operation of its own right now. selfRelaunched is
// whether the recorded profile holds a fresh relaunch marker (see
// SelfRelaunchMarkerFresh): Claude's own word that it quit itself from there.
//
// The decision needs all of these to hold, in order:
//  1. Claude was gone and is now back. A new instance appearing beside a
//     running one is the user opening a second account by hand.
//  2. MCS did not close it. Its own switches and restores close Claude too.
//  3. It was gone for less than selfRelaunchWindow.
//  4. There is a recorded account, it was open before the quit, and it is not
//     among what came back. A record naming an account the user had already
//     closed by hand is stale and does not get to pull them back.
//  5. Claude left its marker in the recorded profile. Without it the quit was
//     the user's (Cmd+Q, then the Dock), and the user is never overruled.
func (g *RelaunchGuard) Observe(running []string, recorded string, mcsOwes, selfRelaunched bool) *Restore {
	if len(running) == 0 {
		g.MarkGone(mcsOwes)
		return nil
	}

	wasGone, goneAt, byMCS, before := !g.goneAt.IsZero(), g.goneAt, g.goneByMCS, g.running
	g.running = append([]string(nil), running...)
	g.goneAt = time.Time{}
	g.goneByMCS = false

	if !wasGone || byMCS || mcsOwes || recorded == "" {
		return nil
	}
	gap := g.now().Sub(goneAt)
	if gap > selfRelaunchWindow {
		log.Printf("[Relaunch Guard] Claude Desktop came back %s after quitting; too long ago to be its own relaunch, leaving it alone.", gap.Round(time.Second))
		return nil
	}
	if containsPath(running, recorded) {
		return nil
	}
	// No history: the guard was started while Claude was already open and has
	// never seen it running, so it cannot say from memory what was open. The
	// marker can: Claude wrote it into the recorded profile seconds before
	// quitting, which is proof that profile was running. This is exactly how
	// the first real update after this shipped was missed (2026-10-24 01:34),
	// so it is not a corner case. Only the recorded account comes back then;
	// bystanders the guard never saw cannot be reopened.
	if len(before) == 0 && selfRelaunched {
		before = []string{recorded}
	}
	if !containsPath(before, recorded) {
		log.Printf("[Relaunch Guard] Claude Desktop came back on %v, but %s was not open before it quit; leaving it alone.", running, recorded)
		return nil
	}
	if !selfRelaunched {
		log.Printf("[Relaunch Guard] Claude Desktop came back on another account %s after leaving %s, but left no relaunch marker there; treating it as the user's own doing.", gap.Round(time.Second), recorded)
		return nil
	}
	return &Restore{Reopen: before, Gap: gap}
}

// SelfRelaunchMarkerFresh reports whether profileDir holds a relaunch marker
// Claude Desktop wrote within selfRelaunchWindow of now. The marker is JSON
// with a ts field in milliseconds since the epoch; anything unreadable counts
// as no marker, because the marker is evidence and evidence that cannot be
// read is not evidence.
func SelfRelaunchMarkerFresh(profileDir string, now time.Time) bool {
	data, err := os.ReadFile(filepath.Join(profileDir, selfRelaunchMarker))
	if err != nil {
		return false
	}
	var m struct {
		TS int64 `json:"ts"`
	}
	if err := json.Unmarshal(data, &m); err != nil || m.TS == 0 {
		return false
	}
	age := now.Sub(time.UnixMilli(m.TS))
	return age >= 0 && age <= selfRelaunchWindow
}

// RestoreAfterSelfRelaunch undoes a self-relaunch: it closes the Claude Desktop
// that came back on the wrong account and reopens every profile in reopen.
//
// It is deliberately not a SafeSwitch. A switch syncs conversations between
// the account being left and the one being entered; here the user never used
// the account Claude landed on, there is nothing to carry over, and the
// record of the active account is already right. Closing and reopening is the
// whole job, and it is the same closing and reopening a switch does, so the
// relaunch debt is recorded the same way: a quit in the middle still puts
// Claude back (see PendingRelaunch).
func (s *Switcher) RestoreAfterSelfRelaunch(reopen []string) error {
	if len(reopen) == 0 {
		return fmt.Errorf("nothing to reopen")
	}
	log.Printf("[Relaunch Guard] Claude Desktop reopened itself on the wrong account; closing it and reopening %v", reopen)
	s.notePendingRelaunch(reopen...)
	if err := s.Platform.TerminateApp(); err != nil {
		s.ClaimPendingRelaunch()
		return fmt.Errorf("could not close the Claude Desktop that relaunched itself: %w", err)
	}
	owed := s.ClaimPendingRelaunch()
	if len(owed) == 0 {
		// A quit handler got there first and has already reopened them.
		return nil
	}
	primaryErr, othersErr := s.launchAll(owed, owed[0])
	if primaryErr != nil {
		return fmt.Errorf("could not reopen %s: %w", owed[0], primaryErr)
	}
	if othersErr != nil {
		log.Printf("[Relaunch Guard] Put the user back, but an account that was open could not be reopened: %v", othersErr)
	}
	log.Printf("[Relaunch Guard] Claude Desktop is back on %v", owed)
	return nil
}
