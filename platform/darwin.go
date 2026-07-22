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
func (d *DarwinPlatform) DetectRunningProfile() (string, error) {
	running, procs, err := d.IsAppRunning()
	if err != nil {
		return "", err
	}
	if !running {
		return "", nil
	}
	profiles, err := d.FindProfiles()
	if err != nil {
		return "", err
	}
	paths := make([]string, 0, len(profiles))
	for _, p := range profiles {
		paths = append(paths, p.Path)
	}
	return matchProfileInProcs(procs, paths), nil
}

// matchProfileInProcs returns the first known profile path that appears as a
// --user-data-dir=<path> argument in any process line, requiring an argument
// boundary (space or end-of-line) after the path so ".../Claude" does not match
// ".../Claude_Profile2". Pure function to keep the space-handling logic tested.
func matchProfileInProcs(procs, profilePaths []string) string {
	const flag = "--user-data-dir="
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
