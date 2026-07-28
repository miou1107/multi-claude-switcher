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
	"encoding/json"
	"fmt"
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
	panelView  = "list" // "list" | "rescan" | "settings" | "sync" | "rename"
	panelStale string   // profile folder being renamed

	panelPlanMu sync.Mutex
	panelPlan   = map[string]string{}

	panelStatusMu   sync.Mutex
	panelStatusMsg  string
	panelBusyStatus bool

	// runPanelReturned is set when runPanel's deferred cleanup (wv.Destroy,
	// log closer) has finished. hidePanelAndExit's fallback os.Exit is gated
	// on it so cleanup is not skipped in the common case where Run() unblocks
	// promptly.
	runPanelReturned atomic.Bool
)

// runPanel is the entry point when the binary is invoked as
// `mcs-tray.exe --panel`. It creates the WebView2 window, positions it near
// the tray icon, wires JS ↔ Go bindings, loads the initial view, and blocks
// on the WebView's message loop. On outside-click or explicit hide the loop
// exits and so does this process — the tray will spawn a new one on the next
// click.
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

	wv, err := newPanelWebView()
	if err != nil {
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
	// click (WM_ACTIVATE with WA_INACTIVE).
	hwnd := uintptr(wv.Window())
	makeBorderlessTopmost(hwnd)
	positionPanelNearTray(hwnd)
	installActivateHook(hwnd)

	reloadPanel()
	wv.Run()
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
		go func() {
			doSwitchPanel(arg)
			reloadPanel()
		}()
	case "showRescan":
		panelSetView("rescan")
		go reloadPanel()
	case "showList":
		panelSetView("list")
		go reloadPanel()
	case "showSettings":
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
		panelSetBusy(true, "Syncing…")
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
	case "openLog":
		home, _ := os.UserHomeDir()
		_ = exec.Command("explorer.exe", filepath.Join(home, ".multi-claude-switcher", "logs")).Start()
	case "openBackups":
		home, _ := os.UserHomeDir()
		_ = exec.Command("explorer.exe", filepath.Join(home, ".multi-claude-switcher", "backups")).Start()
	case "checkUpdates":
		if panelGetBusy() {
			return
		}
		panelSetBusy(true, "Checking for updates…")
		reloadPanel()
		go func() {
			// The tray runs the periodic auto-update. The panel just opens the
			// releases page so the user sees the latest — a full update flow
			// belongs on the persistent tray process, not this transient window.
			_ = exec.Command("cmd", "/c", "start", "https://github.com/miou1107/multi-claude-switcher/releases").Start()
			panelSetBusy(false, "Opened Releases page in your browser.")
			reloadPanel()
		}()
	case "hidePanel":
		hidePanelAndExit()
	case "quit":
		// Signal the tray to quit too by writing a sentinel to stdout, which
		// the parent (mcs-tray) reads. Then exit this panel.
		fmt.Fprintln(os.Stdout, "MCS_QUIT")
		hidePanelAndExit()
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
		accounts := core.ScanAccounts(panelMustFindProfiles())
		htmlStr = panelui.RenderRescan(accounts, panelui.ComputePreselect(accounts, core.LoadManaged()))
	case "sync":
		htmlStr = panelui.RenderSync(panelBuildProfiles(), panelGetStatus(), panelGetBusy())
	case "rename":
		htmlStr = panelui.RenderRename(folder, core.DisplayName(folder))
	case "settings":
		htmlStr = panelui.RenderSettings(panelui.SettingsVM{
			AutoSync:   core.AutoSyncOnSwitch(),
			StartLogin: core.LoginItemEnabled(),
			Version:    core.Version,
			Status:     panelGetStatus(),
			Busy:       panelGetBusy(),
		})
	default:
		htmlStr = panelui.RenderList(panelBuildProfiles())
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

func panelSetBusy(b bool, s string) {
	panelStatusMu.Lock()
	panelBusyStatus = b
	panelStatusMsg = s
	panelStatusMu.Unlock()
}

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
	profiles := panelMustFindProfiles()
	managed := core.LoadManaged()
	running, _ := panelPlat.DetectRunningProfile()
	var out []panelui.ProfileVM
	for _, p := range profiles {
		_, uErr := platform.GetProfileAccountUUID(p.Path)
		if !panelIncludesFolder(managed, p.Name, uErr == nil, p.Managed) {
			continue
		}
		vm := panelui.ProfileVM{
			Folder:  p.Name,
			Name:    core.DisplayName(p.Name),
			Current: p.Path == running,
			Plan:    panelCachedPlan(p.Path),
		}
		out = append(out, vm)
	}
	return out
}

func panelIncludesFolder(managed []string, folder string, hasLiveLogin, managedFlag bool) bool {
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
// account.
func doSwitchPanel(folder string) {
	if folder == "" {
		return
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
		return
	}
	_ = panelSwitcher.SafeSwitch(panelSourceProfilePath(target.Path, profiles), target.Path)
}

func panelSourceProfilePath(targetPath string, profiles []*platform.ProfileInfo) string {
	if running, err := panelPlat.DetectRunningProfile(); err == nil && running != "" && running != targetPath {
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
	return filepath.Join(panelPlat.AppSupportDir(), "Claude")
}

// doPanelBackupAll snapshots every profile that has session data.
func doPanelBackupAll() int {
	bm := core.NewBackupManager("")
	n := 0
	for _, p := range panelMustFindProfiles() {
		if path, err := bm.BackupIfHasData(p.Path); err == nil && path != "" {
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
	rep, err := core.SyncSessions(from, to)
	if err != nil {
		return "Sync failed: " + err.Error()
	}
	return fmt.Sprintf("✓ Copied %d session(s) into %s.", rep.CopiedCount, core.DisplayName(toFolder))
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
	procShellNotifyGetR  = syscall.NewLazyDLL("shell32.dll").NewProc("Shell_NotifyIconGetRect")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
)

const (
	// GetWindowLongPtrW / SetWindowLongPtrW nIndex values. These are negative
	// signed ints in the Win32 headers; we spell them out as 32-bit two's
	// complement so they fit into uintptr on all archs without overflow.
	gwlExStyle  = uintptr(0xFFFFFFEC) // -20
	gwlStyle    = uintptr(0xFFFFFFF0) // -16
	gwlpWndProc = uintptr(0xFFFFFFFC) // -4
	wsPopup     = 0x80000000
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
)

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

// positionPanelNearTray places the panel above the tray icon. Falls back to
// the bottom-right of the primary display.
func positionPanelNearTray(hwnd uintptr) {
	x, y := trayIconAnchor()
	// Anchor the panel's top-right at the icon's top-right, biased up above
	// the taskbar. The tray anchor helper subtracts panel dimensions already.
	procSetWindowPos.Call(
		hwnd,
		hwndTopmost,
		uintptr(x),
		uintptr(y),
		uintptr(panelWidth),
		uintptr(panelHeight),
		uintptr(swpShowWindow),
	)
}

// trayIconAnchor returns the top-left screen coords for the panel window.
// Currently uses the primary display fallback; the exact Shell_NotifyIconGetRect
// path needs the icon's registered NOTIFYICONIDENTIFIER (guid), which the
// tray owns — worth wiring after the first end-to-end pass.
func trayIconAnchor() (x, y int) {
	sx, _, _ := procGetSystemMetrics.Call(uintptr(smCXScreen))
	sy, _, _ := procGetSystemMetrics.Call(uintptr(smCYScreen))
	// Bottom-right, above the (assumed) 48px taskbar.
	x = int(sx) - panelWidth - trayIconMarginPx
	y = int(sy) - panelHeight - 48 - trayIconMarginPx
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
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

func panelWndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	if msg == wmActivate {
		// LOWORD(wparam) == WA_INACTIVE ⇒ window lost focus ⇒ hide + exit.
		if uint32(wparam)&0xFFFF == waInactive {
			// Defer to keep the message pump healthy; exit right after.
			go func() {
				time.Sleep(20 * time.Millisecond)
				hidePanelAndExit()
			}()
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
