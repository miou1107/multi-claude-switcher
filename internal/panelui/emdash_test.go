package panelui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/miou1107/multi-claude-switcher/core"
)

// TestNoEmDashInUserFacingText pins a project rule the review process kept
// missing because nothing checked it: user-facing English carries no em dash.
// It slipped in through a wording fix that was itself correcting a different
// inaccuracy, and only turned up when someone looked at the screen.
//
// Tags and their attributes are stripped first, so this reads what a user
// reads. The report the caller passes in is data, not our copy.
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
	tags := regexp.MustCompile(`(?s)<[^>]*>`)

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
	// Current disables the button with its own reason, which a non-Current
	// fixture cannot show at the same time.
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

	removed := RenderRemoved(RemovedVM{Name: "Old one", Convos: 34, ArchiveDir: "Claude_Old-20260804-142233"})
	// Err set draws the failure branch instead, which the success fixture above
	// can never reach in the same call.
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
	// A non-empty RegistryNote is the partial-failure success branch: the
	// folder moved (ArchiveDir is set) but a registry write afterward did not.
	// The plain fixture above never sets RegistryNote, so this is the only call
	// that reaches the hintw block this test is here to cover.
	removedWithRegistryNote := RenderRemoved(RemovedVM{Name: "Old one", Convos: 34,
		ArchiveDir: "Claude_Old-20260804-142233",
		RegistryNote: "Its display name could not be cleared from the switcher's records. " +
			"If a later account reuses this identity, rename it if the old name reappears."})

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
		"removed":               removed,
		"removed_failed":        removedFailed,
		"removed_registry_note": removedWithRegistryNote,
	}
	for name, h := range views {
		text := tags.ReplaceAllString(h, " ")
		if strings.Contains(text, "—") {
			for _, line := range strings.Split(text, "\n") {
				if strings.Contains(line, "—") {
					t.Errorf("%s: em dash in user-facing text: %q", name, strings.TrimSpace(line))
				}
			}
		}
	}
}
