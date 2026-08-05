package panelui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/miou1107/multi-claude-switcher/core"
)

func progressAccounts() []ProfileVM {
	return []ProfileVM{
		{Folder: "Claude", Name: "Work", Current: true, SignedIn: true},
		{Folder: "Claude_Home", Name: "Home", SignedIn: true},
	}
}

// A nil VM has to leave the list exactly as it was. Every other caller passes
// nil, and a scrim leaking into the idle list would put an invisible sheet over
// a panel nobody asked to block.
func TestSwitchProgressNilRendersNoScrim(t *testing.T) {
	html := RenderList(progressAccounts(), false, "", nil)
	if strings.Contains(html, `class="prog-bg"`) {
		t.Fatal("a nil switch VM rendered the progress scrim")
	}
}

func TestSwitchProgressWorkingCard(t *testing.T) {
	html := RenderList(progressAccounts(), false, "", &SwitchProgressVM{
		Phase: SwitchWorking, Target: "Home",
	})
	for _, want := range []string{
		`class="prog-bg"`,
		`class="prog-spin"`,
		"Switching profile",
		"Claude will restart in a moment.",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("working card is missing %q", want)
		}
	}
	// The working card must not offer a way out. Closing it would leave the user
	// looking at a list while Claude is shut, with nothing saying why.
	if strings.Contains(html, ">Close<") {
		t.Error("the working card offered a Close button")
	}
	// Nor may it auto dismiss: the switch has not finished, and returning to the
	// list mid switch is exactly the blind moment this card exists to remove.
	if strings.Contains(html, "setTimeout") {
		t.Error("the working card scheduled its own dismissal")
	}
}

func TestSwitchProgressDoneCardNamesTheAccount(t *testing.T) {
	html := RenderList(progressAccounts(), false, "", &SwitchProgressVM{
		Phase: SwitchDone, Target: "Home",
	})
	for _, want := range []string{
		"Switched successfully",
		"You are now on Home.",
		// The whole call, not just "setTimeout": a timer that fires a
		// misspelled action leaves the card on screen for good, which is this
		// feature's worst failure and would pass a check for the word alone.
		`setTimeout(function(){send('showList','')},2200)`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("done card is missing %q", want)
		}
	}
	if strings.Contains(html, "Switch failed") {
		t.Error("the done card carried failure wording")
	}
}

// A name with an apostrophe is the bug that shipped in v0.9.1: html.EscapeString
// writes &#39;, the parser turns it back into a quote before inline JS is
// parsed, and the script breaks. The account name is HTML text here, never a JS
// literal, and this test is what keeps it that way.
func TestSwitchProgressDoneCardEscapesTheName(t *testing.T) {
	html := RenderList(progressAccounts(), false, "", &SwitchProgressVM{
		Phase: SwitchDone, Target: "Vin's <b>work</b>",
	})
	if strings.Contains(html, "<b>work</b>") {
		t.Error("the account name was rendered as markup")
	}
	if !strings.Contains(html, "Vin&#39;s") {
		t.Error("the account name was not escaped")
	}
}

func TestSwitchProgressFailedCardShowsTheErrorAndWaits(t *testing.T) {
	html := RenderList(progressAccounts(), false, "", &SwitchProgressVM{
		Phase: SwitchFailed, Target: "Home", Err: "no profile folder there",
	})
	for _, want := range []string{
		"Switch failed",
		"no profile folder there",
		// Same reason as the timer above: a Close button wired to nothing, or to
		// a misspelled action, is a card with no way out at all.
		`onclick="send('showList','')">Close<`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("failed card is missing %q", want)
		}
	}
	// An error the user never read is an error that did not get reported.
	if strings.Contains(html, "setTimeout") {
		t.Error("the failed card dismissed itself before the user could read it")
	}
	if strings.Contains(html, "Switched successfully") {
		t.Error("the failed card carried success wording")
	}
}

// A host that reports a failure without a message still has to say something,
// and it must not be a claim about what was or was not changed: a switch can
// fail after Claude has already been closed.
func TestSwitchProgressFailedCardWithoutAMessage(t *testing.T) {
	html := RenderList(progressAccounts(), false, "", &SwitchProgressVM{Phase: SwitchFailed})
	if !strings.Contains(html, "Switch failed") {
		t.Fatal("failed card lost its heading when no error text was given")
	}
	if !strings.Contains(html, ">Close<") {
		t.Error("failed card without a message left the user no way out")
	}
}

// The account name is not always known: a switch aimed at a folder that has
// been removed cannot name it. The card still has to render.
func TestSwitchProgressWithoutATargetName(t *testing.T) {
	for _, phase := range []SwitchPhase{SwitchWorking, SwitchDone, SwitchFailed} {
		html := RenderList(progressAccounts(), false, "", &SwitchProgressVM{Phase: phase})
		if !strings.Contains(html, `class="prog-bg"`) {
			t.Errorf("phase %v with no target name rendered no card", phase)
		}
		if strings.Contains(html, "You are now on .") {
			t.Errorf("phase %v rendered an empty account name", phase)
		}
	}
}

// A switch that moved the user but failed to sync their sessions is not a
// failed switch. Saying so would contradict the account list right behind the
// card, which already shows the target as current.
func TestSwitchProgressWarningCardKeepsTheSuccessAndWaits(t *testing.T) {
	html := RenderList(progressAccounts(), false, "", &SwitchProgressVM{
		Phase: SwitchDone, Target: "Home",
		Warn: "skipped auto sync: failed to back up source profile",
	})
	for _, want := range []string{
		"Switched successfully",
		"You are now on Home.",
		"skipped auto sync: failed to back up source profile",
		`onclick="send('showList','')">Close<`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("warning card is missing %q", want)
		}
	}
	if strings.Contains(html, "Switch failed") {
		t.Error("a switch that worked was reported as failed")
	}
	// A warning that clears itself after two seconds is a warning nobody read.
	if strings.Contains(html, "setTimeout") {
		t.Error("the warning card dismissed itself")
	}
}

// SwitchOutcome is the three-way split both hosts share. Written once because
// they are separate copies of the same flow and have drifted before, and the
// drift that matters here is one host calling a switch failed while the other
// calls it done.
func TestSwitchOutcomeSplitsTheThreeCases(t *testing.T) {
	if got := SwitchOutcome("Home", nil); got.Phase != SwitchDone || got.Warn != "" || got.Target != "Home" {
		t.Errorf("a clean switch produced %+v", got)
	}

	warn := &core.SwitchedWithWarning{Err: errors.New("failed to auto sync sessions")}
	got := SwitchOutcome("Home", fmt.Errorf("switching: %w", warn))
	if got.Phase != SwitchDone {
		t.Errorf("a sync failure after a completed switch was reported as phase %v, want SwitchDone", got.Phase)
	}
	if !strings.Contains(got.Warn, "failed to auto sync sessions") {
		t.Errorf("the warning lost its reason: %q", got.Warn)
	}
	if got.Err != "" {
		t.Errorf("a completed switch carried a failure message: %q", got.Err)
	}

	got = SwitchOutcome("Home", errors.New("failed to launch target profile"))
	if got.Phase != SwitchFailed {
		t.Errorf("a switch that never landed was reported as phase %v, want SwitchFailed", got.Phase)
	}
	if got.Err != "failed to launch target profile" {
		t.Errorf("the failure lost its reason: %q", got.Err)
	}
}

// The list is still behind the scrim on purpose: the user can see where they
// are. What they cannot do is start a second switch, and the scrim is what
// stops them, rather than the host's busy flag silently dropping the click.
func TestSwitchProgressKeepsTheListBehindIt(t *testing.T) {
	html := RenderList(progressAccounts(), false, "", &SwitchProgressVM{Phase: SwitchWorking})
	if !strings.Contains(html, `>Home<`) {
		t.Error("the list was not rendered behind the progress card")
	}
}
