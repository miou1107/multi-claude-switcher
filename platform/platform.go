package platform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ProfileInfo holds basic information about a detected Claude Desktop profile.
type ProfileInfo struct {
	Name           string         `json:"name"`
	Path           string         `json:"path"`
	Exists         bool           `json:"exists"`
	HasSessionsDir bool           `json:"has_sessions_dir"`
	UUIDBuckets    map[string]int `json:"uuid_buckets"` // UUID -> session count
	// Managed marks a profile that MCS itself created/manages (currently the
	// Windows Store/MSIX profiles, which live in an MCS-owned container). Such a
	// profile is always shown in the menu even before it has any session data,
	// because a freshly created account has none until the user signs in.
	Managed bool `json:"managed"`
}

// Platform defines OS-specific operations required for profile switching and launcher actions.
type Platform interface {
	// AppSupportDir returns the root user data directory for applications (e.g. ~/Library/Application Support).
	AppSupportDir() string

	// FindProfiles locates all available Claude Desktop profiles.
	FindProfiles() ([]*ProfileInfo, error)

	// IsAppRunning checks if any Claude Desktop process is currently active.
	IsAppRunning() (bool, []string, error)

	// DetectRunningProfile returns the --user-data-dir path of the currently
	// running Claude Desktop process, or "" if none / not detectable.
	DetectRunningProfile() (string, error)

	// TerminateApp cleanly closes or terminates all running Claude Desktop processes.
	TerminateApp() error

	// LaunchProfile launches Claude Desktop using the specified profile path via --user-data-dir.
	LaunchProfile(profilePath string) error
}

// GetProfileSessionsDir returns the path to claude-code-sessions under a given profile path.
func GetProfileSessionsDir(profilePath string) string {
	return filepath.Join(profilePath, "claude-code-sessions")
}

// GetProfileConfigPath returns the path to config.json under a given profile path.
func GetProfileConfigPath(profilePath string) string {
	return filepath.Join(profilePath, "config.json")
}

// GetProfileAccountUUID reads the logged-in account UUID (lastKnownAccountUuid)
// from a profile's config.json.
//
// This is the single most important identifier for sync: Claude Desktop's Code
// tab enumerates sessions ONLY from claude-code-sessions/<lastKnownAccountUuid>/.
// Copying sessions under any other bucket name is invisible to the app, so sync
// must always target the bucket named after this UUID.
func GetProfileAccountUUID(profilePath string) (string, error) {
	data, err := os.ReadFile(GetProfileConfigPath(profilePath))
	if err != nil {
		return "", fmt.Errorf("read config.json for %s: %w", profilePath, err)
	}
	var cfg struct {
		LastKnownAccountUUID string `json:"lastKnownAccountUuid"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parse config.json for %s: %w", profilePath, err)
	}
	if cfg.LastKnownAccountUUID == "" {
		return "", fmt.Errorf("no lastKnownAccountUuid in %s (profile not logged in?)", GetProfileConfigPath(profilePath))
	}
	return cfg.LastKnownAccountUUID, nil
}
