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
	"github.com/miou1107/multi-claude-switcher/internal/panelui"
	"github.com/miou1107/multi-claude-switcher/platform"
)

var (
	plat     platform.Platform
	switcher *core.Switcher

	mu           sync.Mutex
	currentView  = "list" // "list" | "rescan" | "settings" | "sync" | "rename"
	renameFolder string   // the folder being renamed in the "rename" view

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

//export goPanelReady
func goPanelReady() { reloadPanel() }

//export goPanelWillOpen
func goPanelWillOpen() {
	setView("list") // always open to the account list
	setStatus("")   // clear any stale feedback
	// Render asynchronously so the popover appears instantly; the account-type
	// scan (a leveldb copy per profile) must not block the click on the main
	// thread. The webview keeps its previous content until the refresh lands.
	go reloadPanel()
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
		go func() {
			doSwitch(arg)
			reloadPanel()
		}()
	case "showRescan":
		setView("rescan")
		go reloadPanel()
	case "showList":
		setView("list")
		go reloadPanel()
	case "showSettings":
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
	case "openLog":
		home, _ := os.UserHomeDir()
		_ = exec.Command("open", filepath.Join(home, ".multi-claude-switcher", "logs")).Start()
	case "openBackups":
		home, _ := os.UserHomeDir()
		_ = exec.Command("open", filepath.Join(home, ".multi-claude-switcher", "backups")).Start()
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
	if owed == "" {
		return
	}
	log.Printf("quit while Claude was closed for an operation; reopening %s first", owed)
	if err := plat.LaunchProfile(owed); err != nil {
		log.Printf("could not reopen Claude Desktop on the way out: %v", err)
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
		accounts := core.ScanAccounts(mustFindProfiles())
		htmlStr = panelui.RenderRescan(accounts, panelui.ComputePreselect(accounts, core.LoadManaged()))
	case "sync":
		htmlStr = panelui.RenderSync(buildProfiles(), getStatus(), getBusy())
	case "rename":
		mu.Lock()
		f := renameFolder
		mu.Unlock()
		htmlStr = panelui.RenderRename(f, core.DisplayName(f))
	case "settings":
		htmlStr = panelui.RenderSettings(panelui.SettingsVM{
			AutoSync:   core.AutoSyncOnSwitch(),
			StartLogin: core.LoginItemEnabled(),
			Version:    core.Version,
			Status:     getStatus(),
			Busy:       getBusy(),
		})
	default:
		htmlStr = panelui.RenderList(buildProfiles(), false)
	}
	c := C.CString(htmlStr)
	defer C.free(unsafe.Pointer(c))
	C.LoadPanelHTML(c)
}

func main() {
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
	_ = switcher.SafeSwitch(sourceProfilePath(target.Path, profiles), target.Path)
}

// buildProfiles lists the managed accounts for the list view.
func buildProfiles() []panelui.ProfileVM {
	running, _ := plat.DetectRunningProfile()
	// Shared with the Windows host on purpose: this used to be a copy in each, and
	// the copies drifted — SignedIn was set in one and not the other, which left the
	// sync screen unable to offer any pair at all on macOS.
	return panelui.BuildProfiles(mustFindProfiles(), core.LoadManaged(), running, cachedPlan)
}

func sourceProfilePath(targetPath string, profiles []*platform.ProfileInfo) string {
	if running, err := plat.DetectRunningProfile(); err == nil && running != "" && running != targetPath {
		return running
	}
	for _, p := range profiles {
		if p.Path != targetPath && p.HasSessionsDir {
			return p.Path
		}
	}
	if len(profiles) > 0 {
		return profiles[0].Path
	}
	return filepath.Join(plat.AppSupportDir(), "Claude")
}
