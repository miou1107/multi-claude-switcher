package platform

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// allowlistStampPrefix is the config.json key Claude Desktop refreshes for the
// organization it is signed in to, once per launch.
const allowlistStampPrefix = "dxt:allowlistLastUpdated:"

// GetProfileActiveOrgUUID reads the organization a profile is currently working
// in.
//
// It matters because sessions live at
// claude-code-sessions/<accountUuid>/<orgUuid>/, and the app reads exactly ONE of
// those organization folders: the one it is signed in to. Conversations filed
// under any other organization are on disk and invisible, which is what made
// importing into a Team account look impossible — sync rewrote the account
// segment and left the organization segment pointing at the source's
// organization, so everything it copied landed in a folder the target never
// opens.
//
// Anthropic does not publish this, so it is read from a side effect: config.json
// carries a "dxt:allowlistLastUpdated:<orgUuid>" stamp per organization, and only
// the signed-in one is refreshed on launch. Measured on a machine with two
// profiles and four organizations between them: each launch updated exactly the
// stamp of the organization whose session folder that profile then read, matching
// the app's own log. The newest stamp is therefore the active organization.
//
// It is a heuristic on a private format, so it fails loudly rather than guessing:
// an unknown organization returns an error, and its caller keeps the behaviour it
// had before rather than filing conversations somewhere arbitrary. Being wrong
// here costs visibility, never data.
func GetProfileActiveOrgUUID(profilePath string) (string, error) {
	data, err := os.ReadFile(GetProfileConfigPath(profilePath))
	if err != nil {
		return "", fmt.Errorf("read config.json for %s: %w", profilePath, err)
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parse config.json for %s: %w", profilePath, err)
	}

	best, bestAt := "", time.Time{}
	for key, raw := range cfg {
		org, ok := strings.CutPrefix(key, allowlistStampPrefix)
		if !ok || org == "" {
			continue
		}
		var stamp string
		if err := json.Unmarshal(raw, &stamp); err != nil {
			continue
		}
		at, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			// A stamp that cannot be read is not evidence of anything. Skipping it
			// leaves a readable one to win, where treating it as the zero time would
			// let a damaged entry outrank every real one on the "no stamps" path.
			continue
		}
		if at.After(bestAt) {
			best, bestAt = org, at
		}
	}
	if best == "" {
		return "", fmt.Errorf("no organization recorded in %s (has this profile been opened since signing in?)",
			GetProfileConfigPath(profilePath))
	}
	return best, nil
}
