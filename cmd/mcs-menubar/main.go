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
	"strconv"
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

	mu           sync.Mutex
	currentView  = "list" // "list" | "rescan" | "settings" | "sync" | "rename" | "newprofile" | "merge" | "removed" | "debug"
	renameFolder string   // the folder being renamed in the "rename" view

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
	setView("list") // always open to the account list
	setStatus("")   // clear any stale feedback
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
		setBusyStatus(true, "Closing Claude Desktop and switching…")
		reloadPanel()
		go func() {
			doSwitch(arg)
			setBusyStatus(false, "")
			reloadPanel()
		}()
	case "showRescan":
		setView("rescan")
		go reloadPanel()
	case "showList":
		setView("list")
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
		setView("settings")
		setStatus("")
		reloadPanel()
	case "showSync":
		setView("sync")
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
		setBusyStatus(true, "Closing Claude Desktop and syncing…")
		reloadPanel()
		go func() {
			setBusyStatus(false, doSync(parts[0], parts[1]))
			reloadPanel()
		}()
	case "confirmManaged":
		var folders []string
		_ = json.Unmarshal([]byte(arg), &folders)
		if len(folders) > 0 {
			_ = core.SetManaged(folders)
		}
		setView("list")
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
		setBusyStatus(true, "Backing up…")
		reloadPanel()
		go func() {
			n := doBackupAll()
			switch {
			case n == 0:
				setBusyStatus(false, "No accounts had sessions to back up.")
			case n == 1:
				setBusyStatus(false, "✓ Backed up 1 account.")
			default:
				setBusyStatus(false, "✓ Backed up "+itoa(n)+" accounts.")
			}
			reloadPanel()
		}()
	case "showRename":
		mu.Lock()
		renameFolder = arg
		currentView = "rename"
		mu.Unlock()
		reloadPanel()
	case "renameSave":
		var pair []string
		if json.Unmarshal([]byte(arg), &pair) == nil && len(pair) == 2 {
			_ = core.SetProfileName(pair[0], pair[1])
		}
		setView("list")
		go reloadPanel()
	case "newProfile":
		// The add card: open the name screen on the plain add path (no account to
		// recover). Same screen as recovery, empty of context.
		mu.Lock()
		newProfileVM = panelui.NewProfileVM{}
		mu.Unlock()
		setView("newprofile")
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
			setStatus("That account is no longer recoverable — run Rescan")
			setView("list")
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
		setView("newprofile")
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
					setBusyStatus(false, "That account is no longer recoverable — run Rescan")
					setView("list")
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
				setView("newprofile")
			} else {
				setView("list")
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
		setView("merge")
		go reloadPanel()
	case "mergeConfirm":
		// arg is "<keepIdentity>|<archiveIdentity>". Identities, not paths: the merge
		// resolves them itself.
		parts := strings.SplitN(arg, "|", 2)
		if len(parts) != 2 || getBusy() {
			return
		}
		keep, archive := parts[0], parts[1]
		setBusyStatus(true, "Merging…")
		reloadPanel()
		go func() {
			if err := plat.TerminateApp(); err != nil {
				setBusyStatus(false, err.Error())
				reloadPanel()
				return
			}
			_, err := core.MergeDuplicates(plat, core.MergeRequest{
				KeepIdentity: keep, ArchiveIdentity: archive,
			})
			if err != nil {
				setBusyStatus(false, err.Error())
				reloadPanel()
				return
			}
			setBusyStatus(false, "Merged.")
			setView("list")
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
		before := accountVM(folder)
		setBusyStatus(true, "Removing…")
		reloadPanel()
		go func() {
			out := panelui.RemovedVM{Folder: folder, Name: before.Name, Convos: before.Convos}
			dest, err := core.RemoveProfile(plat, folder)
			switch {
			case dest != "":
				// Route on the destination, not the error. RemoveProfile can return both:
				// the folder moved but a registry write afterward failed, which is a
				// partial success, not the "nothing was moved" screen — that is the one
				// case where showing the failure screen would send the user looking for
				// an account that has, in fact, already moved.
				out.ArchiveDir = filepath.Base(dest)
				if err != nil {
					// RegistryNote, not the status line: the "removed" screen's only exits
					// are showList (which clears the status before anything renders) and
					// openArchive (which does not reload the panel at all), so a status
					// string set here would never be seen. This has to live on the VM the
					// screen itself draws from.
					out.RegistryNote = err.Error()
				}
			default:
				out.Err = err.Error()
			}
			setBusyStatus(false, "")
			mu.Lock()
			removedVM = out
			currentView = "removed"
			mu.Unlock()
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
			setView("debug")
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
func doBackupAll() int {
	bm := core.NewBackupManager("")
	n := 0
	for _, p := range mustFindProfiles() {
		if !core.ProfileHasSessions(p.Path) {
			continue
		}
		// CreateBackup, not BackupIfHasData: the user pressed a button that says it
		// backs things up, so it has to actually take a snapshot rather than reuse
		// yesterday's and report a number that means nothing.
		if _, err := bm.CreateBackup(p.Path); err == nil {
			n++
		}
	}
	return n
}

func itoa(n int) string {
	return strconv.Itoa(n)
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
func doSync(fromFolder, toFolder string) string {
	from, to := folderPath(fromFolder), folderPath(toFolder)
	if from == "" || to == "" {
		return "Sync failed: account not found."
	}
	// ManualAlign, not SyncSessions: it closes Claude Desktop before writing and
	// reopens the profile the user was on, and it snapshots the target first.
	// Calling SyncSessions directly wrote into a profile Claude was still running
	// on, which risks corrupting the session index it holds open, and skipped the
	// backup the README promises for every write.
	rep, err := switcher.ManualAlign(from, to)
	if err != nil {
		return core.SyncFailureMessage(err)
	}
	msg := core.SyncResultMessage(rep, core.DisplayName(toFolder))
	for _, e := range rep.SkipErrors {
		// The message says "see the log", so it has to actually be there.
		log.Printf("sync skipped a session file: %s", e)
	}
	if len(rep.SkipErrors) > 0 {
		// The panel is a transient popover and ManualAlign has just reopened
		// Claude Desktop, which takes the foreground and dismisses it, so this
		// status line is usually gone before it is read. Files that could not be
		// read are the outcome worth surviving that. A conflict is not: it only
		// means the target's copy was already newer, which is ordinary.
		notify("Some conversations were skipped", msg)
	}
	return msg
}

func setView(v string) {
	mu.Lock()
	currentView = v
	mu.Unlock()
}

// reloadPanel renders the current view and pushes it into the popover webview.
func reloadPanel() {
	mu.Lock()
	view := currentView
	mu.Unlock()

	var htmlStr string
	switch view {
	case "rescan":
		accounts := core.ScanAccounts(mustFindProfiles(), core.LoadPending())
		htmlStr = panelui.RenderRescan(accounts, panelui.ComputePreselect(accounts, core.LoadManaged()))
	case "sync":
		htmlStr = panelui.RenderSync(buildProfiles(), getStatus(), getBusy())
	case "rename":
		mu.Lock()
		f := renameFolder
		mu.Unlock()
		htmlStr = panelui.RenderAccount(accountVM(f))
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
			// outcome is unknown.
			setStatus(planErr.Error())
			setView("list")
			htmlStr = panelui.RenderList(buildProfiles(), newProfileSupported(), getStatus())
			break
		}
		htmlStr = panelui.RenderMerge(a, b, plan, getStatus(), getBusy())
	case "removed":
		mu.Lock()
		vm := removedVM
		mu.Unlock()
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
	c := C.CString(htmlStr)
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

func doSwitch(folder string) {
	if folder == "" {
		return
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
		return
	}
	_ = switcher.SafeSwitch(sourceProfilePath(target.Path, profiles), target.Path, target.Name)
}

// buildProfiles lists the managed accounts for the list view.
func buildProfiles() []panelui.ProfileVM {
	running, _ := plat.DetectRunningProfile()
	// Shared with the Windows host on purpose: this used to be a copy in each, and
	// the copies drifted — SignedIn was set in one and not the other, which left the
	// sync screen unable to offer any pair at all on macOS.
	return panelui.BuildProfiles(mustFindProfiles(), core.LoadManaged(), pendingFolders(), running, cachedPlan)
}

// accountVM finds the row the account screen is about. Built from the same list
// the panel shows, so the conversation count on the confirmation is the number
// the user was already looking at.
func accountVM(folder string) panelui.AccountVM {
	profiles := buildProfiles()
	vm := panelui.AccountVM{Folder: folder, Name: core.DisplayName(folder), OnlyOne: len(profiles) <= 1}
	for _, p := range profiles {
		if p.Folder == folder {
			vm.Name, vm.Convos, vm.Current = p.Name, p.Convos, p.Current
			break
		}
	}
	return vm
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
		return core.MergePlan{}, fmt.Errorf("one of those profiles is no longer there — run Rescan")
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
