//go:build darwin

package main

/*
#include "menubar.h"
*/
import "C"

import (
	"log"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
)

// The relaunch guard, and why the menu-bar app watches Claude Desktop at all.
//
// Claude Desktop installs updates on its own: it downloads the new build,
// waits for the user to go idle, quits and relaunches. That relaunch drops the
// --user-data-dir flag this app started it with, so Claude comes back on the
// default profile — somebody else's account — and the user who had switched
// finds themselves switched back with nothing in this app's log to say why.
// core.RelaunchGuard decides whether a start/stop sequence is that; this file
// feeds it and acts on its verdict.
var (
	relaunchGuard = core.NewRelaunchGuard(nil)
	// relaunchMu serialises everything that touches the guard: it is not safe
	// for concurrent use, and holding the lock across the restore means the
	// restore's own quit-and-relaunch is observed after it, with the debt
	// already settled.
	relaunchMu sync.Mutex
)

// startRelaunchGuard begins watching Claude Desktop start and quit.
//
// It also takes one look at what Claude is running on right now. The guard
// learns what was open from launch events, and when this app starts after
// Claude is already open there has been no such event; without this first
// look the guard would have no history to judge the next quit against.
func startRelaunchGuard() {
	C.WatchClaudeDesktop()
	log.Printf("[Relaunch Guard] Watching for Claude Desktop relaunching itself on the wrong account.")
	go func() {
		time.Sleep(2 * time.Second)
		observeClaude()
	}()
}

// goClaudeAppChanged is called from Objective-C on every Claude Desktop launch
// (launched=1) or quit (launched=0). It returns at once: the notification is
// delivered on the main thread, and nothing below may hold that up.
//
// A quit is recorded immediately from the notification itself (MarkGone), as
// long as the app's own process really is gone, because the process table
// lags: helpers outlive the app for a moment, and a fast relaunch can be up
// again before a later look. The looks that follow are delayed so the table
// has caught up: once after a launch, twice after a quit so a slow shutdown
// is still seen as gone before the relaunch arrives.
//
//export goClaudeAppChanged
func goClaudeAppChanged(launched C.int) {
	delays := []time.Duration{1 * time.Second, 4 * time.Second}
	if launched != 0 {
		delays = []time.Duration{1500 * time.Millisecond}
	}
	go func() {
		if launched == 0 {
			log.Printf("[Relaunch Guard] A Claude Desktop instance quit.")
			noteClaudeQuit()
		} else {
			log.Printf("[Relaunch Guard] A Claude Desktop instance launched.")
		}
		for _, d := range delays {
			time.Sleep(d)
			observeClaude()
		}
	}()
}

// noteClaudeQuit tells the guard Claude has quit, the moment the system says
// so, unless another Claude instance (another account) is still up.
func noteClaudeQuit() {
	relaunchMu.Lock()
	defer relaunchMu.Unlock()
	_, procs, err := plat.IsAppRunning()
	if err != nil || platform.ClaudeMainProcessInProcs(procs) {
		return
	}
	relaunchGuard.MarkGone(mcsHasClaudeClosed())
}

// mcsHasClaudeClosed reports whether an operation of this app's own is the
// reason Claude is (or is about to be) shut: a switch or sync owes a relaunch,
// or any maintenance action is in flight. Busy is wider than "Claude is
// closed" (a backup does not close it), which can only make the guard miss a
// relaunch during a backup, never fight one of its own operations.
func mcsHasClaudeClosed() bool {
	return len(switcher.PendingRelaunch()) > 0 || getBusy()
}

// observeClaude reads what Claude Desktop is running on now, asks the guard
// what it means, and puts the user back when the answer is "Claude relaunched
// itself on the wrong account".
func observeClaude() {
	relaunchMu.Lock()
	defer relaunchMu.Unlock()

	running, err := plat.DetectRunningProfiles()
	if err != nil {
		log.Printf("[Relaunch Guard] Could not list running profiles: %v", err)
		return
	}
	profiles := mustFindProfiles()
	recorded := core.ActiveProfilePath(profiles)
	marker := recorded != "" && core.SelfRelaunchMarkerFresh(recorded, time.Now())
	r := relaunchGuard.Observe(running, recorded, mcsHasClaudeClosed(), marker)
	if r == nil {
		log.Printf("[Relaunch Guard] Claude Desktop is on %v; recorded account %s; nothing to do.", running, recorded)
		return
	}
	log.Printf("[Relaunch Guard] Claude Desktop was gone for %s and came back on %v, not on %s; its relaunch marker is there", r.Gap.Round(time.Second), running, recorded)

	// Under busy for the same reason every other close-and-reopen is: a second
	// operation is refused while this one has Claude shut, and a Quit pressed
	// now is deferred until Claude is back rather than reopening it mid-close.
	setBusyStatus(true, "")
	err = switcher.RestoreAfterSelfRelaunch(r.Reopen)
	setBusyStatus(false, "")
	if err != nil {
		log.Printf("[Relaunch Guard] Could not put the user back: %v", err)
		return
	}
	title, body := core.RelaunchNotice(notificationLanguage(), core.DisplayName(filepath.Base(recorded)))
	notify(title, body)
	go reloadPanel()
}

var quotedLanguage = regexp.MustCompile(`"([^"]+)"`)

// notificationLanguage is the language the panel is shown in, with "auto"
// resolved the way the panel resolves it, from the computer's preferred
// languages (`defaults read -g AppleLanguages`, most preferred first).
func notificationLanguage() string {
	var system []string
	if out, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output(); err == nil {
		for _, m := range quotedLanguage.FindAllStringSubmatch(string(out), -1) {
			system = append(system, m[1])
		}
	}
	return core.ResolveAutoLanguage(core.PanelLanguage(), system)
}
