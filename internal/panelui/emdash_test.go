package panelui

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
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
// The script pass used to be three regexes: strip block comments, strip line
// comments, then match '...' literals. That left three blind spots, all of the
// same kind — a regex cannot know whether the thing it matched was inside
// something else:
//
//   - Double-quoted and template literals were never checked, because the
//     literal pattern only matched '...'.
//   - The line-comment pattern did not know about string contents, so a `//`
//     inside a literal (a URL, say) ate the rest of that line, including any
//     later literal on it.
//   - The literal pattern's negated class did not resolve a JS escape, so a
//     literal containing \' ended at the backslash and split in two.
//
// None was live at the time, which is the trap: this guard's whole history is
// things that were not live until they were. jsStringLiterals below scans the
// script once as a lexer would instead, which closes all three by construction
// rather than by three more patterns to get right.
func emDashViolations(html string) []string {
	scriptBlock := regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`)
	tags := regexp.MustCompile(`</?[a-zA-Z!][^>]*>`)

	var out []string
	withoutScript := scriptBlock.ReplaceAllString(html, " ")
	text := tags.ReplaceAllString(withoutScript, " ")
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "—") {
			out = append(out, "rendered text: "+strings.TrimSpace(line))
		}
	}
	// Attribute copy, which the tag-stripping pass above deletes along with the
	// tag it sits in and which is not a JS literal either. A placeholder or a
	// screen-reader label is text a user reads, so it needs its own pass: the
	// Debug screen's comment box and the new-account name field both carry one
	// today.
	for _, m := range visibleAttr.FindAllStringSubmatch(withoutScript, -1) {
		if strings.Contains(m[2], "—") {
			out = append(out, "attribute copy ("+m[1]+"): "+m[2])
		}
	}
	for _, script := range scriptBlock.FindAllStringSubmatch(html, -1) {
		for _, lit := range jsStringLiterals(script[1]) {
			if strings.Contains(lit, "—") {
				out = append(out, "dialog copy: "+lit)
			}
		}
	}
	return out
}

// visibleAttr matches the attributes whose value a user reads, by name. A
// blanket "every attribute" would drag in onclick handlers, which are
// JavaScript and belong to the script pass, and class lists, which are not
// copy at all.
var visibleAttr = regexp.MustCompile(`(placeholder|title|aria-label|alt)="([^"]*)"`)

// jsStringLiterals returns the contents of every string literal in a piece of
// JavaScript: single-quoted, double-quoted and template. Comments are skipped,
// and a backslash escapes the next character wherever an escape is legal, so a
// quote or a `//` inside a literal does not end it.
//
// A single left-to-right scan, because the states are mutually exclusive: text
// inside a comment is not a literal, and a quote inside a literal is data. That
// is the property the three regexes could not express, and each blind spot they
// had was a case of one state being mistaken for another.
//
// It does not try to be a JavaScript parser, and the one shape it gets wrong
// fails in the dangerous direction, so it is worth naming precisely rather than
// waving at. A regular expression literal is read as division, so a regex
// containing an odd number of quote characters (/don't/) opens a literal that
// stays open until the next real quote in the file closes it — which means the
// literal after it is read as code and never scanned at all. That is
// under-reporting, not over-reporting. There are no regex literals in this
// package's JavaScript today; if one is added, this needs to learn about them.
//
// Unterminated literals are lost the same way, but an unterminated string is a
// syntax error the page would not survive, so it cannot reach a user.
//
// ${} interpolation inside a template literal is treated as ordinary literal
// text, which over-reports (the expression is scanned as copy) and is
// harmless.
func jsStringLiterals(src string) []string {
	const (
		code = iota
		lineComment
		blockComment
		quoted // inside a string literal; quote holds which kind
	)

	var out []string
	var lit strings.Builder
	var quote byte
	state := code

	r := []byte(src)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch state {
		case code:
			switch {
			case c == '/' && i+1 < len(r) && r[i+1] == '/':
				state, i = lineComment, i+1
			case c == '/' && i+1 < len(r) && r[i+1] == '*':
				state, i = blockComment, i+1
			case c == '\'' || c == '"' || c == '`':
				state, quote = quoted, c
				lit.Reset()
			}
		case lineComment:
			if c == '\n' {
				state = code
			}
		case blockComment:
			if c == '*' && i+1 < len(r) && r[i+1] == '/' {
				state, i = code, i+1
			}
		case quoted:
			switch {
			case c == '\\' && i+1 < len(r):
				// The escaped character is data, whatever it is. This is what
				// stops \' from ending the literal early.
				lit.WriteByte(r[i+1])
				i++
			case c == quote:
				out = append(out, lit.String())
				state = code
			default:
				lit.WriteByte(c)
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
	list := RenderList([]ProfileVM{{Folder: "Claude", Name: "Work", SignedIn: true}}, false, "")
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
	}, true, "Backed up 3 accounts")
	// The empty list has its own copy ("No managed accounts yet…") that a
	// non-empty list can never reach in the same call.
	listEmpty := RenderList(nil, false, "")

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

	// The card's four shapes are mutually exclusive in one call, so each gets
	// its own entry, and they are built through the outcome helpers the hosts
	// really use rather than by hand: a fixture that sets the fields directly
	// would still pass if those helpers started wording things differently.
	// The failed one carries a real SafeSwitch message rather than a paraphrase,
	// for the same reason removedFailed above does: this card prints the error
	// verbatim, so a trimmed stand-in trims the part worth checking.
	cardOver := func(vm *ProgressVM) string { return WithProgress(listEmpty, vm) }
	switchWorking := cardOver(SwitchStarting())
	switchDone := cardOver(SwitchOutcome("Work", nil))
	switchWarned := cardOver(SwitchOutcome("Work", &core.SwitchedWithWarning{
		Err: errors.New("skipped auto sync: failed to back up source profile " +
			"(refusing to write without a backup): permission denied")}))
	switchFailed := cardOver(SwitchOutcome("Work",
		errors.New("can't switch to Claude_Old: no profile folder there")))
	// Failure with nothing to report takes the other branch: the card falls back
	// to its own sentence, which no other fixture here can reach.
	progressFailedBare := cardOver(&ProgressVM{Phase: ProgressFailed, Title: "Merge failed"})
	// The other three operations' copy, which the switch fixtures never render.
	syncWorking := cardOver(SyncStarting())
	syncDone := cardOver(SyncOutcome("Work", &core.SyncReport{CopiedCount: 3, ConflictCount: 1,
		SkipErrors: []string{"a.json: unreadable"}}, nil))
	syncFailed := cardOver(SyncOutcome("Work", nil, core.ErrRunningProfileUnknown))
	mergeWorking := cardOver(MergeStarting())
	mergeDone := cardOver(MergeOutcome(nil))
	backupWorking := cardOver(BackupStarting())
	backupDone := cardOver(BackupOutcome(3, 0))
	// Zero accounts takes its own branch, with its own sentence, and a run in
	// which every backup failed takes a third that neither of the others reaches.
	backupNothing := cardOver(BackupOutcome(0, 0))
	backupFailed := cardOver(BackupOutcome(0, 2))
	backupPartial := cardOver(BackupOutcome(2, 1))

	views := map[string]string{
		"debug":                 RenderDebug(DebugVM{Report: "MCS 0.11.2", Comment: "typed", Status: "Copied"}),
		"settings":              RenderSettings(SettingsVM{Version: "0.11.2", Status: "Backed up", AutoSync: true, StartLogin: true}),
		"settings_busy":         RenderSettings(SettingsVM{Version: "0.11.2", Busy: true}),
		"more":                  RenderMore(MoreVM{}),
		"more_busy":             RenderMore(MoreVM{Busy: true}),
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
		"switch_warned":         switchWarned,
		"switch_failed":         switchFailed,
		"progress_failed_bare":  progressFailedBare,
		"sync_working":          syncWorking,
		"sync_done":             syncDone,
		"sync_failed":           syncFailed,
		"merge_working":         mergeWorking,
		"merge_done":            mergeDone,
		"backup_working":        backupWorking,
		"backup_done":           backupDone,
		"backup_nothing":        backupNothing,
		"backup_failed":         backupFailed,
		"backup_partial":        backupPartial,
	}
	for _, missing := range screensWithNoFixture(t, views) {
		t.Errorf("%s renders a screen with no fixture here, so its copy is unchecked", missing)
	}
	for name, h := range views {
		for _, v := range emDashViolations(h) {
			t.Errorf("%s: em dash in %s", name, v)
		}
	}
}

// TestEmDashGuardSeesTheThreeShapesItUsedToMiss pins each of the blind spots
// the old three-regex script pass had. Each case is written the way the guard
// would actually meet it: a real <script> block inside a page.
//
// These are not hypothetical. Every previous failure of this guard was also
// "not live today" right up until it was, and the fix that closed them is
// worth a test that fails if someone reaches for the regexes again.
func TestEmDashGuardSeesTheThreeShapesItUsedToMiss(t *testing.T) {
	for _, tc := range []struct {
		name   string
		script string
		want   string
	}{
		{
			name:   "double-quoted literal",
			script: `askConfirm("a","b","dialog em dash — here","body","OK");`,
			want:   "dialog em dash — here",
		},
		{
			name:   "template literal",
			script: "askConfirm(`dialog em dash — here`);",
			want:   "dialog em dash — here",
		},
		{
			name: "a // inside a literal must not eat the rest of the line",
			// The old line-comment pattern started a comment at the // in the
			// URL and swallowed everything after it, including the copy.
			script: `var u='https://example.invalid/x'; askConfirm('dialog em dash — here');`,
			want:   "dialog em dash — here",
		},
		{
			name: "an escaped apostrophe must not split the literal",
			// The old literal pattern ended its match at the backslash, so the
			// copy after it was read as being outside any string.
			script: `askConfirm('it\'s an em dash — here');`,
			want:   "it's an em dash — here",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := emDashViolations("<p>ok</p><script>" + tc.script + "</script>")
			if len(got) == 0 {
				t.Fatalf("no violation reported for %s: the guard cannot see this shape", tc.name)
			}
			found := false
			for _, g := range got {
				if strings.Contains(g, tc.want) {
					found = true
				}
			}
			if !found {
				t.Errorf("violations = %q, want one containing %q", got, tc.want)
			}
		})
	}
}

// Comments stay invisible. Nobody is shown a comment, and reporting them would
// make the guard noisy enough to be turned off, which is the failure mode that
// ends with real copy shipping.
func TestEmDashGuardStillIgnoresScriptComments(t *testing.T) {
	for _, tc := range []struct{ name, script string }{
		{"line comment", "// an em dash — in a comment\nvar x=1;"},
		{"block comment", "/* an em dash — in a comment */ var x=1;"},
		{"line comment after code", "var x=1; // an em dash — here"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := emDashViolations("<p>ok</p><script>" + tc.script + "</script>"); len(got) != 0 {
				t.Errorf("violations = %q, want none: comments are never shown to a user", got)
			}
		})
	}
}

// A quote inside a comment must not open a literal, and a comment marker
// inside a literal must not open a comment. The two states are mutually
// exclusive, and mistaking one for the other is what every old blind spot was.
func TestEmDashGuardKeepsCommentsAndLiteralsApart(t *testing.T) {
	// The apostrophe in "don't" would open a literal for a scanner that did not
	// know it was inside a comment; everything after it, including the real
	// copy, would then be read as string contents or skipped entirely.
	script := "// don't be fooled\naskConfirm('real em dash — here');"
	got := emDashViolations("<p>ok</p><script>" + script + "</script>")
	if len(got) != 1 || !strings.Contains(got[0], "real em dash — here") {
		t.Errorf("violations = %q, want exactly the dialog copy", got)
	}
}

// Attribute copy is read by users but is deleted by the tag-stripping pass
// along with the tag it lives in, and it is not a JS literal either, so it fell
// between the guard's two passes. Live examples today: the Debug screen's
// comment placeholder and the new-account name field's.
func TestEmDashGuardSeesAttributeCopy(t *testing.T) {
	for _, tc := range []struct{ name, html string }{
		{"placeholder", `<input placeholder="an em dash — here">`},
		{"title", `<span title="an em dash — here">x</span>`},
		{"aria-label", `<button aria-label="an em dash — here">x</button>`},
		{"alt", `<img alt="an em dash — here">`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := emDashViolations("<p>ok</p>" + tc.html); len(got) == 0 {
				t.Errorf("no violation reported for %s: a user reads this text", tc.name)
			}
		})
	}
}

// Attributes that are not copy must stay quiet, or the guard becomes noise and
// gets turned off.
func TestEmDashGuardIgnoresNonCopyAttributes(t *testing.T) {
	html := `<div class="a—b" data-name="x—y" onclick="send('showList','')">ok</div>`
	if got := emDashViolations(html); len(got) != 0 {
		t.Errorf("violations = %q, want none: class names, data-* payloads and handlers are not copy", got)
	}
}

// screensWithNoFixture returns the Render* functions this guard has no fixture
// for, so a screen added without being added here is a failure rather than a
// silent gap.
//
// This exists because one appeared. RenderMore was written, and neither the em
// dash guard's own view list nor the progress card's page list gained an entry,
// so an em dash in the More screen's subtitle passed the whole suite. Verified
// by putting one there and watching everything stay green.
//
// internal/hostparity has the same check for its own list. Nothing in this
// package had one.
func screensWithNoFixture(t *testing.T, views map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || !strings.HasPrefix(fd.Name.Name, "Render") {
				continue
			}
			// RenderNewProfile -> "newprofile", which is the fixture naming
			// convention: a screen's fixtures are its name, optionally followed
			// by an underscore and a variant.
			screen := strings.ToLower(strings.TrimPrefix(fd.Name.Name, "Render"))
			found := false
			for name := range views {
				if base, _, _ := strings.Cut(name, "_"); base == screen {
					found = true
					break
				}
			}
			if !found {
				missing = append(missing, fd.Name.Name)
			}
		}
	}
	sort.Strings(missing)
	return missing
}
