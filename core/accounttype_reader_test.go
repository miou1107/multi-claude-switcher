// core/accounttype_reader_test.go
package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
)

// writeFixtureLS builds a minimal Chromium-style Local Storage LevelDB under
// <profile>/Local Storage/leveldb containing one bootstrap value with the given
// org JSON, and returns the profile path.
func writeFixtureLS(t *testing.T, orgJSON string) string {
	t.Helper()
	profile := t.TempDir()
	ldbDir := filepath.Join(profile, "Local Storage", "leveldb")
	if err := os.MkdirAll(ldbDir, 0755); err != nil {
		t.Fatal(err)
	}
	db, err := leveldb.OpenFile(ldbDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	val := append([]byte{1}, []byte(orgJSON)...) // 1 = UTF-8 encoding tag
	if err := db.Put([]byte("_https://claude.ai\x00\x01bootstrap"), val, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return profile
}

func TestDetectAccountType(t *testing.T) {
	team := writeFixtureLS(t, `{"organizations":[{"name":"Fontrip","rate_limit_tier":"default_raven","billing_type":"stripe_subscription"}]}`)
	if got, err := DetectAccountType(team); err != nil || got != AccountTeam {
		t.Errorf("team: got %v err %v want Team", got, err)
	}

	personal := writeFixtureLS(t, `{"organizations":[{"name":"x's Organization","rate_limit_tier":"default_claude_max_20x","billing_type":"stripe_subscription"}]}`)
	if got, err := DetectAccountType(personal); err != nil || got != AccountPersonal {
		t.Errorf("personal: got %v err %v want Personal", got, err)
	}

	// Missing Local Storage → Unknown + error, never a guess.
	if got, err := DetectAccountType(t.TempDir()); err == nil || got != AccountUnknown {
		t.Errorf("missing LS: got %v err %v want Unknown+error", got, err)
	}
}
