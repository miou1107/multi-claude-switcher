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
	}, false)
	if !strings.Contains(html, "Not signed in yet. Switch here, then sign in.") {
		t.Fatal("the card must say why it is different, or switching to it lands on an unexplained login screen")
	}
	if !strings.Contains(html, `data-folder="ClaudeWork"`) {
		t.Fatal("it must still be switchable; that is how the user signs in")
	}
}

func TestVersionShownFromVariableOnBothViews(t *testing.T) {
	want := "v" + core.Version // the "v" is a literal prefix; the number is never hardcoded
	list := RenderList([]ProfileVM{{Folder: "Claude", Name: "Claude", Current: true, SignedIn: true}}, false)
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
		}, true),
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
// close their Claude, so the safe button holds the focus.
func TestConfirmDialogFocusesCancel(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false)
	if !strings.Contains(html, "getElementById('mcsModalCancel').focus()") {
		t.Error("the confirmation must open with Cancel focused")
	}
	if strings.Contains(html, "getElementById('mcsModalOk').focus()") {
		t.Error("focusing Continue makes Enter destructive on an unread dialog")
	}
}

// TestConfirmDialogWarnsAboutUnsavedWork: the consequence the user cannot see for
// themselves is that Claude is mid-work. Both dialogs carry the same line.
func TestConfirmDialogWarnsAboutUnsavedWork(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false)
	if !strings.Contains(html, `<div class="warn">Anything unsaved in Claude is interrupted.</div>`) {
		t.Errorf("the dialog must say what closing Claude costs:\n%s", html)
	}
}

// TestRenderListAddCardOnlyWhereItLeadsSomewhere: the card starts the
// create-a-profile flow, which today exists only on the Windows Store build.
// Showing it elsewhere would be a button that does nothing.
func TestRenderListAddCardOnlyWhereItLeadsSomewhere(t *testing.T) {
	profiles := []ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}

	with := RenderList(profiles, true)
	if !strings.Contains(with, `onclick="send('newProfile','')"`) {
		t.Errorf("the add card must be offered where the flow exists:\n%s", with)
	}
	if !strings.Contains(with, "Add another account") {
		t.Error("the card needs its label")
	}

	without := RenderList(profiles, false)
	if strings.Contains(without, "Add another account") {
		t.Error("no add card where nothing is behind it")
	}
}

// TestRenderListAddCardShowsOnAnEmptyList: a user with nothing managed yet is
// exactly who needs it, so the empty state must not swallow the card.
func TestRenderListAddCardShowsOnAnEmptyList(t *testing.T) {
	html := RenderList(nil, true)
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
	}, false)
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
	}, false)
	if !strings.Contains(html, "Claude and Claude_Work are the same account") {
		t.Fatalf("equal names must be disambiguated by folder:\n%s", html)
	}
}

func TestRenderListNoWarningWhenAccountsAreUnique(t *testing.T) {
	html := RenderList([]ProfileVM{
		{Folder: "Claude", Name: "Claude", UUID: "a", SignedIn: true},
		{Folder: "Claude_Two", Name: "Two", UUID: "b", SignedIn: true},
	}, false)
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
	}, false)
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
	}, false)
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
