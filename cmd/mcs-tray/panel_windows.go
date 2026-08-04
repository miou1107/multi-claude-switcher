//go:build windows

package main

// panel_windows.go is the WebView2 popup panel: a borderless topmost window
// that shows the same account panel HTML as the macOS NSPopover. It runs as a
// separate process (`mcs-tray.exe --panel`), spawned by the tray on left-click,
// so it can own its own COM apartment and message loop without fighting the
// systray for the main thread. On outside-click (WM_ACTIVATE WA_INACTIVE) the
// window hides and the process exits — the next tray click spawns a fresh
// panel, matching NSPopover's transient behavior.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/core/diagnostics"
	"github.com/miou1107/multi-claude-switcher/internal/clip"
	"github.com/miou1107/multi-claude-switcher/internal/panelui"
	"github.com/miou1107/multi-claude-switcher/platform"
)

const (
	panelWidth  = 400
	panelHeight = 540
)

var (
	panelWV       webview2.WebView
	panelPlat     platform.Platform
	panelSwitcher *core.Switcher

	panelMu    sync.Mutex
	panelView  = "list" // "list" | "rescan" | "settings" | "sync" | "rename" | "newprofile" | "merge" | "removed" | "debug"
	panelStale string   // profile folder being renamed

	// panelDebugComment survives the reload that every action triggers. Without
	// it, pressing Copy wipes what the user just typed.
	panelDebugComment string

	// panelDebugReportCache and panelDebugReportMasker are the report showDebug
	// most recently built, reused by copyDebug, reportProblem and reloadPanel's
	// own "debug" case rather than each rebuilding it. Building gathers every
	// profile's leveldb identity and session tree plus every log file's tail —
	// heavy enough that this repo already caches leveldb reads elsewhere — and
	// without this cache a single Copy or Report click gathered it twice: once
	// to act on, once again when the reload that follows redrew the same
	// screen.
	//
	// There used to be a panelDebugReportReady flag here, false from the
	// moment showDebug cleared the cache until the gather that followed
	// filled it back in, because the view switched to "debug" before the
	// gather had finished. That window is gone: showDebug now gathers to
	// completion (while Settings shows a busy banner, guarded by the same
	// busy flag as backup/sync/merge) and only then sets the view to "debug"
	// and reloads. By the time this view — or copyDebug, or reportProblem —
	// can be reached at all, the cache is already populated, so there is no
	// "not ready" state left to track or to refuse against.
	panelDebugReportCache  string
	panelDebugReportMasker *diagnostics.Masker

	// panelNewProfileVM carries the pending name screen's context between the
	// action that opened it and the render that draws it, including the validation
	// error from a rejected attempt.
	panelNewProfileVM panelui.NewProfileVM
	// panelMergeFolders is the pair being resolved in the "merge" view.
	panelMergeFolders [2]string
	// panelRemovedVM holds the outcome of the last removal, drawn by the
	// "removed" view.
	panelRemovedVM panelui.RemovedVM

	panelPlanMu sync.Mutex
	panelPlan   = map[string]string{}

	panelStatusMu   sync.Mutex
	panelStatusMsg  string
	panelBusyStatus bool
	// panelQuitWhenIdle records that Quit was pressed while an operation was
	// running. The operation reopens Claude on its way out, and the quit follows.
	panelQuitWhenIdle bool

	// runPanelReturned is set when runPanel's deferred cleanup (wv.Destroy,
	// log closer) has finished. hidePanelAndExit's fallback os.Exit is gated
	// on it so cleanup is not skipped in the common case where Run() unblocks
	// promptly.
	runPanelReturned atomic.Bool

	// panelActivated records that the panel has been the active window at
	// least once since the current show. It arms the close-on-deactivate rule
	// in panelWndProc, and is cleared on every park.
	panelActivated atomic.Bool

	// panelHWND is the panel window, published for the JS action handlers,
	// which have no other route to it.
	panelHWND atomic.Uintptr
)

// runPanel is the entry point when the binary is invoked as
// `mcs-tray.exe --panel`. It creates the WebView2 window, wires JS ↔ Go
// bindings, loads the initial view, and blocks on the WebView's message loop.
//
// The process is long-lived: the tray starts it once and keeps it, so a click
// only has to move a window that already exists. Dismissing the panel parks it
// off-screen rather than exiting. With `--anchor X,Y` it instead shows itself
// straight away, which is how it is driven when launched by hand.
func runPanel() {
	// Runs to completion in the caller — Run() below blocks until the panel
	// hides. The deferred cleanup order matters: wv.Destroy() first (needs
	// COM apartment alive), then the log closer, then flip the flag so
	// hidePanelAndExit's fallback knows cleanup happened.
	defer runPanelReturned.Store(true)

	if closer, _, err := core.SetupLogging("mcs-panel"); err == nil {
		defer closer.Close()
	}

	panelPlat = platform.New()
	panelSwitcher = core.NewSwitcher(panelPlat, core.NewBackupManager(""))

	// Must start before the WebView exists: the window it flashes on screen is
	// created and shown inside newPanelWebView.
	stopFlashGuard := suppressWebviewWindowFlash()
	defer stopFlashGuard()

	wv, err := newPanelWebView()
	if err != nil {
		stopFlashGuard()
		showRuntimeMissingDialog(err)
		os.Exit(2)
	}
	panelWV = wv
	defer wv.Destroy()

	wv.SetTitle("Multi-Claude Switcher")
	wv.SetSize(panelWidth, panelHeight, webview2.HintFixed)

	bindPanelHandlers(wv)

	// Windowless style: borderless, topmost, no taskbar entry. Install a
	// message-hook subclass on the HWND so we can hide-and-exit on outside
	// click (WM_ACTIVATE with WA_INACTIVE). The window is parked off-screen by
	// the flash guard throughout, so all of this happens out of sight while the
	// browser renders normally.
	hwnd := uintptr(wv.Window())
	panelHWND.Store(hwnd)
	makeBorderlessTopmost(hwnd)
	applyPanelSize(hwnd)
	installActivateHook(hwnd)

	reloadPanel()

	// The guard has done its job; from here the panel parks and unparks itself.
	stopFlashGuard()

	anchor, standalone := parseAnchorArg(os.Args)
	if standalone {
		// Launched by hand with an explicit anchor: show immediately.
		showPanelAt(hwnd, anchor)
	} else {
		// Started by the tray: stay parked until it asks for us.
		parkPanel(hwnd)
		log.Println("panel warm and parked; waiting for SHOW")
	}

	go readTrayCommands(hwnd, os.Stdin, standalone)
	wv.Run()
}

// readTrayCommands consumes the tray's commands on stdin for the life of the
// process. The only command is `SHOW x,y`.
//
// When the tray started us, stdin closing means the tray is gone — killed from
// Task Manager, or crashed — and a panel that kept running would be an
// invisible orphan holding a WebView2 open with no way to reach it. So we exit
// with it. A standalone panel is left alone: its stdin may be closed from the
// start, which is not a signal about anything.
func readTrayCommands(hwnd uintptr, r io.Reader, standalone bool) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "SHOW ") {
			continue
		}
		anchor, ok := parseAnchorPoint(strings.TrimPrefix(line, "SHOW "))
		if !ok {
			log.Printf("ignoring malformed command from tray: %q", line)
			continue
		}
		showPanelAt(hwnd, anchor)
	}
	if !standalone {
		log.Println("tray closed the command pipe; exiting so no orphan panel is left")
		hidePanelAndExit()
	}
}

// showPanelAt brings the panel into view at anchor.
//
// The window moves first and the content is refreshed behind it. Rebuilding
// the account list is not free — it scans profile directories and reads each
// one's stored plan — and doing it before the move put ~650 ms between the
// click and anything appearing, which is the delay this whole design exists to
// remove. What the panel shows at that instant is not stale in practice:
// parkPanel resets it to a freshly rendered account list on the way out, so the
// background refresh here is only catching changes made since.
func showPanelAt(hwnd uintptr, anchor point) {
	panelActivated.Store(false)
	positionPanelAt(hwnd, anchor)
	logPanelWindowState(hwnd, "after show")
	armCloseOnDeactivate(hwnd)
	notifyTray("MCS_SHOWN")

	go func() {
		// A profile that has since been signed in to is no longer pending, and the
		// panel is the only place that notices.
		profiles := panelMustFindProfiles()
		for _, f := range core.StalePending(core.LoadPending(), profiles) {
			_ = core.RemovePending(f)
		}
		reloadPanel()
	}()
}

// parkPanel moves the panel back off-screen. This is what dismissal does now:
// the window and its browser stay alive, ready for the next show.
//
// It also resets to the account list and re-renders while nobody is looking, so
// the next show has correct content to display from its first frame instead of
// reopening on whatever screen the user happened to leave (Settings, Rename, a
// sync result). Re-rendering doubles as a repaint, which matters because
// WebView2 can throttle a window that has been off-screen for a long time.
func parkPanel(hwnd uintptr) {
	panelActivated.Store(false)
	procSetWindowPos.Call(hwnd, 0,
		winCoord(offscreenPos), winCoord(offscreenPos),
		0, 0,
		uintptr(swpNoSize|swpNoZOrder|swpNoActivate))
	notifyTray("MCS_HIDDEN")

	panelSetView("list")
	panelSetStatus("")
	go reloadPanel()
}

// notifyTray sends one status line to the tray over stdout.
func notifyTray(msg string) {
	fmt.Fprintln(os.Stdout, msg)
}

// newPanelWebView constructs the WebView2 instance. If the WebView2 runtime is
// missing the returned error is used to drive a native MessageBox with an
// install link.
func newPanelWebView() (webview2.WebView, error) {
	// The library panics on missing runtime; recover and surface as an error.
	var wv webview2.WebView
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("webview2 runtime unavailable: %v", r)
			}
		}()
		wv = webview2.NewWithOptions(webview2.WebViewOptions{
			Debug: false,
			WindowOptions: webview2.WindowOptions{
				Title:  "Multi-Claude Switcher",
				Width:  panelWidth,
				Height: panelHeight,
				Center: false,
			},
			AutoFocus: true,
		})
	}()
	if err != nil {
		return nil, err
	}
	if wv == nil {
		return nil, fmt.Errorf("webview2: NewWithOptions returned nil (runtime not installed?)")
	}
	return wv, nil
}

// bindPanelHandlers exposes mcsAction to JS. The panel's shell() send() shim
// calls window.mcsAction(action, folder) — the same actions mac's
// goPanelAction handles.
func bindPanelHandlers(wv webview2.WebView) {
	if err := wv.Bind("mcsAction", func(action, arg string) {
		dispatchAction(action, arg)
	}); err != nil {
		panic(fmt.Errorf("bind mcsAction: %w", err))
	}
}

// dispatchAction runs a panel action on a background goroutine so the JS
// bridge callback returns immediately. Panel reloads are pushed via
// wv.Dispatch (WebView2's main-thread queue).
func dispatchAction(action, arg string) {
	switch action {
	case "switch":
		// Guarded like sync, backup and merge. Without this a second click starts
		// a second SafeSwitch alongside the first, and the two race over one
		// directory: both close Claude, both park the slot, and whichever
		// relaunches Claude first makes the other's rename fail, because Claude
		// recreates the very directory that rename was about to move into.
		if panelGetBusy() {
			return
		}
		panelSetBusy(true, "Closing Claude Desktop and switching…")
		reloadPanel()
		go func() {
			panelSetBusy(false, doSwitchPanel(arg))
			reloadPanel()
		}()
	case "showRescan":
		panelSetView("rescan")
		go reloadPanel()
	case "showList":
		panelSetView("list")
		panelSetStatus("") // a deliberate return to the list starts clean; the paths
		// that want a message set it and render the list themselves without this action
		go reloadPanel()
	case "showSettings":
		// Shared by the plain Settings gear and the Debug view's back button
		// (and its Esc equivalent), which pass the live comment textarea value
		// as arg so leaving Debug does not discard what was typed. showDebug no
		// longer clears the saved comment on entry, so this save is what makes
		// a later Debug visit come back with the text still there. Every other
		// caller passes "", which must not clobber a comment saved this way.
		if arg != "" {
			panelSetDebugComment(arg)
		}
		panelSetView("settings")
		panelSetStatus("")
		reloadPanel()
	case "showSync":
		panelSetView("sync")
		panelSetStatus("")
		go reloadPanel()
	case "sync":
		if panelGetBusy() {
			return
		}
		parts := strings.SplitN(arg, "|", 2)
		if len(parts) != 2 {
			return
		}
		panelSetBusy(true, "Closing Claude Desktop and syncing…")
		reloadPanel()
		go func() {
			panelSetBusy(false, doSyncPanel(parts[0], parts[1]))
			reloadPanel()
		}()
	case "confirmManaged":
		var folders []string
		_ = json.Unmarshal([]byte(arg), &folders)
		if len(folders) > 0 {
			_ = core.SetManaged(folders)
		}
		panelSetView("list")
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
		if panelGetBusy() {
			return
		}
		panelSetBusy(true, "Backing up…")
		reloadPanel()
		go func() {
			n := doPanelBackupAll()
			var msg string
			switch {
			case n == 0:
				msg = "No accounts had sessions to back up."
			case n == 1:
				msg = "✓ Backed up 1 account."
			default:
				msg = "✓ Backed up " + strconv.Itoa(n) + " accounts."
			}
			panelSetBusy(false, msg)
			reloadPanel()
		}()
	case "showRename":
		panelMu.Lock()
		panelStale = arg
		panelView = "rename"
		panelMu.Unlock()
		reloadPanel()
	case "renameSave":
		var pair []string
		if json.Unmarshal([]byte(arg), &pair) == nil && len(pair) == 2 {
			_ = core.SetProfileName(pair[0], pair[1])
		}
		panelSetView("list")
		go reloadPanel()
	case "showDebug":
		// Guarded like backup, sync and merge: the gather below is heavy (a
		// leveldb copy per profile, a session-tree walk, every log tail), and a
		// second click while one is already running must not start another.
		if panelGetBusy() {
			return
		}
		// Gathered to completion before the view switches, not after. The old
		// shape cleared the cache, switched to "debug" immediately, and
		// gathered on a goroutine — which rendered the (empty) debug view, with
		// its comment box, before the report existed. A user who started typing
		// what went wrong the instant they saw that box lost it the moment the
		// finished gather's reloadPanel redrew the same view from an empty
		// panelGetDebugComment(). Staying on the current view with a busy banner
		// keeps the comment box off screen until there is a finished report to
		// show next to it.
		panelSetBusy(true, "Gathering debug info…")
		reloadPanel()
		go func() {
			report, m := panelDebugReport()
			setPanelDebugReportCache(report, m)
			panelSetBusy(false, "")
			panelSetView("debug")
			reloadPanel()
		}()
	case "reportProblem":
		// Guarded like backup, sync and merge. Without this, mashing the button
		// stacked concurrent clip.Set/open calls.
		if panelGetBusy() {
			return
		}
		panelSetDebugComment(arg)
		report, m := getPanelDebugReportCache()
		panelSetBusy(true, "Copying report…")
		reloadPanel()
		go func() {
			full := diagnostics.AppendComment(report, arg, m)
			if err := clip.Set(full); err != nil {
				// The browser is not opened: an issue form with nothing to paste is
				// worse than no browser at all. The comment is left in place too —
				// this is exactly the moment the user still needs it, to retry Copy
				// or Report a problem once whatever broke clip.Set is fixed.
				panelSetBusy(false, "Couldn't copy the report: "+err.Error())
				reloadPanel()
				return
			}
			_ = openURL(diagnostics.IssueURL(arg, m))
			// The comment has done its job: it is in the clipboard body and in the
			// prefilled issue title. Clearing it here, rather than on Debug's next
			// entry, means a stale "still happening" does not sit in the box the
			// next time something unrelated goes wrong.
			panelSetDebugComment("")
			panelSetBusy(false, "Report copied. Paste it into the issue.")
			reloadPanel()
		}()
	case "copyDebug":
		if panelGetBusy() {
			return
		}
		panelSetDebugComment(arg)
		report, _ := getPanelDebugReportCache()
		panelSetBusy(true, "Copying…")
		reloadPanel()
		go func() {
			if err := clip.Set(report); err != nil {
				panelSetBusy(false, "Couldn't copy: "+err.Error())
			} else {
				panelSetBusy(false, "Copied.")
			}
			reloadPanel()
		}()
	case "openLog":
		_ = exec.Command("explorer.exe", core.LogDir()).Start()
	case "openBackups":
		home, _ := os.UserHomeDir()
		_ = exec.Command("explorer.exe", filepath.Join(home, ".multi-claude-switcher", "backups")).Start()
	case "openArchive":
		dir := panelPlat.ArchiveDir()
		// Create it first: until something has been archived the folder does not
		// exist, and explorer on a missing path opens the wrong place.
		_ = os.MkdirAll(dir, 0o755)
		_ = exec.Command("explorer.exe", dir).Start()
	case "checkUpdates":
		if panelGetBusy() {
			return
		}
		// Ask the tray to run the check. The update flow does belong on the
		// persistent process — it owns the update lock, and installing means
		// replacing the running executable and relaunching, which the process being
		// replaced cannot supervise. But this used to open the releases page and
		// stop there, which meant the silent installer could only ever be reached by
		// the background check: the tray's own "Check for Updates" item is macOS-only
		// (see onReadyWindowsPanel), so on Windows there was no button that fetched
		// anything, and the browser opened whether or not an update existed.
		//
		// The protocol is one-way, so the outcome cannot come back here.
		// checkForUpdate(false) reports every case — up to date, failed, unavailable
		// — through a toast, and that is where the answer appears.
		notifyTray("MCS_CHECK_UPDATES")
		panelSetStatus("Checking for updates… the result appears in a notification.")
		reloadPanel()
	case "hidePanel":
		// Esc. Park rather than exit: the process is reused for the next show.
		if hwnd := panelHWND.Load(); hwnd != 0 {
			panelWV.Dispatch(func() { parkPanel(hwnd) })
		}
	case "newProfile":
		// The add card: open the in-panel name screen on the plain add path (no
		// account to recover), the same flow the macOS host uses. This replaces the
		// old hand-off to the tray's native dialogs, so both hosts behave identically.
		panelMu.Lock()
		panelNewProfileVM = panelui.NewProfileVM{}
		panelMu.Unlock()
		panelSetView("newprofile")
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
			panelSetStatus("That account is no longer recoverable — run Rescan")
			panelSetView("list")
			go reloadPanel()
			return
		}
		panelMu.Lock()
		panelNewProfileVM = panelui.NewProfileVM{
			RecoverUUID:   arg,
			SuggestedName: recoverySuggestedName(row),
			Convos:        row.Convos,
		}
		panelMu.Unlock()
		panelSetView("newprofile")
		go reloadPanel()
	case "createProfile":
		var a []string
		if json.Unmarshal([]byte(arg), &a) != nil || len(a) != 2 {
			return
		}
		if panelGetBusy() {
			return
		}
		panelSetBusy(true, "Setting up…")
		reloadPanel()
		go func() {
			req := core.CreateProfileRequest{Name: a[0], RecoverUUID: a[1]}
			if req.RecoverUUID != "" {
				// Re-scan now rather than trusting anything the webview sent back: the
				// sources' paths must come from the scan current at the moment of copy.
				row, ok := ghostRow(req.RecoverUUID)
				if !ok || !row.Recoverable {
					panelSetBusy(false, "That account is no longer recoverable — run Rescan")
					panelSetView("list")
					reloadPanel()
					return
				}
				req.Sources = row.Sources
			}
			_, err := core.NewProfileCreator(panelPlat).Create(req)
			panelSetBusy(false, "")
			if err != nil {
				// Back to the same screen with the reason, and with the name the user
				// typed still in the field so they do not have to retype it.
				panelMu.Lock()
				panelNewProfileVM.SuggestedName = a[0]
				panelNewProfileVM.Err = err.Error()
				panelMu.Unlock()
				panelSetView("newprofile")
				reloadPanel()
				return
			}
			// The Store build's migration watcher runs in the tray process and only
			// starts if a migration was already queued at boot. A create from the
			// panel queues one afterwards, so ask the tray to pick it up.
			notifyTrayMigrationQueued()
			panelSetView("list")
			reloadPanel()
		}()
	case "showMerge":
		parts := strings.SplitN(arg, "|", 2)
		if len(parts) != 2 {
			return
		}
		panelMu.Lock()
		panelMergeFolders = [2]string{parts[0], parts[1]}
		panelMu.Unlock()
		panelSetStatus("")
		panelSetView("merge")
		go reloadPanel()
	case "mergeConfirm":
		// arg is "<keepIdentity>|<archiveIdentity>". Identities, not paths: the merge
		// resolves them itself.
		parts := strings.SplitN(arg, "|", 2)
		if len(parts) != 2 || panelGetBusy() {
			return
		}
		keep, archive := parts[0], parts[1]
		panelSetBusy(true, "Merging…")
		reloadPanel()
		go func() {
			if err := panelPlat.TerminateApp(); err != nil {
				panelSetBusy(false, err.Error())
				reloadPanel()
				return
			}
			_, err := core.MergeDuplicates(panelPlat, core.MergeRequest{
				KeepIdentity: keep, ArchiveIdentity: archive,
			})
			if err != nil {
				panelSetBusy(false, err.Error())
				reloadPanel()
				return
			}
			panelSetBusy(false, "Merged.")
			panelSetView("list")
			reloadPanel()
		}()
	case "removeProfile":
		// arg is the folder identity, straight from askRemove's confirm dialog (or
		// the failure screen's Try again, which round-trips the same value).
		if panelGetBusy() {
			return
		}
		folder := arg
		// Read before the goroutine starts: once RemoveProfile has moved the
		// directory, neither the display name nor the conversation count can be
		// looked up again — panelBuildProfiles no longer has a row for it.
		before := panelAccountVM(folder)
		panelSetBusy(true, "Removing…")
		reloadPanel()
		go func() {
			dest, err := core.RemoveProfile(panelPlat, folder)
			// The decision lives in panelui, shared with mcs-menubar, so the two
			// hosts cannot drift on what a clean removal, a partial failure and
			// an outright failure each do — the way this codebase already
			// shipped a platform difference once.
			outcome := panelui.DecideRemovalOutcome(folder, before.Name, before.Convos, dest, err)
			if outcome.ShowList {
				panelSetBusy(false, outcome.ListStatus)
				panelSetView("list")
			} else {
				panelSetBusy(false, "")
				panelMu.Lock()
				panelRemovedVM = outcome.Removed
				panelView = "removed"
				panelMu.Unlock()
			}
			reloadPanel()
		}()

	case "quit":
		if panelDeferQuitUntilIdle() {
			go reloadPanel()
			return
		}
		reopenClaudeIfWeOweIt()
		// Signal the tray to quit too by writing a sentinel to stdout, which
		// the parent (mcs-tray) reads. Then exit this panel.
		notifyTray("MCS_QUIT")
		hidePanelAndExit()
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
	owed := panelSwitcher.ClaimPendingRelaunch()
	if len(owed) == 0 {
		return
	}
	for _, p := range owed {
		log.Printf("quit while Claude was closed for an operation; reopening %s first", p)
		if err := panelPlat.LaunchProfile(p); err != nil {
			log.Printf("could not reopen Claude Desktop on %s on the way out: %v", p, err)
		}
	}
}

// reloadPanel builds the HTML for the current view and pushes it into the
// webview via the main-thread dispatch queue.
func reloadPanel() {
	panelMu.Lock()
	view := panelView
	folder := panelStale
	panelMu.Unlock()

	var htmlStr string
	switch view {
	case "rescan":
		accounts := core.ScanAccounts(panelMustFindProfiles(), core.LoadPending())
		htmlStr = panelui.RenderRescan(accounts, panelui.ComputePreselect(accounts, core.LoadManaged()))
	case "sync":
		htmlStr = panelui.RenderSync(panelBuildProfiles(), panelGetStatus(), panelGetBusy())
	case "rename":
		htmlStr = panelui.RenderAccount(panelAccountVM(folder))
	case "newprofile":
		panelMu.Lock()
		vm := panelNewProfileVM
		panelMu.Unlock()
		htmlStr = panelui.RenderNewProfile(vm)
	case "merge":
		panelMu.Lock()
		pair := panelMergeFolders
		panelMu.Unlock()
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
			panelSetStatus(planErr.Error())
			panelSetView("list")
			htmlStr = panelui.RenderList(panelBuildProfiles(), newProfileSupported(), panelGetStatus())
			break
		}
		htmlStr = panelui.RenderMerge(a, b, plan, panelGetStatus(), panelGetBusy())
	case "removed":
		panelMu.Lock()
		vm := panelRemovedVM
		panelMu.Unlock()
		htmlStr = panelui.RenderRemoved(vm)
	case "settings":
		htmlStr = panelui.RenderSettings(panelui.SettingsVM{
			AutoSync:   core.AutoSyncOnSwitch(),
			StartLogin: core.LoginItemEnabled(),
			Version:    core.Version,
			Status:     panelGetStatus(),
			Busy:       panelGetBusy(),
		})
	case "debug":
		// Reused, not rebuilt: showDebug is the only path into this view, and it
		// only sets the view once the gather that fills panelDebugReportCache
		// has already finished (see the doc comment on panelDebugReportCache),
		// so this always has a real report to draw. Rebuilding here as well as
		// in copyDebug/reportProblem was the double gather this replaced.
		report, _ := getPanelDebugReportCache()
		htmlStr = panelui.RenderDebug(panelui.DebugVM{
			Report:  report,
			Comment: panelGetDebugComment(),
			Status:  panelGetStatus(),
		})
	default:
		htmlStr = panelui.RenderList(panelBuildProfiles(), newProfileSupported(), panelGetStatus())
	}
	panelWV.Dispatch(func() { panelWV.SetHtml(htmlStr) })
}

func panelSetView(v string) {
	panelMu.Lock()
	panelView = v
	panelMu.Unlock()
}

func panelSetStatus(s string) {
	panelStatusMu.Lock()
	panelStatusMsg = s
	panelStatusMu.Unlock()
}

func panelGetStatus() string {
	panelStatusMu.Lock()
	defer panelStatusMu.Unlock()
	return panelStatusMsg
}

func panelSetDebugComment(s string) {
	panelMu.Lock()
	panelDebugComment = s
	panelMu.Unlock()
}

func panelGetDebugComment() string {
	panelMu.Lock()
	defer panelMu.Unlock()
	return panelDebugComment
}

// panelDebugReport builds the report and hands back the masker that produced
// it, so the user's comment and the issue title are masked with the same
// registrations rather than a fresh, empty one.
func panelDebugReport() (string, *diagnostics.Masker) {
	in := panelBuildDiagnostics()
	return diagnostics.Build(in), diagnostics.NewMaskerFor(in)
}

// setPanelDebugReportCache and getPanelDebugReportCache guard
// panelDebugReportCache / panelDebugReportMasker with the same mutex as the
// comment they are cached alongside.
func setPanelDebugReportCache(report string, m *diagnostics.Masker) {
	panelMu.Lock()
	panelDebugReportCache = report
	panelDebugReportMasker = m
	panelMu.Unlock()
}

func getPanelDebugReportCache() (string, *diagnostics.Masker) {
	panelMu.Lock()
	defer panelMu.Unlock()
	return panelDebugReportCache, panelDebugReportMasker
}

func panelSetBusy(b bool, s string) {
	panelStatusMu.Lock()
	panelBusyStatus = b
	// Once a quit is pending, the status belongs to the quit. Otherwise a later
	// progress update would replace "Finishing up, then quitting…" and the user
	// would think their click was ignored.
	if !panelQuitWhenIdle || !b {
		panelStatusMsg = s
	}
	leaving := !b && panelQuitWhenIdle
	panelStatusMu.Unlock()
	if leaving {
		// Quit was asked for while an operation held Claude closed. It has now
		// finished and reopened Claude through its own path, so this is the moment it
		// is safe to go.
		log.Printf("deferred quit: the operation finished, exiting now")
		notifyTray("MCS_QUIT")
		hidePanelAndExit()
	}
}

// panelDeferQuitUntilIdle records that the user asked to quit while an operation was
// running, and reports whether the quit was deferred.
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
func panelDeferQuitUntilIdle() bool {
	panelStatusMu.Lock()
	if !panelBusyStatus {
		panelStatusMu.Unlock()
		return false
	}
	already := panelQuitWhenIdle
	panelQuitWhenIdle = true
	panelStatusMsg = "Finishing up, then quitting…"
	panelStatusMu.Unlock()
	if already {
		return true // a second click; the timer from the first is still running
	}
	go func() {
		time.Sleep(panelQuitDeferTimeout)
		panelStatusMu.Lock()
		stillBusy := panelBusyStatus && panelQuitWhenIdle
		panelStatusMu.Unlock()
		if !stillBusy {
			return
		}
		log.Printf("deferred quit: operation still running after %s, leaving anyway", panelQuitDeferTimeout)
		reopenClaudeIfWeOweIt()
		notifyTray("MCS_QUIT")
		hidePanelAndExit()
	}()
	return true
}

// panelQuitDeferTimeout bounds how long Quit waits for an operation to finish. A
// sync of a large profile is seconds; anything approaching this is stuck, and the
// user's request to leave wins.
const panelQuitDeferTimeout = 30 * time.Second

func panelGetBusy() bool {
	panelStatusMu.Lock()
	defer panelStatusMu.Unlock()
	return panelBusyStatus
}

func panelMustFindProfiles() []*platform.ProfileInfo {
	p, _ := panelPlat.FindProfiles()
	return p
}

// panelBuildProfiles returns the managed accounts for the list view.
func panelBuildProfiles() []panelui.ProfileVM {
	running, _ := panelPlat.DetectRunningProfile()
	// Shared with the macOS host on purpose: see panelui.BuildProfiles.
	return panelui.BuildProfiles(panelMustFindProfiles(), core.LoadManaged(), panelPendingFolders(), running, panelCachedPlan)
}

// panelAccountVM finds the row the account screen is about. Built from the same
// list the panel shows, so the conversation count on the confirmation is the
// number the user was already looking at.
func panelAccountVM(folder string) panelui.AccountVM {
	profiles := panelBuildProfiles()
	vm := panelui.AccountVM{Folder: folder, Name: core.DisplayName(folder), OnlyOne: len(profiles) <= 1}
	for _, p := range profiles {
		if p.Folder == folder {
			vm.Name, vm.Convos, vm.Current = p.Name, p.Convos, p.Current
			break
		}
	}
	return vm
}

// panelPendingFolders is the folder names of profiles awaiting their one-time
// sign-in, so the list shows a freshly created profile even before it has an account.
func panelPendingFolders() []string {
	var out []string
	for _, e := range core.LoadPending() {
		out = append(out, e.Folder)
	}
	return out
}

// notifyTrayMigrationQueued tells the tray process to (re)start its post-sign-in
// migration watcher. A profile created from this panel queues its Store-build
// migration after the tray's boot-time watcher has already decided nothing was
// pending; this stdout line, read by readPanelMessages, makes the tray look again.
// startMigrationWatcher returns immediately when nothing is queued, so it is safe
// on the standalone build too.
func notifyTrayMigrationQueued() { notifyTray("MCS_MIGRATION_QUEUED") }

// profilePathFor resolves a profile identity to its real path, or "" when there is
// no such profile. A lookup, never a join: on the Store build a profile lives in
// the shared slot or under .mcs-profiles, and the data root is not AppSupportDir(),
// so joining a name onto it produces a path that does not exist.
func profilePathFor(identity string) string {
	for _, p := range panelMustFindProfiles() {
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
	running, _ := panelPlat.DetectRunningProfile()
	vm := panelui.MergeCandidateVM{
		Folder:  identity,
		Name:    core.DisplayName(identity),
		Plan:    panelCachedPlan(path),
		Current: path != "" && path == running,
	}
	if path == "" {
		return vm
	}
	if uuid, err := platform.GetProfileAccountUUID(path); err == nil {
		for _, p := range panelMustFindProfiles() {
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
// recovery step goes through this rather than trusting values round-tripped through
// the webview: the row carries the source profiles' paths, and a path is only valid
// for the scan that produced it.
func ghostRow(uuid string) (core.ScannedAccount, bool) {
	for _, a := range core.ScanAccounts(panelMustFindProfiles(), core.LoadPending()) {
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

// panelCachedPlan looks up the subscription plan, caching per-process.
func panelCachedPlan(path string) string {
	panelPlanMu.Lock()
	p, ok := panelPlan[path]
	panelPlanMu.Unlock()
	if ok {
		return p
	}
	p, _ = core.DetectPlan(path)
	panelPlanMu.Lock()
	panelPlan[path] = p
	panelPlanMu.Unlock()
	return p
}

// doSwitchPanel closes the running Claude and reopens it with the target
// account, returning the status line for the panel.
//
// The error SafeSwitch returns used to be discarded here, and that is how a
// switch that had actually failed — a rename that never landed, a profile left
// parked under .mcs-profiles — looked exactly like one that worked. The user
// found out later, from an account list that had gone strange. Report it.
func doSwitchPanel(folder string) string {
	if folder == "" {
		return ""
	}
	profiles := panelMustFindProfiles()
	var target *platform.ProfileInfo
	for _, p := range profiles {
		if p.Name == folder {
			target = p
			break
		}
	}
	if target == nil {
		return "Switch failed: account not found."
	}
	if err := panelSwitcher.SafeSwitch(panelSourceProfilePath(target.Path, profiles), target.Path, target.Name); err != nil {
		log.Printf("switch to %s failed: %v", folder, err)
		return "Switch failed: " + err.Error()
	}
	return "✓ Switched to " + core.DisplayName(folder) + "."
}

func panelSourceProfilePath(targetPath string, profiles []*platform.ProfileInfo) string {
	return core.SourceProfilePath(panelPlat, targetPath, profiles)
}

// doPanelBackupAll snapshots every profile that has session data.
func doPanelBackupAll() int {
	bm := core.NewBackupManager("")
	n := 0
	for _, p := range panelMustFindProfiles() {
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

// doSyncPanel copies one account's Code sessions into another.
func doSyncPanel(fromFolder, toFolder string) string {
	from := panelFolderPath(fromFolder)
	to := panelFolderPath(toFolder)
	if from == "" || to == "" {
		return "Sync failed: account not found."
	}
	// ManualAlign, not SyncSessions: it closes Claude Desktop before writing and
	// reopens the profile the user was on, and it snapshots the target first.
	// Calling SyncSessions directly wrote into a profile Claude was still running
	// on, which risks corrupting the session index it holds open, and skipped the
	// backup the README promises for every write.
	rep, err := panelSwitcher.ManualAlign(from, to)
	if err != nil {
		return core.SyncFailureMessage(err)
	}
	msg := core.SyncResultMessage(rep, core.DisplayName(toFolder))
	for _, e := range rep.SkipErrors {
		// The message says "see the log", so it has to actually be there.
		log.Printf("sync skipped a session file: %s", e)
	}
	if len(rep.SkipErrors) > 0 {
		// The panel parks itself on losing focus and clears its status as it goes
		// (see parkPanel), and ManualAlign has just reopened Claude Desktop, which
		// takes the foreground. So this status line is usually gone before it is
		// read. Files that could not be read are the outcome worth surviving that.
		// A conflict is not: it only means the target's copy was already newer.
		//
		// notify, not notifyTray: notifyTray's protocol is a fixed set of literal
		// keywords the tray switches on, so it cannot carry text. notify spawns its
		// own toast and works from any process, panel included.
		notify("Some conversations were skipped", msg)
	}
	return msg
}

func panelFolderPath(folder string) string {
	for _, p := range panelMustFindProfiles() {
		if p.Name == folder {
			return p.Path
		}
	}
	return ""
}

// hidePanelAndExit hides the window and terminates the WebView's message loop,
// causing runPanel() to return and this process to exit. The next tray click
// will spawn a new panel.
//
// The fallback os.Exit is only used if Run() has not unblocked within 2s. It
// is gated on runPanelReturned so the common case still runs the deferred
// cleanup (wv.Destroy, log closer) — Terminate on a healthy WebView returns
// well under 2s.
func hidePanelAndExit() {
	if panelWV != nil {
		panelWV.Dispatch(func() { panelWV.Terminate() })
	}
	go func() {
		time.Sleep(2 * time.Second)
		if !runPanelReturned.Load() {
			os.Exit(0)
		}
	}()
}

// ---- Win32 window shaping and hooking (raw syscalls, no CGO) ----

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procGetWindowLongPtr = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtr = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procCallWindowProc   = user32.NewProc("CallWindowProcW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procMonitorFromRect  = user32.NewProc("MonitorFromRect")
	procGetMonitorInfo   = user32.NewProc("GetMonitorInfoW")

	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procAttachThreadInput   = user32.NewProc("AttachThreadInput")
	procBringWindowToTop    = user32.NewProc("BringWindowToTop")
	procGetCurrentThreadID  = syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentThreadId")
	procFindWindowEx        = user32.NewProc("FindWindowExW")
	procGetWindowThreadPID  = user32.NewProc("GetWindowThreadProcessId")
)

// point and rect mirror the Win32 POINT and RECT structs.
type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

// monitorInfo mirrors Win32 MONITORINFO. RcWork is the monitor's usable area,
// i.e. the full bounds minus the taskbar and any other appbars.
type monitorInfo struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
}

const (
	// GetWindowLongPtrW / SetWindowLongPtrW nIndex values. These are negative
	// signed ints in the Win32 headers; we spell them out as 32-bit two's
	// complement so they fit into uintptr on all archs without overflow.
	gwlExStyle       = uintptr(0xFFFFFFEC) // -20
	gwlStyle         = uintptr(0xFFFFFFF0) // -16
	gwlpWndProc      = uintptr(0xFFFFFFFC) // -4
	wsPopup          = 0x80000000
	wsExToolWindow   = 0x00000080
	wsExTopMost      = 0x00000008
	wsExNoActivate   = 0x08000000
	swpNoSize        = 0x0001
	swpNoZOrder      = 0x0004
	swpNoActivate    = 0x0010
	swpShowWindow    = 0x0040
	smCXScreen       = 0
	smCYScreen       = 1
	hwndTopmost      = ^uintptr(0) // -1
	wmActivate       = 0x0006
	waInactive       = 0
	trayIconMarginPx = 8

	swpNoMove       = 0x0002
	swpFrameChanged = 0x0020

	monitorDefaultToNearest = 0x00000002
	// assumedTaskbarPx is only used when the monitor query fails and we have to
	// guess how much of the primary display the taskbar covers.
	assumedTaskbarPx = 48
)

// panelAnchorFlag is the argument the tray uses to tell the panel where the
// tray icon was clicked, e.g. `mcs-tray.exe --panel --anchor 1893,1049`.
const panelAnchorFlag = "--anchor"

// makeBorderlessTopmost strips the frame/caption from the WebView2 window and
// makes it topmost with no taskbar entry — the closest Win32 gets to
// NSPopover's frameless popup.
func makeBorderlessTopmost(hwnd uintptr) {
	style, _, _ := procGetWindowLongPtr.Call(hwnd, gwlStyle)
	// Preserve visibility bits (WS_VISIBLE, WS_CHILD) but drop the caption/frame.
	const wsOverlappedWindow = 0x00CF0000
	style &^= wsOverlappedWindow
	style |= wsPopup
	procSetWindowLongPtr.Call(hwnd, gwlStyle, style)

	ex, _, _ := procGetWindowLongPtr.Call(hwnd, gwlExStyle)
	ex |= wsExToolWindow | wsExTopMost
	procSetWindowLongPtr.Call(hwnd, gwlExStyle, ex)
}

// applyPanelSize fixes the window's size while it is still parked off-screen,
// so the browser has already re-laid out by the time the panel comes into view.
//
// The size matters as much as the position. wv.SetSize ran while the window
// still had a frame, so it called AdjustWindowRect and made the window larger
// than panelWidth×panelHeight to leave room for the caption and borders.
// makeBorderlessTopmost has since removed those, so that slack is now bare
// client area the WebView2 control does not cover, and it paints black down the
// right and bottom edges. Resizing back to panelWidth×panelHeight makes
// client == control again; SWP_FRAMECHANGED makes the frame removal take
// effect, and the resulting WM_SIZE is what go-webview2 uses to resize the
// control.
func applyPanelSize(hwnd uintptr) {
	procSetWindowPos.Call(
		hwnd,
		hwndTopmost,
		0, 0, // ignored because of SWP_NOMOVE
		panelWidth,
		panelHeight,
		uintptr(swpNoMove|swpNoActivate|swpFrameChanged),
	)
}

// positionPanelAt moves the panel from its off-screen parking spot to beside
// the tray icon the user just clicked, the way NSPopover attaches to the macOS
// status button. This is the moment the panel becomes visible to the user, so
// it runs last, once the content has loaded.
func positionPanelAt(hwnd uintptr, anchor point) {
	if anchor == (point{}) {
		// The tray could not read the cursor. Fall back to the bottom-right
		// corner rather than pinning the panel to the top-left origin.
		fb := primaryWorkAreaFallback()
		anchor = point{X: fb.Right, Y: fb.Bottom}
	}
	work := workAreaAt(anchor)
	x, y := panelPlacement(anchor, work, panelWidth, panelHeight)

	procSetWindowPos.Call(
		hwnd,
		hwndTopmost,
		winCoord(x), // may be negative on a monitor left of the primary
		winCoord(y),
		0, 0, // ignored because of SWP_NOSIZE
		uintptr(swpNoSize|swpShowWindow),
	)
	forcePanelForeground(hwnd)
}

// forcePanelForeground makes the panel the active window.
//
// This is required rather than cosmetic: the outside-click dismissal works by
// watching for WM_ACTIVATE/WA_INACTIVE, which only ever arrives if the window
// was active first. A plain SetForegroundWindow is not enough. Windows refuses
// foreground changes from a process that does not own the foreground, and even
// with the tray passing the right over via AllowSetForegroundWindow the call
// was observed succeeding only about half the time.
//
// Attaching this thread's input queue to the current foreground window's thread
// puts both threads in the same input state for the duration, which is what
// makes the request legal. The attachment is undone immediately afterwards, and
// is skipped entirely when the foreground window is already ours.
func forcePanelForeground(hwnd uintptr) {
	fg, _, _ := procGetForegroundWindow.Call()
	if fg == 0 || fg == hwnd {
		procSetForegroundWindow.Call(hwnd)
		return
	}

	fgThread, _, _ := procGetWindowThreadPID.Call(fg, 0)
	self, _, _ := procGetCurrentThreadID.Call()
	if fgThread == 0 || fgThread == self {
		procSetForegroundWindow.Call(hwnd)
		return
	}

	procAttachThreadInput.Call(self, fgThread, 1)
	procSetForegroundWindow.Call(hwnd)
	procBringWindowToTop.Call(hwnd)
	procAttachThreadInput.Call(self, fgThread, 0)
}

// webviewClassName is the window class go-webview2 registers for the window it
// creates. Matched by suppressWebviewWindowFlash.
const webviewClassName = "webview"

// offscreenPos is where the panel window is parked while the browser is being
// embedded. Comfortably outside any real monitor, and within the range Win32
// window coordinates accept.
const offscreenPos int32 = -32000

// winCoord converts a signed screen coordinate into the uintptr form
// SetWindowPos expects. Negative values (a monitor left of or above the
// primary, or the off-screen park position) stay intact in the low 32 bits,
// which is what the Win32 side reads. Written as a function because Go rejects
// the conversion on a negative constant.
func winCoord(v int32) uintptr { return uintptr(uint32(v)) }

// suppressWebviewWindowFlash keeps go-webview2's window out of sight until the
// panel is styled, positioned and filled. It returns a stop function, which is
// idempotent and must be called before showPanelWindow.
//
// Why a watcher at all: NewWithOptions creates the window, calls ShowWindow
// itself, and only then embeds the browser, which takes long enough for the
// empty window to be seen. The handle is not available until all of that
// returns, WindowOptions has no position or visibility field, and the
// WebViewOptions.Window field that would let us supply our own window is
// accepted but never read in this version. Watching from another goroutine is
// the only way to get at the window early without forking the library.
//
// Why it moves the window rather than hiding it: hiding does both too little
// and too much. Too little, because the library calls ShowWindow (and
// UpdateWindow, and SetFocus) right after creating the window, so a hide can
// land before the show and the window blinks at the default position anyway.
// Too much, because WebView2 stops rendering while its host window is hidden
// and does not resume on its own when the window comes back, which leaves a
// correctly placed, correctly sized, entirely blank panel.
//
// Moving sticks and has neither problem: the window stays visible and keeps
// rendering, just at coordinates no monitor covers, and nothing in the library
// ever moves the window (SetSize passes SWP_NOMOVE).
func suppressWebviewWindowFlash() (stop func()) {
	done := make(chan struct{})
	var once sync.Once

	go func() {
		class, err := syscall.UTF16PtrFromString(webviewClassName)
		if err != nil {
			return
		}
		for {
			select {
			case <-done:
				return
			default:
			}
			if hwnd, ok := findOwnWindowByClass(class); ok {
				// Tool window first, and before anything else: the library
				// creates a plain WS_OVERLAPPEDWINDOW, which earns a taskbar
				// button. Leaving that until the styling step further down
				// means a button appears and vanishes again on every open.
				// The shell adds buttons asynchronously, so setting the style
				// this early usually beats it to the punch.
				ex, _, _ := procGetWindowLongPtr.Call(hwnd, gwlExStyle)
				procSetWindowLongPtr.Call(hwnd, gwlExStyle, ex|wsExToolWindow|wsExTopMost)

				procSetWindowPos.Call(hwnd, 0,
					winCoord(offscreenPos), winCoord(offscreenPos),
					0, 0,
					uintptr(swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged))
				return
			}
			// Tight enough that the window is parked within a fraction of a
			// frame of being created, cheap enough to not matter.
			time.Sleep(200 * time.Microsecond)
		}
	}()

	return func() { once.Do(func() { close(done) }) }
}

// findOwnWindowByClass returns the first top-level window of the given class
// that belongs to this process. The class name is not unique across processes:
// any other go-webview2 app registers the same one, so the owner is checked.
func findOwnWindowByClass(class *uint16) (uintptr, bool) {
	self := uint32(os.Getpid())
	var prev uintptr
	for {
		hwnd, _, _ := procFindWindowEx.Call(0, prev, uintptr(unsafe.Pointer(class)), 0)
		if hwnd == 0 {
			return 0, false
		}
		var pid uint32
		procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == self {
			return hwnd, true
		}
		prev = hwnd
	}
}

// parseAnchorArg extracts the "--anchor X,Y" screen point used when the panel
// is launched by hand. ok is false when the flag is absent or malformed, which
// is the normal case: the tray starts the panel without one and drives it with
// SHOW commands instead.
func parseAnchorArg(args []string) (point, bool) {
	for i, a := range args {
		if a != panelAnchorFlag || i+1 >= len(args) {
			continue
		}
		return parseAnchorPoint(args[i+1])
	}
	return point{}, false
}

// parseAnchorPoint parses an "X,Y" screen point. Shared by the --anchor flag
// and the tray's SHOW command so both accept exactly the same thing.
func parseAnchorPoint(s string) (point, bool) {
	xs, ys, found := strings.Cut(s, ",")
	if !found {
		return point{}, false
	}
	x, xErr := strconv.Atoi(strings.TrimSpace(xs))
	y, yErr := strconv.Atoi(strings.TrimSpace(ys))
	if xErr != nil || yErr != nil {
		return point{}, false
	}
	return point{X: int32(x), Y: int32(y)}, true
}

// panelPlacement computes the panel's top-left corner for a tray click at
// anchor. work is the monitor's usable area, which already excludes the
// taskbar, so keeping the panel inside it is all that is needed to handle a
// taskbar on any edge.
func panelPlacement(anchor point, work rect, w, h int32) (x, y int32) {
	// Centre on the click horizontally, as NSPopover centres on the status
	// button, then pull the panel back inside the work area.
	x = anchor.X - w/2
	if maxX := work.Right - w - trayIconMarginPx; x > maxX {
		x = maxX
	}
	if minX := work.Left + trayIconMarginPx; x < minX {
		x = minX
	}

	// The tray lives at whichever edge holds the taskbar, so open the panel on
	// the inner side of the click: upward for a bottom taskbar, downward for a
	// top one.
	if anchor.Y > (work.Top+work.Bottom)/2 {
		y = work.Bottom - h - trayIconMarginPx
	} else {
		y = work.Top + trayIconMarginPx
	}
	if maxY := work.Bottom - h - trayIconMarginPx; y > maxY {
		y = maxY
	}
	if minY := work.Top + trayIconMarginPx; y < minY {
		y = minY
	}
	return x, y
}

// workAreaAt returns the usable area of the monitor holding pt.
func workAreaAt(pt point) rect {
	// MonitorFromRect takes its rect by pointer. MonitorFromPoint would be the
	// natural call but passes POINT by value, whose register packing differs
	// between 32- and 64-bit builds.
	r := rect{Left: pt.X, Top: pt.Y, Right: pt.X + 1, Bottom: pt.Y + 1}
	mon, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), monitorDefaultToNearest)
	if mon != 0 {
		var mi monitorInfo
		mi.CbSize = uint32(unsafe.Sizeof(mi))
		if ok, _, _ := procGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi))); ok != 0 {
			return mi.RcWork
		}
	}
	return primaryWorkAreaFallback()
}

// primaryWorkAreaFallback guesses the primary display's usable area when the
// monitor query fails.
func primaryWorkAreaFallback() rect {
	sx, _, _ := procGetSystemMetrics.Call(uintptr(smCXScreen))
	sy, _, _ := procGetSystemMetrics.Call(uintptr(smCYScreen))
	return rect{Left: 0, Top: 0, Right: int32(sx), Bottom: int32(sy) - assumedTaskbarPx}
}

// installActivateHook subclasses the WebView2 window so we can react to
// WM_ACTIVATE (WA_INACTIVE) by exiting — the analogue of NSPopover's
// transient behavior.
var origWndProc uintptr

func installActivateHook(hwnd uintptr) {
	// Retain the CGo-free callback for the lifetime of the process (this file
	// is short-lived: one panel = one process).
	cb := syscall.NewCallback(panelWndProc)
	old, _, _ := procSetWindowLongPtr.Call(hwnd, gwlpWndProc, cb)
	origWndProc = old
	runtime.KeepAlive(cb)
}

// activateReason names the WM_ACTIVATE wparam values for the log.
func activateReason(wparam uintptr) string {
	switch uint32(wparam) & 0xFFFF {
	case waInactive:
		return "WA_INACTIVE"
	case 1:
		return "WA_ACTIVE"
	case 2:
		return "WA_CLICKACTIVE"
	}
	return "unknown"
}

// logPanelWindowState records where the panel window ended up and whether it is
// actually on screen and focused. The panel is a short-lived GUI process with
// no console, so without this a panel that starts but never becomes visible
// leaves nothing in the log but its startup banner.
func logPanelWindowState(hwnd uintptr, when string) {
	var r rect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	visible, _, _ := procIsWindowVisible.Call(hwnd)
	fg, _, _ := procGetForegroundWindow.Call()
	log.Printf("panel window %s: rect=(%d,%d)-(%d,%d) visible=%v foreground=%v",
		when, r.Left, r.Top, r.Right, r.Bottom, visible != 0, fg == hwnd)
}

// armCloseOnDeactivate enables the outside-click dismissal once the panel is
// confirmed to be the foreground window.
//
// Arming on WM_ACTIVATE alone is not reliable here: the window is created,
// focused and hidden before the message hook is installed, so re-showing it
// need not produce a fresh WM_ACTIVATE for the hook to see, and the panel would
// then never dismiss itself. Polling the foreground window is what actually
// reflects the state we care about. If the panel never reaches the foreground
// the rule stays disarmed on purpose: a panel that was never active will never
// receive a genuine deactivation either, and Esc still closes it.
func armCloseOnDeactivate(hwnd uintptr) {
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if fg, _, _ := procGetForegroundWindow.Call(); fg == hwnd {
				panelActivated.Store(true)
				log.Println("panel reached the foreground; outside-click dismissal armed")
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		log.Println("panel never reached the foreground; outside-click dismissal stays disarmed (Esc still closes)")
	}()
}

func panelWndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	if msg == wmActivate {
		log.Printf("panel WM_ACTIVATE wparam=%#x (%s)", wparam, activateReason(wparam))
		if uint32(wparam)&0xFFFF == waInactive {
			// WA_INACTIVE means the window lost focus, which is the outside
			// click that should dismiss the panel — but only once the panel
			// has genuinely been the active window. Straight after a tray
			// click the shell is still settling the foreground, and acting on
			// that first deactivation would close the panel before the user
			// ever sees it.
			if panelActivated.Load() {
				// Defer to keep the message pump healthy; park right after.
				go func() {
					time.Sleep(20 * time.Millisecond)
					panelWV.Dispatch(func() { parkPanel(hwnd) })
				}()
			} else {
				log.Println("panel deactivated before it was ever active; ignoring")
			}
		} else {
			panelActivated.Store(true)
		}
	}
	// Delegate everything else to the original WebView2 WndProc.
	ret, _, _ := procCallWindowProc.Call(origWndProc, hwnd, uintptr(msg), wparam, lparam)
	return ret
}

// showRuntimeMissingDialog surfaces a native MessageBox when WebView2 runtime
// is absent, and opens the Microsoft install page in the default browser.
func showRuntimeMissingDialog(cause error) {
	msg := "The Multi-Claude Switcher panel needs the Microsoft WebView2 " +
		"Runtime, which is not installed.\n\nClick OK to open the Microsoft " +
		"download page in your browser. Rerun Multi-Claude Switcher after " +
		"installing.\n\n(" + cause.Error() + ")"
	msgw, _ := syscall.UTF16PtrFromString(msg)
	titlew, _ := syscall.UTF16PtrFromString("Multi-Claude Switcher — WebView2 Runtime missing")
	const mbOK = 0x00000000
	const mbIconInfo = 0x00000040
	messageBox := user32.NewProc("MessageBoxW")
	messageBox.Call(0, uintptr(unsafe.Pointer(msgw)), uintptr(unsafe.Pointer(titlew)), uintptr(mbOK|mbIconInfo))
	_ = exec.Command("cmd", "/c", "start", "https://developer.microsoft.com/en-us/microsoft-edge/webview2/").Start()
}
