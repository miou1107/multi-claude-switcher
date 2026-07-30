//go:build windows

package platform

import (
	"encoding/base64"
	"fmt"
	"log"
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

// isMSIX reports whether the Store/MSIX build is the active target. The
// standalone build is preferred when both are installed.
func (w *WindowsPlatform) isMSIX() bool {
	if _, err := findClaudeExecutable(); err == nil {
		return false
	}
	return msixRoamingDir() != ""
}

func (w *WindowsPlatform) FindProfiles() ([]*ProfileInfo, error) {
	if w.isMSIX() {
		return w.msixFindProfiles()
	}

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

// msixFindProfiles lists the MCS-managed Store profiles: the active one (the
// slot) named per state, plus every parked profile under the container. All are
// marked Managed so the tray shows them even before they have session data.
func (w *WindowsPlatform) msixFindProfiles() ([]*ProfileInfo, error) {
	roaming := msixRoamingDir()
	if roaming == "" {
		return nil, fmt.Errorf("Store Claude Desktop data directory not found")
	}
	return w.msixFindProfilesIn(roaming)
}

// msixFindProfilesIn takes the roaming dir explicitly so it can be tested without
// a real Store install, the same way msixParkForNewIn and msixSwapToIn are.
func (w *WindowsPlatform) msixFindProfilesIn(roaming string) ([]*ProfileInfo, error) {
	st := readMSIXStateIn(roaming)

	var profiles []*ProfileInfo
	slot := msixSlotDir(roaming)
	if fi, err := os.Stat(slot); err == nil && fi.IsDir() {
		p := w.inspectProfile(st.Current, slot)
		p.Managed = true
		profiles = append(profiles, p)
	} else {
		// No slot directory. That is a real, expected state: creating a profile
		// parks the live slot and leaves the slot absent on purpose so the packaged
		// app makes a clean one. state.json still names the current profile and the
		// user has just been told to sign in to it, so it has to be listed. Before
		// this, the account list silently dropped the current profile for that whole
		// window.
		profiles = append(profiles, &ProfileInfo{
			Name: st.Current, Path: slot, Exists: false,
			UUIDBuckets: map[string]int{}, Managed: true,
		})
	}
	if entries, err := os.ReadDir(msixContainerDir(roaming)); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				p := w.inspectProfile(e.Name(), filepath.Join(msixContainerDir(roaming), e.Name()))
				p.Managed = true
				profiles = append(profiles, p)
			}
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
	if w.isMSIX() {
		// The Store build always runs out of the single slot dir; its identity is
		// whichever profile MCS last swapped in (tracked in state). Return the slot
		// path so it matches the current profile's ProfileInfo.Path.
		running, _, err := w.IsAppRunning()
		if err != nil {
			return "", err
		}
		if !running {
			return "", nil
		}
		return msixSlotDir(msixRoamingDir()), nil
	}

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
	if w.isMSIX() {
		return w.msixLaunchProfile(profilePath)
	}
	exe, err := findClaudeExecutable()
	if err != nil {
		return err
	}
	// A profile with no account is about to be signed in to, and that sign-in
	// comes back through claude://, which the shell resolves without our
	// --user-data-dir. Hold the handler on this profile until the account
	// appears, or the new login lands in the default profile instead. Profiles
	// that already have an account need no callback and are left alone.
	if _, err := GetProfileAccountUUID(profilePath); err != nil {
		log.Printf("%s has no account yet; holding the claude:// handler for its sign-in", profilePath)
		HoldProtocolHandlerForSignIn(profilePath)
	}
	// Start (not Run) so we return immediately, like macOS `open -n`.
	return exec.Command(exe, "--user-data-dir="+profilePath).Start()
}

// msixLaunchProfile switches the Store build to the profile at profilePath by
// swapping it into the live slot, then relaunching the packaged app. If the path
// already is the slot (the current profile), it just reopens the app. Caller
// (SafeSwitch) has already terminated Claude.
func (w *WindowsPlatform) msixLaunchProfile(profilePath string) error {
	roaming := msixRoamingDir()
	if roaming == "" {
		return fmt.Errorf("Store Claude Desktop data directory not found")
	}
	if sameWindowsPath(profilePath, msixSlotDir(roaming)) {
		return msixLaunch()
	}
	if err := msixSwapToIn(roaming, filepath.Base(profilePath)); err != nil {
		return err
	}
	return msixLaunch()
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

// CreateProfile makes a new profile and returns its identity and data directory.
//
// The two differ on the Store build and that difference is the whole reason this
// returns both: there, the identity is the name written to state.json while the
// directory is the shared slot, always called "Claude". Deriving the identity from
// the directory yields "Claude" for every profile, which names a profile
// FindProfiles never reports.
func (w *WindowsPlatform) CreateProfile(clean string) (string, string, error) {
	if w.isMSIX() {
		roaming := msixRoamingDir()
		if roaming == "" {
			return "", "", fmt.Errorf("Store Claude Desktop data directory not found")
		}
		if err := msixParkForNewIn(roaming, clean); err != nil {
			return "", "", err
		}
		// The slot is deliberately absent now: the packaged app creates a clean one
		// on next launch, which is what makes this a signed-out profile.
		return clean, msixSlotDir(roaming), nil
	}
	root := w.AppSupportDir()
	if root == "" {
		return "", "", fmt.Errorf("could not determine %%APPDATA%% directory")
	}
	identity := profileFolderPrefix + clean
	path := filepath.Join(root, identity)
	if _, err := os.Stat(path); err == nil {
		return "", "", fmt.Errorf("a profile folder named %q already exists", identity)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", "", fmt.Errorf("create profile folder: %w", err)
	}
	return identity, path, nil
}

func (w *WindowsPlatform) PrepareRecovery(newProfilePath string, sources []RecoverySource) error {
	if w.isMSIX() {
		// msixParkForNewIn already set PendingMigrateFrom on the parked profile, and
		// msixAttemptMigrationIn copies the bucket matching whatever account the user
		// signs in as — which is exactly this recovery. Nothing to do, and nothing
		// may be written into a slot the app has not created yet.
		//
		// It copies from the one profile it parked, so a ghost split across several
		// profiles recovers only that profile's share here. The rest stays visible as
		// a ghost and can be recovered on a second pass.
		return nil
	}
	return prepareRecoveryByCopy(newProfilePath, sources)
}

func (w *WindowsPlatform) PrepareArchive(keepIdentity, archiveIdentity string) (string, string, error) {
	if !w.isMSIX() {
		root := w.AppSupportDir()
		if root == "" {
			return "", "", fmt.Errorf("could not determine %%APPDATA%% directory")
		}
		return filepath.Join(root, keepIdentity), filepath.Join(root, archiveIdentity), nil
	}
	roaming := msixRoamingDir()
	if roaming == "" {
		return "", "", fmt.Errorf("Store Claude Desktop data directory not found")
	}
	if strings.EqualFold(readMSIXStateIn(roaming).Current, archiveIdentity) {
		// The profile to archive is the slot occupant. Renaming the slot away would
		// leave state.json naming a directory that does not exist, so swap the keeper
		// in first: the keeper becomes the active profile — where the user wants to
		// end up anyway — and the other lands in .mcs-profiles, ready to be renamed
		// out. msixSwapToIn rolls its own parking back on failure.
		if err := msixSwapToIn(roaming, keepIdentity); err != nil {
			return "", "", err
		}
	}
	// Resolve after any swap: state.json has moved, and so have both directories.
	return msixProfilePath(roaming, keepIdentity), msixProfilePath(roaming, archiveIdentity), nil
}

// ArchiveDir keeps Store archives inside the package container, beside
// .mcs-profiles, because renames within the container are what the shipped code
// already does successfully and moving out of an MSIX virtualized container is
// unverified. msixFindProfiles enumerates only the slot and .mcs-profiles, so the
// archive stays invisible to scanning either way.
func (w *WindowsPlatform) ArchiveDir() string {
	if w.isMSIX() {
		if roaming := msixRoamingDir(); roaming != "" {
			return filepath.Join(roaming, ".mcs-archive")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "multi-claude-switcher-archive")
	}
	return filepath.Join(home, ".multi-claude-switcher", "archive")
}
