package panelui

import (
	"sync"
	"testing"
	"time"
)

func card(title string) *ProgressVM {
	return &ProgressVM{Phase: ProgressWorking, Title: title}
}

func TestNewPanelStateStartsIdle(t *testing.T) {
	s := NewPanelState("list", nil)
	snap := s.Snapshot()
	if snap.View != "list" {
		t.Errorf("View = %q, want %q", snap.View, "list")
	}
	if snap.Progress != nil {
		t.Errorf("Progress = %+v, want nil", snap.Progress)
	}
	if snap.RenameOpen {
		t.Error("RenameOpen = true, want false")
	}
}

func TestSetViewTakesTheCardDown(t *testing.T) {
	s := NewPanelState("list", nil)
	s.SetProgress(card("Switching profile"))
	s.SetView("settings")
	if got := s.Snapshot().Progress; got != nil {
		t.Errorf("Progress = %+v after navigating away, want nil", got)
	}
}

// Navigating to the list is navigation like any other. The distinction that
// matters is SetView vs SetViewKeeping, not which view is named: making "list"
// special here is what would let a host clear the card on the path that opens
// the panel.
func TestSetViewTakesTheCardDownEvenReturningToTheList(t *testing.T) {
	s := NewPanelState("settings", nil)
	s.SetProgress(card("Syncing"))
	s.SetView("list")
	if got := s.Snapshot().Progress; got != nil {
		t.Errorf("Progress = %+v, want nil: showList is the user dismissing the card", got)
	}
}

func TestSetViewKeepingLeavesTheCardUp(t *testing.T) {
	s := NewPanelState("settings", nil)
	vm := card("Switching profile")
	s.SetProgress(vm)
	s.SetViewKeeping("list")
	if got := s.Snapshot().Progress; got != vm {
		t.Errorf("Progress = %+v, want the card still up: opening the panel mid-operation must not hide it", got)
	}
}

func TestBothViewMovesEndAnInlineRename(t *testing.T) {
	for _, tc := range []struct {
		name string
		move func(*PanelState)
	}{
		{"SetView", func(s *PanelState) { s.SetView("settings") }},
		{"SetViewKeeping", func(s *PanelState) { s.SetViewKeeping("settings") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewPanelState("list", nil)
			s.SetRenameOpen(true)
			tc.move(s)
			if s.Snapshot().RenameOpen {
				t.Error("RenameOpen = true after navigating: the editor's markup is gone, and a stuck flag freezes the list for good")
			}
		})
	}
}

func TestSetProgressNilTakesTheCardDown(t *testing.T) {
	s := NewPanelState("list", nil)
	s.SetProgress(card("Backing up"))
	s.SetProgress(nil)
	if got := s.Snapshot().Progress; got != nil {
		t.Errorf("Progress = %+v, want nil", got)
	}
}

func TestStickyFollowsWhetherACardIsOnScreen(t *testing.T) {
	var mu sync.Mutex
	var seen []bool
	s := NewPanelState("list", func(on bool) {
		mu.Lock()
		seen = append(seen, on)
		mu.Unlock()
	})

	s.SetProgress(card("Switching profile")) // true
	s.SetViewKeeping("list")                 // true: card still up
	s.SetView("settings")                    // false: navigation took it down

	mu.Lock()
	defer mu.Unlock()
	want := []bool{true, true, false}
	if len(seen) != len(want) {
		t.Fatalf("sticky notifications = %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("sticky notifications = %v, want %v", seen, want)
		}
	}
}

// The hazard this closes: with the notification delivered after the lock is
// released, two goroutines changing the state can have their notifications land
// in the opposite order. On macOS that leaves the popover in
// ApplicationDefined with no card on screen, so the panel never closes by
// itself again for the rest of the session, with nothing to explain why.
//
// Holding the lock across the notification makes that impossible by
// construction. This test proves the lock is still held by showing that no
// other mutation can land while the observer is running.
func TestStickyIsDeliveredWhileTheStateIsStillLocked(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	// Only the first notification blocks, and it must not block the ones after
	// it. Gating this with sync.Once instead would make the test pass
	// regardless: Once.Do makes every later caller wait for the first to
	// return, so the second mutation would be held up by the test's own
	// scaffolding rather than by the lock under test.
	var mu sync.Mutex
	first := true
	s := NewPanelState("list", func(bool) {
		mu.Lock()
		isFirst := first
		first = false
		mu.Unlock()
		if isFirst {
			close(entered)
			<-release
		}
	})

	go s.SetProgress(card("Switching profile"))
	<-entered

	landed := make(chan struct{})
	go func() {
		s.SetView("settings")
		close(landed)
	}()

	select {
	case <-landed:
		close(release)
		t.Fatal("a second mutation landed while the sticky observer was still running: the notification is not delivered under the lock, so a later change can have its notification overtaken by an earlier one")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	<-landed
}

func TestNilStickyObserverIsAllowed(t *testing.T) {
	s := NewPanelState("list", nil) // the Windows host has nothing to notify
	s.SetProgress(card("Syncing"))
	s.SetViewKeeping("list")
	s.SetView("settings")
	if s.Snapshot().Progress != nil {
		t.Error("Progress non-nil after navigating away")
	}
}

func TestSnapshotStickyMatchesTheCard(t *testing.T) {
	s := NewPanelState("list", nil)
	if s.Snapshot().Sticky() {
		t.Error("Sticky() = true with no card up")
	}
	s.SetProgress(card("Switching profile"))
	if !s.Snapshot().Sticky() {
		t.Error("Sticky() = false with a card up: the popover would close itself the moment Claude takes the foreground, which is the event a switch ends with")
	}
}

func TestHoldReloadProtectsAHalfTypedRename(t *testing.T) {
	for _, tc := range []struct {
		name string
		snap Snapshot
		want bool
	}{
		{"editing the list, nothing else happening", Snapshot{View: "list", RenameOpen: true}, true},
		{"not editing", Snapshot{View: "list"}, false},
		{"editing, but a card is up", Snapshot{View: "list", RenameOpen: true, Progress: card("Switching profile")}, false},
		{"editing flag stuck on another screen", Snapshot{View: "settings", RenameOpen: true}, false},
		{"idle list", Snapshot{View: "list"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.snap.HoldReload(); got != tc.want {
				t.Errorf("HoldReload() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A card must win over a rename in flight. Holding the reload swallowed the
// whole card: no sign of the operation while it ran, then a stale outcome
// appearing out of nowhere whenever the edit happened to end.
func TestACardOverridesTheRenameHold(t *testing.T) {
	s := NewPanelState("list", nil)
	s.SetRenameOpen(true)
	if !s.Snapshot().HoldReload() {
		t.Fatal("HoldReload() = false while renaming with no card: a reload would wipe what the user typed")
	}
	s.SetProgress(card("Switching profile"))
	if s.Snapshot().HoldReload() {
		t.Error("HoldReload() = true with a card up: the card would never be drawn")
	}
}

// Exercises every mutator against Snapshot concurrently. Its job is to give the
// race detector something to work with (`go test -race`), which is what catches
// a field added later and left outside the lock.
//
// It is NOT a check that Snapshot reads all three fields in one hold. That was
// tried: an implementation deliberately rewritten to take two separate holds
// still passed, because the interleaving needed to observe the difference is
// far rarer than this loop can reach. The one-hold rule is held by reading
// Snapshot, not by this test, and the invariant assertion below is a backstop
// that costs nothing rather than a guarantee.
func TestSnapshotSurvivesConcurrentMutation(t *testing.T) {
	s := NewPanelState("list", nil)
	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			s.SetProgress(card("Switching profile"))
			s.SetView("settings")
			s.SetViewKeeping("list")
			s.SetRenameOpen(true)
			s.SetRenameOpen(false)
		}
	}()

	for i := 0; i < 2000; i++ {
		snap := s.Snapshot()
		// SetView is the only path that reaches "settings", and it nils the
		// card in the same lock hold. Seeing both is a torn read.
		if snap.View == "settings" && snap.Progress != nil {
			close(stop)
			wg.Wait()
			t.Fatal("snapshot showed a card over the settings screen, which no single state ever holds: the fields were not read together")
		}
	}
	close(stop)
	wg.Wait()
}
