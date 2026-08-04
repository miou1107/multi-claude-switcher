//go:build darwin

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type DarwinPlatform struct{}

func New() Platform {
	return &DarwinPlatform{}
}

func (d *DarwinPlatform) AppSupportDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support")
}

func (d *DarwinPlatform) FindProfiles() ([]*ProfileInfo, error) {
	appSup := d.AppSupportDir()
	if appSup == "" {
		return nil, fmt.Errorf("could not determine user home directory")
	}

	entries, err := os.ReadDir(appSup)
	if err != nil {
		return nil, err
	}

	var profiles []*ProfileInfo
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "Claude") {
			fullPath := filepath.Join(appSup, entry.Name())
			info := d.inspectProfile(entry.Name(), fullPath)
			profiles = append(profiles, info)
		}
	}
	return profiles, nil
}

func (d *DarwinPlatform) inspectProfile(name, path string) *ProfileInfo {
	info := &ProfileInfo{
		Name:        name,
		Path:        path,
		Exists:      true,
		UUIDBuckets: make(map[string]int),
	}

	sessionsDir := GetProfileSessionsDir(path)
	if fi, err := os.Stat(sessionsDir); err == nil && fi.IsDir() {
		info.HasSessionsDir = true
		uuidEntries, err := os.ReadDir(sessionsDir)
		if err == nil {
			for _, uuidEntry := range uuidEntries {
				if uuidEntry.IsDir() {
					uuidPath := filepath.Join(sessionsDir, uuidEntry.Name())
					count := countJSONFiles(uuidPath)
					info.UUIDBuckets[uuidEntry.Name()] = count
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

func (d *DarwinPlatform) IsAppRunning() (bool, []string, error) {
	cmd := exec.Command("ps", "aux")
	out, err := cmd.Output()
	if err != nil {
		return false, nil, err
	}

	lines := strings.Split(string(out), "\n")
	var procs []string
	for _, line := range lines {
		if strings.Contains(line, "Claude.app") || (strings.Contains(line, "--user-data-dir") && strings.Contains(line, "Claude")) {
			if !strings.Contains(line, "grep") && !strings.Contains(line, "probe_runner") {
				procs = append(procs, strings.TrimSpace(line))
			}
		}
	}
	return len(procs) > 0, procs, nil
}

func (d *DarwinPlatform) TerminateApp() error {
	running, _, err := d.IsAppRunning()
	if err != nil {
		return err
	}
	if !running {
		return nil
	}

	// Graceful pkill first
	_ = exec.Command("pkill", "-f", "Claude.app").Run()
	time.Sleep(1 * time.Second)

	// Check if still running
	stillRunning, _, _ := d.IsAppRunning()
	if stillRunning {
		// Force kill
		_ = exec.Command("pkill", "-9", "-f", "Claude.app").Run()
		time.Sleep(500 * time.Millisecond)
	}

	// Confirm the app is actually gone. Returning success while a process is
	// still holding the profile would let the caller sync into a live-writing
	// profile and corrupt the shared index.
	stillRunning, _, err = d.IsAppRunning()
	if err != nil {
		return err
	}
	if stillRunning {
		return fmt.Errorf("failed to terminate Claude Desktop: process still running after force kill")
	}
	return nil
}

// DetectRunningProfile returns the --user-data-dir path of the running Claude
// Desktop process. Profile paths routinely contain spaces (the default is
// ".../Application Support/Claude"), and `ps` renders args space-joined without
// quoting, so we cannot tokenize the command line on spaces. Instead we match
// against the known profile paths and require an argument boundary after the
// match, so ".../Claude" never matches ".../Claude_Profile2".
// It reports the first profile found when several are running, so callers that
// need all of them must use DetectRunningProfiles instead.
func (d *DarwinPlatform) DetectRunningProfile() (string, error) {
	running, err := d.DetectRunningProfiles()
	if err != nil || len(running) == 0 {
		return "", err
	}
	return running[0], nil
}

// DetectRunningProfiles returns every profile Claude Desktop is running on. See
// runningProfilesInProcs for why a process with no --user-data-dir is the
// default profile rather than an unknown one.
func (d *DarwinPlatform) DetectRunningProfiles() ([]string, error) {
	running, procs, err := d.IsAppRunning()
	if err != nil {
		return nil, err
	}
	if !running {
		return nil, nil
	}
	profiles, err := d.FindProfiles()
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(profiles))
	for _, p := range profiles {
		paths = append(paths, p.Path)
	}
	return runningProfilesInProcs(procs, paths, filepath.Join(d.AppSupportDir(), "Claude")), nil
}

// userDataDirFlag is how Claude Desktop is told which profile to run on. MCS
// always passes it when launching; anything else that opens the app does not.
const userDataDirFlag = "--user-data-dir="

// mainProcessMarker identifies Claude Desktop's own process as opposed to the
// Electron helpers under it. The helpers live in Frameworks/Claude Helper*.app,
// so their executable path reads ".../Claude Helper.app/Contents/MacOS/Claude
// Helper" and never contains this substring.
const mainProcessMarker = "Claude.app/Contents/MacOS/Claude"

// runningProfilesInProcs returns every known profile Claude Desktop is running
// on, in the order the processes appear, without repeats.
//
// Two things make this more than "matchProfileInProcs, but all of them":
//
// A main process carrying no --user-data-dir is the DEFAULT profile. Claude
// Desktop launched by anything other than MCS — the Dock, Spotlight, a login
// item, its own updater relaunching it — passes no flag and falls back to
// <AppSupport>/Claude. Matching on the flag alone made that process invisible,
// so MCS reported whichever OTHER profile was running as the one the user was
// on, and "reopen what was running" reopened the wrong account.
//
// The absent flag only means the default on the app's own process. Helpers and
// the crashpad handler routinely run with no path of their own, and counting
// those would report the default profile as running whenever any profile was.
func runningProfilesInProcs(procs, profilePaths []string, defaultPath string) []string {
	var out []string
	seen := make(map[string]bool, len(profilePaths))
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, line := range procs {
		if p := matchProfileInProcs([]string{line}, profilePaths); p != "" {
			add(p)
			continue
		}
		if strings.Contains(line, mainProcessMarker) && !strings.Contains(line, userDataDirFlag) {
			add(defaultPath)
		}
	}
	return out
}

// matchProfileInProcs returns the first known profile path that appears as a
// --user-data-dir=<path> argument in any process line, requiring an argument
// boundary (space or end-of-line) after the path so ".../Claude" does not match
// ".../Claude_Profile2". Pure function to keep the space-handling logic tested.
func matchProfileInProcs(procs, profilePaths []string) string {
	const flag = userDataDirFlag
	for _, line := range procs {
		for _, path := range profilePaths {
			needle := flag + path
			idx := strings.Index(line, needle)
			if idx < 0 {
				continue
			}
			after := idx + len(needle)
			if after == len(line) || line[after] == ' ' {
				return path
			}
		}
	}
	return ""
}

func (d *DarwinPlatform) LaunchProfile(profilePath string) error {
	cmd := exec.Command("open", "-n", "-a", "Claude", "--args", fmt.Sprintf("--user-data-dir=%s", profilePath))
	return cmd.Run()
}

// CreateProfile makes a sibling data directory Claude Desktop populates on first
// launch. Here the identity and the directory name coincide; they do not on the
// Store build, which is why both are returned.
func (d *DarwinPlatform) CreateProfile(clean string) (string, string, error) {
	appSup := d.AppSupportDir()
	if appSup == "" {
		return "", "", fmt.Errorf("could not determine user home directory")
	}
	identity := profileFolderPrefix + clean
	path := filepath.Join(appSup, identity)
	if _, err := os.Stat(path); err == nil {
		return "", "", fmt.Errorf("a profile folder named %q already exists", identity)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", "", fmt.Errorf("create profile folder: %w", err)
	}
	return identity, path, nil
}

func (d *DarwinPlatform) PrepareRecovery(newProfilePath string, sources []RecoverySource) error {
	return prepareRecoveryByCopy(newProfilePath, sources)
}

// PrepareArchive has nothing to prepare here: every profile is its own directory,
// so any of them can be renamed away without disturbing the others. It still
// resolves the two identities, so resolution happens in exactly one place per
// platform.
func (d *DarwinPlatform) PrepareArchive(keepIdentity, archiveIdentity string) (string, string, error) {
	appSup := d.AppSupportDir()
	if appSup == "" {
		return "", "", fmt.Errorf("could not determine user home directory")
	}
	return filepath.Join(appSup, keepIdentity), filepath.Join(appSup, archiveIdentity), nil
}

// PrepareRemove has nothing to refuse here: every profile is its own directory,
// so any of them can be renamed away without disturbing the others.
func (d *DarwinPlatform) PrepareRemove(identity string) (string, error) {
	appSup := d.AppSupportDir()
	if appSup == "" {
		return "", fmt.Errorf("could not determine user home directory")
	}
	return filepath.Join(appSup, identity), nil
}

// ArchiveDir keeps archives in MCS's own directory, beside backups/. It is outside
// AppSupportDir() and therefore outside FindProfiles' scan path, which is what
// stops an archived profile reappearing on the next Rescan, and it is on the same
// volume so archiving is a rename.
func (d *DarwinPlatform) ArchiveDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "multi-claude-switcher-archive")
	}
	return filepath.Join(home, ".multi-claude-switcher", "archive")
}

// InstallKind is always "macos" here: Claude Desktop on macOS ships only one way.
func (d *DarwinPlatform) InstallKind() string { return "macos" }
