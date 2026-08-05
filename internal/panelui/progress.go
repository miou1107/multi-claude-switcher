package panelui

import (
	"errors"
	"html"
	"strconv"
	"strings"

	"github.com/miou1107/multi-claude-switcher/core"
)

// ProgressPhase is where an operation has got to. It is deliberately not a pair
// of bools: "working", "done" and "failed" are three cards with three different
// jobs, and a bool for each would let a host describe an operation that both
// worked and did not.
type ProgressPhase int

const (
	// ProgressWorking is the zero value on purpose. A host that sets a progress
	// VM without saying which phase has, at that moment, only ever started one.
	ProgressWorking ProgressPhase = iota
	ProgressDone
	ProgressFailed
)

// ProgressVM is the card the panel shows over whatever screen the user is on
// while a long operation runs, and for a moment after it ends.
//
// It exists because the panel had a modal for questions and a banner for
// results and nothing at all for the interval between them. Every operation
// that closes Claude Desktop takes seconds, and for those seconds the panel
// looked like it had ignored the click. Worse, the banner reporting the outcome
// was routinely dismissed before it could be read: reopening Claude hands the
// foreground back to Claude, and both hosts used to close the panel when that
// happened. There is a note in the sync code admitting exactly that, with a
// system notification bolted on to rescue the one case nobody could afford to
// miss.
//
// One view model for every operation rather than one per operation, because the
// two hosts are separate copies of the same flow and have drifted before, and
// four copies each would be four more chances to drift.
type ProgressVM struct {
	Phase ProgressPhase

	// Title is the headline: "Switching profile", "Sync finished". Written by
	// the caller because only it knows what is happening.
	Title string

	// Detail is the sentence under it, and may be empty when the title has
	// already said everything.
	Detail string

	// Warn is something that went wrong without stopping the operation: the
	// account switched but its sessions did not sync, the sync ran but some
	// files could not be read. Read only when Phase is ProgressDone, where it
	// keeps the tick and the title and makes the card wait to be closed rather
	// than clearing itself. Calling these failures would contradict the screen
	// behind the card, which already shows the work as done.
	Warn string

	// Err is the failure text, read only when Phase is ProgressFailed.
	Err string

	// Dismiss is the panel action the card's Close button and its auto dismiss
	// send: where the user lands once the card is gone. Empty means the account
	// list. Only the names in dismissActions are honoured, so a caller cannot
	// put an arbitrary string into the page's JS.
	Dismiss string
}

// dismissActions is the allowlist for ProgressVM.Dismiss. The value is written
// straight into an onclick, so it is checked against known names rather than
// escaped and hoped over: escaping is what failed in v0.9.1, when EscapeString
// turned an apostrophe into &#39; and the HTML parser turned it back before the
// inline JS was parsed.
var dismissActions = map[string]bool{
	"showList":     true,
	"showSync":     true,
	"showSettings": true,
}

// dismissAction resolves Dismiss to an action name that is safe to write into
// the page. Anything unrecognised falls back to the account list, which every
// screen can be reached from.
func dismissAction(name string) string {
	if dismissActions[name] {
		return name
	}
	return "showList"
}

// WithProgress draws vm's card over an already-rendered page.
//
// A separate step rather than a parameter on each renderer: the card is an
// overlay that does not care what is underneath it, and four renderers each
// taking a view model is four places for one host to pass it and the other to
// forget. Here every screen gets the card from one call in each host's reload,
// and adding a fifth screen needs no change at all.
//
// A nil VM returns the page untouched, which is the ordinary case.
func WithProgress(page string, vm *ProgressVM) string {
	if vm == nil {
		return page
	}
	// The seam is shell()'s own closing tag, which this package writes, not
	// arbitrary HTML handed in from elsewhere. TestEveryScreenCanCarryTheCard is
	// what keeps that true for every renderer.
	i := strings.LastIndex(page, "</body>")
	if i < 0 {
		// Not a page this package built. Appending still shows the card: the
		// alternative is dropping it, and a silently missing progress card is
		// the failure this whole feature exists to remove.
		return page + renderProgress(vm)
	}
	return page[:i] + renderProgress(vm) + page[i:]
}

// renderProgress builds the centred card and the scrim under it.
//
// The card sits where the confirmation dialog sat, so pressing the button in
// that dialog replaces the question with the progress in place and the user's
// eyes never move.
func renderProgress(vm *ProgressVM) string {
	if vm == nil {
		return ""
	}
	esc := html.EscapeString
	dismiss := `send('` + dismissAction(vm.Dismiss) + `','')`
	detail := ""
	if vm.Detail != "" {
		detail = `<p>` + esc(vm.Detail) + `</p>`
	}
	var inner string
	switch vm.Phase {
	case ProgressDone:
		if vm.Warn != "" {
			// The work happened, so the tick and the title stay; what did not
			// work is said underneath, and the card waits, because a warning
			// that clears itself after two seconds is a warning nobody read.
			inner = `<div class="prog-mark ok">&#10003;</div>
    <h2>` + esc(vm.Title) + `</h2>` + detail + `
    <div class="prog-warn">` + esc(vm.Warn) + `</div>
    <button class="btn btn-light" onclick="` + dismiss + `">Close</button>`
			break
		}
		// Dismisses itself: the work is over, and a card the user has to close
		// to get back to a screen that already shows the change is a chore. The
		// failed card below does the opposite, and for the opposite reason.
		inner = `<div class="prog-mark ok">&#10003;</div>
    <h2>` + esc(vm.Title) + `</h2>` + detail + `
    <script>setTimeout(function(){` + dismiss + `},2200);</script>`
	case ProgressFailed:
		// Never a claim about what was or was not changed: these operations can
		// fail after Claude has already been closed, and "nothing was changed"
		// would be a lie in exactly the case the user most needs the truth.
		msg := "It did not finish."
		if vm.Err != "" {
			msg = vm.Err
		}
		inner = `<div class="prog-mark bad">!</div>
    <h2>` + esc(vm.Title) + `</h2>
    <p class="prog-err">` + esc(msg) + `</p>
    <button class="btn btn-light" onclick="` + dismiss + `">Close</button>`
	default:
		// No Close button and no timer. Closing it would leave the user looking
		// at an ordinary screen while Claude is shut, with nothing saying why,
		// which is the blind moment this card exists to remove.
		inner = `<div class="prog-spin"></div>
    <h2>` + esc(vm.Title) + `</h2>` + detail
	}
	return `
<div class="prog-bg"><div class="prog" role="status" aria-live="polite">
    ` + inner + `
</div></div>`
}

// SwitchStarting is the card raised the moment a switch begins.
func SwitchStarting() *ProgressVM {
	return &ProgressVM{
		Title:  "Switching profile",
		Detail: "Claude will restart in a moment.",
	}
}

// SwitchOutcome turns what SafeSwitch returned into the card that says so.
//
// The three-way split is the point. A nil error is a plain success; a
// core.SwitchedWithWarning means the user WAS moved and only the optional
// session sync failed, so calling it a failed switch would contradict the
// account list sitting right behind the card; anything else really did leave
// them where they were, or worse.
func SwitchOutcome(target string, err error) *ProgressVM {
	if err != nil {
		var warn *core.SwitchedWithWarning
		if !errors.As(err, &warn) {
			return &ProgressVM{Phase: ProgressFailed, Title: "Switch failed", Err: err.Error()}
		}
	}
	vm := &ProgressVM{Phase: ProgressDone, Title: "Switched successfully"}
	if target != "" {
		vm.Detail = "You are now on " + target + "."
	}
	if err != nil {
		vm.Warn = err.Error()
	}
	return vm
}

// SyncStarting is the card raised while a manual sync runs.
func SyncStarting() *ProgressVM {
	return &ProgressVM{
		Title:   "Syncing conversations",
		Detail:  "Claude closes while this runs, and reopens when it is done.",
		Dismiss: "showSync",
	}
}

// SyncOutcome turns a finished manual sync into its card.
//
// Files that could not be read are a warning, not a failure: the run continued
// past them deliberately, and the conversations that did copy really did copy.
// They are also the one outcome this panel used to lose, which is why the sync
// code grew a system notification for them.
func SyncOutcome(target string, rep *core.SyncReport, err error) *ProgressVM {
	if err != nil {
		return &ProgressVM{
			Phase: ProgressFailed, Title: "Sync failed",
			Err: core.SyncFailureReason(err), Dismiss: "showSync",
		}
	}
	summary, skipped := core.SyncResultParts(rep, target)
	return &ProgressVM{
		Phase: ProgressDone, Title: "Sync finished",
		Detail: summary, Warn: skipped, Dismiss: "showSync",
	}
}

// MergeStarting is the card raised while two duplicate accounts are merged.
func MergeStarting() *ProgressVM {
	return &ProgressVM{
		Title:  "Merging accounts",
		Detail: "Claude closes while this runs, and reopens when it is done.",
	}
}

// MergeOutcome turns a finished merge into its card. It lands on the account
// list either way: on success one of the two rows is gone, which is the
// confirmation, and on failure the list is where the user tries again from.
func MergeOutcome(err error) *ProgressVM {
	if err != nil {
		return &ProgressVM{Phase: ProgressFailed, Title: "Merge failed", Err: err.Error()}
	}
	return &ProgressVM{
		Phase: ProgressDone, Title: "Accounts merged",
		Detail: "The two accounts are now one.",
	}
}

// BackupStarting is the card raised while every account is backed up.
func BackupStarting() *ProgressVM {
	return &ProgressVM{Title: "Backing up accounts", Dismiss: "showSettings"}
}

// BackupOutcome reports how many accounts were backed up, and how many tried
// and failed. The run carries on past a failure, so one call can be both.
//
// failed is a separate count rather than folded into done because the card
// states a cause. With only a total, a run in which every backup failed looks
// exactly like a run in which no account had anything to back up, and the panel
// said the latter, under a green tick: a false cause on the one screen whose
// purpose is not lying.
func BackupOutcome(done, failed int) *ProgressVM {
	if done == 0 && failed > 0 {
		return &ProgressVM{
			Phase: ProgressFailed, Title: "Backup failed",
			Err:     plural(failed, "account", "accounts") + " could not be backed up (see the log).",
			Dismiss: "showSettings",
		}
	}
	vm := &ProgressVM{Phase: ProgressDone, Title: "Backup finished", Dismiss: "showSettings"}
	switch {
	case done == 0:
		vm.Title = "Nothing to back up"
		vm.Detail = "No account had any conversations stored yet."
	default:
		vm.Detail = "Backed up " + plural(done, "account", "accounts") + "."
	}
	if failed > 0 {
		vm.Warn = plural(failed, "other account", "other accounts") + " could not be backed up (see the log)."
	}
	return vm
}

// plural renders a count with its noun, so "1 accounts" never reaches a screen.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
