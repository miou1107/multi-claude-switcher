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

// GetProfileSignedInOrgs returns every organization this profile has been
// signed into, as recorded by its own allowlist stamps.
//
// It exists because the ACTIVE organization is a heuristic (the newest stamp)
// while membership is not: a stamp is written when a profile is signed into an
// organization, so an organization with no stamp is one this profile has never
// opened and therefore cannot be the one Claude is reading right now.
//
// That distinction is what makes the misfiled-conversation cleanup safe. The
// pre-0.11.2 sync defect copied conversations in under the SOURCE profile's
// organization, which the target profile had typically never joined, so the
// folders it created are exactly the ones with no stamp. Relying on the active
// organization alone would mean trusting the heuristic to be right about which
// folder is live, and being wrong there would move conversations out of the
// folder the user is working in.
//
// Unlike GetProfileActiveOrgUUID this does not care when a stamp was written,
// only that one exists, so a malformed timestamp still counts as membership.
// Erring towards "this profile has been here" is the safe direction for every
// caller.
func GetProfileSignedInOrgs(profilePath string) (map[string]bool, error) {
	data, err := os.ReadFile(GetProfileConfigPath(profilePath))
	if err != nil {
		return nil, fmt.Errorf("read config.json for %s: %w", profilePath, err)
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config.json for %s: %w", profilePath, err)
	}
	out := map[string]bool{}
	for key := range cfg {
		if org, ok := strings.CutPrefix(key, allowlistStampPrefix); ok && org != "" {
			out[org] = true
		}
	}
	return out, nil
}
