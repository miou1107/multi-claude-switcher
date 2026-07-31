//go:build windows

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnderDir(t *testing.T) {
	const slot = `C:\Users\A\AppData\Local\Packages\Claude_x\LocalCache\Roaming\Claude`

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"the directory itself", slot, true},
		{"a file directly inside", slot + `\config.json`, true},
		{"nested deeply", slot + `\claude-code\2.1.219\claude.exe`, true},
		{"case differs", `c:\users\a\appdata\local\packages\claude_x\localcache\roaming\CLAUDE\x.exe`, true},
		{"uncleaned separators", slot + `\.\ChromeNativeHost\..\ChromeNativeHost\h.exe`, true},
		// The bug this guards: a prefix test without a segment boundary would call
		// the sibling a blocker and kill whatever runs from it.
		{"sibling sharing the prefix", slot + ` Backup\claude.exe`, false},
		{"sibling with no separator", slot + `2\claude.exe`, false},
		{"the parent", `C:\Users\A\AppData\Local\Packages\Claude_x\LocalCache\Roaming`, false},
		{"somewhere else", `C:\Program Files\WindowsApps\Claude_x\app\Claude.exe`, false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := underDir(c.path, slot); got != c.want {
				t.Errorf("underDir(%q, slot) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func TestMSIXAncestors(t *testing.T) {
	procs := []slotProc{
		{pid: 100, ppid: 0, name: "explorer.exe"},
		{pid: 200, ppid: 100, name: "Claude.exe"},
		{pid: 300, ppid: 200, name: "claude.exe"},
		{pid: 400, ppid: 300, name: "mcs-tray.exe"},
		{pid: 500, ppid: 100, name: "unrelated.exe"},
	}

	chain := msixAncestors(procs, 400)
	for _, pid := range []int{400, 300, 200, 100} {
		if !chain[pid] {
			t.Errorf("pid %d should be in the chain above 400", pid)
		}
	}
	if chain[500] {
		t.Error("pid 500 is a sibling branch, not an ancestor")
	}

	if chain := msixAncestors(procs, 999); len(chain) != 1 || !chain[999] {
		t.Errorf("an unknown pid should protect only itself, got %v", chain)
	}
}

// PIDs are reused, so a parent link can point back into the chain. The walk must
// stop rather than spin: this runs under the test timeout and would hang without
// the cycle guard.
func TestMSIXAncestorsStopsOnACycle(t *testing.T) {
	procs := []slotProc{
		{pid: 10, ppid: 20},
		{pid: 20, ppid: 10},
	}
	chain := msixAncestors(procs, 10)
	if !chain[10] || !chain[20] {
		t.Errorf("both pids should be protected, got %v", chain)
	}
}

func TestMSIXFindSlotBlockers(t *testing.T) {
	const (
		slot = `C:\Users\A\AppData\Local\Packages\Claude_x\LocalCache\Roaming\Claude`
		virt = `C:\Users\A\AppData\Roaming\Claude` // the MSIX redirect of the same dir
	)
	aliases := []string{slot, virt}

	procs := []slotProc{
		{pid: 100, ppid: 0, name: "explorer.exe", exePath: `C:\WINDOWS\explorer.exe`},
		// Desktop itself: runs from the package, not from the slot, and is already
		// gone by the time this pass runs.
		{pid: 200, ppid: 100, name: "Claude.exe", exePath: `C:\Program Files\WindowsApps\Claude_x\app\Claude.exe`},
		// Blocker 1: a different image name, so the Claude.exe query never saw it.
		{pid: 300, ppid: 100, name: "chrome-native-host.exe", exePath: slot + `\ChromeNativeHost\chrome-native-host.exe`},
		// Blocker 2: reported under the virtualized path, so a test against the
		// LocalCache spelling alone never saw it either.
		{pid: 400, ppid: 100, name: "claude.exe", exePath: virt + `\claude-code\2.1.219\claude.exe`},
		// No executable path (access denied on a system process): not actionable.
		{pid: 500, ppid: 100, name: "System", exePath: ""},
		// MCS itself, running from its own install dir.
		{pid: 600, ppid: 100, name: "mcs-tray.exe", exePath: `C:\Users\A\AppData\Local\Programs\Multi-Claude Switcher\mcs-tray.exe`},
	}

	kill, protected := msixFindSlotBlockers(procs, aliases, 600)
	if len(protected) != 0 {
		t.Errorf("nothing in MCS's own chain runs from the slot here, got %v", describeProcs(protected))
	}
	if got := describeProcs(kill); got != "chrome-native-host.exe (pid 300), claude.exe (pid 400)" {
		t.Errorf("wrong blockers: %s", got)
	}
}

// The dangerous case: MCS was started from a Claude Code session that itself runs
// out of the slot. Closing that would kill MCS between the park and the rollback,
// leaving the user's data parked under .mcs-profiles with nothing to put it back.
func TestMSIXFindSlotBlockersProtectsItsOwnChain(t *testing.T) {
	const slot = `C:\Users\A\AppData\Local\Packages\Claude_x\LocalCache\Roaming\Claude`
	aliases := []string{slot}

	procs := []slotProc{
		{pid: 300, ppid: 0, name: "claude.exe", exePath: slot + `\claude-code\2.1.219\claude.exe`},
		{pid: 400, ppid: 300, name: "mcs.exe", exePath: `C:\Users\A\AppData\Local\Programs\Multi-Claude Switcher\mcs.exe`},
		{pid: 500, ppid: 0, name: "chrome-native-host.exe", exePath: slot + `\ChromeNativeHost\chrome-native-host.exe`},
	}

	kill, protected := msixFindSlotBlockers(procs, aliases, 400)
	if got := describeProcs(protected); got != "claude.exe (pid 300)" {
		t.Errorf("MCS's own ancestor should be protected, got %q", got)
	}
	if got := describeProcs(kill); got != "chrome-native-host.exe (pid 500)" {
		t.Errorf("unrelated blockers should still be closeable, got %q", got)
	}
}

// msixSlotAliases decides which paths mean "inside the live slot", and every path
// it accepts becomes a licence to terminate whatever runs from there. The
// virtualized %APPDATA%\Claude spelling is only the slot on a machine where the
// MSIX runtime redirects it; on a machine where that is an ordinary folder
// belonging to some other Claude install, accepting it would close processes that
// have nothing to do with this switch. Hence the os.SameFile check, and hence
// these tests.
func TestMSIXSlotAliasesRejectsAnUnrelatedAppData(t *testing.T) {
	roaming := t.TempDir()
	slot := msixSlotDir(roaming)
	if err := os.MkdirAll(slot, 0o755); err != nil {
		t.Fatal(err)
	}

	// A real, existing %APPDATA%\Claude that is simply a different directory.
	other := t.TempDir()
	if err := os.MkdirAll(filepath.Join(other, msixSlotName), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPDATA", other)

	aliases := msixSlotAliases(roaming)
	if len(aliases) != 1 || !strings.EqualFold(aliases[0], slot) {
		t.Fatalf("an unrelated %%APPDATA%%\\Claude must not be treated as the slot, got %v", aliases)
	}
}

func TestMSIXSlotAliasesWithoutAnAppDataClaude(t *testing.T) {
	roaming := t.TempDir()
	if err := os.MkdirAll(msixSlotDir(roaming), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("APPDATA unset", func(t *testing.T) {
		t.Setenv("APPDATA", "")
		if got := msixSlotAliases(roaming); len(got) != 1 {
			t.Errorf("want just the real slot, got %v", got)
		}
	})
	t.Run("APPDATA has no Claude dir", func(t *testing.T) {
		t.Setenv("APPDATA", t.TempDir())
		if got := msixSlotAliases(roaming); len(got) != 1 {
			t.Errorf("want just the real slot, got %v", got)
		}
	})
}

// The positive half: when %APPDATA%\Claude really does resolve to the slot, the
// alias must be kept — without it the Claude Code CLI is invisible, because
// Win32_Process reports it under the redirected spelling. A junction stands in
// for the MSIX redirect; it needs no privilege, but skip rather than fail if the
// environment refuses it.
func TestMSIXSlotAliasesKeepsARedirectedAppData(t *testing.T) {
	roaming := t.TempDir()
	slot := msixSlotDir(roaming)
	if err := os.MkdirAll(slot, 0o755); err != nil {
		t.Fatal(err)
	}
	appData := t.TempDir()
	link := filepath.Join(appData, msixSlotName)
	if err := exec.Command("cmd", "/c", "mklink", "/J", link, slot).Run(); err != nil {
		t.Skipf("cannot create a junction here: %v", err)
	}
	t.Setenv("APPDATA", appData)

	aliases := msixSlotAliases(roaming)
	if len(aliases) != 2 {
		t.Fatalf("the redirected spelling must be kept, got %v", aliases)
	}
	// And it has to actually match the path Win32_Process reports for the CLI.
	cli := filepath.Join(link, "claude-code", "2.1.219", "claude.exe")
	if !underDir(cli, aliases[1]) {
		t.Errorf("%q should be inside alias %q", cli, aliases[1])
	}
}

func TestParseProcessTable(t *testing.T) {
	const us = "\x1f"
	out := "100" + us + "0" + us + "explorer.exe" + us + `C:\WINDOWS\explorer.exe` + "\r\n" +
		"\r\n" + // blank line
		"garbage without separators\r\n" +
		"notanumber" + us + "0" + us + "x.exe" + us + `C:\x.exe` + "\r\n" +
		"200" + us + "100" + us + "short.exe" + us + "\r\n" + // no path: kept, filtered later
		"300" + us + "100" + us + "chrome-native-host.exe" + us + `C:\Program Files\A B\h.exe` + "\r\n"

	procs := parseProcessTable(out)
	if len(procs) != 3 {
		t.Fatalf("expected 3 usable rows, got %d: %+v", len(procs), procs)
	}
	if procs[0].pid != 100 || procs[0].exePath != `C:\WINDOWS\explorer.exe` {
		t.Errorf("first row wrong: %+v", procs[0])
	}
	if procs[1].pid != 200 || procs[1].exePath != "" {
		t.Errorf("a row with no executable path should survive parsing: %+v", procs[1])
	}
	// A path containing spaces must not be split; the separator is what delimits.
	if procs[2].ppid != 100 || procs[2].exePath != `C:\Program Files\A B\h.exe` {
		t.Errorf("last row wrong: %+v", procs[2])
	}
}
