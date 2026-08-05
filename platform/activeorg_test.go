package platform

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeConfig(t *testing.T, profile, body string) {
	t.Helper()
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GetProfileConfigPath(profile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestGetProfileActiveOrgUUID pins how the organization a profile is currently
// working in is read back. Claude Desktop refreshes one organization's extension
// allowlist on each launch — the one it is signed in to — so the newest of those
// stamps names the organization whose conversations the app actually shows.
func TestGetProfileActiveOrgUUID(t *testing.T) {
	const (
		team     = "d129c8c1-7834-4e6c-84a4-dc19dfeedc8f"
		personal = "245fb00c-4b74-4d8d-9ba8-3580e216ff85"
	)

	t.Run("the most recently refreshed organization wins", func(t *testing.T) {
		p := t.TempDir()
		writeConfig(t, p, `{
			"lastKnownAccountUuid": "035899b2",
			"dxt:allowlistLastUpdated:`+personal+`": "2026-07-08T00:57:34.685Z",
			"dxt:allowlistLastUpdated:`+team+`": "2026-08-04T01:14:05.939Z"
		}`)
		got, err := GetProfileActiveOrgUUID(p)
		if err != nil {
			t.Fatalf("GetProfileActiveOrgUUID: %v", err)
		}
		if got != team {
			t.Errorf("got %q, want %q", got, team)
		}
	})

	t.Run("a single organization needs no comparison", func(t *testing.T) {
		p := t.TempDir()
		writeConfig(t, p, `{"dxt:allowlistLastUpdated:`+personal+`": "2026-07-08T00:57:34.685Z"}`)
		got, err := GetProfileActiveOrgUUID(p)
		if err != nil {
			t.Fatalf("GetProfileActiveOrgUUID: %v", err)
		}
		if got != personal {
			t.Errorf("got %q, want %q", got, personal)
		}
	})

	t.Run("no stamps at all is not an organization of zero", func(t *testing.T) {
		p := t.TempDir()
		writeConfig(t, p, `{"lastKnownAccountUuid": "035899b2"}`)
		if got, err := GetProfileActiveOrgUUID(p); err == nil {
			t.Errorf("got %q with no error; an unknown organization must be reported, not guessed", got)
		}
	})

	t.Run("an unreadable stamp is skipped rather than winning", func(t *testing.T) {
		p := t.TempDir()
		writeConfig(t, p, `{
			"dxt:allowlistLastUpdated:`+personal+`": "2026-07-08T00:57:34.685Z",
			"dxt:allowlistLastUpdated:`+team+`": "not a timestamp"
		}`)
		got, err := GetProfileActiveOrgUUID(p)
		if err != nil {
			t.Fatalf("GetProfileActiveOrgUUID: %v", err)
		}
		if got != personal {
			t.Errorf("got %q, want the one with a readable stamp (%q)", got, personal)
		}
	})

	t.Run("no config.json", func(t *testing.T) {
		if _, err := GetProfileActiveOrgUUID(t.TempDir()); err == nil {
			t.Error("a profile with no config.json must report an error")
		}
	})
}

// TestGetProfileActiveOrgUUIDIgnoresMalformedKeys guards the two ways a config.json
// entry can look like an organization stamp without being one.
func TestGetProfileActiveOrgUUIDIgnoresMalformedKeys(t *testing.T) {
	const real = "245fb00c-4b74-4d8d-9ba8-3580e216ff85"
	p := t.TempDir()
	writeConfig(t, p, `{
		"`+allowlistStampPrefix+`": "2030-01-01T00:00:00Z",
		"`+allowlistStampPrefix+`nested": {"at": "2030-01-01T00:00:00Z"},
		"`+allowlistStampPrefix+real+`": "2026-07-08T00:57:34.685Z"
	}`)
	got, err := GetProfileActiveOrgUUID(p)
	if err != nil {
		t.Fatalf("GetProfileActiveOrgUUID: %v", err)
	}
	if got != real {
		t.Errorf("got %q, want the one real organization (%q)", got, real)
	}
}

func TestGetProfileSignedInOrgs(t *testing.T) {
	dir := t.TempDir()
	cfg := `{
		"lastKnownAccountUuid": "acct",
		"dxt:allowlistLastUpdated:orgA": "2026-08-01T00:00:00Z",
		"dxt:allowlistLastUpdated:orgB": "2026-08-05T00:00:00Z",
		"dxt:allowlistLastUpdated:orgBROKEN": "not a timestamp",
		"dxt:allowlistLastUpdated:": "2026-08-05T00:00:00Z",
		"somethingElse": 1
	}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := GetProfileSignedInOrgs(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A malformed timestamp still counts as membership: this answers "has this
	// profile ever been here", and erring towards yes is the safe direction.
	want := map[string]bool{"orgA": true, "orgB": true, "orgBROKEN": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("signed-in orgs = %v, want %v", got, want)
	}
}

func TestGetProfileSignedInOrgsFailsRatherThanGuessing(t *testing.T) {
	if _, err := GetProfileSignedInOrgs(t.TempDir()); err == nil {
		t.Error("an unreadable config returned a membership set instead of an error")
	}
}
