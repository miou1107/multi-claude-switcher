package platform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// GetProfileClaudeVersion reads which Claude Desktop version a profile last saw.
//
// Read from config.json rather than from the installed app, because config.json
// is already being read for the account and the organization, and the key is the
// same on every platform — where the app itself is an Info.plist on macOS, a
// versioned directory name on the Windows standalone build, and a package
// identity on the Store build.
func GetProfileClaudeVersion(profilePath string) (string, error) {
	data, err := os.ReadFile(GetProfileConfigPath(profilePath))
	if err != nil {
		return "", fmt.Errorf("read config.json: %w", err)
	}
	var cfg struct {
		UpdaterLastSeenVersion string `json:"updaterLastSeenVersion"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parse config.json: %w", err)
	}
	if cfg.UpdaterLastSeenVersion == "" {
		return "", fmt.Errorf("no updaterLastSeenVersion in %s", GetProfileConfigPath(profilePath))
	}
	return cfg.UpdaterLastSeenVersion, nil
}

// GetProfileClaudeCodeVersion reads the bundled CLI's version, which Claude
// Desktop records only as a directory name: <profile>/claude-code/<version>/.
// More than one can be present after an update, so the newest wins.
func GetProfileClaudeCodeVersion(profilePath string) (string, error) {
	dir := filepath.Join(profilePath, "claude-code")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dir, err)
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "" && e.Name()[0] != '.' {
			versions = append(versions, e.Name())
		}
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("no version directory under %s", dir)
	}
	sort.Slice(versions, func(i, j int) bool { return lessVersion(versions[j], versions[i]) })
	return versions[0], nil
}

// lessVersion compares dotted versions component by component as numbers, so
// 2.1.9 sorts below 2.1.219 instead of above it the way text would.
func lessVersion(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		ai, bi := versionPart(as, i), versionPart(bs, i)
		if ai != bi {
			return ai < bi
		}
	}
	return a < b
}

func versionPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, err := strconv.Atoi(parts[i])
	if err != nil {
		return 0
	}
	return n
}
