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
	return nil
}

func (d *DarwinPlatform) LaunchProfile(profilePath string) error {
	cmd := exec.Command("open", "-n", "-a", "Claude", "--args", fmt.Sprintf("--user-data-dir=%s", profilePath))
	return cmd.Run()
}
