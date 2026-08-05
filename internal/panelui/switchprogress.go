package panelui

import "html"

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
