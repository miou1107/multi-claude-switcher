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
//  2. Single-quoted string literals found INSIDE every one of those
//     carved-out <script> bodies, comments stripped first. A JS comment is
//     not shown to a user and must not be flagged (that is what forced
//     code-comment rewording in an earlier round of this work, for no
//     user-facing benefit) — but the literal strings askConfirm and its
//     callers pass as title/body/label ARE exactly what a user reads in a
//     dialog, so they are pulled out of the script and checked deliberately,
//     rather than left to whatever the tag-stripping pass happened to leave
//     behind by accident. Comments have to go first: this file's own JS
//     comments are full of plain-English apostrophes ("doesn't", "wasn't"),
//     and a naive single-quote scan reads those as string delimiters too,
//     which misaligns every quote pairing after the first one and hides
//     real dialog literals behind it — the same shape of bug as the
//     tag-stripping fix above, one level down.
//
//     "Every one" is load-bearing: RenderNewProfile draws a small inline
//     <script> of its own (autofocusing the new-name input) BEFORE shell()'s
//     big script — the one with askConfirm and the four dialog helpers —
//     gets appended to the body. An earlier version of this function used
//     FindStringSubmatch, which returns only the FIRST match, so on exactly
//     that view (plus "newprofile_recover", the other fixture built on the
//     same renderer) it read the tiny autofocus script and never looked at
//     the shell's at all — measured directly: an em dash injected into
//     askRemove's copy was caught on "list" and "settings" but silently
//     missed on the account screen this guard's history was written
//     against (since deleted; its Rename/Remove actions moved onto the
//     account list's own rows, which carry no extra <script> of their own),
//     because coverage of the shell's own dialog copy survived only because
//     other fixtures happened to share it. FindAllStringSubmatch fixes that
//     by checking every <script> block a page contains, not assuming there
//     is exactly one.
//
// Known blind spots, left in this comment rather than a report nobody
// reopens, because this guard's whole history is things that were "not live
// today" until they were:
//   - Double-quoted strings ("...") and template literals (`...`) are
//     invisible to quotedLiteral, which only matches '...'. Every dialog
//     string in shell() today is single-quoted, so nothing is live, but a
//     future double-quoted or templated literal would not be checked.
//   - lineComment ("//[^\n]*") does not know about string contents: a `//`
//     appearing inside a single-quoted literal (e.g. a URL) would eat the
//     rest of that line, including any later quoted literal on it, before
//     quotedLiteral ever sees it. Nothing in shell() today puts "//" inside a
//     string.
//   - quotedLiteral's negated class ([^'\\]*) does not resolve a JS escape:
//     a literal containing \' (an escaped apostrophe) ends the match at the
//     backslash instead of treating \' as one character, splitting the
//     literal in two. Nothing in shell() today writes \' — every apostrophe
//     that could appear in real data (a folder or display name) travels as
//     data-* and is read back with dataset instead of being interpolated
//     into a JS string at all, which is the whole point of that pattern (see
//     the v0.9.1 bug notes throughout render.go).
func emDashViolations(html string) []string {
	scriptBlock := regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`)
	blockComment := regexp.MustCompile(`(?s)/\*.*?\*/`)
	lineComment := regexp.MustCompile(`//[^\n]*`)
	quotedLiteral := regexp.MustCompile(`'([^'\\]*)'`)
	tags := regexp.MustCompile(`</?[a-zA-Z!][^>]*>`)

	var out []string
	withoutScript := scriptBlock.ReplaceAllString(html, " ")
	text := tags.ReplaceAllString(withoutScript, " ")
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "—") {
			out = append(out, "rendered text: "+strings.TrimSpace(line))
		}
	}
	for _, script := range scriptBlock.FindAllStringSubmatch(html, -1) {
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

// TestEmDashGuardCoversTheFourDialogHelpers confirms coverage by exercising
// emDashViolations itself, not a second, hand-rolled extraction that could
// silently diverge from it. An earlier version of this test re-implemented
// the same regex steps inline instead of calling emDashViolations, which
// meant it could keep passing even while the real function's bug (only the
// first <script> block on a page was ever read; see emDashViolations' doc
// comment) hid the very same copy on every OTHER view that carries more than
// one script block — a parallel implementation agreeing with itself proves
// nothing about the function it was supposed to be testing.
//
// For each of the four dialog helpers, a known, harmless piece of its real
// rendered copy is swapped for a version carrying an em dash, and
// emDashViolations — called on the resulting page, exactly as
// TestNoEmDashInUserFacingText calls it — must report it. askSwitch, askSync
// and askRemove are all checked on RenderList (which has only the shell's own
// script — askRemove moved there with the row menu that replaced the deleted
// Account settings screen); askReport on RenderDebug.
func TestEmDashGuardCoversTheFourDialogHelpers(t *testing.T) {
	list := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false, "", nil)
	debug := RenderDebug(DebugVM{Report: "MCS 0.11.2"})

	cases := []struct{ name, html, from, to string }{
		{"askSwitch", list, "Claude closes and reopens signed in as", "Claude — closes and reopens signed in as"},
		{"askSync", list, "then Claude reopens where you were", "then Claude — reopens where you were"},
		{"askReport", debug, "GitHub issues are public", "GitHub — issues are public"},
		{"askRemove", list, "It comes off your list", "It — comes off your list"},
	}
	for _, c := range cases {
		if !strings.Contains(c.html, c.from) {
			t.Fatalf("fixture broken: %s's known copy %q was not found on its own rendered page", c.name, c.from)
		}
		injected := strings.Replace(c.html, c.from, c.to, 1)
		if v := emDashViolations(injected); len(v) == 0 {
			t.Errorf("%s's copy is not reached by emDashViolations: an em dash injected into %q was not caught", c.name, c.from)
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
	}, true, "Backed up 3 accounts", nil)
	// The empty list has its own copy ("No managed accounts yet…") that a
	// non-empty list can never reach in the same call.
	listEmpty := RenderList(nil, false, "", nil)

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

	// The switch card's three phases are mutually exclusive in one call, so each
	// gets its own entry. The failed one carries a real SafeSwitch message
	// rather than a paraphrase, for the same reason removedFailed above does:
	// this screen prints the error verbatim, so a trimmed stand-in trims the
	// part worth checking. The done card is the only one that names the
	// account, and the working card the only one with no way out of it.
	switchWorking := RenderList(nil, false, "", &SwitchProgressVM{Phase: SwitchWorking, Target: "Work"})
	switchDone := RenderList(nil, false, "", &SwitchProgressVM{Phase: SwitchDone, Target: "Work"})
	switchFailed := RenderList(nil, false, "", &SwitchProgressVM{Phase: SwitchFailed, Target: "Work",
		Err: "can't switch to Claude_Old: no profile folder there"})
	// Failure with nothing to report takes the other branch: the card falls back
	// to its own sentence, which no other fixture here can reach.
	switchFailedNoMessage := RenderList(nil, false, "", &SwitchProgressVM{Phase: SwitchFailed})

	views := map[string]string{
		"debug":                 RenderDebug(DebugVM{Report: "MCS 0.11.2", Comment: "typed", Status: "Copied"}),
		"settings":              RenderSettings(SettingsVM{Version: "0.11.2", Status: "Backed up", AutoSync: true, StartLogin: true}),
		"list":                  list,
		"list_empty":            listEmpty,
		"rescan":                rescan,
		"newprofile":            newProfile,
		"newprofile_recover":    newProfileRecover,
		"merge":                 merge,
		"merge_neither_current": mergeNeitherCurrent,
		"sync":                  sync,
		"sync_empty":            syncEmpty,
		"removed_failed":        removedFailed,
		"removed_registry_note": removedWithRegistryNote,
		"switch_working":        switchWorking,
		"switch_done":           switchDone,
		"switch_failed":         switchFailed,
		"switch_failed_bare":    switchFailedNoMessage,
	}
	for name, h := range views {
		for _, v := range emDashViolations(h) {
			t.Errorf("%s: em dash in %s", name, v)
		}
	}
}
