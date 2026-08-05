package panelui

import "sync"

// PanelState is the panel's view state: which screen is up, whether a row's
// inline rename editor is open, and whether a progress card is over the top.
//
// It lives here rather than as loose variables in each host because both hosts
// held their own copy of the same three fields and the same rules for how they
// interact, kept in step only by someone reading the two files side by side.
// Every rule below was learned from a defect in one host that the other either
// shared or was one edit away from sharing:
//
//   - Navigating clears the card. Returning to the list does NOT, because
//     "go to the list" is also how the panel opens and how it is dismissed, so
//     clearing there made an operation in flight vanish the moment the user
//     pressed Escape and reopened, and made a failure that landed while the
//     panel was shut get reported nowhere.
//   - Navigating clears the rename editor, since the editor lived in markup
//     that navigation replaces. Clearing here rather than in each caller is
//     what stops a stuck flag freezing the list for good.
//   - The sticky notification is delivered while the lock is still held, so a
//     later state change cannot have its notification overtaken by an earlier
//     one. Released first, two goroutines could leave the macOS popover in
//     ApplicationDefined with no card on screen: the panel would then never
//     close by itself again for the rest of the session, with nothing on screen
//     to explain why.
//
// The zero value is not usable. Build one with NewPanelState.
type PanelState struct {
	mu         sync.Mutex
	view       string
	renameOpen bool
	progress   *ProgressVM
	onSticky   func(bool)
}

// NewPanelState returns a state on the given view with no card and no rename in
// progress.
//
// onSticky is called whenever "is a card on screen" may have changed, with the
// new value, and is called while the state is locked. It must therefore not
// call back into the same PanelState. macOS passes a function that flips the
// popover between transient and application-defined behaviour, which only
// dispatches to the main queue and so cannot re-enter. Windows has no such
// need and passes nil, which is allowed.
func NewPanelState(view string, onSticky func(bool)) *PanelState {
	return &PanelState{view: view, onSticky: onSticky}
}

// SetView moves to a screen the way the user navigating there would: the card
// comes down and any inline rename ends.
//
// Returning to the account list is navigation like any other here. The callers
// that reach the list WITHOUT the user navigating (opening the panel, an
// operation moving there on its way to putting up its own card) must use
// SetViewKeeping instead.
func (s *PanelState) SetView(v string) {
	s.mu.Lock()
	s.view = v
	s.renameOpen = false
	s.progress = nil
	s.notifyLocked()
	s.mu.Unlock()
}

// SetViewKeeping moves to a screen without taking down a card that is still on
// it. For the callers that are not the user navigating: opening or parking the
// panel, and an operation moving to the list on its way to reporting its own
// outcome.
func (s *PanelState) SetViewKeeping(v string) {
	s.mu.Lock()
	s.view = v
	s.renameOpen = false
	s.notifyLocked()
	s.mu.Unlock()
}

// SetProgress puts up, updates or takes down the card. A nil vm takes it down.
func (s *PanelState) SetProgress(vm *ProgressVM) {
	s.mu.Lock()
	s.progress = vm
	s.notifyLocked()
	s.mu.Unlock()
}

// SetRenameOpen records whether a row's inline rename editor is on screen. It
// is client-side state mirrored here because only the host knows when a reload
// is about to replace the document out from under it.
func (s *PanelState) SetRenameOpen(open bool) {
	s.mu.Lock()
	s.renameOpen = open
	s.mu.Unlock()
}

// Snapshot is a consistent read of the whole state, taken under one lock hold.
type Snapshot struct {
	View       string
	RenameOpen bool
	Progress   *ProgressVM
}

// Snapshot reads every field under a single lock hold, so the view a host
// renders and the card it draws over that view are from the same instant.
func (s *PanelState) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{View: s.view, RenameOpen: s.renameOpen, Progress: s.progress}
}

// Sticky reports whether a card is on screen, which is what the macOS popover
// reads as it opens to decide whether it is allowed to close itself.
func (s Snapshot) Sticky() bool { return s.Progress != nil }

// HoldReload reports whether a pending reload should be skipped to protect a
// half-typed rename.
//
// A reload replaces the whole document, so a backup or sync finishing while the
// user is mid-edit used to take away what they had typed, silently. The list
// goes a few seconds stale instead and the next reload catches it up.
//
// A card overrides that. Renaming one row does not stop the user clicking
// another and switching to it, and holding the reload then swallowed the whole
// card: no sign of the operation while it ran, and a stale "Switched
// successfully" appearing out of nowhere whenever the edit happened to end. The
// half-typed name is the smaller loss, and the card covers the editor anyway.
func (s Snapshot) HoldReload() bool {
	return s.View == "list" && s.RenameOpen && s.Progress == nil
}

// notifyLocked delivers the sticky value. The caller must hold the lock; see
// the type comment for why that is the point rather than an oversight.
func (s *PanelState) notifyLocked() {
	if s.onSticky != nil {
		s.onSticky(s.progress != nil)
	}
}
