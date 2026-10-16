package panelui

import (
	"errors"
	"testing"
	"time"

	"github.com/miou1107/multi-claude-switcher/core"
)

// renderedViews is every screen a user can land on, built once and shared by
// the guards that read rendered copy: the em dash check and the localization
// coverage check. It lives apart from both so neither owns it, and so
// screensWithNoFixture keeps measuring one list rather than two that drift.
func renderedViews(t *testing.T) map[string]string {
	t.Helper()
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
		// Current account: its own card layout and "In use now" sub-copy.
		{Folder: "Claude", Name: "Work", Plan: "Pro", Convos: 3, Current: true, SignedIn: true, UUID: "dup-uuid"},
		// Not signed in: the "Switch here, then sign in" sub-copy, distinct from
		// a ready one, which has no subtitle.
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

	return map[string]string{
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
}
