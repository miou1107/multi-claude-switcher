package panelui

import (
	"errors"
	"html"

	"github.com/miou1107/multi-claude-switcher/core"
)

// SwitchPhase is where a switch has got to. It is deliberately not a bool pair:
// "working", "done" and "failed" are three cards with three different jobs, and
// a bool for each would let a host describe a switch that both worked and did
// not.
type SwitchPhase int

const (
	// SwitchWorking is the zero value on purpose. A host that sets a progress VM
	// without saying which phase has, at that moment, only ever started one.
	SwitchWorking SwitchPhase = iota
	SwitchDone
	SwitchFailed
)

// SwitchProgressVM is what the panel shows over the account list while a switch
// runs, and for a moment after it ends.
//
// It exists because the panel had a modal for questions and a banner for
// results and nothing at all for the interval between them: a switch closes
// Claude Desktop, moves the user and reopens it, and for that second and a half
// the panel looked like it had ignored the click.
type SwitchProgressVM struct {
	Phase SwitchPhase

	// Target is the display name of the account being switched to, quoted back
	// to the user so the confirmation names the account rather than saying
	// something happened. Empty when the host cannot name it, which is a real
	// case: a switch aimed at a folder that has since been removed.
	Target string

	// Err is the failure text, read only when Phase is SwitchFailed.
	Err string

	// Warn is something that went wrong AFTER the user was already moved: the
	// account changed, and the optional session sync did not. Read only when
	// Phase is SwitchDone, where it turns the card into "switched, but", which
	// then waits to be closed instead of clearing itself. Saying "Switch failed"
	// here would contradict the list behind the card, which already shows the
	// target as the current account. See core.SwitchedWithWarning.
	Warn string
}

// SwitchOutcome turns what SafeSwitch returned into the card that says so.
//
// Shared by the two hosts rather than written twice: they are separate copies of
// the same flow and have drifted before, and the drift that matters here is one
// host telling the user their switch failed while the other says it worked.
//
// The three-way split is the point. A nil error is a plain success; a
// core.SwitchedWithWarning means the user WAS moved and only the optional
// session sync failed, so calling it a failed switch would contradict the
// account list sitting right behind the card; anything else really did leave
// them where they were, or worse.
func SwitchOutcome(target string, err error) *SwitchProgressVM {
	if err == nil {
		return &SwitchProgressVM{Phase: SwitchDone, Target: target}
	}
	var warn *core.SwitchedWithWarning
	if errors.As(err, &warn) {
		return &SwitchProgressVM{Phase: SwitchDone, Target: target, Warn: err.Error()}
	}
	return &SwitchProgressVM{Phase: SwitchFailed, Target: target, Err: err.Error()}
}

// renderSwitchProgress builds the centred card and the scrim under it. A nil VM
// renders nothing, which is what every caller other than a running switch
// passes: a scrim on an idle list would put an invisible sheet over a panel
// nobody asked to block.
//
// The card sits where the confirmation dialog sat, so pressing Switch replaces
// the question with the progress in place and the user's eyes never move.
func renderSwitchProgress(vm *SwitchProgressVM) string {
	if vm == nil {
		return ""
	}
	esc := html.EscapeString
	var inner string
	switch vm.Phase {
	case SwitchDone:
		// Naming the account is the whole point of the line; with no name there
		// is nothing to say that the heading has not already said, so the line
		// is dropped rather than padded out with "the account you picked".
		sub := ""
		if vm.Target != "" {
			sub = `<p>You are now on ` + esc(vm.Target) + `.</p>`
		}
		if vm.Warn != "" {
			// The switch worked, so the tick and the heading stay; what did not
			// work is said underneath, and the card waits, because a warning
			// that clears itself after two seconds is a warning nobody read.
			inner = `<div class="prog-mark ok">&#10003;</div>
    <h2>Switched successfully</h2>` + sub + `
    <div class="prog-warn">` + esc(vm.Warn) + `</div>
    <button class="btn btn-light" onclick="send('showList','')">Close</button>`
			break
		}
		// Dismisses itself: the switch is over, and a card the user has to close
		// to get back to a list that already shows the change is a chore. The
		// failed card below does the opposite, and for the opposite reason.
		inner = `<div class="prog-mark ok">&#10003;</div>
    <h2>Switched successfully</h2>` + sub + `
    <script>setTimeout(function(){send('showList','')},2200);</script>`
	case SwitchFailed:
		// Never a claim about what was or was not changed: a switch can fail
		// after Claude has already been closed, and "nothing was changed" would
		// be a lie in exactly the case the user most needs the truth.
		msg := "The switch did not complete."
		if vm.Err != "" {
			msg = vm.Err
		}
		inner = `<div class="prog-mark bad">!</div>
    <h2>Switch failed</h2>
    <p class="prog-err">` + esc(msg) + `</p>
    <button class="btn btn-light" onclick="send('showList','')">Close</button>`
	default:
		// No Close button and no timer. Closing it would leave the user looking
		// at an ordinary list while Claude is shut, with nothing on screen
		// saying why, which is the blind moment this card exists to remove.
		inner = `<div class="prog-spin"></div>
    <h2>Switching profile</h2>
    <p>Claude will restart in a moment.</p>`
	}
	return `
<div class="prog-bg"><div class="prog" role="status" aria-live="polite">
    ` + inner + `
</div></div>`
}
