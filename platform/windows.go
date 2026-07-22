//go:build windows

package platform

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// WindowsPlatform implements Platform for Claude Desktop on Windows.
//
// Primary target: the STANDALONE (non-Store) Claude Desktop build from
// claude.com/download, which installs a directly-invocable claude.exe and
// respects --user-data-dir, mirroring the macOS model. This is "Option B" in
// docs/superpowers/specs/2026-07-23-windows-port-foundation-design-draft.md.
//
// The MSIX / Microsoft-Store (enterprise) build is NOT yet supported for
// launching: its executable lives under an ACL-locked WindowsApps directory and
// virtualizes its data dir, and whether it forwards a custom --user-data-dir is
// unverified (design draft Option A, probe 3). The detection / termination
// methods below already recognise both builds; only LaunchProfile and the data
// root are standalone-only for now.
type WindowsPlatform struct{}

func New() Platform {
	return &WindowsPlatform{}
}

// AppSupportDir returns the roaming app-data root (%APPDATA%), the Windows
// analog of macOS ~/Library/Application Support. For the standalone build the
// default profile is %APPDATA%\Claude and MCS-managed profiles are sibling
// %APPDATA%\Claude<Name> dirs, mirroring darwin's Claude<Name> layout.
//
// NOTE (MSIX, deferred): the Store build's real data root is
// %LOCALAPPDATA%\Packages\Claude_<hash>\LocalCache\Roaming\Claude, discovered by
// globbing Packages\Claude_*. Add an MSIX branch here once Option A is decided.
func (w *WindowsPlatform) AppSupportDir() string {
	if appData := os.Getenv("APPDATA"); appData != "" {
		return appData
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "AppData", "Roaming")
}

func (w *WindowsPlatform) FindProfiles() ([]*ProfileInfo, error) {
	root := w.AppSupportDir()
	if root == "" {
		return nil, fmt.Errorf("could not determine %%APPDATA%% directory")
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var profiles []*ProfileInfo
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "Claude") {
			fullPath := filepath.Join(root, entry.Name())
			profiles = append(profiles, w.inspectProfile(entry.Name(), fullPath))
		}
	}
	return profiles, nil
}

// inspectProfile mirrors the darwin implementation. It is duplicated here rather
// than shared because darwin.go carries a //go:build darwin tag; a later cleanup
// could hoist inspectProfile/countJSONFiles into platform.go (no build tag).
func (w *WindowsPlatform) inspectProfile(name, path string) *ProfileInfo {
	info := &ProfileInfo{
		Name:        name,
		Path:        path,
		Exists:      true,
		UUIDBuckets: make(map[string]int),
	}

	sessionsDir := GetProfileSessionsDir(path)
	if fi, err := os.Stat(sessionsDir); err == nil && fi.IsDir() {
		info.HasSessionsDir = true
		if uuidEntries, err := os.ReadDir(sessionsDir); err == nil {
			for _, uuidEntry := range uuidEntries {
				if uuidEntry.IsDir() {
					uuidPath := filepath.Join(sessionsDir, uuidEntry.Name())
					info.UUIDBuckets[uuidEntry.Name()] = countJSONFiles(uuidPath)
				}
			}
		}
	}
	return info
}

func countJSONFiles(dirPath string) int {
	count := 0
	_ = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(info.Name(), ".json") {
			count++
		}
		return nil
	})
	return count
}

// procInfo is one Claude.exe process as reported by Win32_Process.
type procInfo struct {
	pid     int
	exePath string
	cmdLine string
}

// IsAppRunning reports whether any Claude Desktop process is active, returning
// the command line of each so DetectRunningProfile can parse --user-data-dir.
// It counts only Desktop processes, deliberately excluding the Claude Code CLI
// (also named claude.exe, but living under \claude-code\).
func (w *WindowsPlatform) IsAppRunning() (bool, []string, error) {
	procs, err := queryClaudeProcesses()
	if err != nil {
		return false, nil, err
	}
	var lines []string
	for _, p := range procs {
		if isDesktopProcess(p) {
			lines = append(lines, p.cmdLine)
		}
	}
	return len(lines) > 0, lines, nil
}

// isDesktopProcess decides whether a claude.exe process is the Desktop app
// rather than the bundled Claude Code CLI. The CLI is named claude.exe too but
// lives under ...\claude-code\<ver>\claude.exe; killing or counting it would be
// wrong (and, when MCS runs inside a Desktop Code tab, self-destructive).
func isDesktopProcess(p procInfo) bool {
	hay := strings.ToLower(p.exePath + " " + p.cmdLine)
	if strings.Contains(hay, `\claude-code\`) {
		return false // the Claude Code CLI, not the Desktop app
	}
	if strings.Contains(hay, `\anthropicclaude\`) {
		return true // standalone build
	}
	if strings.Contains(hay, `\windowsapps\claude_`) {
		return true // MSIX / Store build
	}
	return false
}

// DetectRunningProfile returns the profile path of the running Desktop process,
// matched against known profiles, or "" if none / not detectable.
//
// NOTE (MSIX, deferred): the Store build reports its virtualized default path
// (%APPDATA%\Claude) in the command line, which is NOT where its files actually
// live (LocalCache). For the standalone target the reported path IS the real
// path, so a direct match works.
func (w *WindowsPlatform) DetectRunningProfile() (string, error) {
	running, procs, err := w.IsAppRunning()
	if err != nil {
		return "", err
	}
	if !running {
		return "", nil
	}
	profiles, err := w.FindProfiles()
	if err != nil {
		return "", err
	}
	for _, line := range procs {
		udd := extractUserDataDir(line)
		if udd == "" {
			continue
		}
		for _, p := range profiles {
			if sameWindowsPath(udd, p.Path) {
				return p.Path, nil
			}
		}
	}
	return "", nil
}

// extractUserDataDir pulls the value of --user-data-dir= out of a command line.
// On Windows the path may be quoted ("--user-data-dir=\"C:\\...\\Claude\"") or
// bare (--user-data-dir=C:\...\Claude), so both forms are handled.
func extractUserDataDir(cmdLine string) string {
	const flag = "--user-data-dir="
	idx := strings.Index(cmdLine, flag)
	if idx < 0 {
		return ""
	}
	rest := cmdLine[idx+len(flag):]
	if rest == "" {
		return ""
	}
	if rest[0] == '"' {
		rest = rest[1:]
		if end := strings.IndexByte(rest, '"'); end >= 0 {
			return rest[:end]
		}
		return rest
	}
	if end := strings.IndexByte(rest, ' '); end >= 0 {
		return rest[:end]
	}
	return rest
}

// sameWindowsPath compares two Windows paths case-insensitively after cleaning,
// since NTFS is case-insensitive and separators/./ segments may differ.
func sameWindowsPath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// TerminateApp closes all Claude Desktop processes and confirms they are gone.
// It kills by PID (only Desktop PIDs), never by image name, so the identically
// named Claude Code CLI is never affected. Returning success while a process
// still holds the profile would let the caller sync into a live-writing profile
// and corrupt the shared session index, so the final state is verified.
func (w *WindowsPlatform) TerminateApp() error {
	desktopPIDs := func() []int {
		procs, err := queryClaudeProcesses()
		if err != nil {
			return nil
		}
		var pids []int
		for _, p := range procs {
			if isDesktopProcess(p) && p.pid > 0 {
				pids = append(pids, p.pid)
			}
		}
		return pids
	}

	pids := desktopPIDs()
	if len(pids) == 0 {
		return nil
	}

	// Graceful close first: taskkill without /F posts WM_CLOSE to the tree.
	for _, pid := range pids {
		c := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T")
		hideConsole(c)
		_ = c.Run()
	}
	time.Sleep(1 * time.Second)

	if still, _, _ := w.IsAppRunning(); still {
		// Force kill the tree.
		for _, pid := range desktopPIDs() {
			c := exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid), "/T")
			hideConsole(c)
			_ = c.Run()
		}
		time.Sleep(500 * time.Millisecond)
	}

	still, _, err := w.IsAppRunning()
	if err != nil {
		return err
	}
	if still {
		return fmt.Errorf("failed to terminate Claude Desktop: process still running after force kill")
	}
	return nil
}

// LaunchProfile launches the standalone Claude Desktop with the given profile
// as its --user-data-dir. If only the MSIX/Store build is installed, the
// standalone executable will not be found and a descriptive error is returned.
func (w *WindowsPlatform) LaunchProfile(profilePath string) error {
	exe, err := findClaudeExecutable()
	if err != nil {
		return err
	}
	// Start (not Run) so we return immediately, like macOS `open -n`.
	return exec.Command(exe, "--user-data-dir="+profilePath).Start()
}

// findClaudeExecutable locates the standalone Claude Desktop executable. Squirrel
// installs a version-independent stub at %LOCALAPPDATA%\AnthropicClaude\claude.exe;
// the versioned binary lives under app-<ver>\claude.exe as a fallback.
//
// NOTE: these paths are the expected standalone layout and are UNVERIFIED
// (design draft probe 4). Confirm on a real standalone install before shipping.
func findClaudeExecutable() (string, error) {
	local := os.Getenv("LOCALAPPDATA")
	var candidates []string
	if local != "" {
		candidates = append(candidates, filepath.Join(local, "AnthropicClaude", "claude.exe"))
		if matches, _ := filepath.Glob(filepath.Join(local, "AnthropicClaude", "app-*", "claude.exe")); len(matches) > 0 {
			candidates = append(candidates, matches...)
		}
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("standalone Claude Desktop executable not found under %%LOCALAPPDATA%%\\AnthropicClaude; " +
		"install the standalone build from claude.com/download (the enterprise MSIX/Store build is not yet supported for launching)")
}

// queryClaudeProcesses returns every claude.exe process (Desktop and CLI alike)
// with its PID, executable path and command line, via Win32_Process. Callers
// filter with isDesktopProcess. Fields are separated by an ASCII Unit Separator
// (0x1F) which cannot appear in a Windows command line, avoiding delimiter
// collisions with paths or JSON-bearing arguments.
func queryClaudeProcesses() ([]procInfo, error) {
	const us = "\x1f"
	script := `$us=[char]31
Get-CimInstance Win32_Process -Filter "Name='Claude.exe'" -ErrorAction SilentlyContinue | ForEach-Object { "$($_.ProcessId)$us$($_.ExecutablePath)$us$($_.CommandLine)" }`

	out, err := runPowerShell(script)
	if err != nil {
		return nil, fmt.Errorf("query Claude processes: %w", err)
	}

	var procs []procInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, us, 3)
		pid, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		p := procInfo{pid: pid}
		if len(parts) >= 2 {
			p.exePath = parts[1]
		}
		if len(parts) >= 3 {
			p.cmdLine = parts[2]
		}
		procs = append(procs, p)
	}
	return procs, nil
}

// runPowerShell runs a script via powershell.exe -EncodedCommand. Base64/UTF-16LE
// encoding sidesteps all Windows command-line quoting pitfalls (the script may
// contain quotes, spaces and special characters) and needs no shell.
func runPowerShell(script string) (string, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", psEncodedCommand(script))
	hideConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func psEncodedCommand(script string) string {
	u16 := utf16.Encode([]rune(script))
	buf := make([]byte, 0, len(u16)*2)
	for _, c := range u16 {
		buf = append(buf, byte(c), byte(c>>8)) // little-endian
	}
	return base64.StdEncoding.EncodeToString(buf)
}
