//go:build windows

package main

// onready_windows.go is the Windows tray's minimal onReady: it shows the
// icon and a small right-click menu (Show panel / Quit / About). The
// account list, switch, sync, rename, settings — everything that used to
// live in the tray context menu — is now inside the WebView2 popup panel
// spawned by `mcs-tray.exe --panel`.
//
// getlantern/systray on Windows does not expose a per-icon left-click hook,
// so left-click opens the same context menu; picking "Show panel" opens the
// popup. This is one extra click vs. macOS's NSPopover, and is the shortest
// path to shipping without forking systray.

import (
	"bufio"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/getlantern/systray"
)

func onReadyWindowsPanel() {
	setTrayIcon()
	systray.SetTooltip("Multi-Claude Switcher")

	mShow := systray.AddMenuItem("Show panel", "Open the Multi-Claude Switcher panel")
	systray.AddSeparator()
	mAbout := systray.AddMenuItem("About Multi-Claude Switcher", "About")
	mQuit := systray.AddMenuItem("Quit", "Quit Multi-Claude Switcher")

	go func() {
		for range mShow.ClickedCh {
			spawnPanel()
		}
	}()

	go func() {
		for range mAbout.ClickedCh {
			showAbout() // reuses the tray dialog helper for consistency
		}
	}()

	go func() {
		<-mQuit.ClickedCh
		systray.Quit()
	}()

	// Auto-update: the persistent tray owns the periodic check, so upgrades
	// still land silently even if the user never opens the panel.
	startUpdateChecker()

	// Store build: resume any pending first-login session migration.
	startMigrationWatcher()
}

// spawnPanel launches this same executable in --panel mode as a detached
// subprocess. The panel exits on outside-click; the next tray click spawns a
// fresh one, matching macOS NSPopover transient behavior.
func spawnPanel() {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("spawn panel: cannot resolve own exe: %v", err)
		return
	}
	cmd := exec.Command(exe, "--panel")
	// Detach so the panel outlives this handler's goroutine and does not tie
	// its stdio to the tray's log stream (except MCS_QUIT — see below).
	panelStdout, err := cmd.StdoutPipe()
	if err == nil {
		go readPanelMessages(panelStdout)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("spawn panel: %v", err)
		return
	}
	// Reap the process so it does not become a zombie.
	go func() { _ = cmd.Wait() }()
}

// readPanelMessages watches the panel process's stdout for MCS_ sentinels.
// The panel writes MCS_QUIT before exiting when the user picks Quit from the
// panel's Settings — the tray then terminates itself so both processes go
// down together.
func readPanelMessages(r io.ReadCloser) {
	defer r.Close()
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "MCS_QUIT" {
			log.Println("Panel requested Quit; shutting down tray.")
			systray.Quit()
			return
		}
	}
}
