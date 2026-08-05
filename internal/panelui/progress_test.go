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

func listWith(vm *ProgressVM) string {
	return WithProgress(RenderList(progressAccounts(), false, ""), vm)
}

// A nil VM has to leave the page exactly as it was. Every screen goes through
// WithProgress on every reload, so a scrim leaking out of a nil would put an
// invisible sheet over a panel nobody asked to block.
func TestProgressNilLeavesThePageUntouched(t *testing.T) {
	page := RenderList(progressAccounts(), false, "")
	if got := WithProgress(page, nil); got != page {
		t.Fatal("a nil progress VM changed the page")
	}
}

// The card is an overlay, so it must land inside the document rather than after
// it. This is the contract WithProgress relies on; the test below checks every
// renderer actually honours it.
func TestProgressCardGoesInsideTheBody(t *testing.T) {
	got := listWith(&ProgressVM{Title: "Switching profile"})
	card := strings.Index(got, `class="prog-bg"`)
	body := strings.LastIndex(got, "</body>")
	if card < 0 || body < 0 || card > body {
		t.Fatalf("card at %d, </body> at %d: the card must be inside the body", card, body)
	}
}

// Every screen a long operation can be started from has to be able to carry the
// card. WithProgress finds its seam by looking for </body>, so a renderer that
// did not go through shell() would silently append the card after the document
// instead, which browsers tolerate and reviewers do not see.
func TestEveryScreenCanCarryTheCard(t *testing.T) {
	vm := &ProgressVM{Title: "Working"}
	pages := map[string]string{
		"list":       RenderList(progressAccounts(), false, ""),
		"sync":       RenderSync(progressAccounts(), "", false),
		"settings":   RenderSettings(SettingsVM{Version: "0.13.1"}),
		"rescan":     RenderRescan(nil, map[string]bool{}),
		"newprofile": RenderNewProfile(NewProfileVM{SuggestedName: "Work"}),
		"removed":    RenderRemoved(RemovedVM{Name: "Old one"}),
		"debug":      RenderDebug(DebugVM{Report: "MCS 0.13.1"}),
		"merge": RenderMerge(
			MergeCandidateVM{Folder: "Claude", Name: "Work", Current: true},
			MergeCandidateVM{Folder: "Claude_2", Name: "Work 2"},
			core.MergePlan{Combined: 3}, "", false),
	}
	for name, page := range pages {
		got := WithProgress(page, vm)
		if strings.Count(got, `class="prog-bg"`) != 1 {
			t.Errorf("%s: expected exactly one card", name)
			continue
		}
		if strings.Index(got, `class="prog-bg"`) > strings.LastIndex(got, "</body>") {
			t.Errorf("%s: the card landed outside the document", name)
		}
	}
}

func TestProgressWorkingCard(t *testing.T) {
	html := listWith(&ProgressVM{
		Title: "Switching profile", Detail: "Claude will restart in a moment.",
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
	// looking at an ordinary screen while Claude is shut, with nothing saying why.
	if strings.Contains(html, ">Close<") {
		t.Error("the working card offered a Close button")
	}
	// Nor may it auto dismiss: the work has not finished, and returning to the
	// screen mid operation is exactly the blind moment this card removes.
	if strings.Contains(html, "setTimeout") {
		t.Error("the working card scheduled its own dismissal")
	}
}

func TestProgressDoneCardClearsItself(t *testing.T) {
	html := listWith(&ProgressVM{
		Phase: ProgressDone, Title: "Switched successfully", Detail: "You are now on Home.",
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
}

// Something that went wrong without stopping the work keeps the tick and the
// title. Calling it a failure would contradict the screen behind the card,
// which already shows the work as done.
func TestProgressWarningCardKeepsTheSuccessAndWaits(t *testing.T) {
	html := listWith(&ProgressVM{
		Phase: ProgressDone, Title: "Sync finished", Detail: "Copied 12 conversations into Home.",
		Warn: "2 files could not be read and were skipped (see the log).", Dismiss: "showSync",
	})
	for _, want := range []string{
		"Sync finished",
		"Copied 12 conversations into Home.",
		"2 files could not be read and were skipped (see the log).",
		`onclick="send('showSync','')">Close<`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("warning card is missing %q", want)
		}
	}
	// A warning that clears itself after two seconds is a warning nobody read.
	if strings.Contains(html, "setTimeout") {
		t.Error("the warning card dismissed itself")
	}
}

func TestProgressFailedCardShowsTheReasonAndWaits(t *testing.T) {
	html := listWith(&ProgressVM{
		Phase: ProgressFailed, Title: "Switch failed", Err: "no profile folder there",
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
}

// A host that reports a failure without a message still has to say something,
// and it must not be a claim about what was or was not changed: these
// operations can fail after Claude has already been closed.
func TestProgressFailedCardWithoutAMessage(t *testing.T) {
	html := listWith(&ProgressVM{Phase: ProgressFailed, Title: "Merge failed"})
	if !strings.Contains(html, "Merge failed") {
		t.Fatal("failed card lost its heading when no error text was given")
	}
	if !strings.Contains(html, ">Close<") {
		t.Error("failed card without a message left the user no way out")
	}
}

// Text with an apostrophe is the bug that shipped in v0.9.1: html.EscapeString
// writes &#39;, the parser turns it back into a quote before inline JS is
// parsed, and the script breaks. Everything the caller supplies is HTML text
// here, never a JS literal, and this test is what keeps it that way.
func TestProgressEscapesEverythingTheCallerSupplies(t *testing.T) {
	html := listWith(&ProgressVM{
		Phase: ProgressDone, Title: "Vin's <b>switch</b>", Detail: "on Vin's <i>Mac</i>",
		Warn: "Vin's <u>warning</u>",
	})
	for _, leaked := range []string{"<b>switch</b>", "<i>Mac</i>", "<u>warning</u>"} {
		if strings.Contains(html, leaked) {
			t.Errorf("%q was rendered as markup", leaked)
		}
	}
	if !strings.Contains(html, "Vin&#39;s") {
		t.Error("an apostrophe was not escaped")
	}
}

// Dismiss is written straight into an onclick, so it is checked against known
// action names rather than escaped. An unknown one must not reach the page.
func TestProgressDismissIsAllowlisted(t *testing.T) {
	html := listWith(&ProgressVM{
		Phase: ProgressFailed, Title: "Sync failed", Dismiss: "');alert(1);('",
	})
	if strings.Contains(html, "alert(1)") {
		t.Fatal("an unknown dismiss action was written into the page")
	}
	if !strings.Contains(html, `send('showList','')`) {
		t.Error("an unknown dismiss action must fall back to the account list")
	}
}

// SwitchOutcome is the three-way split both hosts share. Written once because
// they are separate copies of the same flow and have drifted before, and the
// drift that matters here is one host calling a switch failed while the other
// calls it done.
func TestSwitchOutcomeSplitsTheThreeCases(t *testing.T) {
	if got := SwitchOutcome("Home", nil); got.Phase != ProgressDone || got.Warn != "" ||
		got.Detail != "You are now on Home." {
		t.Errorf("a clean switch produced %+v", got)
	}

	warn := &core.SwitchedWithWarning{Err: errors.New("failed to auto sync sessions")}
	got := SwitchOutcome("Home", fmt.Errorf("switching: %w", warn))
	if got.Phase != ProgressDone {
		t.Errorf("a sync failure after a completed switch was reported as phase %v, want ProgressDone", got.Phase)
	}
	if !strings.Contains(got.Warn, "failed to auto sync sessions") {
		t.Errorf("the warning lost its reason: %q", got.Warn)
	}
	if got.Err != "" {
		t.Errorf("a completed switch carried a failure message: %q", got.Err)
	}
	if got.Detail != "You are now on Home." {
		t.Errorf("a completed switch stopped naming the account: %q", got.Detail)
	}

	got = SwitchOutcome("Home", errors.New("failed to launch target profile"))
	if got.Phase != ProgressFailed {
		t.Errorf("a switch that never landed was reported as phase %v, want ProgressFailed", got.Phase)
	}
	if got.Err != "failed to launch target profile" {
		t.Errorf("the failure lost its reason: %q", got.Err)
	}
}

// The account name is not always known: a switch aimed at a folder that has
// been removed cannot name it. The card still has to render, without an empty
// gap where the name should be.
func TestSwitchOutcomeWithoutATargetName(t *testing.T) {
	got := SwitchOutcome("", nil)
	if got.Detail != "" {
		t.Errorf("an unnamed account produced the detail line %q", got.Detail)
	}
	if !strings.Contains(listWith(got), `class="prog-bg"`) {
		t.Error("an unnamed account rendered no card at all")
	}
}

// Files that could not be read are a warning, not a failure: the run continued
// past them deliberately and the conversations that did copy really did copy.
// They are also the one outcome this panel used to lose, which is why the sync
// code grew a system notification for them.
func TestSyncOutcomeTreatsUnreadableFilesAsAWarning(t *testing.T) {
	rep := &core.SyncReport{CopiedCount: 12, SkipErrors: []string{"a.json: bad", "b.json: bad"}}
	got := SyncOutcome("Home", rep, nil)
	if got.Phase != ProgressDone {
		t.Fatalf("a sync that copied 12 conversations was reported as phase %v", got.Phase)
	}
	if !strings.Contains(got.Detail, "12 conversations") {
		t.Errorf("the summary lost the count: %q", got.Detail)
	}
	// The skipped files belong in the warning box, not repeated in the summary.
	if !strings.Contains(got.Warn, "2 files could not be read") {
		t.Errorf("the skipped files are missing from the warning: %q", got.Warn)
	}
	if strings.Contains(got.Detail, "could not be read") {
		t.Errorf("the skipped files were said twice: %q", got.Detail)
	}
}

func TestSyncOutcomeFailureUsesTheActionableMessage(t *testing.T) {
	got := SyncOutcome("Home", nil, core.ErrRunningProfileUnknown)
	if got.Phase != ProgressFailed {
		t.Fatalf("a failed sync was reported as phase %v", got.Phase)
	}
	// core translates this one into something the user can act on. The card must
	// not go around it and print the raw error.
	if got.Err != "Quit Claude Desktop first, then try Sync again." {
		t.Errorf("the card printed %q instead of the actionable message", got.Err)
	}
	if got.Dismiss != "showSync" {
		t.Errorf("a failed sync sent the user to %q, not back to Sync", got.Dismiss)
	}
}

func TestBackupOutcomeCounts(t *testing.T) {
	if got := BackupOutcome(0, 0); strings.Contains(got.Detail, "0 accounts") {
		t.Errorf("a backup that found nothing said %q", got.Detail)
	}
	if got := BackupOutcome(1, 0); got.Detail != "Backed up 1 account." {
		t.Errorf("one account produced %q", got.Detail)
	}
	if got := BackupOutcome(3, 0); got.Detail != "Backed up 3 accounts." {
		t.Errorf("three accounts produced %q", got.Detail)
	}
	for _, n := range []int{0, 1, 3} {
		if got := BackupOutcome(n, 0); got.Dismiss != "showSettings" {
			t.Errorf("backup of %d sent the user to %q, not back to Settings", n, got.Dismiss)
		}
	}
}

// Every backup failing used to render as a green tick over "No account had any
// conversations stored yet": a stated cause that is false, on the one screen
// whose purpose is not lying. A disk full or a bad permission on the backup
// root is exactly how a user reaches it.
func TestBackupOutcomeDoesNotBlameEmptinessForFailure(t *testing.T) {
	got := BackupOutcome(0, 4)
	if got.Phase != ProgressFailed {
		t.Fatalf("four failed backups produced phase %v, want ProgressFailed", got.Phase)
	}
	if strings.Contains(got.Detail, "conversations stored") || strings.Contains(got.Err, "conversations stored") {
		t.Errorf("a failed backup blamed the user having no data: %+v", got)
	}
	if !strings.Contains(got.Err, "4 accounts could not be backed up") {
		t.Errorf("the failure lost its count: %q", got.Err)
	}
}

// A partial failure is still a success for the accounts that worked, so it
// keeps the tick and puts the rest in the warning box, where the card waits to
// be closed instead of clearing itself.
func TestBackupOutcomePartialFailureWarns(t *testing.T) {
	got := BackupOutcome(2, 1)
	if got.Phase != ProgressDone {
		t.Fatalf("two of three backed up produced phase %v, want ProgressDone", got.Phase)
	}
	if got.Detail != "Backed up 2 accounts." {
		t.Errorf("the successful count was lost: %q", got.Detail)
	}
	if got.Warn != "1 other account could not be backed up (see the log)." {
		t.Errorf("the failed count was lost or mis-pluralised: %q", got.Warn)
	}
}

func TestMergeOutcome(t *testing.T) {
	if got := MergeOutcome(nil); got.Phase != ProgressDone || got.Title != "Accounts merged" {
		t.Errorf("a clean merge produced %+v", got)
	}
	got := MergeOutcome(errors.New("could not read the archive"))
	if got.Phase != ProgressFailed || got.Err != "could not read the archive" {
		t.Errorf("a failed merge produced %+v", got)
	}
}

// The screen underneath is still rendered on purpose: the user can see where
// they are. What they cannot do is reach it, and the scrim is what stops them,
// rather than the host's busy flag silently dropping the click. That only holds
// if the scrim stacks above everything else the panel can have open, so the
// z-index is the thing worth pinning: a row menu is 5 and the confirmation
// dialog is 10, and a card drawn under either would leave a live control on a
// screen the user is being told to wait on.
func TestProgressScrimStacksAboveEveryOtherLayer(t *testing.T) {
	html := listWith(&ProgressVM{Title: "Switching profile"})
	if !strings.Contains(html, `>Home<`) {
		t.Error("the list was not rendered behind the progress card")
	}
	if !strings.Contains(html, ".prog-bg{position:fixed;inset:0;background:rgba(30,20,50,.32);display:flex;align-items:center;justify-content:center;z-index:11") {
		t.Error("the progress scrim is not at z-index 11, above the dialog (10) and the row menu (5)")
	}
}
