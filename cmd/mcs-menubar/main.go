//go:build darwin

// Command mcs-menubar is the menu-bar app for Multi-Claude Switcher: a native
// NSStatusItem whose click shows an NSPopover hosting a WKWebView — the styled
// account panel, rendered from Go. Written in direct CGO Objective-C (menubar.m)
// because it is compatible with current macOS, unlike darwinkit. Must run inside
// a .app bundle. Every screen (account list, Rescan picker) lives in the one
// popover webview — no separate windows. The page calls back via
// window.webkit.messageHandlers.mcs.
package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#include <stdlib.h>
#include "menubar.h"
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/core/diagnostics"
	"github.com/miou1107/multi-claude-switcher/internal/clip"
	"github.com/miou1107/multi-claude-switcher/internal/panelui"
	"github.com/miou1107/multi-claude-switcher/platform"
)

var (
	plat     platform.Platform
	switcher *core.Switcher

	// panelState holds which screen is up, whether a row's rename editor is
	// open, and whether a progress card is over the top, along with the rules for
	// how those three interact. It lives in internal/panelui so this host and the
	// Windows one cannot drift: every rule in it was learned from a defect in one
	// of them. Views: "list" | "rescan" | "settings" | "sync" | "newprofile" |
	// "merge" | "removed" | "debug".
	//
	// The sticky observer it is built with ties the popover's auto-close to
	// whether a card is on screen, and panelui calls it while still holding its
	// lock, so a later change cannot have its notification overtaken by an
	// earlier one.
	panelState = panelui.NewPanelState("list", setPopoverSticky)

	mu sync.Mutex

	// debugComment survives the reload that every action triggers. Without it,
	// pressing Copy wipes what the user just typed.
	debugComment string

	// debugReportCache and debugReportMasker are the report showDebug most
	// recently built, reused by copyDebug, reportProblem and reloadPanel's own
	// "debug" case rather than each rebuilding it. Building gathers every
	// profile's leveldb identity and session tree plus every log file's tail —
	// heavy enough that this repo already caches leveldb reads elsewhere — and
	// without this cache a single Copy or Report click gathered it twice: once
	// to act on, once again when the reload that follows redrew the same
	// screen.
	//
	// There used to be a debugReportReady flag here, false from the moment
	// showDebug cleared the cache until the gather that followed filled it
	// back in, because the view switched to "debug" before the gather had
	// finished. That window is gone: showDebug now gathers to completion
	// (while Settings shows a busy banner, guarded by the same busy flag as
	// backup/sync/merge) and only then sets the view to "debug" and reloads.
	// By the time this view — or copyDebug, or reportProblem — can be reached
	// at all, the cache is already populated, so there is no "not ready" state
	// left to track or to refuse against.
	debugReportCache  string
	debugReportMasker *diagnostics.Masker

	// newProfileVM carries the pending name screen's context between the action
	// that opened it and the render that draws it, including the validation error
	// from a rejected attempt.
	newProfileVM panelui.NewProfileVM
	// mergeFolders is the pair being resolved in the "merge" view.
	mergeFolders [2]string
	// removedVM holds the outcome of the last removal, drawn by the "removed" view.
	removedVM panelui.RemovedVM

	planMu    sync.Mutex
	planCache = map[string]string{} // profile path -> plan label (leveldb read is heavy; cache it)

	statusMu  sync.Mutex
	statusMsg string // transient feedback shown in the Settings view
	busy      bool   // a maintenance action (e.g. backup) is in progress
	// quitWhenIdle records that Quit was pressed while an operation was running.
	// The operation reopens Claude on its way out, and the quit happens after.
	quitWhenIdle bool
)

func setBusyStatus(b bool, s string) {
	statusMu.Lock()
	busy = b
	// Once a quit is pending, the status belongs to the quit. Otherwise a later
	// progress update would replace "Finishing up, then quitting…" and the user
	// would think their click was ignored.
	if !quitWhenIdle || !b {
		statusMsg = s
	}
	leaving := !b && quitWhenIdle
	statusMu.Unlock()
	if leaving {
		// Quit was asked for while an operation held Claude closed. It has now
		// finished and reopened Claude through its own path, so this is the moment
		// it is safe to go.
		log.Printf("deferred quit: the operation finished, exiting now")
		C.TerminateApp()
	}
}

// deferQuitUntilIdle records that the user asked to quit while an operation was
// running, and returns whether the quit was deferred.
//
// Quitting mid-operation is the problem this avoids twice over. The operation has
// Claude closed and is the only thing that knows how to reopen it, so exiting kills
// the goroutine and leaves the user with neither app. But reopening Claude from here
// and then exiting is not safe either: the operation may still be copying into the
// profile Claude would then have open, which is exactly the corruption MCS closes
// Claude to avoid. So neither — wait for it to finish, then quit.
//
// The wait is bounded, because an operation that never completes must not turn Quit
// into a dead button.
func deferQuitUntilIdle() bool {
	statusMu.Lock()
	if !busy {
		statusMu.Unlock()
		return false
	}
	already := quitWhenIdle
	quitWhenIdle = true
	statusMsg = "Finishing up, then quitting…"
	statusMu.Unlock()
	if already {
		return true // a second click; the timer from the first is still running
	}
	go func() {
		time.Sleep(quitDeferTimeout)
		statusMu.Lock()
		stillBusy := busy && quitWhenIdle
		statusMu.Unlock()
		if !stillBusy {
			return
		}
		log.Printf("deferred quit: operation still running after %s, leaving anyway", quitDeferTimeout)
		reopenClaudeIfWeOweIt()
		C.TerminateApp()
	}()
	return true
}

// quitDeferTimeout bounds how long Quit waits for an operation to finish. A sync of
// a large profile is seconds; anything approaching this is stuck, and the user's
// request to leave wins.
const quitDeferTimeout = 30 * time.Second

func getBusy() bool {
	statusMu.Lock()
	defer statusMu.Unlock()
	return busy
}

func setStatus(s string) {
	statusMu.Lock()
	statusMsg = s
	statusMu.Unlock()
}

func getStatus() string {
	statusMu.Lock()
	defer statusMu.Unlock()
	return statusMsg
}

func setDebugComment(s string) {
	mu.Lock()
	debugComment = s
	mu.Unlock()
}

func getDebugComment() string {
	mu.Lock()
	defer mu.Unlock()
	return debugComment
}

// debugReport builds the report and hands back the masker that produced it, so
// the user's comment and the issue title are masked with the same registrations
// rather than a fresh, empty one.
func debugReport() (string, *diagnostics.Masker) {
	in := buildDiagnostics()
	return diagnostics.Build(in), diagnostics.NewMaskerFor(in)
}

// setDebugReportCache and getDebugReportCache guard debugReportCache /
// debugReportMasker with the same mutex as the comment they are cached
// alongside.
func setDebugReportCache(report string, m *diagnostics.Masker) {
	mu.Lock()
	debugReportCache = report
	debugReportMasker = m
	mu.Unlock()
}

func getDebugReportCache() (string, *diagnostics.Masker) {
	mu.Lock()
	defer mu.Unlock()
	return debugReportCache, debugReportMasker
}

//export goPanelReady
func goPanelReady() { reloadPanel() }

//export goPanelWillOpen
func goPanelWillOpen() {
	panelState.SetViewKeeping("list") // always open to the account list
	setStatus("")                     // clear any stale feedback
	// Render asynchronously so the popover appears instantly; the account-type
	// scan (a leveldb copy per profile) must not block the click on the main
	// thread. The webview keeps its previous content until the refresh lands.
	go func() {
		// A profile that has since been signed in to is no longer pending, and the
		// panel is the only place that notices.
		profiles := mustFindProfiles()
		for _, f := range core.StalePending(core.LoadPending(), profiles) {
			_ = core.RemovePending(f)
		}
		reloadPanel()
	}()
}

// cachedPlan returns a profile's subscription label (e.g. "Max 20×", "Team"),
// reading (heavy) Local Storage only on a cache miss. Plans are stable per
// logged-in dir.
func cachedPlan(path string) string {
	planMu.Lock()
	p, ok := planCache[path]
	planMu.Unlock()
	if ok {
		return p
	}
	p, _ = core.DetectPlan(path)
	planMu.Lock()
	planCache[path] = p
	planMu.Unlock()
	return p
}

// goProgressSticky reports whether a card is on screen, for the popover to read
// synchronously as it opens. See the comment at its call site in menubar.m.
//
//export goProgressSticky
func goProgressSticky() C.int {
	if panelState.Snapshot().Sticky() {
		return 1
	}
	return 0
}

//export goPanelAction
func goPanelAction(caction, cfolder *C.char) {
	action := C.GoString(caction)
	arg := C.GoString(cfolder)
	switch action {
	case "switch":
		// Guarded like sync, backup, merge and removal, and like the Windows host
		// has been since the switch was written there. Two of these at once race
		// over one directory: both close Claude, and whichever relaunches first
		// makes the other's work fail on a folder Claude has recreated underneath
		// it. Against a removal in flight it is worse than a failure, because a
		// switch onto the account being archived relaunches Claude on the very
		// directory RemoveProfile is between checking and renaming.
		if getBusy() {
			return
		}
		// The card carries the message now, so the status banner stays empty:
		// two copies of "switching" on one screen, one of them in the colour
		// this panel uses for "done", is worse than one.
		setBusyStatus(true, "")
		panelState.SetProgress(panelui.SwitchStarting())
		reloadPanel()
		go func() {
			err := doSwitch(arg)
			if err != nil {
				log.Printf("switch to %s: %v", arg, err)
			}
			vm := panelui.SwitchOutcome(core.DisplayName(arg), err)
			// The card goes up before busy comes down, so there is no instant in
			// which the panel accepts a second switch while still showing the
			// first one running.
			panelState.SetProgress(vm)
			setBusyStatus(false, "")
			reloadPanel()
		}()
	case "showRescan":
		panelState.SetView("rescan")
		go reloadPanel()
	case "showList":
		panelState.SetView("list")
		setStatus("") // a deliberate return to the list starts clean; the paths that
		// want a message set it and render the list themselves without this action
		go reloadPanel()
	case "showSettings":
		// Shared by the plain Settings gear and the Debug view's back button
		// (and its Esc equivalent), which pass the live comment textarea value
		// as arg so leaving Debug does not discard what was typed. showDebug no
		// longer clears the saved comment on entry, so this save is what makes
		// a later Debug visit come back with the text still there. Every other
		// caller passes "", which must not clobber a comment saved this way.
		if arg != "" {
			setDebugComment(arg)
		}
		panelState.SetView("settings")
		setStatus("")
		reloadPanel()
	case "showSync":
		panelState.SetView("sync")
		setStatus("")
		go reloadPanel() // buildProfiles may read leveldb; don't stall the click
	case "sync":
		if getBusy() {
			return
		}
		parts := strings.SplitN(arg, "|", 2)
		if len(parts) != 2 {
			return
		}
		setBusyStatus(true, "")
		panelState.SetProgress(panelui.SyncStarting())
		reloadPanel()
		go func() {
			panelState.SetProgress(doSync(parts[0], parts[1]))
			setBusyStatus(false, "")
			reloadPanel()
		}()
	case "confirmManaged":
		var folders []string
		_ = json.Unmarshal([]byte(arg), &folders)
		if len(folders) > 0 {
			_ = core.SetManaged(folders)
		}
		panelState.SetView("list")
		go reloadPanel()
	case "toggleAutoSync":
		_ = core.SetAutoSyncOnSwitch(!core.AutoSyncOnSwitch())
		reloadPanel()
	case "toggleLogin":
		if core.LoginItemEnabled() {
			_ = core.DisableLoginItem()
		} else if exe, err := os.Executable(); err == nil {
			_ = core.EnableLoginItem(exe)
		}
		reloadPanel()
	case "backup":
		if getBusy() {
			return // already backing up; ignore repeat clicks
		}
		setBusyStatus(true, "")
		panelState.SetProgress(panelui.BackupStarting())
		reloadPanel()
		go func() {
			panelState.SetProgress(panelui.BackupOutcome(doBackupAll()))
			setBusyStatus(false, "")
			reloadPanel()
		}()
	case "renameOpen":
		panelState.SetRenameOpen(arg == "1")
	case "renameSave":
		// The editor is gone either way; clear the hold before anything can
		// return early, or the list would stay frozen.
		panelState.SetRenameOpen(false)
		// Guarded like every other action that writes: renaming during a removal
		// would write names.json after RemoveProfile had just cleared it, putting
		// back the one piece of residue the design calls out as felt by the user,
		// since a later profile reusing the identity inherits it.
		if getBusy() {
			setStatus("Busy right now. Try the rename again in a moment.")
			go reloadPanel()
			return
		}
		var pair []string
		if json.Unmarshal([]byte(arg), &pair) == nil && len(pair) == 2 {
			_ = core.SetProfileName(pair[0], pair[1])
		}
		panelState.SetView("list")
		go reloadPanel()
	case "newProfile":
		// The add card: open the name screen on the plain add path (no account to
		// recover). Same screen as recovery, empty of context.
		mu.Lock()
		newProfileVM = panelui.NewProfileVM{}
		mu.Unlock()
		panelState.SetView("newprofile")
		go reloadPanel()
	case "showRecover":
		// arg is the account UUID. The source profiles are looked up from a fresh
		// scan when the recovery runs: a ghost can have several, and their paths are
		// only valid for the scan that produced them.
		if arg == "" {
			return
		}
		row, ok := ghostRow(arg)
		if !ok || !row.Recoverable {
			setStatus("That account is no longer recoverable. Run Rescan")
			panelState.SetView("list")
			go reloadPanel()
			return
		}
		mu.Lock()
		newProfileVM = panelui.NewProfileVM{
			RecoverUUID:   arg,
			SuggestedName: recoverySuggestedName(row),
			Convos:        row.Convos,
		}
		mu.Unlock()
		panelState.SetView("newprofile")
		go reloadPanel()
	case "createProfile":
		var a []string
		if json.Unmarshal([]byte(arg), &a) != nil || len(a) != 2 {
			return
		}
		if getBusy() {
			return
		}
		setBusyStatus(true, "Setting up…")
		reloadPanel()
		go func() {
			req := core.CreateProfileRequest{Name: a[0], RecoverUUID: a[1]}
			if req.RecoverUUID != "" {
				// Re-scan now rather than trusting anything the webview sent back: the
				// sources' paths must come from the scan current at the moment of copy.
				row, ok := ghostRow(req.RecoverUUID)
				if !ok || !row.Recoverable {
					setBusyStatus(false, "That account is no longer recoverable. Run Rescan")
					panelState.SetView("list")
					reloadPanel()
					return
				}
				req.Sources = row.Sources
			}
			_, err := core.NewProfileCreator(plat).Create(req)
			setBusyStatus(false, "")
			if err != nil {
				// Back to the same screen with the reason, and with the name the user
				// typed still in the field so they do not have to retype it.
				mu.Lock()
				newProfileVM.SuggestedName = a[0]
				newProfileVM.Err = err.Error()
				mu.Unlock()
				panelState.SetView("newprofile")
			} else {
				panelState.SetView("list")
			}
			reloadPanel()
		}()
	case "showMerge":
		parts := strings.SplitN(arg, "|", 2)
		if len(parts) != 2 {
			return
		}
		mu.Lock()
		mergeFolders = [2]string{parts[0], parts[1]}
		mu.Unlock()
		setStatus("")
		panelState.SetView("merge")
		go reloadPanel()
	case "mergeConfirm":
		// arg is "<keepIdentity>|<archiveIdentity>". Identities, not paths: the merge
		// resolves them itself.
		parts := strings.SplitN(arg, "|", 2)
		if len(parts) != 2 || getBusy() {
			return
		}
		keep, archive := parts[0], parts[1]
		setBusyStatus(true, "")
		panelState.SetProgress(panelui.MergeStarting())
		reloadPanel()
		go func() {
			err := plat.TerminateApp()
			if err == nil {
				_, err = core.MergeDuplicates(plat, core.MergeRequest{
					KeepIdentity: keep, ArchiveIdentity: archive,
				})
			}
			// The card lands on the list either way, so the view moves before it
			// goes up: on success one of the two rows is gone, which is the
			// confirmation, and on failure the list is where the user tries
			// again from. Keeping the merge screen would leave a keeper/archive
			// choice on screen for accounts that are already one.
			panelState.SetViewKeeping("list")
			panelState.SetProgress(panelui.MergeOutcome(err))
			setBusyStatus(false, "")
			reloadPanel()
		}()
	case "removeProfile":
		// arg is the folder identity, straight from askRemove's confirm dialog (or
		// the failure screen's Try again, which round-trips the same value).
		if getBusy() {
			return
		}
		folder := arg
		// Read before the goroutine starts: once RemoveProfile has moved the
		// directory, neither the display name nor the conversation count can be
		// looked up again — buildProfiles no longer has a row for it.
		beforeName, beforeConvos := removalRow(folder)
		setBusyStatus(true, "Removing…")
		reloadPanel()
		go func() {
			dest, err := core.RemoveProfile(plat, folder)
			// The decision lives in panelui, shared with mcs-tray, so the two
			// hosts cannot drift on what a clean removal, a partial failure and
			// an outright failure each do — the way this codebase already
			// shipped a platform difference once.
			outcome := panelui.DecideRemovalOutcome(folder, beforeName, beforeConvos, dest, err)
			if outcome.ShowList {
				setBusyStatus(false, outcome.ListStatus)
				panelState.SetView("list")
			} else {
				setBusyStatus(false, "")
				mu.Lock()
				removedVM = outcome.Removed
				mu.Unlock()
				// The view moves after the model it draws is in place. SetView
				// rather than a bare assignment: this is the user arriving at an
				// outcome screen, so it ends an inline rename the same way every
				// other navigation does, and takes down any card still up.
				// Removal raises none of its own, but a switch or sync card
				// outlives the busy flag that guarded it: it stays until the user
				// dismisses it, and by then busy is false, so nothing stops a
				// removal starting underneath it. Leaving it would draw a stale
				// "Switched successfully" over the removal outcome.
				panelState.SetView("removed")
			}
			reloadPanel()
		}()
	case "showDebug":
		// Guarded like backup, sync and merge: the gather below is heavy (a
		// leveldb copy per profile, a session-tree walk, every log tail), and a
		// second click while one is already running must not start another.
		if getBusy() {
			return
		}
		// Gathered to completion before the view switches, not after. The old
		// shape cleared the cache, switched to "debug" immediately, and
		// gathered on a goroutine — which rendered the (empty) debug view, with
		// its comment box, before the report existed. A user who started typing
		// what went wrong the instant they saw that box lost it the moment the
		// finished gather's reloadPanel redrew the same view from an empty
		// getDebugComment(). Staying on the current view with a busy banner
		// keeps the comment box off screen until there is a finished report to
		// show next to it.
		setBusyStatus(true, "Gathering debug info…")
		reloadPanel()
		go func() {
			report, m := debugReport()
			setDebugReportCache(report, m)
			setBusyStatus(false, "")
			panelState.SetView("debug")
			reloadPanel()
		}()
	case "reportProblem":
		// Guarded like backup, sync and merge. Without this, mashing the button
		// stacked concurrent clip.Set/open calls.
		if getBusy() {
			return
		}
		setDebugComment(arg)
		report, m := getDebugReportCache()
		setBusyStatus(true, "Copying report…")
		reloadPanel()
		go func() {
			full := diagnostics.AppendComment(report, arg, m)
			if err := clip.Set(full); err != nil {
				// The browser is not opened: an issue form with nothing to paste is
				// worse than no browser at all. The comment is left in place too —
				// this is exactly the moment the user still needs it, to retry Copy
				// or Report a problem once whatever broke clip.Set is fixed.
				setBusyStatus(false, "Couldn't copy the report: "+err.Error())
				reloadPanel()
				return
			}
			_ = exec.Command("open", diagnostics.IssueURL(arg, m)).Start()
			// The comment has done its job: it is in the clipboard body and in the
			// prefilled issue title. Clearing it here, rather than on Debug's next
			// entry, means a stale "still happening" does not sit in the box the
			// next time something unrelated goes wrong.
			setDebugComment("")
			setBusyStatus(false, "Report copied. Paste it into the issue.")
			reloadPanel()
		}()
	case "copyDebug":
		if getBusy() {
			return
		}
		setDebugComment(arg)
		report, _ := getDebugReportCache()
		setBusyStatus(true, "Copying…")
		reloadPanel()
		go func() {
			if err := clip.Set(report); err != nil {
				setBusyStatus(false, "Couldn't copy: "+err.Error())
			} else {
				setBusyStatus(false, "Copied.")
			}
			reloadPanel()
		}()
	case "openLog":
		_ = exec.Command("open", core.LogDir()).Start()
	case "openBackups":
		home, _ := os.UserHomeDir()
		_ = exec.Command("open", filepath.Join(home, ".multi-claude-switcher", "backups")).Start()
	case "openArchive":
		dir := plat.ArchiveDir()
		// Create it first: until something has been archived the folder does not
		// exist, and `open` on a missing path fails with a dialog.
		_ = os.MkdirAll(dir, 0o755)
		_ = exec.Command("open", dir).Start()
	case "checkUpdates":
		if getBusy() {
			return
		}
		setBusyStatus(true, "Checking for updates…")
		reloadPanel()
		go manualCheckAndInstall()
	case "hidePanel":
		C.ClosePopover()
	case "quit":
		if deferQuitUntilIdle() {
			go reloadPanel()
			return
		}
		reopenClaudeIfWeOweIt()
		C.TerminateApp()
	}
}

// reopenClaudeIfWeOweIt puts Claude Desktop back before MCS goes away.
//
// A switch or a sync closes Claude, does its work, and reopens it. Quitting in
// that window kills the goroutine doing the work, so nothing ever reopens Claude
// and the user is left with neither app. Claiming the debt rather than reading it
// means the operation, if it survives long enough to finish, will not open a
// second window.
func reopenClaudeIfWeOweIt() {
	owed := switcher.ClaimPendingRelaunch()
	if len(owed) == 0 {
		return
	}
	for _, p := range owed {
		log.Printf("quit while Claude was closed for an operation; reopening %s first", p)
		if err := plat.LaunchProfile(p); err != nil {
			log.Printf("could not reopen Claude Desktop on %s on the way out: %v", p, err)
		}
	}
}

// doBackupAll backs up every profile that has session data and returns how many
// were backed up.
// doBackupAll returns how many accounts were backed up and how many tried and
// failed.
//
// The two are counted separately because the card reports a cause, not just a
// number: with only a total, a run where every backup failed is indistinguishable
// from a run where no account had anything to back up, and the panel said the
// latter, under a green tick. The per-account error is also logged now, which it
// never was.
func doBackupAll() (done, failed int) {
	bm := core.NewBackupManager("")
	for _, p := range mustFindProfiles() {
		if !core.ProfileHasSessions(p.Path) {
			continue
		}
		// CreateBackup, not BackupIfHasData: the user pressed a button that says it
		// backs things up, so it has to actually take a snapshot rather than reuse
		// yesterday's and report a number that means nothing.
		if _, err := bm.CreateBackup(p.Path); err != nil {
			log.Printf("backup of %s failed: %v", p.Path, err)
			failed++
			continue
		}
		done++
	}
	return done, failed
}

func folderPath(folder string) string {
	for _, p := range mustFindProfiles() {
		if p.Name == folder {
			return p.Path
		}
	}
	return ""
}

// doSync copies one account's Code sessions into another and returns a result
// message for the status banner.
func doSync(fromFolder, toFolder string) *panelui.ProgressVM {
	from, to := folderPath(fromFolder), folderPath(toFolder)
	if from == "" || to == "" {
		return panelui.SyncOutcome("", nil, fmt.Errorf("that account is no longer there. Run Rescan"))
	}
	// ManualAlign, not SyncSessions: it closes Claude Desktop before writing and
	// reopens the profile the user was on, and it snapshots the target first.
	// Calling SyncSessions directly wrote into a profile Claude was still running
	// on, which risks corrupting the session index it holds open, and skipped the
	// backup the README promises for every write.
	rep, err := switcher.ManualAlign(from, to)
	if err != nil {
		return panelui.SyncOutcome(core.DisplayName(toFolder), nil, err)
	}
	for _, e := range rep.SkipErrors {
		// The card says "see the log", so it has to actually be there.
		log.Printf("sync skipped a session file: %s", e)
	}
	if len(rep.SkipErrors) > 0 {
		// Belt and braces. The card now survives Claude taking the foreground,
		// which is what used to lose this message, but a notification also
		// reaches a user who has already walked away from the panel. Files that
		// could not be read are the one outcome worth that: a conflict is not,
		// it only means the target's copy was already newer, which is ordinary.
		notify("Some conversations were skipped", core.SyncResultMessage(rep, core.DisplayName(toFolder)))
	}
	return panelui.SyncOutcome(core.DisplayName(toFolder), rep, nil)
}

// setPopoverSticky ties the panel's auto-close to whether a card is on screen.
// It is panelState's sticky observer rather than something the switch action
// calls, so there is no path that puts a card up and leaves the panel free to
// close itself the moment Claude Desktop takes the foreground, which is the
// event a switch ends with.
//
// panelui calls this while holding its own lock. That is deliberate: released
// first, one goroutine's call could be overtaken by another's, leaving the
// popover stuck in ApplicationDefined with no card on screen, so the panel
// would never close by itself again for the rest of the session with nothing to
// explain why. Safe because SetPopoverSticky only dispatches to the main queue,
// so it cannot re-enter that lock.
func setPopoverSticky(on bool) {
	v := C.int(0)
	if on {
		v = 1
	}
	C.SetPopoverSticky(v)
}

// reloadPanel renders the current view and pushes it into the popover webview.
func reloadPanel() {
	// One read for the whole render: the view drawn and the card drawn over it
	// come from the same instant, rather than from two reads a change can fall
	// between.
	snap := panelState.Snapshot()

	// See Snapshot.HoldReload: a reload replaces the document, so one landing
	// mid-rename used to take away what the user had typed. A card overrides
	// that, or holding the reload swallows the card entirely.
	if snap.HoldReload() {
		return
	}

	// rendered is the screen this pass actually produced, which is not always
	// snap.View: the merge branch below can fall back to the list and move the
	// view itself. The push at the end compares against this, not against the
	// view we started from.
	rendered := snap.View

	var htmlStr string
	switch snap.View {
	case "rescan":
		accounts := core.ScanAccounts(mustFindProfiles(), core.LoadPending())
		htmlStr = panelui.RenderRescan(accounts, panelui.ComputePreselect(accounts, core.LoadManaged()))
	case "sync":
		htmlStr = panelui.RenderSync(buildProfiles(), getStatus(), getBusy())
	case "newprofile":
		mu.Lock()
		vm := newProfileVM
		mu.Unlock()
		htmlStr = panelui.RenderNewProfile(vm)
	case "merge":
		mu.Lock()
		pair := mergeFolders
		mu.Unlock()
		a, b := mergeCandidate(pair[0]), mergeCandidate(pair[1])
		// Preselect whichever is in use, so compute the plan for that direction.
		// Swapping the keeper changes nothing shown: the union and the conflict
		// count are symmetric except for which side wins a conflict.
		keep, archive := pair[0], pair[1]
		if b.Current && !a.Current {
			keep, archive = pair[1], pair[0]
		}
		plan, planErr := mergePlanFor(keep, archive)
		if planErr != nil {
			// Fall back to the list with the reason rather than showing a merge whose
			// outcome is unknown. Keeping any card: this runs during a render, and
			// a merge already in flight re-computes its plan against accounts it is
			// halfway through archiving, so the plan failing here is expected and
			// must not take down the card reporting on that very merge.
			setStatus(planErr.Error())
			panelState.SetViewKeeping("list")
			rendered = "list"
			htmlStr = panelui.RenderList(buildProfiles(), newProfileSupported(), getStatus())
			break
		}
		htmlStr = panelui.RenderMerge(a, b, plan, getStatus(), getBusy())
	case "removed":
		mu.Lock()
		vm := removedVM
		mu.Unlock()
		vm.Status = getStatus()
		htmlStr = panelui.RenderRemoved(vm)
	case "settings":
		htmlStr = panelui.RenderSettings(panelui.SettingsVM{
			AutoSync:   core.AutoSyncOnSwitch(),
			StartLogin: core.LoginItemEnabled(),
			Version:    core.Version,
			Status:     getStatus(),
			Busy:       getBusy(),
		})
	case "debug":
		// Reused, not rebuilt: showDebug is the only path into this view, and it
		// only sets the view once the gather that fills debugReportCache has
		// already finished (see the doc comment on debugReportCache), so this
		// always has a real report to draw. Rebuilding here as well as in
		// copyDebug/reportProblem was the double gather this replaced.
		report, _ := getDebugReportCache()
		htmlStr = panelui.RenderDebug(panelui.DebugVM{
			Report:  report,
			Comment: getDebugComment(),
			Status:  getStatus(),
		})
	default:
		htmlStr = panelui.RenderList(buildProfiles(), newProfileSupported(), getStatus())
	}
	// One call for every screen, rather than a view model threaded through each
	// renderer: the card is an overlay that does not care what is underneath it,
	// and passing it per-renderer is how one host ends up drawing it on a screen
	// the other forgot.
	//
	// Re-read before pushing. The render above can take seconds (leveldb per
	// profile), reloadPanel is not serialized, and a faster reload started
	// later can already have pushed the current screen. Publishing this one on
	// top would put a stale page up with nothing scheduled to correct it.
	//
	// The card comes from the fresh read for the same reason: pinning it at the
	// top of the render meant a merge whose outcome landed mid-render was drawn
	// with the "Merging" spinner still on it, and that spinner was then the last
	// thing on screen.
	cur := panelState.Snapshot()
	if cur.View != rendered {
		return
	}
	c := C.CString(panelui.WithProgress(htmlStr, cur.Progress))
	defer C.free(unsafe.Pointer(c))
	C.LoadPanelHTML(c)
}

func main() {
	// Without this the menu-bar process logs to stderr only, which a bundled .app
	// discards: there was no log file on macOS at all. Diagnostics reports include
	// the log, so an unlogged host produces an empty section.
	if c, _, err := core.SetupLogging("mcs-menubar"); err == nil {
		defer func() { _ = c.Close() }()
	}
	plat = platform.New()
	switcher = core.NewSwitcher(plat, core.NewBackupManager(""))
	startUpdateChecker() // periodic background self-update
	C.RunMenuBar()
}

func mustFindProfiles() []*platform.ProfileInfo {
	p, _ := plat.FindProfiles()
	return p
}

// doSwitch moves the user to folder and reports whether it worked.
//
// It used to discard SafeSwitch's error, which made a failed switch look
// identical to a successful one: Claude stayed shut or stayed on the old
// account, and the panel said nothing. The Windows host has always returned it.
func doSwitch(folder string) error {
	if folder == "" {
		// Capitalised on purpose, here and below: these are not wrapped by
		// anything, they are printed verbatim as the one sentence under the
		// card's heading. ST1005 disagrees; the user reads the screen.
		return fmt.Errorf("No account was named")
	}
	profiles := mustFindProfiles()
	var target *platform.ProfileInfo
	for _, p := range profiles {
		if p.Name == folder {
			target = p
			break
		}
	}
	if target == nil {
		return fmt.Errorf("That account is no longer there. Run Rescan")
	}
	return switcher.SafeSwitch(sourceProfilePath(target.Path, profiles), target.Path, target.Name)
}

// buildProfiles lists the managed accounts for the list view.
func buildProfiles() []panelui.ProfileVM {
	running, _ := plat.DetectRunningProfile()
	// Shared with the Windows host on purpose: this used to be a copy in each, and
	// the copies drifted — SignedIn was set in one and not the other, which left the
	// sync screen unable to offer any pair at all on macOS.
	return panelui.BuildProfiles(mustFindProfiles(), core.LoadManaged(), pendingFolders(), running, cachedPlan)
}

// removalRow looks up the display name and conversation count for a folder
// about to be removed, from the same list the panel shows, so the removal
// outcome quotes the number the user was already looking at. Read before
// RemoveProfile runs: once it has moved the directory, buildProfiles no
// longer has a row for it.
func removalRow(folder string) (name string, convos int) {
	name = core.DisplayName(folder)
	for _, p := range buildProfiles() {
		if p.Folder == folder {
			return p.Name, p.Convos
		}
	}
	return name, 0
}

// pendingFolders is the folder names of profiles awaiting their one-time sign-in,
// so the list shows a freshly created profile even before it has an account.
func pendingFolders() []string {
	var out []string
	for _, e := range core.LoadPending() {
		out = append(out, e.Folder)
	}
	return out
}

// newProfileSupported reports whether "Add another account" applies on this host.
// It always does on macOS: a profile is a sibling data directory MCS creates and
// Claude Desktop populates on first launch, the same model as the Windows Store
// build. (The Windows standalone build, whose profiles the user picks by hand,
// returns false for exactly this reason.)
func newProfileSupported() bool { return true }

// profilePathFor resolves a profile identity to its real path by looking it up
// among the discovered profiles, and returns "" when there is no such profile.
//
// A lookup, never filepath.Join onto the data root: a guessed path is worse than
// an honest failure, because it reads as a profile that is simply empty.
func profilePathFor(identity string) string {
	for _, p := range mustFindProfiles() {
		if p.Name == identity {
			return p.Path
		}
	}
	return ""
}

// mergeCandidate builds one side of the merge screen: the display name, plan, how
// many conversations it holds for its own account, and whether Claude is running
// on it.
func mergeCandidate(identity string) panelui.MergeCandidateVM {
	path := profilePathFor(identity)
	running, _ := plat.DetectRunningProfile()
	vm := panelui.MergeCandidateVM{
		Folder:  identity,
		Name:    core.DisplayName(identity),
		Plan:    cachedPlan(path),
		Current: path != "" && path == running,
	}
	if path == "" {
		return vm
	}
	if uuid, err := platform.GetProfileAccountUUID(path); err == nil {
		for _, p := range mustFindProfiles() {
			if p.Name == identity {
				vm.Convos = p.UUIDBuckets[uuid]
			}
		}
	}
	return vm
}

// mergePlanFor computes what the merge would do, so the screen shows the total the
// user will actually get rather than the sum of two counts. A failure here means
// the merge screen must not be shown at all: offering a merge whose outcome could
// not be computed is worse than reporting the problem.
func mergePlanFor(keepIdentity, archiveIdentity string) (core.MergePlan, error) {
	keepPath, archivePath := profilePathFor(keepIdentity), profilePathFor(archiveIdentity)
	if keepPath == "" || archivePath == "" {
		return core.MergePlan{}, fmt.Errorf("one of those profiles is no longer there. Run Rescan")
	}
	uuid, err := platform.GetProfileAccountUUID(keepPath)
	if err != nil {
		return core.MergePlan{}, fmt.Errorf("%s has no account signed in", core.DisplayName(keepIdentity))
	}
	plan, err := core.MergePreview(keepPath, archivePath, uuid)
	if err != nil {
		return core.MergePlan{}, err
	}
	return *plan, nil
}

// ghostRow re-scans and returns the current row for an orphaned account. Every
// recovery step goes through this rather than trusting values round-tripped
// through the webview: the row carries the source profiles' paths, and a path is
// only valid for the scan that produced it.
func ghostRow(uuid string) (core.ScannedAccount, bool) {
	for _, a := range core.ScanAccounts(mustFindProfiles(), core.LoadPending()) {
		if a.UUID == uuid && !a.Complete {
			return a, true
		}
	}
	return core.ScannedAccount{}, false
}

// recoverySuggestedName proposes a name for a recovered account, dated by when it
// was last used so the user can tell two recoveries apart.
func recoverySuggestedName(row core.ScannedAccount) string {
	if !row.LastUpdated.IsZero() {
		return "Recovered " + row.LastUpdated.Format("2006-01-02")
	}
	return "Recovered"
}

func sourceProfilePath(targetPath string, profiles []*platform.ProfileInfo) string {
	return core.SourceProfilePath(plat, targetPath, profiles)
}
