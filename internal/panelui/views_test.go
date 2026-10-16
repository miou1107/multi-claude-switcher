package panelui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/miou1107/multi-claude-switcher/core"
)

// Four of core's own error messages, exactly as the screens below print them.
// They are named rather than written inline because the localization guard
// lists the same strings as text the dictionaries are not asked to answer: a
// second copy of a sentence this long would go stale, and a stale copy there
// means a sentence nothing checks.
var (
	errArchiveFailed = `couldn't archive Old one: Claude may still be holding its files. ` +
		`Fully quit Claude and try again. (rename /Users/x/Library/Application Support/Claude_Old: permission denied)`
	errSwitchFailed = "can't switch to Claude_Old: no profile folder there"
	errSyncSkipped  = "skipped auto sync: failed to back up source profile " +
		"(refusing to write without a backup): permission denied"
	// errors.Join hands these over newline-separated and the screen draws a
	// line per complaint, so each line reaches a reader on its own.
	errRegistryNote = []string{
		"Old one was removed, but some of what the switcher had recorded about it could not be cleared.",
		"The switcher's own account list still mentions it. Nothing needs doing: the panel only shows accounts whose folder is still there. (write managed.json: permission denied)",
		`Its name is still recorded as "Old one". If you sign in to this account again later it will come back under that name, which you can change with Rename. (write names.json: permission denied)`,
	}
)

// renderedViews is every screen a user can land on, built once and shared by
// the guards that read rendered copy: the em dash check and the localization
// coverage check. It lives apart from both so neither owns it, and so
// screensWithNoFixture keeps measuring one list rather than two that drift.
func renderedViews(t *testing.T) map[string]string {
	t.Helper()
	rescan := RenderRescan([]core.ScannedAccount{
		// Pending: no account yet, just a folder waiting for sign-in. It shares
		// its branch with SignedOut below, so one row would draw both shapes —
		// but each carries a different sentence, and the sentence is what these
		// guards read. Every note here is the one core really hands over.
		// Shorthand sat in their place until the localization check went in and
		// found three sentences on this screen that neither dictionary answered:
		// a made-up note is a note no guard has ever seen.
		{HomeFolder: "Claude_pending", Pending: true, Note: core.PendingSignInNote},
		// Signed out inside Claude: same branch, the other sentence.
		{HomeFolder: "Claude_out", SignedOut: true, Note: core.SignedOutNote},
		// Ghost, ranked recoverable: ID + conversations exist, no profile folder.
		{UUID: "u-recoverable", Recoverable: true, Convos: 2, Note: core.RecoverableGhostNote},
		// Ghost, dead end: no folder, no bucket, nothing to recover. This note
		// has no exported name — it is what core's deriveNote returns for an
		// incomplete row, spelled the same way core's own tests spell it.
		{UUID: "u-dead", Convos: 0, Note: "Invalid account data"},
		// Complete, normal row, and the one that matters most here: no
		// LastUpdated at all. This is the exact shape that shipped the em dash
		// this test exists to catch, so the fixture must be this, not a
		// same-looking row with a date filled in.
		// On a Team plan, so the 🏢 Team badge is drawn: the plan name is left in
		// English on purpose, and the localization guard records that decision.
		{UUID: "u-complete", Complete: true, HomeFolder: "Claude", Email: "work@example.com", Account: core.AccountTeam, Convos: 3, LastUpdated: time.Time{}},
	}, map[string]bool{})

	list := RenderList([]ProfileVM{
		// Current account: its own card layout and "In use now" sub-copy.
		{Folder: "Claude", Name: "Work", Plan: "Pro", Convos: 3, Current: true, SignedIn: true, UUID: "dup-uuid"},
		// Not signed in: the "Switch here, then sign in" sub-copy, distinct from
		// a ready one, which has no subtitle.
		{Folder: "Claude_new", Name: "New one", SignedIn: false},
		// Two profiles sharing a UUID: the duplicate-account warning banner.
		{Folder: "Claude_dup", Name: "Work", Plan: "Pro", Convos: 1, SignedIn: true, UUID: "dup-uuid"},
		// The status banner carries whatever the host last put there. This is
		// one of the host's own lines, not a stand-in: a failed Recover sets it
		// and then sends the user straight to this screen.
	}, true, "That account is no longer recoverable. Run Rescan")
	// The empty list has its own copy ("No managed accounts yet…") that a
	// non-empty list can never reach in the same call.
	listEmpty := RenderList(nil, false, "")

	// The error line is the one createProfile really returns when the name box
	// comes back empty, for the same reason the notes above are core's own.
	newProfile := RenderNewProfile(NewProfileVM{SuggestedName: "Work", Convos: 0, Err: "No account was named"})
	// RecoverUUID switches the title, subtitle and hint text to the recovery
	// variant; mutually exclusive with the add variant above in one call.
	newProfileRecover := RenderNewProfile(NewProfileVM{RecoverUUID: "abcd1234efgh", SuggestedName: "Old one", Convos: 5})

	merge := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Work", Plan: "Pro", Convos: 3, Current: true},
		MergeCandidateVM{Folder: "Claude_2", Name: "Work 2", Plan: "Pro", Convos: 5},
		core.MergePlan{Combined: 8, Conflicts: 1, Unreadable: 1},
		// What the hosts show when planning the merge fails: they pass the
		// error's own text through to this banner.
		"one of those profiles is no longer there. Run Rescan", false)
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
	removedFailed := RenderRemoved(RemovedVM{Folder: "Claude_Old", Name: "Old one", Err: errArchiveFailed})
	// A non-empty RegistryNote is the other live branch: the folder moved but a
	// registry write afterward did not, so this is the only call that reaches the
	// hintw block this test is here to cover.
	// Two entries, newline-separated, as errors.Join really hands them over: the
	// multi-line branch is the one that draws a line per complaint.
	removedWithRegistryNote := RenderRemoved(RemovedVM{Name: "Old one", Convos: 34,
		RegistryNote: strings.Join(errRegistryNote, "\n")})

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
	switchWarned := cardOver(SwitchOutcome("Work", &core.SwitchedWithWarning{Err: errors.New(errSyncSkipped)}))
	switchFailed := cardOver(SwitchOutcome("Work", errors.New(errSwitchFailed)))
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
		// Both status lines below are the ones the hosts set on these two
		// screens: the Debug screen's copy button, and the Settings screen's
		// update check on Windows.
		"debug":                 RenderDebug(DebugVM{Report: "MCS 0.11.2", Comment: "typed", Status: "Report copied. Paste it into the issue."}),
		"settings":              RenderSettings(SettingsVM{Version: "0.11.2", Status: "Checking for updates… the answer will pop up in a moment.", AutoSync: true, StartLogin: true}),
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
