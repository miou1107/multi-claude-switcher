//go:build darwin

package platform

import (
	"os"
	"path/filepath"
)

// ClaudeApp names the Claude Desktop the switcher drives: its version, or ""
// when it cannot be read, and EditionMac, or "" when Claude.app is not found
// where `open -a Claude` looks first.
func ClaudeApp() (version, edition string) {
	candidates := []string{"/Applications/Claude.app"}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "Applications", "Claude.app"))
	}
	for _, app := range candidates {
		data, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
		if err != nil {
			continue
		}
		return plistShortVersion(data), EditionMac
	}
	return "", ""
}
