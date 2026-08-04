package platform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// SamePath reports whether two paths name the same profile directory.
//
// Profile paths reach MCS from two directions that spell them differently: the
// platform reports the canonical path it discovered, while a caller may pass
// whatever the user typed (`mcs switch <path>`), trailing separator and all. On
// Windows the same directory is also routinely spelled with different casing.
// Comparing raw strings there makes one directory look like two, which is how a
// profile gets launched twice or excluded from a set it belongs to.
func SamePath(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// ProfileInfo holds basic information about a detected Claude Desktop profile.
type ProfileInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// Exists is false only for a profile MCS knows about that currently has no
	// directory. That is a real state on the Windows Store build: creating a
	// profile parks the live slot and leaves the slot absent so the packaged app
	// makes a clean one, so between those two moments state.json names a profile
	// with nothing on disk. It must still be listed — the user has just been told
	// to sign in to it.
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

	// DetectRunningProfiles returns every profile Claude Desktop is currently
	// running on, or nil if none / not detectable. More than one is normal: each
	// profile runs as its own instance and opening one does not close another.
	//
	// Callers that are about to close Claude Desktop must use this rather than
	// DetectRunningProfile. Closing it closes every profile at once, so an
	// operation that reopens only one leaves the user's other accounts shut.
	DetectRunningProfiles() ([]string, error)

	// TerminateApp cleanly closes or terminates all running Claude Desktop processes.
	TerminateApp() error

	// LaunchProfile launches Claude Desktop using the specified profile path via --user-data-dir.
	LaunchProfile(profilePath string) error

	// CreateProfile makes a new profile that Claude Desktop will populate on its
	// next launch. It returns the profile's IDENTITY — the name FindProfiles will
	// report for it, and the key every MCS registry uses — and the directory its
	// data will live in.
	//
	// The two are returned separately because they are not the same thing and
	// neither is derivable from the other. On the standalone builds the identity is
	// the directory name; on the Store build the identity is the name written to
	// state.json while the directory is the shared slot, always called "Claude".
	// Callers must use what is returned and must never take filepath.Base of the
	// directory.
	//
	// The directory is not guaranteed to exist yet: the Store build deliberately
	// leaves its slot absent so the packaged app creates a clean one.
	//
	// Caller must have terminated Claude first, and must pass a name
	// core.ValidateProfileName has accepted and cleaned.
	CreateProfile(clean string) (identity string, dataDir string, err error)

	// PrepareRecovery arranges for the saved conversations named by sources to end
	// up in newProfilePath once the user signs in. The standalone builds copy the
	// buckets across now; the Store build has already queued the copy as part of
	// CreateProfile and does nothing here.
	PrepareRecovery(newProfilePath string, sources []RecoverySource) error

	// PrepareArchive takes two profiles by IDENTITY, puts them into a state where
	// the second can be archived by a plain rename, and returns where each one's
	// data sits afterwards.
	//
	// It takes identities rather than paths because resolving an identity to a path
	// is a platform concern, and because a Store-build swap moves both directories —
	// so any path the caller was holding is stale by the time this returns.
	//
	// It exists for the Store build, where the active profile occupies a shared slot
	// that state.json names. Renaming that slot away would leave state.json pointing
	// at nothing, so only a parked profile may be archived; when the caller wants to
	// keep the parked one, the two are swapped first. Elsewhere it only resolves the
	// paths.
	//
	// Caller must have terminated Claude first.
	PrepareArchive(keepIdentity, archiveIdentity string) (keepPath string, archivePath string, err error)

	// ArchiveDir returns the root that archived profiles are parked under. It is
	// chosen per platform so archiving is a same-volume rename and the result sits
	// outside FindProfiles' scan path, which is what stops an archived profile
	// reappearing on the next Rescan.
	ArchiveDir() string

	// InstallKind names which Claude Desktop install this machine has, for bug
	// reports: "standalone", "store", "macos". The two Windows builds behave
	// differently enough that a report which does not say which one it is cannot
	// be acted on.
	InstallKind() string
}

// profileFolderPrefix is what MCS names a profile folder it creates, chosen so
// FindProfiles' "Claude" prefix match picks it up. It duplicates
// core.ProfileFolderPrefix on purpose: platform must not import core.
const profileFolderPrefix = "Claude_"

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
