//go:build windows

package main

// onready_windows.go is the Windows tray's minimal onReady. It mirrors the
// macOS menu bar: clicking the icon opens the panel directly, and the panel
// owns the whole UI (account list, switch, sync, rename, settings, quit).
//
// fyne.io/systray exposes a left-click hook (SetOnTapped); when one is set,
// its Windows WM_LBUTTONUP path calls the hook instead of popping the context
// menu. Right-click has no hook installed here, so it falls through to the
// library's default showMenu() and surfaces the one-item safety menu below.
//
// The right-click Quit is a deliberate deviation from macOS: if the WebView2
// runtime is missing or the panel fails to start, the panel's own Quit is
// unreachable and the user would otherwise have to kill the process from Task
// Manager.
//
// The panel is a second process (`mcs-tray.exe --panel`) that is started once,
// with the tray, and kept alive parked off-screen. A click moves an existing
// window rather than starting a program, which is the difference between an
// instant panel and a 1.5-2s wait. See
// docs/superpowers/specs/2026-07-28-windows-warm-panel-design.md.

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"fyne.io/systray"
	"github.com/miou1107/multi-claude-switcher/platform"
)

// panelReopenGuard suppresses the reopen that would otherwise follow a
// close-by-outside-click. Clicking the tray icon while the panel is open first
// deactivates the panel (which parks it), and only then delivers WM_LBUTTONUP
// to the tray. Without the guard the click would immediately ask for it back,
// so the icon could never toggle the panel shut.
const panelReopenGuard = 400 * time.Millisecond

// panelRespawnDelay is how long to wait before restarting a panel process that
// died on its own. It doubles up to panelRespawnDelayMax so a panel that
// cannot start (missing WebView2 runtime, say) does not spin.
const (
	panelRespawnDelay    = 2 * time.Second
	panelRespawnDelayMax = 5 * time.Minute
)

var (
	// panelShown tracks whether the panel is currently on screen, from the
	// MCS_SHOWN / MCS_HIDDEN messages it sends.
	panelShown atomic.Bool
	// panelHiddenAtNano is when the panel last reported parking itself, or 0.
	panelHiddenAtNano atomic.Int64

	// panelProcMu guards the handles to the live panel process.
	panelProcMu sync.Mutex
	panelStdin  io.WriteCloser
	panelProc   *os.Process

	// trayQuitting stops the respawn loop once the tray is on its way out.
	trayQuitting atomic.Bool
)

func onReadyWindowsPanel() {
	setTrayIcon()
	systray.SetTooltip("Multi-Claude Switcher")

	// Left click toggles the panel, the same single click as the macOS
	// NSPopover status button.
	systray.SetOnTapped(togglePanel)

	// Right click keeps a single-item safety menu (see file comment).
	mQuit := systray.AddMenuItem("Quit", "Quit Multi-Claude Switcher")

	go func() {
		<-mQuit.ClickedCh
		systray.Quit()
	}()

	// Start the panel now so the first click is as fast as the rest.
	go superviseWarmPanel()

	// Auto-update: the persistent tray owns the periodic check, so upgrades
	// still land silently even if the user never opens the panel.
	startUpdateChecker()

	// Store build: resume any pending first-login session migration.
	startMigrationWatcher()
}

// restoreProtocolHandler puts the claude:// handler back to the form Claude
// Desktop registers. The switcher repoints it at the active profile so a
// sign-in callback opens the right account, and owns putting it back.
func restoreProtocolHandler() {
	if err := platform.ReleaseProtocolHandlerHold(); err != nil {
		log.Printf("could not restore the claude:// handler: %v", err)
	}
}

// stopPanelProcess shuts the warm panel down. Called when the tray exits so no
// panel is left orphaned.
func stopPanelProcess() {
	trayQuitting.Store(true)
	panelProcMu.Lock()
	p := panelProc
	panelProc = nil
	panelProcMu.Unlock()
	if p != nil {
		_ = p.Kill()
	}
}

// superviseWarmPanel keeps exactly one panel process alive for as long as the
// tray runs, restarting it with backoff if it dies on its own.
func superviseWarmPanel() {
	delay := panelRespawnDelay
	for !trayQuitting.Load() {
		start := time.Now()
		if err := runWarmPanel(); err != nil {
			log.Printf("warm panel: %v", err)
		}
		if trayQuitting.Load() {
			return
		}
		// A panel that ran for a decent while and then died is an ordinary
		// crash: restart promptly. One that dies immediately is broken;
		// back off so it cannot spin.
		if time.Since(start) > time.Minute {
			delay = panelRespawnDelay
		}
		log.Printf("warm panel exited; restarting in %s", delay)
		time.Sleep(delay)
		if delay *= 2; delay > panelRespawnDelayMax {
			delay = panelRespawnDelayMax
		}
	}
}

// runWarmPanel starts one panel process and blocks until it exits.
func runWarmPanel() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot resolve own exe: %w", err)
	}
	// No --anchor: that is what tells the panel to start parked and wait for a
	// SHOW rather than appearing straight away.
	cmd := exec.Command(exe, "--panel")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	panelProcMu.Lock()
	panelStdin = stdin
	panelProc = cmd.Process
	panelProcMu.Unlock()

	readPanelMessages(stdout)

	err = cmd.Wait()

	panelProcMu.Lock()
	panelStdin = nil
	panelProc = nil
	panelProcMu.Unlock()
	panelShown.Store(false)

	return err
}

// togglePanel is the tray icon's left-click handler. It asks the panel to come
// into view, or does nothing when the click is the one that just dismissed it.
func togglePanel() {
	if !shouldSpawnPanel(panelShown.Load(), sincePanelClosed(time.Now())) {
		return
	}
	// Hand the panel the right to take the foreground, once per show. Right
	// after a tray click the shell owns the foreground, so without this the
	// panel cannot become active, and an inactive panel never receives the
	// deactivation that dismisses it. The grant is consumed by the panel's next
	// SetForegroundWindow, so it has to be reissued every time.
	allowPanelForeground()

	x, y, ok := cursorPos()
	if !ok {
		// Without a cursor position the panel falls back to the bottom-right
		// of the primary display, which is still better than not opening.
		x, y = 0, 0
	}
	if err := sendToPanel(fmt.Sprintf("SHOW %d,%d", x, y)); err != nil {
		log.Printf("show panel: %v", err)
	}
}

// allowPanelForeground grants the panel process the right to take the
// foreground. The tray may do this because the click made it the foreground
// process.
func allowPanelForeground() {
	panelProcMu.Lock()
	p := panelProc
	panelProcMu.Unlock()
	if p == nil {
		return
	}
	if ok, _, err := procAllowSetForeground.Call(uintptr(uint32(p.Pid))); ok == 0 {
		log.Printf("AllowSetForegroundWindow(%d) refused: %v", p.Pid, err)
	}
}

// shouldSpawnPanel decides whether a tray click should open the panel.
// Split out from togglePanel so the toggle rule is unit-testable without a
// tray, a display, or a real panel process.
func shouldSpawnPanel(shown bool, sinceClose time.Duration) bool {
	if shown {
		// The panel is up and has already lost focus to this very click, so it
		// is on its way out. Let it close; this click is the "toggle shut".
		return false
	}
	return sinceClose >= panelReopenGuard
}

// sincePanelClosed reports how long ago the panel last parked itself. It
// returns a duration far past panelReopenGuard when it has never been shown,
// so the first click always opens it.
func sincePanelClosed(now time.Time) time.Duration {
	closed := panelHiddenAtNano.Load()
	if closed == 0 {
		return time.Hour
	}
	return now.Sub(time.Unix(0, closed))
}

// sendToPanel writes one command line to the panel's stdin.
func sendToPanel(line string) error {
	panelProcMu.Lock()
	w := panelStdin
	panelProcMu.Unlock()
	if w == nil {
		return fmt.Errorf("panel is not running yet")
	}
	_, err := io.WriteString(w, line+"\n")
	return err
}

// readPanelMessages consumes the panel's stdout until it closes, tracking the
// panel's visibility and honouring a Quit chosen from inside the panel.
func readPanelMessages(r io.ReadCloser) {
	defer r.Close()
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		switch strings.TrimSpace(sc.Text()) {
		case "MCS_SHOWN":
			panelShown.Store(true)
		case "MCS_HIDDEN":
			panelShown.Store(false)
			panelHiddenAtNano.Store(time.Now().UnixNano())
		case "MCS_NEW_PROFILE":
			// The panel cannot run this itself: the flow shows native dialogs and
			// ends by relaunching the tray, both of which belong to this process.
			// The panel has already hidden itself, so the dialogs come up clean.
			// Still reachable from the tray menu; the panel now uses the in-panel
			// name screen instead.
			log.Println("Panel requested the add-an-account flow.")
			go runNewProfileFlow()
		case "MCS_MIGRATION_QUEUED":
			// The panel created a profile and queued its Store-build migration after
			// this process's boot-time watcher had already given up. Look again;
			// startMigrationWatcher is a no-op when nothing is queued.
			log.Println("Panel queued a profile migration; restarting the watcher.")
			startMigrationWatcher()
		case "MCS_CHECK_UPDATES":
			// The panel's "Check for updates" button. It has to run here: this
			// process owns the update lock and survives the install, which replaces
			// the executable and relaunches. checkForUpdate reports back through a
			// toast, so nothing has to travel the other way down this pipe.
			//
			// This is the only manual route on Windows. The tray menu's "Check for
			// Updates" item lives on the macOS onReady; onReadyWindowsPanel builds a
			// menu with Quit alone.
			log.Println("Panel requested an update check.")
			go checkForUpdate(false)
		case "MCS_QUIT":
			log.Println("Panel requested Quit; shutting down tray.")
			stopPanelProcess()
			systray.Quit()
			return
		}
	}
}

var procAllowSetForeground = user32.NewProc("AllowSetForegroundWindow")

// cursorPos returns the mouse position in physical screen pixels.
func cursorPos() (x, y int32, ok bool) {
	var pt point
	r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	if r == 0 {
		return 0, 0, false
	}
	return pt.X, pt.Y, true
}
