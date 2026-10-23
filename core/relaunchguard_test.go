package core

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// The scenario every test here starts from: the user is on FTData (a non-default
// profile MCS opened with --user-data-dir), Claude Desktop finishes installing an
// update in the background, quits itself, and comes back a few seconds later
// with no flag — on the default profile, which is somebody else's account.
const (
	guardDefault = "/Users/me/Library/Application Support/Claude"
	guardFTData  = "/Users/me/Library/Application Support/Claude_Profile2"
	guardOther   = "/Users/me/Library/Application Support/Claude_Other"
)

// guardClock is a hand-driven clock so the tests can say "ten seconds later"
// without sleeping.
type guardClock struct{ t time.Time }

func (c *guardClock) now() time.Time          { return c.t }
func (c *guardClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newGuardUnderTest() (*RelaunchGuard, *guardClock) {
	c := &guardClock{t: time.Date(2026, 10, 22, 20, 35, 0, 0, time.UTC)}
	return NewRelaunchGuard(c.now), c
}

// The marker Claude leaves behind in the profile it quit from is what proves
// the relaunch was Claude's own doing; see SelfRelaunchMarkerFresh.
const (
	withMarker    = true
	withoutMarker = false
)

func TestRelaunchGuardRestoresTheAccountClaudeDroppedWhenItRelaunchedItself(t *testing.T) {
	g, clock := newGuardUnderTest()

	if r := g.Observe([]string{guardFTData}, guardFTData, false, withoutMarker); r != nil {
		t.Fatalf("nothing has happened yet, got restore %+v", r)
	}
	if r := g.Observe(nil, guardFTData, false, withoutMarker); r != nil {
		t.Fatalf("Claude quitting is not yet a reason to act, got restore %+v", r)
	}
	clock.advance(10 * time.Second)
	r := g.Observe([]string{guardDefault}, guardFTData, false, withMarker)
	if r == nil {
		t.Fatal("Claude came back on the default profile ten seconds after leaving FTData, marker present; expected a restore")
	}
	if len(r.Reopen) != 1 || r.Reopen[0] != guardFTData {
		t.Fatalf("expected to reopen FTData only, got %v", r.Reopen)
	}
}

func TestRelaunchGuardLeavesClaudeAloneWhenItComesBackOnTheRecordedAccount(t *testing.T) {
	g, clock := newGuardUnderTest()
	g.Observe([]string{guardFTData}, guardFTData, false, withoutMarker)
	g.Observe(nil, guardFTData, false, withoutMarker)
	clock.advance(10 * time.Second)
	if r := g.Observe([]string{guardFTData}, guardFTData, false, withMarker); r != nil {
		t.Fatalf("Claude came back on the right account; expected nothing to do, got %+v", r)
	}
}

func TestRelaunchGuardLeavesAloneTheUserReopeningClaudeFromTheDock(t *testing.T) {
	g, clock := newGuardUnderTest()
	g.Observe([]string{guardFTData}, guardFTData, false, withoutMarker)
	// Cmd+Q, then a click on the Dock ten seconds later: same shape as the
	// updater's relaunch, but Claude left no marker because it did not quit
	// itself.
	g.Observe(nil, guardFTData, false, withoutMarker)
	clock.advance(10 * time.Second)
	if r := g.Observe([]string{guardDefault}, guardFTData, false, withoutMarker); r != nil {
		t.Fatalf("no marker means the user opened Claude; it must be left alone, got %+v", r)
	}
}

func TestRelaunchGuardIgnoresAMarkerOlderThanTheWindow(t *testing.T) {
	g, clock := newGuardUnderTest()
	g.Observe([]string{guardFTData}, guardFTData, false, withoutMarker)
	g.Observe(nil, guardFTData, false, withoutMarker)
	clock.advance(10 * time.Minute)
	if r := g.Observe([]string{guardDefault}, guardFTData, false, withMarker); r != nil {
		t.Fatalf("ten minutes is not Claude's own relaunch whatever is on disk; got %+v", r)
	}
}

func TestRelaunchGuardIgnoresAQuitThatMCSItselfCaused(t *testing.T) {
	g, clock := newGuardUnderTest()
	g.Observe([]string{guardFTData}, guardFTData, false, withoutMarker)
	// A switch closes Claude while MCS owes a relaunch; whatever comes back next
	// is MCS's own doing, even if the record has not caught up yet.
	g.Observe(nil, guardFTData, true, withoutMarker)
	clock.advance(5 * time.Second)
	if r := g.Observe([]string{guardDefault}, guardFTData, false, withMarker); r != nil {
		t.Fatalf("MCS closed Claude itself; the guard must not fight its own switch, got %+v", r)
	}
}

func TestRelaunchGuardIgnoresASecondAccountOpenedWhileTheFirstIsStillRunning(t *testing.T) {
	g, _ := newGuardUnderTest()
	g.Observe([]string{guardFTData}, guardFTData, false, withoutMarker)
	if r := g.Observe([]string{guardFTData, guardDefault}, guardFTData, false, withMarker); r != nil {
		t.Fatalf("the user opened a second account from the Dock; nothing vanished, got %+v", r)
	}
}

func TestRelaunchGuardDoesNotRestoreAnAccountThatWasNotOpenBefore(t *testing.T) {
	g, clock := newGuardUnderTest()
	// The record says FTData but the user had closed it and was on the default
	// profile by hand. A stale record must not drag them back to FTData.
	g.Observe([]string{guardDefault}, guardFTData, false, withoutMarker)
	g.Observe(nil, guardFTData, false, withoutMarker)
	clock.advance(10 * time.Second)
	if r := g.Observe([]string{guardDefault}, guardFTData, false, withMarker); r != nil {
		t.Fatalf("FTData was not open before the quit; expected nothing, got %+v", r)
	}
}

func TestRelaunchGuardReopensEveryAccountThatWasOpen(t *testing.T) {
	g, clock := newGuardUnderTest()
	g.Observe([]string{guardFTData, guardOther}, guardFTData, false, withoutMarker)
	g.Observe(nil, guardFTData, false, withoutMarker)
	clock.advance(10 * time.Second)
	r := g.Observe([]string{guardDefault}, guardFTData, false, withMarker)
	if r == nil {
		t.Fatal("expected a restore")
	}
	if len(r.Reopen) != 2 || r.Reopen[0] != guardFTData || r.Reopen[1] != guardOther {
		t.Fatalf("both accounts that were open should come back, got %v", r.Reopen)
	}
}

func TestRelaunchGuardDoesNothingWithoutARecordedAccount(t *testing.T) {
	g, clock := newGuardUnderTest()
	g.Observe([]string{guardFTData}, "", false, withoutMarker)
	g.Observe(nil, "", false, withoutMarker)
	clock.advance(10 * time.Second)
	if r := g.Observe([]string{guardDefault}, "", false, withMarker); r != nil {
		t.Fatalf("MCS never put the user anywhere, so it has no account to defend; got %+v", r)
	}
}

func TestRelaunchGuardStartsAFreshCycleAfterARestore(t *testing.T) {
	g, clock := newGuardUnderTest()
	g.Observe([]string{guardFTData}, guardFTData, false, withoutMarker)
	g.Observe(nil, guardFTData, false, withoutMarker)
	clock.advance(10 * time.Second)
	if r := g.Observe([]string{guardDefault}, guardFTData, false, withMarker); r == nil {
		t.Fatal("expected the first restore")
	}
	// The restore itself closes Claude (MCS owes the relaunch) and reopens
	// FTData. That round trip must not look like another self-relaunch.
	g.Observe(nil, guardFTData, true, withoutMarker)
	clock.advance(3 * time.Second)
	if r := g.Observe([]string{guardFTData}, guardFTData, false, withMarker); r != nil {
		t.Fatalf("the restore's own relaunch must be quiet, got %+v", r)
	}
	// And the next real self-relaunch is caught again.
	g.Observe(nil, guardFTData, false, withoutMarker)
	clock.advance(10 * time.Second)
	if r := g.Observe([]string{guardDefault}, guardFTData, false, withMarker); r == nil {
		t.Fatal("the guard should keep working after one restore")
	}
}

// TestRelaunchGuardCatchesARelaunchThatWasNeverSeenGone covers the fast case:
// Claude's helpers are still winding down, or the new instance is already up,
// by the time the process table is read, so no observation ever reads empty.
// The host records the quit from the system's own notification instead.
func TestRelaunchGuardCatchesARelaunchThatWasNeverSeenGone(t *testing.T) {
	g, clock := newGuardUnderTest()
	g.Observe([]string{guardFTData}, guardFTData, false, withoutMarker)
	g.MarkGone(false)
	clock.advance(8 * time.Second)
	r := g.Observe([]string{guardDefault}, guardFTData, false, withMarker)
	if r == nil {
		t.Fatal("the quit was reported by the system even though no observation read empty; expected a restore")
	}
	if len(r.Reopen) != 1 || r.Reopen[0] != guardFTData {
		t.Fatalf("expected to reopen FTData, got %v", r.Reopen)
	}
}

// TestRelaunchGuardMeasuresTheGapFromTheFirstQuit: a second empty observation
// (the host looks twice after a quit) must not restart the clock.
func TestRelaunchGuardMeasuresTheGapFromTheFirstQuit(t *testing.T) {
	g, clock := newGuardUnderTest()
	g.Observe([]string{guardFTData}, guardFTData, false, withoutMarker)
	g.Observe(nil, guardFTData, false, withoutMarker)
	clock.advance(4 * time.Minute)
	g.Observe(nil, guardFTData, false, withoutMarker)
	clock.advance(2 * time.Minute)
	if r := g.Observe([]string{guardDefault}, guardFTData, false, withMarker); r != nil {
		t.Fatalf("six minutes since the first quit is outside the window; the second look must not have reset it, got %+v", r)
	}
}

// TestRestoreAfterSelfRelaunchClosesTheWrongAccountAndReopensTheRightOnes
// covers the action the decision above leads to: Claude is on the default
// profile; MCS closes it and brings back what was open.
func TestRestoreAfterSelfRelaunchClosesTheWrongAccountAndReopensTheRightOnes(t *testing.T) {
	mp := &mockPlatform{running: true, detectedAll: []string{guardDefault}}
	s := NewSwitcher(mp, nil)

	if err := s.RestoreAfterSelfRelaunch([]string{guardFTData, guardOther}); err != nil {
		t.Fatalf("RestoreAfterSelfRelaunch: %v", err)
	}
	if !mp.terminated {
		t.Fatal("the account Claude relaunched onto must be closed first")
	}
	assertLaunched(t, mp.launchedPaths, guardFTData, guardOther)
	if owed := s.PendingRelaunch(); len(owed) != 0 {
		t.Fatalf("nothing should still be owed after the restore, got %v", owed)
	}
}

func TestRestoreAfterSelfRelaunchOwesNothingWhenClaudeCouldNotBeClosed(t *testing.T) {
	mp := &mockPlatform{running: true, detectedAll: []string{guardDefault}, terminateErr: errors.New("still running")}
	s := NewSwitcher(mp, nil)

	if err := s.RestoreAfterSelfRelaunch([]string{guardFTData}); err == nil {
		t.Fatal("a Claude that would not close is a failed restore")
	}
	if mp.launched {
		t.Fatal("nothing may be launched while the wrong instance is still up")
	}
	if owed := s.PendingRelaunch(); len(owed) != 0 {
		t.Fatalf("nothing was closed, so nothing is owed; got %v", owed)
	}
}

// SelfRelaunchMarkerFresh reads the file Claude Desktop writes into its profile
// just before quitting for an update, and tells a fresh one from a stale one.
func TestSelfRelaunchMarkerFresh(t *testing.T) {
	now := time.Date(2026, 10, 22, 20, 35, 16, 0, time.UTC)
	write := func(t *testing.T, body string) string {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "stealth-relaunch"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	ts := func(d time.Duration) string {
		return `{"ts":` + strconv.FormatInt(now.Add(-d).UnixMilli(), 10) + `,"windowVisible":true}`
	}

	if !SelfRelaunchMarkerFresh(write(t, ts(10*time.Second)), now) {
		t.Error("a marker written ten seconds ago is the relaunch that just happened")
	}
	if SelfRelaunchMarkerFresh(write(t, ts(20*time.Minute)), now) {
		t.Error("a marker from twenty minutes ago belongs to an earlier relaunch")
	}
	if SelfRelaunchMarkerFresh(write(t, "not json"), now) {
		t.Error("an unreadable marker proves nothing")
	}
	if SelfRelaunchMarkerFresh(t.TempDir(), now) {
		t.Error("no marker, no self-relaunch")
	}
}
