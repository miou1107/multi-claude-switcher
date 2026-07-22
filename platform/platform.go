package platform

import (
	"path/filepath"
)

// ProfileInfo holds basic information about a detected Claude Desktop profile.
type ProfileInfo struct {
	Name            string            `json:"name"`
	Path            string            `json:"path"`
	Exists          bool              `json:"exists"`
	HasSessionsDir  bool              `json:"has_sessions_dir"`
	UUIDBuckets     map[string]int    `json:"uuid_buckets"` // UUID -> session count
}

// Platform defines OS-specific operations required for profile switching and launcher actions.
type Platform interface {
	// AppSupportDir returns the root user data directory for applications (e.g. ~/Library/Application Support).
	AppSupportDir() string

	// FindProfiles locates all available Claude Desktop profiles.
	FindProfiles() ([]*ProfileInfo, error)

	// IsAppRunning checks if any Claude Desktop process is currently active.
	IsAppRunning() (bool, []string, error)

	// TerminateApp cleanly closes or terminates all running Claude Desktop processes.
	TerminateApp() error

	// LaunchProfile launches Claude Desktop using the specified profile path via --user-data-dir.
	LaunchProfile(profilePath string) error
}

// GetProfileSessionsDir returns the path to claude-code-sessions under a given profile path.
func GetProfileSessionsDir(profilePath string) string {
	return filepath.Join(profilePath, "claude-code-sessions")
}
