//go:build windows

package platform

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// Slot blockers.
//
// Parking the slot is a directory rename, and Windows refuses to rename a
// directory that a running program was loaded from. Closing Claude Desktop is
// not enough, because other programs live INSIDE the slot and keep running after
// Desktop exits:
//
//	<slot>\ChromeNativeHost\chrome-native-host.exe   the Chrome bridge helper,
//	                                                 started by Chrome and
//	                                                 restarted by it on demand
//	<slot>\claude-code\<ver>\claude.exe              the Claude Code CLI
//
// Neither is reachable from the Name='Claude.exe' query TerminateApp filters:
// chrome-native-host.exe has a different image name entirely, and
// isDesktopProcess excludes \claude-code\ on purpose. The CLI hides a second
// time as well — the MSIX runtime redirects %APPDATA%\Claude to the slot, and
// Win32_Process reports that redirected spelling, so testing the reported path
// against the LocalCache path alone does not match it either. msixSlotAliases
// covers both spellings.
//
// Left running, they turn a switch into a bare "Access is denied" from
// os.Rename with nothing in the log naming what was in the way.

// slotProc is one row of the process table: enough to test path containment and
// to walk the parent chain.
type slotProc struct {
	pid     int
	ppid    int
	name    string
	exePath string
}

// msixSlotAliases returns every path prefix that means "inside the live slot".
//
// The first is the real directory. The second is the virtualized %APPDATA%\Claude
// that the MSIX runtime redirects to it, which is what Win32_Process reports for
// anything started inside the package container. The redirect is confirmed with
// os.SameFile rather than assumed: on a machine where %APPDATA%\Claude is an
// ordinary directory belonging to some other install, matching it would close
// processes that have nothing to do with this slot.
func msixSlotAliases(roaming string) []string {
	slot := filepath.Clean(msixSlotDir(roaming))
	aliases := []string{slot}
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return aliases
	}
	virt := filepath.Clean(filepath.Join(appData, msixSlotName))
	if strings.EqualFold(virt, slot) {
		return aliases
	}
	vfi, err := os.Stat(virt)
	if err != nil {
		return aliases
	}
	sfi, err := os.Stat(slot)
	if err != nil {
		return aliases
	}
	if os.SameFile(vfi, sfi) {
		aliases = append(aliases, virt)
	}
	return aliases
}

// underDir reports whether path is dir itself or something inside it. NTFS is
// case-insensitive, and the test is on whole path segments so a slot at
// ...\Claude does not swallow a sibling called ...\Claude Backup.
func underDir(path, dir string) bool {
	p := strings.ToLower(filepath.Clean(path))
	d := strings.ToLower(filepath.Clean(dir))
	if p == d {
		return true
	}
	if !strings.HasSuffix(d, string(filepath.Separator)) {
		d += string(filepath.Separator)
	}
	return strings.HasPrefix(p, d)
}

// msixAncestors returns pid plus every process above it in the parent chain.
// Killing anything in that set kills MCS in the middle of a swap, which would
// leave the slot parked with nothing left running to roll it back — the user's
// data directory would simply appear to be gone.
//
// Windows reuses PIDs, so a recorded parent may since have been replaced by an
// unrelated process and the chain can over-protect. That is the safe direction:
// a switch that stops with a message naming what is in the way beats one that
// terminates itself halfway through.
func msixAncestors(procs []slotProc, pid int) map[int]bool {
	byPID := make(map[int]slotProc, len(procs))
	for _, p := range procs {
		byPID[p.pid] = p
	}
	chain := map[int]bool{pid: true}
	for cur := pid; ; {
		p, ok := byPID[cur]
		if !ok || p.ppid <= 0 || chain[p.ppid] {
			return chain
		}
		chain[p.ppid] = true
		cur = p.ppid
	}
}

// msixFindSlotBlockers splits the processes running out of the slot into the ones
// that can be closed and the ones that must not be (see msixAncestors).
func msixFindSlotBlockers(procs []slotProc, aliases []string, selfPID int) (kill, protected []slotProc) {
	chain := msixAncestors(procs, selfPID)
	for _, p := range procs {
		if p.pid <= 0 || p.exePath == "" {
			continue
		}
		inside := false
		for _, a := range aliases {
			if underDir(p.exePath, a) {
				inside = true
				break
			}
		}
		if !inside {
			continue
		}
		if chain[p.pid] {
			protected = append(protected, p)
		} else {
			kill = append(kill, p)
		}
	}
	return kill, protected
}

// msixQueryProcessTable lists every process with its parent, image name and
// executable path. Unlike queryClaudeProcesses this cannot filter by image name:
// the blockers are identified by where they run from, not by what they are
// called. Fields use an ASCII Unit Separator, which cannot appear in a path.
func msixQueryProcessTable() ([]slotProc, error) {
	script := `$us=[char]31
Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | ForEach-Object { "$($_.ProcessId)$us$($_.ParentProcessId)$us$($_.Name)$us$($_.ExecutablePath)" }`
	out, err := runPowerShell(script)
	if err != nil {
		return nil, fmt.Errorf("query process table: %w", err)
	}
	return parseProcessTable(out), nil
}

// parseProcessTable turns the query output into rows, skipping malformed lines
// rather than failing a whole switch over one of them.
func parseProcessTable(out string) []slotProc {
	const us = "\x1f"
	var procs []slotProc
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, us, 4)
		if len(parts) < 4 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			continue
		}
		ppid, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		procs = append(procs, slotProc{pid: pid, ppid: ppid, name: parts[2], exePath: parts[3]})
	}
	return procs
}

// taskkillTree closes pid and its child tree, forcing the kill when force is set.
// Same mechanism TerminateApp uses.
func taskkillTree(pid int, force bool) {
	args := []string{"/PID", strconv.Itoa(pid), "/T"}
	if force {
		args = append([]string{"/F"}, args...)
	}
	c := exec.Command("taskkill", args...)
	hideConsole(c)
	_ = c.Run()
}

// killSlotBlocker closes p and its child tree, but only while p is still a
// process running out of the slot.
//
// taskkill matches on PID alone, and Windows reuses PIDs. Between listing the
// process table and reaching this call the blocker can exit and its number be
// handed to something unrelated, which taskkill /T would then take down along
// with its whole child tree. TerminateApp carries the same exposure but kills a
// set filtered by image name; this one kills whatever sits at a path, so a stale
// row is that much more dangerous.
//
// Opening a handle first does two jobs. It proves the identity, and it pins the
// number: Windows cannot reuse a PID while a handle to that process is open, so
// the kill that follows cannot land on a different process. (Children reached by
// /T are not pinned, which is inherent to killing a tree by PID.)
//
// The test is containment in the slot rather than string equality with the
// recorded path. WMI and QueryFullProcessImageName need not spell a redirected
// path identically, and "is this still running out of the slot" is the property
// that actually licenses the kill.
func killSlotBlocker(p slotProc, aliases []string, force bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(p.pid))
	if err != nil {
		return // already gone; nothing to kill and nothing to warn about
	}
	defer windows.CloseHandle(h)

	exe, err := processImagePath(h)
	if err != nil {
		log.Printf("[msix] could not confirm what pid %d is now (%v); leaving it alone", p.pid, err)
		return
	}
	for _, a := range aliases {
		if underDir(exe, a) {
			log.Printf("[msix] closing slot blocker %s (pid %d) force=%v %s", p.name, p.pid, force, exe)
			taskkillTree(p.pid, force)
			return
		}
	}
	log.Printf("[msix] pid %d is now %s, which is not in the slot; the number was reused, leaving it alone", p.pid, exe)
}

// processImagePath returns the executable path behind an open process handle,
// growing the buffer for the long paths Windows now allows.
func processImagePath(h windows.Handle) (string, error) {
	buf := make([]uint16, windows.MAX_PATH)
	for {
		n := uint32(len(buf))
		err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n)
		if err == nil {
			return windows.UTF16ToString(buf[:n]), nil
		}
		if err != windows.ERROR_INSUFFICIENT_BUFFER || len(buf) >= 32768 {
			return "", err
		}
		buf = make([]uint16, len(buf)*2)
	}
}

// describeProcs names processes for a message the user has to act on, so it says
// "chrome-native-host.exe (pid 19024)" rather than a bare path.
func describeProcs(procs []slotProc) string {
	names := make([]string, 0, len(procs))
	for _, p := range procs {
		names = append(names, fmt.Sprintf("%s (pid %d)", p.name, p.pid))
	}
	return strings.Join(names, ", ")
}

// msixClearSlotBlockers closes everything still running out of the live slot so
// the directory can be renamed. Run it after TerminateApp's Desktop pass and
// before the rename.
//
// It reports nothing back, and that is the whole point. TerminateApp's contract
// is that a non-nil error means Claude Desktop is STILL UP and nothing was
// closed; core.Switcher relies on exactly that to conclude there is no relaunch
// debt to honour, and drops it (see the ClaimPendingRelaunch calls in
// core/switch.go and core/align.go). By the time this function runs Desktop is
// already closed, so returning an error here would leave the user with Claude
// shut and MCS convinced it owed them nothing — the one outcome those call sites
// are written to prevent.
//
// A blocker that survives is not silently swallowed either: the rename that
// follows fails on its own, renameWithRetry names the holders in its error, and
// msixSwapToIn rolls the slot back. That recovery path already existed and is
// where this belongs.
func msixClearSlotBlockers(roaming string) {
	aliases := msixSlotAliases(roaming)
	selfPID := os.Getpid()

	remaining := func() (kill, protected []slotProc, err error) {
		procs, err := msixQueryProcessTable()
		if err != nil {
			return nil, nil, err
		}
		kill, protected = msixFindSlotBlockers(procs, aliases, selfPID)
		return kill, protected, nil
	}

	// Sweep rather than kill once. Chrome restarts chrome-native-host.exe on
	// demand, so a helper that dies here can be back before the rename runs, and
	// taskkill reports success for "termination signalled" rather than "process
	// gone". The first pass is graceful; later passes force.
	const sweeps = 3
	for i := 0; i < sweeps; i++ {
		kill, protected, err := remaining()
		if err != nil {
			// Not a reason to hold up the switch: the rename may well succeed, and if
			// it does not, renameWithRetry reports whatever it can find.
			log.Printf("[msix] could not list processes to check the slot: %v", err)
			return
		}
		for _, p := range protected {
			log.Printf("[msix] NOT closing %s (pid %d): MCS is running under it, and closing it would stop the switch halfway", p.name, p.pid)
		}
		if len(kill) == 0 {
			return
		}
		force := i > 0
		for _, p := range kill {
			// Not taskkillTree directly: the rows were read at the top of this sweep
			// and a PID can be recycled before the loop reaches it. See killSlotBlocker.
			killSlotBlocker(p, aliases, force)
		}
		time.Sleep(750 * time.Millisecond)
	}
	if kill, _, err := remaining(); err == nil && len(kill) > 0 {
		log.Printf("[msix] still holding the slot after %d sweeps: %s", sweeps, describeProcs(kill))
	}
}

// msixDescribeHolders names whatever is still running out of dir, for the log
// line after a rename gives up. Best effort: it returns "" when nothing is found
// or the process table cannot be read.
//
// When dir is the slot it checks the virtualized spelling too, since that is
// exactly the case where the holder tends to be the one MCS cannot see.
func msixDescribeHolders(dir string) string {
	aliases := []string{filepath.Clean(dir)}
	if strings.EqualFold(filepath.Base(dir), msixSlotName) {
		aliases = msixSlotAliases(filepath.Dir(dir))
	}
	procs, err := msixQueryProcessTable()
	if err != nil {
		return ""
	}
	var inside []slotProc
	for _, p := range procs {
		if p.exePath == "" {
			continue
		}
		for _, a := range aliases {
			if underDir(p.exePath, a) {
				inside = append(inside, p)
				break
			}
		}
	}
	if len(inside) == 0 {
		return ""
	}
	return describeProcs(inside)
}
