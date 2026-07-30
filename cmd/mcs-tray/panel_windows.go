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

	go reloadPanel()
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
		// Esc. Park rather than exit: the process is reused for the next show.
		if hwnd := panelHWND.Load(); hwnd != 0 {
			panelWV.Dispatch(func() { parkPanel(hwnd) })
		}
	case "quit":
		// Signal the tray to quit too by writing a sentinel to stdout, which
		// the parent (mcs-tray) reads. Then exit this panel.
		notifyTray("MCS_QUIT")
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
			Folder:   p.Name,
			Name:     core.DisplayName(p.Name),
			Current:  p.Path == running,
			Plan:     panelCachedPlan(p.Path),
			SignedIn: uErr == nil,
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
	if rep.ConflictCount > 0 {
		// The panel parks itself on losing focus and clears its status as it goes
		// (see parkPanel), and ManualAlign has just reopened Claude Desktop, which
		// takes the foreground. So the status line this returns is usually gone
		// before it is read. A clash is the one outcome the user has to know
		// about, so it also goes somewhere that outlives the panel.
		// notify, not notifyTray: notifyTray's protocol is a fixed set of literal
		// keywords the tray switches on, so it cannot carry text. notify spawns its
		// own toast and works from any process, panel included.
		notify("Sync finished with clashes", msg)
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
