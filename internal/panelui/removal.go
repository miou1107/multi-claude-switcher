package panelui

// RemovalOutcome is what the panel does next after core.RemoveProfile
// returns. Both hosts (cmd/mcs-menubar and cmd/mcs-tray) call
// DecideRemovalOutcome to get one of these rather than each re-implementing
// the same three-way split, which is exactly the kind of thing this codebase
// has already let drift between the two platforms once.
type RemovalOutcome struct {
	// ShowList is true for a clean removal: the folder moved and nothing went
	// wrong afterward. The caller sets its list status to ListStatus and
	// switches to the list view; no RemovedVM screen is shown at all, since
	// the row disappearing from the list is confirmation enough.
	ShowList bool
	// ListStatus is set only when ShowList is true.
	ListStatus string

	// Removed is set when ShowList is false: either the removal failed
	// outright, or it moved the folder but left something behind. The caller
	// shows the "removed" view with this VM.
	Removed RemovedVM
}

// DecideRemovalOutcome turns core.RemoveProfile's (dest, err) into what the
// panel shows next. folder, name and convos describe the account as it was
// immediately before removal ran: once the folder has moved, neither the
// display name nor the conversation count can be looked up again, so both
// hosts read them before calling core.RemoveProfile and pass them straight
// through here.
func DecideRemovalOutcome(folder, name string, convos int, dest string, err error) RemovalOutcome {
	switch {
	case dest != "" && err == nil:
		// A clean removal: the row disappearing from the list is the
		// confirmation. Routing to its own result screen bought a fourth click
		// on Done for nothing the list banner cannot already say.
		return RemovalOutcome{ShowList: true, ListStatus: name + " removed. Nothing was deleted."}
	case dest != "":
		// The folder moved but a registry write afterward failed — a partial
		// success, not the "nothing was moved" screen, which would send the
		// user looking for an account that has, in fact, already moved. The
		// complaint has to live on the result screen's own VM, not the status
		// line: the "removed" screen's only exit is showList, which clears the
		// status before anything renders.
		return RemovalOutcome{Removed: RemovedVM{Folder: folder, Name: name, Convos: convos, RegistryNote: err.Error()}}
	default:
		// core.RemoveProfile's contract is that dest == "" always pairs with a
		// non-nil error, but this function's whole purpose is to stop hosts
		// from carrying assumptions like that themselves — so it does not
		// trust the assumption either, including from itself. err == nil here
		// would nil-pointer-panic on err.Error() inside a render path neither
		// host recovers around, which is exactly the kind of drift this
		// function exists to catch, and worse than the panic already rejected
		// elsewhere in this file's history: that one at least carried a
		// message saying what went wrong. A generic message is the fallback,
		// not a crash.
		msg := "The removal failed, and no reason was given. Try again."
		if err != nil {
			msg = err.Error()
		}
		return RemovalOutcome{Removed: RemovedVM{Folder: folder, Name: name, Convos: convos, Err: msg}}
	}
}
