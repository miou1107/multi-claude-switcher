package core

import (
	"errors"
	"fmt"
	"strings"
)

// ProfileFolderPrefix is what a profile folder MCS creates is named with, so
// that platform.FindProfiles (which matches a "Claude" prefix) picks it up.
const ProfileFolderPrefix = "Claude_"

// reservedProfileName is the default profile's folder name. A user-supplied name
// producing it would collide with the profile Claude Desktop already owns.
const reservedProfileName = "Claude"

// ValidateProfileName reports whether name is usable for a new profile, and
// returns the cleaned form to use from then on. It runs before anything is
// created, so a rejected name never leaves a partial profile behind.
//
// The cleaned name is returned rather than just validated because it becomes the
// profile's display name and, on the standalone builds, part of its identity.
// Callers must pass this value on, never the raw input: trimming in one place and
// creating from another is how " Work " ends up as an identity with spaces around
// it — which is a live bug in the Store build's own name handling.
//
// Platform-specific limits — reserved names, collisions with an existing profile —
// are checked by the platform layer, which knows what a collision is on its own
// filesystem layout (platform/windows_msix.go's msixValidateNameIn).
func ValidateProfileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("enter a name for this account")
	}
	if strings.EqualFold(name, reservedProfileName) {
		return "", fmt.Errorf("%q is taken by the default profile, pick another name", reservedProfileName)
	}
	if strings.HasPrefix(name, ".") {
		return "", errors.New("a name can't start with a dot")
	}
	if strings.Contains(name, "..") {
		return "", errors.New("a name can't contain ..")
	}
	// Allow letters, digits, space, dash, underscore. Everything else is either a
	// path separator, a Windows-illegal filename character, or a control
	// character, and the point of an allowlist is that none of them need naming.
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == ' ', r == '-', r == '_':
		default:
			return "", errors.New("use only letters, numbers, spaces, dashes and underscores")
		}
	}
	return name, nil
}

// ProfileFolderName maps a cleaned name to the folder that holds it on the
// standalone builds. Call only with a value ValidateProfileName returned; it does
// no trimming of its own, on purpose, so a raw name cannot slip through.
func ProfileFolderName(clean string) string {
	return ProfileFolderPrefix + clean
}
