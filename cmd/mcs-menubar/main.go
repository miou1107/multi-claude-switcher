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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
)

func setBusyStatus(b bool, s string) {
	statusMu.Lock()
	busy = b
	statusMsg = s
	statusMu.Unlock()
}

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
		C.TerminateApp()
	}
}

// doBackupAll backs up every profile that has session data and returns how many
// were backed up.
func doBackupAll() int {
	bm := core.NewBackupManager("")
	n := 0
	for _, p := range mustFindProfiles() {
		if path, err := bm.BackupIfHasData(p.Path); err == nil && path != "" {
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
	if rep.ConflictCount > 0 {
		// The panel is a transient popover and ManualAlign has just reopened
		// Claude Desktop, which takes the foreground and dismisses it. So the
		// status line this returns is usually gone before it is read. A clash is
		// the one outcome the user has to know about, so it also goes somewhere
		// that outlives the panel.
		notify("Sync finished with clashes", msg)
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
		htmlStr = panelui.RenderList(buildProfiles())
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
	profiles := mustFindProfiles()
	managed := core.LoadManaged()
	running, _ := plat.DetectRunningProfile()
	var out []panelui.ProfileVM
	for _, p := range profiles {
		_, uErr := platform.GetProfileAccountUUID(p.Path)
		if !panelIncludes(managed, p.Name, uErr == nil, p.Managed) {
			continue
		}
		vm := panelui.ProfileVM{Folder: p.Name, Name: core.DisplayName(p.Name), Current: p.Path == running, Plan: cachedPlan(p.Path)}
		out = append(out, vm)
	}
	return out
}

func panelIncludes(managed []string, folder string, hasLiveLogin, managedFlag bool) bool {
	if managed != nil {
		for _, m := range managed {
			if m == folder {
				return true
			}
		}
		return false
	}
	return hasLiveLogin || managedFlag
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
