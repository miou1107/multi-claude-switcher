package panelui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/miou1107/multi-claude-switcher/core"
)

// emDashViolations runs the guard's two passes over one rendered page and
// returns every violation found:
//
//  1. Everything OUTSIDE <script>...</script>, tag-stripped and read as plain
//     text. The tag pattern requires the character right after < to be a
//     letter, /, or ! (a real opening tag, closing tag, or <!doctype>), not
//     the old, unconditional <[^>]*>: that pattern cannot tell a real tag's <
//     from a stray one, and JS has them — e.g. the comparison i<cards.length
//     in the shell's own toggleMergePick. A single such < with no nearby >
//     made the old regex swallow everything up to the next > found ANYWHERE
//     later in the string, including one sitting inside a later string
//     literal. On the Account (Current) screen that swallowed 2156
//     characters in one match, hiding several em dashes already present in
//     the shell's JS comments from every version of this guard that ran the
//     old pattern (measured directly; see the accompanying report for both
//     numbers). Carving the whole <script> block out before this pass runs
//     removes the stray-< problem at its root, rather than trying to out-
//     tighten a pattern that is fighting JS syntax it was never meant to
//     parse.
//
//  2. Single-quoted string literals found INSIDE that carved-out <script>
//     body, comments stripped first. A JS comment is not shown to a user and
//     must not be flagged (that is what forced code-comment rewording in an
//     earlier round of this work, for no user-facing benefit) — but the
//     literal strings askConfirm and its callers pass as title/body/label ARE
//     exactly what a user reads in a dialog, so they are pulled out of the
//     script and checked deliberately, rather than left to whatever the
//     tag-stripping pass happened to leave behind by accident. Comments have
//     to go first: this file's own JS comments are full of plain-English
//     apostrophes ("doesn't", "wasn't"), and a naive single-quote scan reads
//     those as string delimiters too, which misaligns every quote pairing
//     after the first one and hides real dialog literals behind it — the same
//     shape of bug as the tag-stripping fix above, one level down.
func emDashViolations(html string) []string {
	scriptBlock := regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`)
	blockComment := regexp.MustCompile(`(?s)/\*.*?\*/`)
	lineComment := regexp.MustCompile(`//[^\n]*`)
	quotedLiteral := regexp.MustCompile(`'([^'\\]*)'`)
	tags := regexp.MustCompile(`</?[a-zA-Z!][^>]*>`)

	var out []string
	script := scriptBlock.FindStringSubmatch(html)
	withoutScript := scriptBlock.ReplaceAllString(html, " ")
	text := tags.ReplaceAllString(withoutScript, " ")
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "—") {
			out = append(out, "rendered text: "+strings.TrimSpace(line))
		}
	}
	if len(script) == 2 {
		code := blockComment.ReplaceAllString(script[1], "")
		code = lineComment.ReplaceAllString(code, "")
		for _, m := range quotedLiteral.FindAllStringSubmatch(code, -1) {
			if strings.Contains(m[1], "—") {
				out = append(out, "dialog copy: "+m[1])
			}
		}
	}
	return out
}

// TestEmDashGuardCatchesTextInsideTheOldBlindWindow proves the fix in
// emDashViolations rather than asserting it in prose: it reproduces the
// swallow bug's shape directly — a stray < with no matching > nearby,
// followed later by a > sitting inside a string literal — instead of relying
// on today's shell() happening to have one (that window moves with every
// edit to the shell, which is exactly how it went unnoticed before). Then it
// checks the fixed guard two ways: a JS comment inside that window is
// legitimately still invisible (comments are never shown to a user), but a
// dialog string literal in the very same window is not.
func TestEmDashGuardCatchesTextInsideTheOldBlindWindow(t *testing.T) {
	oldTagPattern := regexp.MustCompile(`(?s)<[^>]*>`)
	commentOnly := `<p>real text</p><script>if (i<len) { /* fine, a comment em dash — here */ } if (x >= 1) {} </script><p>more text</p>`
	withDialog := `<p>real text</p><script>if (i<len) { /* fine, a comment em dash — here */ } askConfirm('a','b','dialog em dash — here','body','OK'); if (x >= 1) {} </script><p>more text</p>`

	// Fixture check: confirm this shape really did defeat the old pattern,
	// so the rest of this test is proving something, not asserting a fact
	// about a bug that was never actually reproduced.
	if strings.Contains(oldTagPattern.ReplaceAllString(withDialog, " "), "—") {
		t.Fatal("fixture broken: expected the old pattern to swallow this shape (including both em dashes) entirely")
	}

	if v := emDashViolations(commentOnly); len(v) != 0 {
		t.Fatalf("a JS comment is not user-facing text; it must not be flagged: %v", v)
	}
	if v := emDashViolations(withDialog); len(v) == 0 {
		t.Fatal("an em dash inside a quoted dialog string must still be caught, even inside what used to be the blind window")
	}
}

// TestEmDashGuardCoversTheFourDialogHelpers confirms the guard's second pass
// (quoted literals pulled out of <script>) actually reaches the copy each of
// askSwitch, askSync, askReport and askRemove passes to askConfirm — the
// thing "pulling them out deliberately" is supposed to guarantee, rather than
// something asserted only in the doc comment above.
func TestEmDashGuardCoversTheFourDialogHelpers(t *testing.T) {
	h := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false, "")
	scriptBlock := regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`)
	script := scriptBlock.FindStringSubmatch(h)
	if len(script) != 2 {
		t.Fatal("fixture broken: no <script> block found")
	}
	// Comments stripped first, same as emDashViolations: this file's own JS
	// comments carry plain-English apostrophes that would otherwise misalign
	// every quote pairing that follows.
	code := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(script[1], "")
	code = regexp.MustCompile(`//[^\n]*`).ReplaceAllString(code, "")
	quoted := regexp.MustCompile(`'([^'\\]*)'`).FindAllString(code, -1)
	joined := strings.Join(quoted, " ")
	for name, want := range map[string]string{
		"askSwitch": "Claude closes and reopens signed in as",
		"askSync":   "then Claude reopens where you were",
		"askReport": "GitHub issues are public",
		"askRemove": "It comes off your list",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("%s's copy %q is not among the quoted literals the guard checks", name, want)
		}
	}
}

// TestNoEmDashInUserFacingText pins a project rule the review process kept
// missing because nothing checked it: user-facing English carries no em dash.
// It slipped in through a wording fix that was itself correcting a different
// inaccuracy, and only turned up when someone looked at the screen.
//
// emDashViolations does the actual checking (see its doc comment for how the
// two passes split rendered text from dialog copy). The report the caller
// passes in is data, not our copy.
//
// Every renderer the user can actually land on belongs in this map — a
// renderer missing here is a renderer this guard does not cover at all, not
// merely one this particular fixture skips.
//
// A renderer being in the map is not the same as this guard having seen
// everything it can print: a fixture that only ever takes one branch of an
// if/switch inside the renderer hides whatever the other branches say, just
// as surely as leaving the renderer out of the map entirely. That is exactly
// how the "no date yet" cell on Rescan shipped a literal em dash for a whole
// review round: the fixture used here was built to have a LastUpdated on
// every account, so the branch that renders the placeholder never ran. Fixed
// by making the fixture take that branch instead of by keeping it that shape
// and hoping. Where one call cannot reach two mutually exclusive branches at
// once (e.g. Merge only ever draws two cards, and a candidate is either the
// keeper or not), a second call is concatenated into the same entry rather
// than adding a same-shaped fixture that would leave the same gap. Which
// branches each fixture reaches, and which it still cannot in one call, is
// recorded in this task's report rather than left for the next reader to
// rediscover.
func TestNoEmDashInUserFacingText(t *testing.T) {
	rescan := RenderRescan([]core.ScannedAccount{
		// Pending: no account yet, just a folder waiting for sign-in. Shares its
		// branch with SignedOut (same copy either way), so this one row stands
		// for both.
		{HomeFolder: "Claude_pending", Pending: true, Note: "Ready to sign in"},
		// Ghost, ranked recoverable: ID + conversations exist, no profile folder.
		{UUID: "u-recoverable", Recoverable: true, Convos: 2, Note: "Recoverable"},
		// Ghost, dead end: no folder, no bucket, nothing to recover.
		{UUID: "u-dead", Convos: 0, Note: "No conversations left"},
		// Complete, normal row, and the one that matters most here: no
		// LastUpdated at all. This is the exact shape that shipped the em dash
		// this test exists to catch, so the fixture must be this, not a
		// same-looking row with a date filled in.
		{UUID: "u-complete", Complete: true, HomeFolder: "Claude", Email: "work@example.com", Convos: 3, LastUpdated: time.Time{}},
	}, map[string]bool{})

	list := RenderList([]ProfileVM{
		// Current account: its own card layout and "Current account" sub-copy.
		{Folder: "Claude", Name: "Work", Plan: "Pro", Convos: 3, Current: true, SignedIn: true, UUID: "dup-uuid"},
		// Not signed in: the "Switch here, then sign in" sub-copy, distinct from
		// the plain "Switch to this account" of a ready one.
		{Folder: "Claude_new", Name: "New one", SignedIn: false},
		// Two profiles sharing a UUID: the duplicate-account warning banner.
		{Folder: "Claude_dup", Name: "Work", Plan: "Pro", Convos: 1, SignedIn: true, UUID: "dup-uuid"},
	}, true, "Backed up 3 accounts")
	// The empty list has its own copy ("No managed accounts yet…") that a
	// non-empty list can never reach in the same call.
	listEmpty := RenderList(nil, false, "")

	account := RenderAccount(AccountVM{Folder: "Claude_Old", Name: "Old one", Convos: 34})
	// Current drops the hint line and carries data-current instead, which a
	// non-Current fixture cannot show at the same time.
	accountCurrent := RenderAccount(AccountVM{Folder: "Claude_Live", Name: "Live", Convos: 12, Current: true})

	newProfile := RenderNewProfile(NewProfileVM{SuggestedName: "Work", Convos: 0, Err: "That name is already taken"})
	// RecoverUUID switches the title, subtitle and hint text to the recovery
	// variant; mutually exclusive with the add variant above in one call.
	newProfileRecover := RenderNewProfile(NewProfileVM{RecoverUUID: "abcd1234efgh", SuggestedName: "Old one", Convos: 5})

	merge := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Work", Plan: "Pro", Convos: 3, Current: true},
		MergeCandidateVM{Folder: "Claude_2", Name: "Work 2", Plan: "Pro", Convos: 5},
		core.MergePlan{Combined: 8, Conflicts: 1, Unreadable: 1}, "Could not compute a plan", false)
	// With neither candidate Current, the keeper reads "Keep this one" alone
	// instead of "In use now · keep this one" — a third variant the call above
	// cannot also produce, since a card is either the keeper or not.
	mergeNeitherCurrent := RenderMerge(
		MergeCandidateVM{Folder: "Claude_3", Name: "Third", Plan: "Free", Convos: 0},
		MergeCandidateVM{Folder: "Claude_4", Name: "Fourth", Plan: "Free", Convos: 0},
		core.MergePlan{Combined: 0}, "", true)

	sync := RenderSync([]ProfileVM{
		{Folder: "Claude", Name: "Work", Plan: "Pro", Convos: 3, SignedIn: true},
		{Folder: "Claude_2", Name: "Personal", Plan: "Free", Convos: 5, SignedIn: true},
		{Folder: "Claude_3", Name: "Waiting", SignedIn: false},
	}, "", false)
	// With fewer than two signed-in profiles, count stays 0 and the screen
	// shows its own "add at least two" copy instead of any direction card —
	// unreachable from the fixture above, which needs two signed-in profiles
	// to say anything else at all.
	syncEmpty := RenderSync([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, "", false)

	// RenderRemoved is called only when a removal did not go cleanly now (item 4
	// sends a clean removal straight back to the list with a banner instead), so
	// the two calls below are the only two branches left to cover — there is no
	// third, plain-success fixture: it would call the same code as
	// removedWithRegistryNote below, just with RegistryNote left empty, and would
	// not exercise anything this guard does not already see.
	//
	// The message is the one core.ArchiveProfile really returns, not a paraphrase.
	// A shortened stand-in was here before and it hid a live em dash for a whole
	// review round: this screen prints RemoveProfile's error verbatim, so a fixture
	// that trims the sentence trims exactly the part worth checking. core's own
	// TestNoEmDashInErrorStrings reads that package's source and is what keeps the
	// two from drifting apart; this stays real text so the rendered screen is
	// checked as the user meets it.
	removedFailed := RenderRemoved(RemovedVM{Folder: "Claude_Old", Name: "Old one",
		Err: `couldn't archive Old one: Claude may still be holding its files. ` +
			`Fully quit Claude and try again. (rename /Users/x/Library/Application Support/Claude_Old: permission denied)`})
	// A non-empty RegistryNote is the other live branch: the folder moved but a
	// registry write afterward did not, so this is the only call that reaches the
	// hintw block this test is here to cover.
	// Two entries, newline-separated, as errors.Join really hands them over: the
	// multi-line branch is the one that draws a line per complaint.
	removedWithRegistryNote := RenderRemoved(RemovedVM{Name: "Old one", Convos: 34,
		RegistryNote: "Old one was removed, but some of what the switcher had recorded about it could not be cleared.\n" +
			"The switcher's own account list still mentions it. Nothing needs doing: the panel only shows accounts whose folder is still there. (write managed.json: permission denied)\n" +
			"Its name is still recorded as \"Old one\". If you sign in to this account again later it will come back under that name, which you can change with Rename. (write names.json: permission denied)"})

	views := map[string]string{
		"debug":                 RenderDebug(DebugVM{Report: "MCS 0.11.2", Comment: "typed", Status: "Copied"}),
		"settings":              RenderSettings(SettingsVM{Version: "0.11.2", Status: "Backed up", AutoSync: true, StartLogin: true}),
		"list":                  list,
		"list_empty":            listEmpty,
		"rescan":                rescan,
		"account":               account,
		"account_current":       accountCurrent,
		"newprofile":            newProfile,
		"newprofile_recover":    newProfileRecover,
		"merge":                 merge,
		"merge_neither_current": mergeNeitherCurrent,
		"sync":                  sync,
		"sync_empty":            syncEmpty,
		"removed_failed":        removedFailed,
		"removed_registry_note": removedWithRegistryNote,
	}
	for name, h := range views {
		for _, v := range emDashViolations(h) {
			t.Errorf("%s: em dash in %s", name, v)
		}
	}
}
