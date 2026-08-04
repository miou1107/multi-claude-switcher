package panelui

import (
	"strings"
	"testing"
	"time"

	"github.com/miou1107/multi-claude-switcher/core"
)

// dupPillMarkup is the rendered pill, not its class name. shell() puts every class
// name into the <style> block of every page, so asserting on a bare class name
// passes whether or not the element was rendered — and its negation can never fail.
const dupPillMarkup = `<span class="dup-pill">Duplicate</span>`

// The panel host cannot be tested without a display, so these cover the part
// that decides what the user is shown: both hosts consume this same output.

func TestComputePreselect(t *testing.T) {
	accounts := []core.ScannedAccount{
		{HomeFolder: "Claude", Complete: true, UUID: "aaa"},
		{HomeFolder: "ClaudeWork", SignedOut: true, Note: core.SignedOutNote},
		{UUID: "zzz"}, // ghost: no folder, never selectable
	}

	t.Run("first run preselects everything switchable or ready to sign in", func(t *testing.T) {
		pre := ComputePreselect(accounts, nil)
		if !pre["Claude"] {
			t.Error("the signed-in account should be preselected")
		}
		if !pre["ClaudeWork"] {
			t.Error("a folder awaiting sign-in should be preselected; leaving it out is what hid it")
		}
		if len(pre) != 2 {
			t.Errorf("ghosts are not selectable, got %v", pre)
		}
	})

	t.Run("an existing registry is authoritative", func(t *testing.T) {
		pre := ComputePreselect(accounts, []string{"ClaudeWork"})
		if pre["Claude"] {
			t.Error("not in the registry, so not preselected")
		}
		if !pre["ClaudeWork"] {
			t.Error("in the registry, so preselected")
		}
	})

	t.Run("configured empty preselects nothing", func(t *testing.T) {
		if pre := ComputePreselect(accounts, []string{}); len(pre) != 0 {
			t.Errorf("got %v", pre)
		}
	})
}

func TestRenderRescanShowsProfileAwaitingSignIn(t *testing.T) {
	accounts := []core.ScannedAccount{
		{HomeFolder: "Claude", Complete: true, UUID: "aaa-bbb", Email: "me@example.com"},
		{HomeFolder: "ClaudeWork", SignedOut: true, Note: core.SignedOutNote},
	}
	html := RenderRescan(accounts, ComputePreselect(accounts, nil))

	if !strings.Contains(html, "ClaudeWork") {
		t.Fatal("the folder awaiting sign-in must appear at all")
	}
	if !strings.Contains(html, core.SignedOutNote) {
		t.Fatal("it must say what to do about it, not just appear")
	}
	// It has to be tickable: managing it is what puts it in the account list,
	// which is the only way to switch to it and sign in.
	if !strings.Contains(html, `data-folder="ClaudeWork"`) {
		t.Fatal("it must be a selectable card")
	}
	if !strings.Contains(html, "me@example.com") {
		t.Fatal("the signed-in account should still render as before")
	}
}

// A profile MCS has just created is in the same position as one the user made by
// hand: a real folder with no account in it yet. Rescan drew it as a ghost
// instead — "Unrecognized account", no name, no tick box — because the ghost
// branch claims anything without a live account. The user would have been told
// their brand-new profile was unrecognised, and had no way to select it.
func TestRenderRescanShowsAJustCreatedProfileAsSelectable(t *testing.T) {
	accounts := []core.ScannedAccount{
		{HomeFolder: "Claude", Complete: true, UUID: "aaa-bbb", Email: "me@example.com"},
		{HomeFolder: "Claude_Work", Pending: true, Note: core.PendingSignInNote},
	}
	html := RenderRescan(accounts, ComputePreselect(accounts, nil))

	if strings.Contains(html, "Unrecognized account") {
		t.Fatal("a profile MCS just created must not be drawn as an unrecognised ghost")
	}
	if !strings.Contains(html, "Claude_Work") {
		t.Fatal("it must be named, not anonymous")
	}
	if !strings.Contains(html, `data-folder="Claude_Work"`) {
		t.Fatal("it must be tickable — selecting it is what puts it in the account list")
	}
	if !strings.Contains(html, core.PendingSignInNote) {
		t.Fatal("it must say what the user still has to do")
	}
}

func TestRenderSyncSkipsProfilesWithoutAnAccount(t *testing.T) {
	profiles := []ProfileVM{
		{Folder: "Claude", Name: "Claude", SignedIn: true},
		{Folder: "ClaudeWork", Name: "Work"}, // no account yet
		{Folder: "ClaudeThird", Name: "Third", SignedIn: true},
	}
	html := RenderSync(profiles, "", false)

	// Only the two signed-in accounts can be paired, in both directions.
	if strings.Contains(html, `data-to="ClaudeWork"`) || strings.Contains(html, `data-from="ClaudeWork"`) {
		t.Fatal("a profile with no account has no session bucket; syncing it can only fail")
	}
	if !strings.Contains(html, `data-from="Claude" data-to="ClaudeThird"`) {
		t.Fatal("the two signed-in accounts should still be offered")
	}
	if !strings.Contains(html, "not signed in yet") {
		t.Fatal("silently omitting it looks like a bug; say why it is missing")
	}
}

func TestRenderListFlagsProfileAwaitingSignIn(t *testing.T) {
	html := RenderList([]ProfileVM{
		{Folder: "Claude", Name: "Claude", Current: true, SignedIn: true},
		{Folder: "ClaudeWork", Name: "Work"},
	}, false, "")
	if !strings.Contains(html, "Not signed in yet. Switch here, then sign in.") {
		t.Fatal("the card must say why it is different, or switching to it lands on an unexplained login screen")
	}
	if !strings.Contains(html, `data-folder="ClaudeWork"`) {
		t.Fatal("it must still be switchable; that is how the user signs in")
	}
}

// TestRenderListRowButtonIsAWrenchMenu pins change 1 of the row-menu redesign:
// the three-dot button (itself a redesign of an earlier pencil) is gone, and a
// bordered wrench opens a menu anchored to the row instead of navigating to a
// separate screen. It must keep stopping the click from bubbling to the card's
// own switch handler: that guard already existed for the three-dot button, and
// dropping it here would make every "options" click also switch accounts.
//
// The icon is asserted as inline SVG on purpose. Set as a character instead,
// every wrench in Unicode resolves to the colour emoji font on macOS, which
// would put one full-colour icon among a panel of flat monochrome ones and
// render differently again in WebView2.
func TestRenderListRowButtonIsAWrenchMenu(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", Current: true, SignedIn: true}}, false, "")
	if strings.Contains(html, ">✎<") || strings.Contains(html, ">⋯<") {
		t.Fatal("neither the old pencil nor the old three-dot glyph may still be the row button")
	}
	if !strings.Contains(html, `<button type="button" class="chevbtn" aria-label="Account options" aria-haspopup="menu" aria-expanded="false" onclick="event.stopPropagation();toggleRowMenu(this)"><svg`) {
		t.Fatalf("want the wrench button with its aria attributes, stopPropagation and toggle handler intact:\n%s", html)
	}
	if !strings.Contains(html, `stroke="currentColor"`) {
		t.Fatalf("the icon must be drawn inline, not set as a character that macOS resolves to colour emoji:\n%s", html)
	}
	if !strings.Contains(html, `<div class="rowmenu" role="menu">`) {
		t.Fatalf("the dropdown needs the menu role:\n%s", html)
	}
	if !strings.Contains(html, `role="menuitem" onclick="event.stopPropagation();startRename(this)">Change name</button>`) {
		t.Fatalf("want a Change name menu item with the menuitem role:\n%s", html)
	}
	if strings.Contains(html, "showRename") {
		t.Fatal("showRename was the old screen-navigation action; it must not survive the redesign")
	}
}

// TestRowMenuOnlyOneOpenAtATimeAndClosesOnOutsideClick pins the mechanics
// behind "opening one row's menu closes any other" and "clicking anywhere
// else on the page closes it": there is no JS runtime in this test suite, so
// the only thing that can be asserted on is the literal source of
// toggleRowMenu (which closes every other menu before it opens its own) and
// the document-level click listener that closes whatever is open when the
// click did not land inside a .rowmenu-wrap.
func TestRowMenuOnlyOneOpenAtATimeAndClosesOnOutsideClick(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false, "")
	if !strings.Contains(html, "function toggleRowMenu(btn){") {
		t.Fatalf("toggleRowMenu must exist:\n%s", html)
	}
	if !strings.Contains(html, "closeAllRowMenus();\n    if (!willOpen) return;") {
		t.Fatalf("toggleRowMenu must close every other menu before deciding whether to open its own:\n%s", html)
	}
	// Capture phase, and it swallows the click. On the bubble phase the card's
	// own switch handler had already run, so dismissing row A's menu by clicking
	// row B also raised "Switch to B?", one Enter away from closing the user's
	// Claude. The listener must only swallow while a menu is actually open, or an
	// ordinary click on a row would stop working.
	if !strings.Contains(html, `if (e.target.closest('.rowmenu-wrap')) return;`) ||
		!strings.Contains(html, `if (!document.querySelector('.rowmenu.open')) return;`) {
		t.Fatalf("a click outside any row menu's wrap must close whatever is open, and only then:\n%s", html)
	}
	if !strings.Contains(html, "    e.stopPropagation();\n    e.preventDefault();\n  }, true);") {
		t.Fatalf("the dismissing click must be swallowed in the capture phase, before the card's own handler:\n%s", html)
	}
}

// TestRowMenuAndRenameAreMutuallyExclusive pins two states nothing else handled:
// two rows in edit mode at once, and a menu opened while another row was being
// renamed. Both silently discarded whatever had been typed.
func TestRowMenuAndRenameAreMutuallyExclusive(t *testing.T) {
	html := RenderList([]ProfileVM{
		{Folder: "Claude", Name: "Work", SignedIn: true},
		{Folder: "Claude_Two", Name: "Two", SignedIn: true},
	}, false, "")
	if !strings.Contains(html, "function cancelAllRenames(){") {
		t.Fatalf("want a single place that ends every open rename:\n%s", html)
	}
	if !strings.Contains(html, "    closeAllRowMenus();\n    // One row at a time.") {
		t.Fatalf("starting a rename must end any other open rename:\n%s", html)
	}
	if !strings.Contains(html, "  function toggleRowMenu(btn){\n    cancelAllRenames();") {
		t.Fatalf("opening a row menu must end an in-progress rename:\n%s", html)
	}
}

// TestEscapeCancelsAnOpenRenameBeforeHidingThePanel pins the Tab-then-Escape
// case: rowRenameKey only sees Escape while focus is in the input, and Tab
// reaches the row's own Cancel and Save buttons. From there Escape used to fall
// through the whole chain to hidePanel and shut the panel on a half-typed name.
func TestEscapeCancelsAnOpenRenameBeforeHidingThePanel(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false, "")
	editing := strings.Index(html, "var editing = document.querySelector('.card.renaming');")
	hide := strings.Index(html, "send('hidePanel','');")
	if editing < 0 {
		t.Fatalf("Escape must notice an open rename:\n%s", html)
	}
	if hide < 0 || editing > hide {
		t.Fatalf("the rename check must come before Escape hides the panel:\n%s", html)
	}
}

// TestRowMenuOpensUpwardNearTheBottomDecidedInJS pins the requirement that the
// downward-vs-upward choice is computed from the button's actual position at
// click time, in the script, rather than guessed in Go from a row's index —
// Go has no way to know how tall the rendered popover actually is, since that
// depends on how many rows and banners are on screen.
func TestRowMenuOpensUpwardNearTheBottomDecidedInJS(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false, "")
	if !strings.Contains(html, "var r = btn.getBoundingClientRect();") ||
		!strings.Contains(html, `menu.classList.toggle('up', r.bottom > window.innerHeight - 90);`) {
		t.Fatalf("the up/down decision must read the button's real position at click time:\n%s", html)
	}
	if !strings.Contains(html, ".rowmenu.up{top:auto;bottom:calc(100% + 4px)}") {
		t.Fatalf("want the CSS that actually flips the menu upward:\n%s", html)
	}
}

// TestRowMenuButtonShowsItsOpenStateWithoutTouchingItsIcon pins how the open
// state is carried now that the icon is an SVG element rather than a character:
// by the .open class alone. The earlier version wrote textContent to swap a
// chevron glyph, which against an inline SVG would delete the icon outright the
// first time a menu closed.
func TestRowMenuButtonShowsItsOpenStateWithoutTouchingItsIcon(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false, "")
	if strings.Contains(html, "btn.textContent=") || strings.Contains(html, "b.textContent=") {
		t.Fatalf("writing textContent on the button would delete its inline SVG icon:\n%s", html)
	}
	if !strings.Contains(html, "btn.classList.add('open');") {
		t.Fatalf("opening must mark the button open:\n%s", html)
	}
	if !strings.Contains(html, ".chevbtn:hover,.chevbtn.open{") {
		t.Fatalf("the open class needs a visible style, or the state is invisible:\n%s", html)
	}
	if !strings.Contains(html, ".chevbtn{width:28px;height:28px;flex:none;border:1px solid") {
		t.Fatalf("the button must be bordered; unboxed it read as decoration and went unnoticed:\n%s", html)
	}
}

// TestAskRemoveClosesTheRowMenuBeforeOpeningTheDialog pins the fix for a bug the
// Escape-ordering test below could not see, because it was written assuming a row
// menu and the modal are never open at once. They can be: the menu item's own
// stopPropagation keeps the document click handler from closing the menu, so
// without this the menu stays .open behind the scrim, and Escape (which closes an
// open row menu first, deliberately) spends the user's first press on something
// invisible while the dialog appears to ignore the key.
func TestAskRemoveClosesTheRowMenuBeforeOpeningTheDialog(t *testing.T) {
	h := RenderList([]ProfileVM{
		{Folder: "Claude", Name: "Work", Convos: 2, SignedIn: true},
		{Folder: "Claude_Two", Name: "Two", SignedIn: true},
	}, false, "")
	fn := jsFunctions(h)["askRemove"]
	closes := strings.Index(fn, "closeAllRowMenus();")
	asks := strings.Index(fn, "askConfirm(")
	if closes < 0 {
		t.Fatalf("askRemove must close the row menu it was chosen from:\n%s", fn)
	}
	if asks < 0 || closes > asks {
		t.Fatalf("the menu must be closed before any dialog is raised, or Escape lands on the hidden menu:\n%s", fn)
	}
}

// TestEscapeClosesRowMenuBeforeItsOtherMeanings pins the ordering requirement
// directly: the row-menu-closing branch must appear, in source, before every
// other branch already in the panel's Escape chain (the modal, the debug
// textarea, and the generic INPUT/TEXTAREA-backs-out-to-the-list case), since
// whichever branch runs first is the one Escape actually does.
func TestEscapeClosesRowMenuBeforeItsOtherMeanings(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false, "")
	rowMenuIdx := strings.Index(html, `if(document.querySelector('.rowmenu.open')) { closeAllRowMenus(); return; }`)
	modalIdx := strings.Index(html, `if(document.getElementById('mcsModal').classList.contains('on')) { closeConfirm(); return; }`)
	inputIdx := strings.Index(html, `if(ae && (ae.tagName==='INPUT' || ae.tagName==='TEXTAREA')) { send('showList',''); return; }`)
	if rowMenuIdx < 0 || modalIdx < 0 || inputIdx < 0 {
		t.Fatalf("fixture broken: one of the three Escape branches is missing entirely:\n%s", html)
	}
	if !(rowMenuIdx < modalIdx && rowMenuIdx < inputIdx) {
		t.Fatalf("closing an open row menu must be checked before the modal or the generic input case:\nrowMenu@%d modal@%d input@%d", rowMenuIdx, modalIdx, inputIdx)
	}
}

// TestRenderListRenamesInPlace pins change 2: Rename no longer navigates to a
// separate screen. Each row already carries an input holding its current
// name, plus Cancel and Save controls, hidden until the card's own .renaming
// class is toggled on by startRename (asserted separately) — so the swap to
// editable is instant and needs no round trip to Go.
func TestRenderListRenamesInPlace(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude_Work", Name: "Work account", SignedIn: true}}, false, "")
	if !strings.Contains(html, `<input class="rnrow" type="text" value="Work account" onclick="event.stopPropagation()" onkeydown="rowRenameKey(event,this)">`) {
		t.Fatalf("want an inline input pre-filled with the current name:\n%s", html)
	}
	if !strings.Contains(html, `<button type="button" class="rncancel" onclick="event.stopPropagation();cancelRename(this)">Cancel</button>`) {
		t.Fatalf("want a Cancel control:\n%s", html)
	}
	if !strings.Contains(html, `<button type="button" class="rnsave" onclick="event.stopPropagation();rowRenameSave(this)">Save</button>`) {
		t.Fatalf("want a Save control:\n%s", html)
	}
	if !strings.Contains(html, ".card.renaming .viewrow{display:none}") || !strings.Contains(html, ".card.renaming .editrow{display:flex}") {
		t.Fatalf("the CSS that actually swaps view for edit is missing:\n%s", html)
	}
}

// TestRowRenameSaveSendsTheSameActionAndPayload pins the requirement that
// saving from the row still sends the existing renameSave action with the
// same [folder, value] JSON payload the old Account settings screen sent, so
// the Go side reading it needs no change. Enter must save; the same function
// this test reads is what rowRenameKey calls on Enter.
func TestRowRenameSaveSendsTheSameActionAndPayload(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false, "")
	if !strings.Contains(html, `send('renameSave', JSON.stringify([card.dataset.folder, v]));`) {
		t.Fatalf("Save must send the same action and payload shape as before:\n%s", html)
	}
	if !strings.Contains(html, `if (e.key === 'Enter') { e.preventDefault(); rowRenameSave(input); }`) {
		t.Fatalf("Enter must save:\n%s", html)
	}
}

// TestRowRenameEscapeCancelsLocallyWithoutARoundTrip pins the other half of
// "Enter saves, Escape cancels": Escape inside the row's rename input must
// cancel immediately, in JS, rather than falling through to the panel's
// generic "focus is in some INPUT, back out to the list" Escape handling —
// stopPropagation is what keeps it from also reaching that generic branch.
func TestRowRenameEscapeCancelsLocallyWithoutARoundTrip(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false, "")
	if !strings.Contains(html, `else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); cancelRename(input); }`) {
		t.Fatalf("Escape must cancel locally and stop the event from bubbling further:\n%s", html)
	}
}

// TestRenamingRowIsNotASwitchTarget pins the requirement that while a row is
// being renamed, the card must not act as a switch target: the card's onclick
// guard reads the very .renaming class startRename/cancelRename toggle, so
// there is no separate flag to fall out of sync with it.
func TestRenamingRowIsNotASwitchTarget(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false, "")
	if !strings.Contains(html, `onclick="if(!this.classList.contains('renaming'))askSwitch(this.dataset.folder,this.dataset.name)"`) {
		t.Fatalf("the switch handler must be guarded by the renaming class:\n%s", html)
	}
}

// TestRowMenuRemoveUsesTheSameAskRemoveAsBefore pins change 3: Remove goes
// straight to the existing confirmation, unchanged. The menu item is wired to
// the same askRemove(this) call the old Account settings screen used, and
// carries the same data-* the confirmation (and the informational dialog for
// the account in use) reads: folder, name, conversation count, and whether
// Claude has it open.
func TestRowMenuRemoveUsesTheSameAskRemoveAsBefore(t *testing.T) {
	notCurrent := RenderList([]ProfileVM{
		{Folder: "Claude_Old", Name: "Old one", Convos: 34, SignedIn: true},
		{Folder: "Claude_Two", Name: "Two", SignedIn: true},
	}, false, "")
	if !strings.Contains(notCurrent, `<button type="button" role="menuitem" class="danger" data-folder="Claude_Old" data-name="Old one" data-convos="34" data-current="0" onclick="event.stopPropagation();askRemove(this)">Remove from list</button>`) {
		t.Fatalf("want the Remove menu item wired to askRemove with the folder, name, conversation count and current marker:\n%s", notCurrent)
	}

	current := RenderList([]ProfileVM{
		{Folder: "Claude_Live", Name: "Live", Convos: 12, Current: true, SignedIn: true},
		{Folder: "Claude_Two", Name: "Two", SignedIn: true},
	}, false, "")
	if !strings.Contains(current, `data-folder="Claude_Live" data-name="Live" data-convos="12" data-current="1"`) {
		t.Fatalf("the account Claude has open must carry data-current=\"1\" so askRemove opens the informational dialog:\n%s", current)
	}
}

// TestRowMenuHidesRemoveWhenItIsTheOnlyProfile pins the OnlyOne rule carried
// over from the deleted Account settings screen: removing the last account
// would leave an empty panel with no way back, so the menu holds Change name
// alone.
func TestRowMenuHidesRemoveWhenItIsTheOnlyProfile(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude", Name: "Claude", Convos: 5, SignedIn: true}}, false, "")
	// Not a bare "Remove" check: the shared shell script's askRemove/askConfirm
	// source carries the word "Remove" as a dialog label regardless of whether
	// any row offers the menu item, so that substring is present on every page
	// ever rendered and its absence could never be asserted on meaningfully.
	if strings.Contains(html, `role="menuitem" class="danger"`) {
		t.Fatalf("the only account listed must not offer a Remove menu item:\n%s", html)
	}
	if !strings.Contains(html, ">Change name<") {
		t.Fatal("Change name must still be offered even with Remove hidden")
	}
}

// TestRowMenuAndRenameUseDataAttributesNotInlineArgs guards the v0.9.1 bug
// class for the new markup specifically: html.EscapeString turns an
// apostrophe into &#39;, which the HTML parser decodes back to ' before the
// inline JS is parsed, so a folder or display name containing one must travel
// as data-* and be read back with dataset (or, for the rename input, the DOM
// value property) rather than be interpolated into an inline handler string.
func TestRowMenuAndRenameUseDataAttributesNotInlineArgs(t *testing.T) {
	html := RenderList([]ProfileVM{
		{Folder: "Claude_O'Brien", Name: "Pat O'Brien", Convos: 2, SignedIn: true},
		{Folder: "Claude_Two", Name: "Two", SignedIn: true},
	}, false, "")
	if strings.Contains(html, `askRemove('`) || strings.Contains(html, `startRename('`) || strings.Contains(html, `askSwitch('`) {
		t.Fatalf("no inline string args (v0.9.1 bug class):\n%s", html)
	}
	if !strings.Contains(html, `data-folder="Claude_O&#39;Brien" data-name="Pat O&#39;Brien"`) {
		t.Fatalf("the card must carry the apostrophe-bearing folder and name as data-*:\n%s", html)
	}
	if !strings.Contains(html, `value="Pat O&#39;Brien"`) {
		t.Fatalf("the rename input's value must still be set from the (escaped) name:\n%s", html)
	}
}

func TestRenderListShowsAStatusMessage(t *testing.T) {
	// A merge that could not be computed, a recovery that came too late, or a merge
	// that succeeded all end back on the list. Without a banner the list re-renders
	// unchanged and the click reads as having done nothing.
	profiles := []ProfileVM{{Folder: "Claude", Name: "Claude", Current: true, SignedIn: true}}
	if got := RenderList(profiles, true, "Merged."); !strings.Contains(got, `<div class="status">Merged.</div>`) {
		t.Fatalf("a status message must be shown on the list:\n%s", got)
	}
	if got := RenderList(profiles, true, ""); strings.Contains(got, `<div class="status">`) {
		t.Fatalf("no message, no banner:\n%s", got)
	}
}

func TestVersionShownFromVariableOnBothViews(t *testing.T) {
	want := "v" + core.Version // the "v" is a literal prefix; the number is never hardcoded
	list := RenderList([]ProfileVM{{Folder: "Claude", Name: "Claude", Current: true, SignedIn: true}}, false, "")
	if !strings.Contains(list, want) {
		t.Fatalf("account list must show %q sourced from core.Version", want)
	}
	settings := RenderSettings(SettingsVM{Version: core.Version})
	if !strings.Contains(settings, want) {
		t.Fatalf("settings must show %q in the same format as the list", want)
	}
}

func TestRenderRescanRecoverableGhostOffersRecovery(t *testing.T) {
	accounts := []core.ScannedAccount{{
		UUID: "bbbbbbbb-0000-4000-8000-000000000002", Complete: false,
		Recoverable: true, Convos: 94,
		Sources:     []core.GhostSource{{Folder: "Claude", Path: "/data/Claude", Convos: 94}},
		LastUpdated: time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC),
		Note:        core.RecoverableGhostNote,
	}}
	html := RenderRescan(accounts, nil)
	if !strings.Contains(html, "Signed out in Claude Desktop") {
		t.Fatalf("recoverable ghost needs its own heading:\n%s", html)
	}
	if strings.Contains(html, "Unrecognized account") {
		t.Fatal("a recoverable account is not unrecognised")
	}
	if !strings.Contains(html, `data-uuid="bbbbbbbb-0000-4000-8000-000000000002"`) {
		t.Fatalf("want a Recover action carrying the account:\n%s", html)
	}
	// Assert on the rendered note, not the class name: shell() emits .note-todo and
	// .note-bad in the <style> block of every page, so a bare class-name check is
	// true regardless of what was rendered, and its negation can never fail.
	if !strings.Contains(html, `<div class="note-todo">`+core.RecoverableGhostNote+`</div>`) {
		t.Fatalf("recoverable note uses the blue style, not the red one:\n%s", html)
	}
	if strings.Contains(html, `<div class="note-bad">`) {
		t.Fatal("red is reserved for dead ghosts")
	}
	if !strings.Contains(html, "94 chats") {
		t.Fatal("the conversation count is how the user recognises the account")
	}
}

func TestRenderRescanDeadGhostStaysReadOnly(t *testing.T) {
	accounts := []core.ScannedAccount{{
		UUID: "dead", Complete: false, Recoverable: false, Note: "Invalid account data",
	}}
	html := RenderRescan(accounts, nil)
	if !strings.Contains(html, "Unrecognized account") {
		t.Fatal("a dead ghost keeps its existing heading")
	}
	if strings.Contains(html, "showRecover") {
		t.Fatal("nothing to recover, so no Recover button")
	}
	if !strings.Contains(html, `<div class="note-bad">Invalid account data</div>`) {
		t.Fatal("dead ghost keeps the red note")
	}
}

func TestRenderRescanRecoverableGhostIsNotSelectable(t *testing.T) {
	accounts := []core.ScannedAccount{{
		UUID: "u", Complete: false, Recoverable: true, Convos: 1,
		Sources: []core.GhostSource{{Folder: "Claude", Path: "/data/Claude", Convos: 1}},
		Note:    core.RecoverableGhostNote,
	}}
	html := RenderRescan(accounts, nil)
	// It has no folder to manage yet, so it must not join the checkbox set that
	// Confirm submits.
	if strings.Contains(html, `class="card selectable`) {
		t.Fatalf("a ghost cannot be managed, only recovered:\n%s", html)
	}
}

func TestRenderRescanGhostStaysReadOnly(t *testing.T) {
	accounts := []core.ScannedAccount{{UUID: "zzz-yyy", Convos: 4, Note: "Invalid account data"}}
	html := RenderRescan(accounts, nil)

	if !strings.Contains(html, "Unrecognized account") {
		t.Fatal("ghost row missing")
	}
	if strings.Contains(html, `data-folder="`) {
		t.Fatal("a ghost has no folder to manage and must not be selectable")
	}
}

// TestNoActionClosesClaudeWithoutAsking is the guard for the rule, not for one
// screen: any click handler that would close the user's Claude must go through
// askConfirm first.
//
// Sync shipped without it. The button called send('sync', …) straight from the
// card, so a single click closed Claude Desktop with no warning at all, while the
// switch path two screens over had a confirmation the whole time. A warning one
// code path can skip is not a warning, so this test reads the rendered markup
// rather than trusting each call site.
func TestNoActionClosesClaudeWithoutAsking(t *testing.T) {
	pages := map[string]string{
		"list": RenderList([]ProfileVM{
			{Folder: "Claude", Name: "Work", SignedIn: true, Current: true},
			{Folder: "Claude_P", Name: "Personal", SignedIn: true},
		}, true, ""),
		"sync": RenderSync([]ProfileVM{
			{Folder: "Claude", Name: "Work", SignedIn: true, Convos: 94},
			{Folder: "Claude_P", Name: "Personal", SignedIn: true, Convos: 12},
		}, "", false),
	}
	// Reaching either action from an onclick without the dialog in between is the
	// defect. The strings are deliberately the ones an author would write by
	// habit.
	for _, bad := range []string{`onclick="send('sync'`, `onclick="send('switch'`, "syncDir("} {
		for page, html := range pages {
			if strings.Contains(html, bad) {
				t.Errorf("%s page reaches a Claude-closing action via %s without confirming", page, bad)
			}
		}
	}
}

func TestRenderSyncAsksBeforeClosingClaude(t *testing.T) {
	html := RenderSync([]ProfileVM{
		{Folder: "Claude", Name: "Work", SignedIn: true, Convos: 94},
		{Folder: "Claude_P", Name: "Personal", SignedIn: true, Convos: 12},
	}, "", false)

	if !strings.Contains(html, "askSync(this.dataset.from,this.dataset.to") {
		t.Fatalf("sync must route through the confirmation:\n%s", html)
	}
	// The dialog has to name the direction and the volume; "Sync?" on its own does
	// not tell the user what is about to happen to their conversations.
	if !strings.Contains(html, `data-from-name="Work" data-to-name="Personal" data-convos="94"`) {
		t.Fatalf("confirmation needs both names and the source count:\n%s", html)
	}
}

// TestConfirmDialogFocusesCancel: Enter on a dialog the user has not read must not
// close their Claude, so the safe button holds the focus — unless there is no
// Cancel to give it to (the informational dialog, see
// TestInformationalDialogFocusesOKNotAHiddenCancel), which is the one case
// where OK is the only, non-destructive button and focusing it is safe.
func TestConfirmDialogFocusesCancel(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false, "")
	if !strings.Contains(html, `: document.getElementById('mcsModalCancel')).focus();`) {
		t.Error("the confirmation must open with Cancel focused when Cancel is shown")
	}
	if strings.Contains(html, "getElementById('mcsModalOk').focus()") {
		t.Error("focusing Continue unconditionally makes Enter destructive on an unread dialog")
	}
}

// TestInformationalDialogFocusesOKNotAHiddenCancel pins the fix for a dialog
// that used to focus nothing at all: hiding Cancel with display:none and then
// unconditionally calling .focus() on it is a no-op, since a hidden element
// cannot take focus. That left focus on whatever was behind the overlay (the
// "Remove this account" button that opened it), so Tab walked the screen
// underneath while the dialog was up, and Enter re-fired that button and
// re-opened the same dialog. There is no JS runtime in this test suite, so the
// source of askConfirm's closing focus call is the only thing that can be
// asserted on, the same way TestConfirmDialogFocusesCancel reads it.
func TestInformationalDialogFocusesOKNotAHiddenCancel(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false, "")
	if !strings.Contains(html, `(action === '' ? ok : document.getElementById('mcsModalCancel')).focus();`) {
		t.Fatalf("askConfirm must focus OK when Cancel is hidden, and Cancel otherwise:\n%s", html)
	}
}

// TestConfirmDialogWarnsAboutUnsavedWork: the consequence the user cannot see for
// themselves is that Claude is mid-work. Both dialogs carry the same line.
func TestConfirmDialogWarnsAboutUnsavedWork(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false, "")
	if !strings.Contains(html, `<div class="warn">Anything unsaved in Claude is interrupted.</div>`) {
		t.Errorf("the dialog must say what closing Claude costs:\n%s", html)
	}
}

// TestRenderListAddCardOnlyWhereItLeadsSomewhere: the card starts the
// create-a-profile flow, which today exists only on the Windows Store build.
// Showing it elsewhere would be a button that does nothing.
func TestRenderListAddCardOnlyWhereItLeadsSomewhere(t *testing.T) {
	profiles := []ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}

	with := RenderList(profiles, true, "")
	if !strings.Contains(with, `onclick="send('newProfile','')"`) {
		t.Errorf("the add card must be offered where the flow exists:\n%s", with)
	}
	if !strings.Contains(with, "Add another account") {
		t.Error("the card needs its label")
	}

	without := RenderList(profiles, false, "")
	if strings.Contains(without, "Add another account") {
		t.Error("no add card where nothing is behind it")
	}
}

// TestRenderListAddCardShowsOnAnEmptyList: a user with nothing managed yet is
// exactly who needs it, so the empty state must not swallow the card.
func TestRenderListAddCardShowsOnAnEmptyList(t *testing.T) {
	html := RenderList(nil, true, "")
	if !strings.Contains(html, "Add another account") {
		t.Errorf("an empty list still offers the way to add one:\n%s", html)
	}
	if !strings.Contains(html, "Run Rescan to add some") {
		t.Error("the existing empty-state text must survive alongside it")
	}
}

func TestRenderListWarnsAboutDuplicates(t *testing.T) {
	html := RenderList([]ProfileVM{
		{Folder: "Claude", Name: "Claude", UUID: "same", SignedIn: true},
		{Folder: "Claude_Work", Name: "Work", UUID: "same", SignedIn: true},
		{Folder: "Claude_Solo", Name: "Solo", UUID: "solo", SignedIn: true},
	}, false, "")
	if !strings.Contains(html, "the same account") {
		t.Fatalf("want a duplicate warning:\n%s", html)
	}
	// Folder names go through data-* and are read back with dataset, never
	// interpolated into an inline JS string. That is the v0.9.1 bug class: a folder
	// containing an apostrophe becomes &#39; via html.EscapeString, which the HTML
	// parser decodes back to ' before the JS is parsed.
	if !strings.Contains(html, `data-dup-a="Claude" data-dup-b="Claude_Work"`) {
		t.Fatalf("warning must offer the merge for that group:\n%s", html)
	}
	// Assert on the markup, not the class name: shell() emits every class name in
	// its <style> block on every page, so strings.Contains("dup-pill") is true even
	// on a page with no pills at all.
	if got := strings.Count(html, dupPillMarkup); got != 2 {
		t.Fatalf("both duplicate cards must be marked, got %d:\n%s", got, html)
	}
}

func TestRenderListDuplicateWarningDisambiguatesEqualNames(t *testing.T) {
	// Two folders can carry the same display name. Naming both in the warning would
	// read "Claude and Claude are the same account"; fall back to the folders.
	html := RenderList([]ProfileVM{
		{Folder: "Claude", Name: "Claude", UUID: "same", SignedIn: true},
		{Folder: "Claude_Work", Name: "Claude", UUID: "same", SignedIn: true},
	}, false, "")
	if !strings.Contains(html, "Claude and Claude_Work are the same account") {
		t.Fatalf("equal names must be disambiguated by folder:\n%s", html)
	}
}

func TestRenderListNoWarningWhenAccountsAreUnique(t *testing.T) {
	html := RenderList([]ProfileVM{
		{Folder: "Claude", Name: "Claude", UUID: "a", SignedIn: true},
		{Folder: "Claude_Two", Name: "Two", UUID: "b", SignedIn: true},
	}, false, "")
	if strings.Contains(html, "the same account") {
		t.Fatal("no duplicates, no warning")
	}
	if strings.Contains(html, dupPillMarkup) {
		t.Fatal("no duplicates, no pills")
	}
}

func TestRenderListDuplicateWarningIgnoresProfilesWithNoAccount(t *testing.T) {
	// Two profiles awaiting sign-in both have an empty UUID. That is not two
	// profiles sharing an account.
	html := RenderList([]ProfileVM{
		{Folder: "Claude_A", Name: "A", UUID: "", SignedIn: false},
		{Folder: "Claude_B", Name: "B", UUID: "", SignedIn: false},
	}, false, "")
	if strings.Contains(html, "the same account") {
		t.Fatalf("empty UUIDs must not group:\n%s", html)
	}
}

func TestRenderListOneWarningForTheFirstGroupOnly(t *testing.T) {
	html := RenderList([]ProfileVM{
		{Folder: "Claude_A", Name: "A", UUID: "x", SignedIn: true},
		{Folder: "Claude_B", Name: "B", UUID: "x", SignedIn: true},
		{Folder: "Claude_C", Name: "C", UUID: "y", SignedIn: true},
		{Folder: "Claude_D", Name: "D", UUID: "y", SignedIn: true},
	}, false, "")
	if strings.Count(html, "the same account") != 1 {
		t.Fatalf("one group at a time, got %d warnings:\n%s", strings.Count(html, "the same account"), html)
	}
	if !strings.Contains(html, `data-dup-a="Claude_A" data-dup-b="Claude_B"`) {
		t.Fatal("the first group by folder order goes first")
	}
	// All four cards are still flagged, so the user can see the second pair is
	// coming.
	if got := strings.Count(html, dupPillMarkup); got != 4 {
		t.Fatalf("every duplicate card is marked, got %d", got)
	}
}

func TestRenderNewProfileAddVariant(t *testing.T) {
	html := RenderNewProfile(NewProfileVM{})
	if !strings.Contains(html, "Add another account") {
		t.Fatalf("title:\n%s", html)
	}
	if !strings.Contains(html, `value=""`) {
		t.Fatal("the add path starts with an empty name")
	}
	// The copy is "a <b>different account</b>", so the phrase survives the markup.
	// Wrapping only the word — "a <b>different</b> account" — makes this assertion
	// fail against its own implementation, which is how the first draft of this task
	// shipped a test that could not pass.
	if !strings.Contains(html, "different account") {
		t.Fatal("the add path must warn against signing in as an existing account")
	}
	if strings.Contains(html, "conversations come back") {
		t.Fatal("no recovery copy on the add path")
	}
}

func TestRenderNewProfileRecoverVariant(t *testing.T) {
	html := RenderNewProfile(NewProfileVM{
		RecoverUUID:   "bbbbbbbb-0000-4000-8000-000000000002",
		SuggestedName: "Recovered 2026-07-29",
		Convos:        94,
	})
	if !strings.Contains(html, "Recover this account") {
		t.Fatalf("title:\n%s", html)
	}
	if !strings.Contains(html, `value="Recovered 2026-07-29"`) {
		t.Fatal("the recovery path pre-fills the name")
	}
	if !strings.Contains(html, "bbbbbbbb") {
		t.Fatal("must say which account to sign in as")
	}
	if !strings.Contains(html, "94") {
		t.Fatal("the conversation count helps the user recognise the account")
	}
	if strings.Contains(html, "different account") {
		t.Fatal("the different-account warning belongs to the add path only")
	}
}

func TestRenderNewProfileShowsAnError(t *testing.T) {
	html := RenderNewProfile(NewProfileVM{Err: "use only letters, numbers, spaces, dashes and underscores"})
	if !strings.Contains(html, "use only letters") {
		t.Fatalf("a rejected name must say why:\n%s", html)
	}
}

func TestRenderNewProfilePassesContextThroughDataAttributes(t *testing.T) {
	html := RenderNewProfile(NewProfileVM{RecoverUUID: "u-1"})
	// The v0.9.1 bug class: values must never be interpolated into inline JS string
	// arguments.
	if !strings.Contains(html, `data-uuid="u-1"`) {
		t.Fatalf("context must travel as data attributes:\n%s", html)
	}
	if strings.Contains(html, "createProfileSave('") {
		t.Fatalf("no inline string args:\n%s", html)
	}
}

func TestRenderNewProfileEscapesTheSuggestedName(t *testing.T) {
	html := RenderNewProfile(NewProfileVM{SuggestedName: `a"><script>x</script>`})
	if strings.Contains(html, "<script>x</script>") {
		t.Fatalf("suggested name must be escaped:\n%s", html)
	}
}

func TestRenderMergePreselectsTheProfileInUse(t *testing.T) {
	a := MergeCandidateVM{Folder: "Claude", Name: "Claude", Convos: 99}
	b := MergeCandidateVM{Folder: "Claude_Work", Name: "Work", Convos: 42, Current: true}
	html := RenderMerge(a, b, core.MergePlan{Combined: 141}, "", false)

	// Keeping the one already in use means no re-sign-in, so it is the default.
	//
	// Assert on the class and the folder together. Comparing their positions
	// separately cannot fail: the class attribute precedes data-folder inside every
	// card, so the index of "selected" is below the index of either folder whichever
	// card carries it.
	if !strings.Contains(html, `class="card selectable selected" data-folder="Claude_Work"`) {
		t.Fatalf("the in-use profile must be the preselected one:\n%s", html)
	}
	if !strings.Contains(html, `class="card selectable" data-folder="Claude"`) {
		t.Fatalf("the other profile must not be preselected:\n%s", html)
	}
	if !strings.Contains(html, "Will be archived") {
		t.Fatal("the other card must say what happens to it")
	}
}

func TestRenderMergePreselectsTheFirstWhenNeitherIsInUse(t *testing.T) {
	// Claude is quit by the time a merge runs, so "in use" can be unknown. The
	// screen must never render with nothing chosen.
	html := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Claude", Convos: 1},
		MergeCandidateVM{Folder: "Claude_Work", Name: "Work", Convos: 1},
		core.MergePlan{Combined: 2}, "", false)
	if !strings.Contains(html, `class="card selectable selected" data-folder="Claude"`) {
		t.Fatalf("fall back to the first card:\n%s", html)
	}
}

func TestRenderMergeShowsThePlansCombinedTotalNotTheSum(t *testing.T) {
	// Both profiles hold 99 and 42 conversations, 20 of them the same records, so
	// the keeper ends up with 121 — not 141. The screen must show what the merge
	// computed, or it promises conversations that do not exist.
	html := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Claude", Convos: 99, Current: true},
		MergeCandidateVM{Folder: "Claude_Work", Name: "Work", Convos: 42},
		core.MergePlan{Combined: 121}, "", false)
	if !strings.Contains(html, "121") {
		t.Fatalf("want the plan's union total:\n%s", html)
	}
	if strings.Contains(html, "141") {
		t.Fatalf("the sum of both sides double-counts shared records:\n%s", html)
	}
	if !strings.Contains(html, "archived, not deleted") {
		t.Fatal("must say nothing is deleted")
	}
}

func TestRenderMergeDisclosesConflicts(t *testing.T) {
	html := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Claude", Convos: 99, Current: true},
		MergeCandidateVM{Folder: "Claude_Work", Name: "Work", Convos: 42},
		core.MergePlan{Combined: 121, Conflicts: 3}, "", false)
	if !strings.Contains(html, "3 conversations exist in both") {
		t.Fatalf("a conflict strands a version in the archive; say so first:\n%s", html)
	}
}

func TestRenderMergeDisclosesUnreadableFiles(t *testing.T) {
	// A file that could not be read is left where it is, so the keeper ends up with
	// fewer than promised. Say so rather than quietly delivering a smaller number.
	html := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Claude", Convos: 99, Current: true},
		MergeCandidateVM{Folder: "Claude_Work", Name: "Work", Convos: 42},
		core.MergePlan{Combined: 121, Unreadable: 2}, "", false)
	if !strings.Contains(html, "2 files couldn't be read") {
		t.Fatalf("unreadable files must be disclosed:\n%s", html)
	}

	none := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Claude", Convos: 99, Current: true},
		MergeCandidateVM{Folder: "Claude_Work", Name: "Work", Convos: 42},
		core.MergePlan{Combined: 121}, "", false)
	if strings.Contains(none, "couldn't be read") {
		t.Fatalf("no unreadable files, no note:\n%s", none)
	}
}

func TestRenderMergeSaysNothingAboutConflictsWhenThereAreNone(t *testing.T) {
	html := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Claude", Convos: 99, Current: true},
		MergeCandidateVM{Folder: "Claude_Work", Name: "Work", Convos: 42},
		core.MergePlan{Combined: 141}, "", false)
	if strings.Contains(html, "exist in both") {
		t.Fatalf("no conflicts, no warning:\n%s", html)
	}
}

func TestRenderMergeUsesDataAttributesNotInlineArgs(t *testing.T) {
	html := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Claude", Current: true},
		MergeCandidateVM{Folder: "Claude_Work", Name: "Work"},
		core.MergePlan{}, "", false)
	if strings.Contains(html, "mergeConfirm('") {
		t.Fatalf("no inline string args (v0.9.1 bug class):\n%s", html)
	}
	if !strings.Contains(html, "toggleMergePick(this)") {
		t.Fatalf("cards must switch the pick through a handler:\n%s", html)
	}
}

func TestRenderMergeBusyDisablesTheAction(t *testing.T) {
	a := MergeCandidateVM{Folder: "Claude", Name: "Claude", Current: true}
	b := MergeCandidateVM{Folder: "Claude_Work", Name: "Work"}

	busy := RenderMerge(a, b, core.MergePlan{Combined: 1}, "Merging…", true)
	if !strings.Contains(busy, "Merging…") {
		t.Fatal("status must be shown")
	}
	// Assert on the button, not the word: shell()'s CSS contains ".sbtn:disabled",
	// so strings.Contains(html, "disabled") is true on every page ever rendered and
	// this test would pass with busy=false.
	if !strings.Contains(busy, `<button class="btn btn-primary" disabled`) {
		t.Fatalf("a merge in flight must not be startable twice:\n%s", busy)
	}

	idle := RenderMerge(a, b, core.MergePlan{Combined: 1}, "", false)
	if strings.Contains(idle, `<button class="btn btn-primary" disabled`) {
		t.Fatalf("the button must be live when no merge is running:\n%s", idle)
	}
}

func TestRenderSettingsOffersTheArchiveFolder(t *testing.T) {
	html := RenderSettings(SettingsVM{Version: "0.11.0"})
	if !strings.Contains(html, "Open archive folder") {
		t.Fatalf("merged-away profiles have to be findable:\n%s", html)
	}
	if !strings.Contains(html, "send('openArchive'") {
		t.Fatalf("want the openArchive action:\n%s", html)
	}
}

// TestRenderDebugShowsWhatWillBePublished pins the three things the screen
// exists for: the report itself, a way to say what went wrong, and a statement
// of what was replaced with stand-ins. The notice is not decoration — it is the only place
// the user is told the report was masked at all.
func TestRenderDebugShowsWhatWillBePublished(t *testing.T) {
	h := RenderDebug(DebugVM{Report: "MCS 0.11.2\naccount-1", Comment: ""})

	for _, want := range []string{
		"MCS 0.11.2",
		"account-1",
		// Assert on the element, not the word: shell()'s askReport literal
		// contains "already replaced with stand-ins" and "Copy and open", so
		// asserting on that wording, or on "Copy", would be true on every page
		// ever rendered (shell() is shared by all views) and these two
		// assertions would still pass with the .dbgnote block or the Copy
		// button deleted outright.
		`class="dbgnote"`,
		`send('showSettings', document.getElementById('dbgc').value)`,
		`id="dbgc"`,
		"Report a problem",
		`send('copyDebug'`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q from the debug view", want)
		}
	}
}

// TestRenderDebugEscapesTheReportAndTheComment stops a log line containing
// markup from rewriting the panel it is displayed in.
func TestRenderDebugEscapesTheReportAndTheComment(t *testing.T) {
	h := RenderDebug(DebugVM{
		Report:  `<script>alert(1)</script>`,
		Comment: `</textarea><img src=x onerror=alert(1)>`,
	})
	if strings.Contains(h, "<script>alert(1)</script>") {
		t.Error("the report was not escaped")
	}
	if strings.Contains(h, "</textarea><img") {
		t.Error("the comment was not escaped")
	}
	if !strings.Contains(h, "&lt;script&gt;") {
		t.Error("the report should still be readable once escaped")
	}
}

// TestRenderDebugExplainsTheUnregisteredMarker: a user who sees
// "[redacted: unregistered]" in their own report needs to know it means MCS
// found something it did not recognise and blocked it, not that MCS is
// hiding something else from them.
func TestRenderDebugExplainsTheUnregisteredMarker(t *testing.T) {
	h := RenderDebug(DebugVM{Report: "[redacted: unregistered]"})
	if !strings.Contains(h, "[redacted: unregistered]") {
		t.Fatalf("fixture broken: report marker missing:\n%s", h)
	}
	if !strings.Contains(h, "marks something that looked like an address or an ID and was blocked") {
		t.Errorf("the notice must explain what the marker means:\n%s", h)
	}
}

// TestRenderDebugBackButtonKeepsTheComment pins the fix for the data loss the
// back button shared with Esc (see the keydown handler in shell()): both used
// to send showSettings with no argument, discarding whatever the user had
// typed since it was never sent to Go until Copy or Report a problem.
//
// This only proves the back button's onclick reads the live textarea at
// click time — it says nothing about whether a comment handed to Go that way
// actually survives a return trip to this view. That half is
// TestRenderDebugPutsTheSavedCommentInTheTextarea below: together they cover
// both ends of the round trip that showSettings/showDebug do between them.
func TestRenderDebugBackButtonKeepsTheComment(t *testing.T) {
	h := RenderDebug(DebugVM{Report: "MCS 0.11.2"})
	if !strings.Contains(h, `send('showSettings', document.getElementById('dbgc').value)`) {
		t.Errorf("the back button must carry the comment back to Go:\n%s", h)
	}
}

// TestRenderDebugPutsTheSavedCommentInTheTextarea proves the other half of
// the round trip: a Comment saved by a previous Debug visit (back button or
// Esc, both routed through showSettings, saved by the host, and no longer
// cleared when showDebug runs again) must come back out inside the textarea,
// not just travel through JS untouched. Without this, a renderer that always
// emitted an empty textarea would still pass every other Debug test — the
// bug this pins was exactly that omission.
func TestRenderDebugPutsTheSavedCommentInTheTextarea(t *testing.T) {
	h := RenderDebug(DebugVM{Report: "MCS 0.11.2", Comment: "still happening after the update"})
	if !strings.Contains(h, `id="dbgc"`) {
		t.Fatalf("fixture broken: no textarea with id=dbgc:\n%s", h)
	}
	if !strings.Contains(h, `>still happening after the update</textarea>`) {
		t.Errorf("a saved comment must be rendered inside the textarea:\n%s", h)
	}
}

// TestRenderSettingsOffersDebugInfo keeps the screen reachable. A view nothing
// links to is a view nobody uses.
func TestRenderSettingsOffersDebugInfo(t *testing.T) {
	h := RenderSettings(SettingsVM{Version: "0.11.2"})
	if !strings.Contains(h, `send('showDebug','')`) {
		t.Error("Settings must offer Debug info")
	}
}

// TestAskRemoveWordsTheConversationCountNaturally pins all three branches of
// askRemove's conversation-count wording at once, because there is no JS
// runtime in this test suite to execute the ternary and observe its output —
// the literal source line is the only thing that can be asserted on. Zero
// gets its own sentence ("Nothing is deleted") rather than fall through to
// the plural branch and read "Its 0 conversations": a freshly created,
// never-signed-in profile is the single most likely account to be removed,
// so zero is not a rare edge case here. The confirmation is one sentence with
// no separate warning line (see TestRemoveConfirmationHasNoWarningBlock).
//
// askRemove is shared JS in shell(), identical on every page, so any page
// carrying it proves the same thing; RenderList is what actually reaches it
// now that the row menu replaced the deleted Account settings screen.
func TestAskRemoveWordsTheConversationCountNaturally(t *testing.T) {
	// Two profiles, not one: with a single account listed the Remove menu item is
	// not rendered at all, so a one-profile fixture asserts only on shared shell
	// script and would pass unchanged with Remove deleted from every row.
	h := RenderList([]ProfileVM{
		{Folder: "Claude", Name: "Some name", Convos: 1, SignedIn: true},
		{Folder: "Claude_Two", Name: "Two", SignedIn: true},
	}, false, "")
	if !strings.Contains(h, `onclick="event.stopPropagation();askRemove(this)"`) {
		t.Fatalf("precondition: this fixture must actually render a Remove menu item:\n%s", h)
	}
	for _, want := range []string{
		`n === 0 ? 'It comes off your list. Nothing is deleted, and you can sign in to it again any time.'`,
		`n === 1 ? 'It comes off your list. Its 1 conversation is kept, not deleted. Signing in to this account again starts a new copy of it, without that conversation.'`,
		`'It comes off your list. Its ' + n + ' conversations are kept, not deleted. Signing in to this account again starts a new copy of it, without those conversations.'`,
	} {
		if !strings.Contains(h, want) {
			t.Fatalf("askRemove must phrase zero, one, and many conversations naturally, missing %q:\n%s", want, h)
		}
	}
}

// TestAskRemoveDoesNotPromiseConversationsComeBack pins finding 4 of the
// review: the previous copy ("Its N conversations are not deleted, and you
// can add it back by signing in to it again") joined the kept-not-deleted
// fact and the sign-in-again fact into one sentence, which reads as a promise
// that signing in again restores the old conversations. It does not: signing
// in again starts a new profile, and the archived conversations do not come
// back into it on their own. Both facts must still be present, and true, but
// must not be joined so one implies the other.
func TestAskRemoveDoesNotPromiseConversationsComeBack(t *testing.T) {
	h := RenderList([]ProfileVM{{Folder: "Claude", Name: "Some name", Convos: 34, SignedIn: true}}, false, "")
	fn := jsFunctions(h)["askRemove"]
	if strings.Contains(fn, "not deleted, and you can add it back") {
		t.Fatalf("the kept fact and the sign-in-again fact must not be joined into one implying sentence:\n%s", fn)
	}
	if !strings.Contains(fn, "kept, not deleted") || !strings.Contains(fn, "starts a new copy of it, without those conversations") {
		t.Fatalf("both facts must still be stated, honestly: kept, and a fresh sign-in does not bring them along:\n%s", fn)
	}
}

// TestRemoveConfirmationHasNoWarningBlock pins the collapse from three
// implementation-vocabulary statements (title, body, warning) down to a title
// and a body: askRemove now passes an empty warn string, which askConfirm must
// treat as "hide the block" rather than falling back to the shared "Anything
// unsaved…" line — plain `warn || default` cannot tell an omitted argument
// from an explicitly empty one, since both are falsy, so askConfirm has to
// check arguments.length instead.
func TestRemoveConfirmationHasNoWarningBlock(t *testing.T) {
	// Two profiles: see TestAskRemoveWordsTheConversationCountNaturally.
	h := RenderList([]ProfileVM{
		{Folder: "Claude", Name: "Some name", Convos: 3, SignedIn: true},
		{Folder: "Claude_Two", Name: "Two", SignedIn: true},
	}, false, "")
	if !strings.Contains(h, `onclick="event.stopPropagation();askRemove(this)"`) {
		t.Fatalf("precondition: this fixture must actually render a Remove menu item:\n%s", h)
	}
	fn := jsFunctions(h)["askRemove"]
	if !strings.Contains(fn, `what, 'Remove', '', 'destructive'`) {
		t.Fatalf("askRemove must pass an empty warn string to askConfirm:\n%s", fn)
	}
	if !strings.Contains(h, `warnEl.style.display = warnText ? '' : 'none';`) {
		t.Fatalf("askConfirm must hide the warning block when warn is empty:\n%s", h)
	}
	if !strings.Contains(h, `arguments.length >= 6 ? warn : 'Anything unsaved in Claude is interrupted.'`) {
		t.Fatalf("askConfirm must distinguish an omitted warn from an explicitly empty one:\n%s", h)
	}
}

// TestAskRemoveOpensInformationalDialogForTheAccountInUse pins the redesign of
// item 2: pressing the (now always-live) remove button while Claude has the
// account open must not reach the destructive confirmation at all. It has to
// branch to an informational dialog instead, with the exact copy the design
// specifies and no second modal (askConfirm is reused with an empty action).
func TestAskRemoveOpensInformationalDialogForTheAccountInUse(t *testing.T) {
	// Two profiles: see TestAskRemoveWordsTheConversationCountNaturally.
	h := RenderList([]ProfileVM{
		{Folder: "Claude_Live", Name: "Live", Convos: 12, Current: true, SignedIn: true},
		{Folder: "Claude_Two", Name: "Two", SignedIn: true},
	}, false, "")
	if !strings.Contains(h, `data-current="1"`) {
		t.Fatalf("precondition: this fixture must render the current account's Remove item:\n%s", h)
	}
	fn := jsFunctions(h)["askRemove"]
	if !strings.Contains(fn, `el.dataset.current === '1'`) {
		t.Fatalf("askRemove must branch on the current-account marker:\n%s", fn)
	}
	if !strings.Contains(fn, `askConfirm('', '', 'Claude is open on '+el.dataset.name,`) {
		t.Fatalf("the informational dialog must use action='' and the specified title:\n%s", fn)
	}
	if !strings.Contains(fn, `'Switch to another account first, then you can remove it.', 'Got it', '');`) {
		t.Fatalf("the informational dialog must have the specified body and a single 'Got it' button:\n%s", fn)
	}
}

// TestInformationalDialogHasNoCancelAndDoesNotSend pins the two mechanics an
// action, an informational dialog, needs: askConfirm hides Cancel rather than leaving a
// button with nothing to cancel, and okConfirm, on an informational dialog,
// closes without calling send at all — there is nothing to confirm.
func TestInformationalDialogHasNoCancelAndDoesNotSend(t *testing.T) {
	h := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false, "")
	if !strings.Contains(h, `document.getElementById('mcsModalCancel').style.display = action === '' ? 'none' : '';`) {
		t.Fatalf("askConfirm must hide Cancel for an informational dialog:\n%s", h)
	}
	if !strings.Contains(h, `function okConfirm(){ var p=_pending; closeConfirm(); if(p && p.a) send(p.a, p.arg); }`) {
		t.Fatalf("okConfirm must not send when there is no action:\n%s", h)
	}
}

// TestRemovalIsTheOnlyDestructiveConfirmation pins the two halves of the
// destructive styling, and the boundary that makes it mean anything.
//
// There is no JS runtime here, so the source of the shared askConfirm and its
// callers is the only thing that can be asserted on, the same way
// TestAskRemoveWordsTheConversationCountNaturally reads its ternary.
//
// The boundary is the point: askConfirm is shared with switching, syncing and
// reporting a problem. Those close Claude or publish something, which is worth
// a dialog but is not destructive, and painting their confirm button red too
// would turn red into the colour of "confirm".
func TestRemovalIsTheOnlyDestructiveConfirmation(t *testing.T) {
	// Two profiles: see TestAskRemoveWordsTheConversationCountNaturally.
	h := RenderList([]ProfileVM{
		{Folder: "Claude_Old", Name: "Old one", Convos: 3, SignedIn: true},
		{Folder: "Claude_Two", Name: "Two", SignedIn: true},
	}, false, "")
	if !strings.Contains(h, `onclick="event.stopPropagation();askRemove(this)"`) {
		t.Fatalf("precondition: this fixture must actually render a Remove menu item:\n%s", h)
	}

	// Filled red, per the design: a background, not merely red lettering.
	if !strings.Contains(h, ".btn-danger{background:linear-gradient(135deg,#d5566d,#c0392b);color:#fff") {
		t.Fatalf("the destructive confirm button is not filled red:\n%s", h)
	}
	if !strings.Contains(h, `ok.classList.toggle('btn-danger', kind==='destructive')`) ||
		!strings.Contains(h, `ok.classList.toggle('btn-primary', kind!=='destructive')`) {
		t.Fatal("askConfirm does not paint the confirm button from its kind, both ways round")
	}

	// The account screen's own button: red text inside a red border.
	if !strings.Contains(h, ".sbtn.danger{color:#b0455f;border:1.5px solid #e0a3b1}") {
		t.Fatalf("the remove button on the account screen has no red border:\n%s", h)
	}

	// Only askRemove asks for it.
	for name, fn := range jsFunctions(h) {
		wantDestructive := name == "askRemove"
		if got := strings.Contains(fn, "'destructive'"); got != wantDestructive {
			t.Errorf("%s: destructive=%v, want %v\n%s", name, got, wantDestructive, fn)
		}
	}
}

// jsFunctions returns the source of each ask* helper in the rendered shell,
// keyed by name, so a test can assert on one dialog's arguments without
// matching another's.
func jsFunctions(h string) map[string]string {
	out := map[string]string{}
	for _, name := range []string{"askRemove", "askSwitch", "askSync", "askReport"} {
		start := strings.Index(h, "function "+name+"(")
		if start < 0 {
			continue
		}
		// Up to the next function declaration, which is enough: these helpers are
		// declared one after another and each is a single askConfirm call.
		rest := h[start+1:]
		end := strings.Index(rest, "\n  function ")
		if end < 0 {
			end = len(rest)
		}
		out[name] = rest[:end]
	}
	return out
}

// TestRenderRemovedFallsBackToAPlainSuccessScreenRatherThanCrashing pins the
// deliberate choice behind finding "Critical" of the round-2 review: this
// function must NOT panic for a RemovedVM with neither Err nor RegistryNote
// set, even though no host is supposed to build one that way any more
// (DecideRemovalOutcome, which is what actually enforces the routing, is
// tested separately). RenderRemoved is called straight from each host's
// reloadPanel on goroutines and webview callbacks neither host recovers
// around, so a panic here would not surface a bug to a developer — it would
// take down the user's whole panel, mid-removal, right after telling them
// their account was being removed. A harmless, truthful "<name> removed"
// screen is the correct fallback for a host that regressed the routing, not
// a crash.
func TestRenderRemovedFallsBackToAPlainSuccessScreenRatherThanCrashing(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RenderRemoved must not panic for a RemovedVM with neither Err nor RegistryNote set, got panic: %v", r)
		}
	}()
	h := RenderRemoved(RemovedVM{Name: "Old one", Convos: 34})
	if !strings.Contains(h, "Old one removed") {
		t.Fatalf("the fallback must still be a truthful, readable screen, not empty output:\n%s", h)
	}
	if strings.Contains(h, `class="hintw"`) {
		t.Fatalf("no registry complaint was set, so no hintw block should render:\n%s", h)
	}
}

// TestRenderRemovedSaysItIsArchivedNotDeleted pins the two things this screen
// exists to say, and nothing more. It used to also print the archived
// folder's generated name and carry a button into the archive; both were dropped
// as clutter for a fact almost nobody acts on, and Settings still has a way in.
// Asserting on their ABSENCE is what stops them creeping back.
//
// A RegistryNote is set here because a clean removal with nothing left behind
// no longer reaches this screen in normal operation (DecideRemovalOutcome
// routes it to the list instead; see
// TestRenderRemovedFallsBackToAPlainSuccessScreenRatherThanCrashing for what
// RenderRemoved itself does if that routing is ever bypassed), so the only
// way left to reach the "<name> removed" wording this test pins, other than
// that fallback, is the partial-failure branch.
func TestRenderRemovedSaysItIsArchivedNotDeleted(t *testing.T) {
	h := RenderRemoved(RemovedVM{Name: "Old one", Convos: 34, RegistryNote: "its name is still recorded"})
	if !strings.Contains(h, "Old one removed") {
		t.Fatal("the screen does not say what happened")
	}
	if !strings.Contains(h, "in your archive, not deleted") {
		t.Fatalf("the screen does not say the folder is kept:\n%s", h)
	}
	if strings.Contains(h, "openArchive") {
		t.Fatal("the open-archive button is back on the result screen")
	}
	if strings.Contains(h, "folder called") {
		t.Fatal("the archived folder's generated name is back on the result screen")
	}
}

// TestRenderRemovedShowsARegistryComplaintOnSuccess pins the partial-failure
// case: the folder moved (Err is empty, so this is the success variant), but a
// registry write afterward failed. That complaint has to land somewhere the user
// can actually read it. The status line does not survive this screen's only exit,
// showList, which clears it before rendering, so it has to be on the VM this
// screen draws from.
func TestRenderRemovedShowsARegistryComplaintOnSuccess(t *testing.T) {
	h := RenderRemoved(RemovedVM{Name: "Old one", Convos: 3,
		RegistryNote: "its display name is still recorded, and a later account reusing this identity would inherit it"})
	if !strings.Contains(h, "Old one removed") {
		t.Fatal("a partial failure must still read as a success: the folder did move")
	}
	if strings.Contains(h, "was not removed") {
		t.Fatal("a partial failure must not draw the failure screen: the folder did move")
	}
	if !strings.Contains(h, "its display name is still recorded, and a later account reusing this identity would inherit it") {
		t.Fatalf("the registry complaint is not shown anywhere on the success screen:\n%s", h)
	}
}

// TestRenderRemovedSplitsARegistryComplaintIntoLines pins the shape
// errors.Join really produces: entries separated by newlines. HTML collapses a
// newline to a space, so rendered into one div two separate things that could
// not be cleared read as a single run-on sentence, and the reader cannot tell
// where one ends.
func TestRenderRemovedSplitsARegistryComplaintIntoLines(t *testing.T) {
	h := RenderRemoved(RemovedVM{Name: "Old one", Convos: 3,
		RegistryNote: "Old one was removed, but some of it could not be cleared.\n" +
			"The switcher's own account list still mentions it.\n" +
			"Its name is still recorded as \"Old one\"."})
	if n := strings.Count(h, `class="noteline"`); n != 3 {
		t.Fatalf("want one line per joined entry, got %d:\n%s", n, h)
	}
	// A blank entry (a trailing newline, say) must not draw an empty line.
	h = RenderRemoved(RemovedVM{Name: "Old one",
		RegistryNote: "Only one thing went wrong.\n"})
	if n := strings.Count(h, `class="noteline"`); n != 1 {
		t.Fatalf("a trailing newline must not draw an empty line, got %d:\n%s", n, h)
	}
}

func TestRenderRemovedSaysNothingMovedOnFailure(t *testing.T) {
	h := RenderRemoved(RemovedVM{Folder: "Claude_Old", Name: "Old one",
		Err: "Claude may still be holding its files."})
	if !strings.Contains(h, "was not removed") {
		t.Fatal("a failure does not read as a failure")
	}
	if !strings.Contains(h, "still on your list") {
		t.Fatal("a failure does not say the account survived")
	}
	if !strings.Contains(h, "Claude may still be holding its files.") {
		t.Fatal("the underlying reason is not shown")
	}
}

// TestRenderRemovedWordsTheConversationCountNaturally pins all three branches of
// the success screen's conversation-count wording, mirroring
// TestAskRemoveWordsTheConversationCountNaturally: zero, one, and many each read
// differently rather than falling through to a plural that would say "0
// conversations" or "1 conversations".
// A RegistryNote is set on all three so each call reaches the branch that is
// still live: a clean removal with nothing left behind (Convos alone, no
// note) no longer reaches this screen at all (item 4).
func TestRenderRemovedWordsTheConversationCountNaturally(t *testing.T) {
	zero := RenderRemoved(RemovedVM{Name: "Fresh", RegistryNote: "left something behind"})
	if !strings.Contains(zero, "Its folder is in your archive, not deleted.") {
		t.Fatalf("zero conversations must not read as a count:\n%s", zero)
	}
	one := RenderRemoved(RemovedVM{Name: "Solo", Convos: 1, RegistryNote: "left something behind"})
	if !strings.Contains(one, "Its 1 conversation is in your archive, not deleted.") {
		t.Fatalf("one conversation must not be pluralized:\n%s", one)
	}
	many := RenderRemoved(RemovedVM{Name: "Busy", Convos: 5, RegistryNote: "left something behind"})
	if !strings.Contains(many, "Its 5 conversations are in your archive, not deleted.") {
		t.Fatalf("many conversations must be pluralized:\n%s", many)
	}
}

// TestRenderRemovedTryAgainUsesDataAttributesNotInlineArgs guards the v0.9.1
// bug class: the folder must travel as data-* and be read back with dataset,
// never interpolated straight into the onclick string, since
// html.EscapeString turns an apostrophe into &#39; which the HTML parser
// decodes back to ' before the inline JS is parsed.
func TestRenderRemovedTryAgainUsesDataAttributesNotInlineArgs(t *testing.T) {
	h := RenderRemoved(RemovedVM{Folder: "Claude_O'Brien", Name: "O'Brien", Err: "disk full"})
	if strings.Contains(h, `removeProfile','Claude`) {
		t.Fatalf("no inline string args (v0.9.1 bug class):\n%s", h)
	}
	if !strings.Contains(h, `onclick="send('removeProfile',this.dataset.folder)"`) {
		t.Fatalf("Try again must read the folder back from dataset:\n%s", h)
	}
}

// TestRenderRemovedShowsProgressOnRetry pins the Try again feedback. The retry
// re-runs a removal that can take seconds behind a rename retry loop, and with
// nowhere to draw the host's status the screen redrew identically and the button
// read as dead.
func TestRenderRemovedShowsProgressOnRetry(t *testing.T) {
	h := RenderRemoved(RemovedVM{Folder: "Claude_Old", Name: "Old one",
		Err: "Claude may still be holding its files.", Status: "Removing…"})
	if !strings.Contains(h, `<div class="status">Removing…</div>`) {
		t.Fatalf("the failure screen must be able to show the host's progress line:\n%s", h)
	}
	quiet := RenderRemoved(RemovedVM{Folder: "Claude_Old", Name: "Old one",
		Err: "Claude may still be holding its files."})
	if strings.Contains(quiet, `<div class="status">`) {
		t.Fatalf("no status, no banner:\n%s", quiet)
	}
}
