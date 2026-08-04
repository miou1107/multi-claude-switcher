package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetProfileClaudeVersion(t *testing.T) {
	t.Run("reads the updater's last seen version", func(t *testing.T) {
		p := t.TempDir()
		if err := os.WriteFile(GetProfileConfigPath(p),
			[]byte(`{"lastKnownAccountUuid":"x","updaterLastSeenVersion":"1.24012.11"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := GetProfileClaudeVersion(p)
		if err != nil {
			t.Fatalf("GetProfileClaudeVersion: %v", err)
		}
		if got != "1.24012.11" {
			t.Errorf("got %q, want 1.24012.11", got)
		}
	})

	t.Run("a config without the key is reported, not guessed", func(t *testing.T) {
		p := t.TempDir()
		if err := os.WriteFile(GetProfileConfigPath(p), []byte(`{"lastKnownAccountUuid":"x"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, err := GetProfileClaudeVersion(p); err == nil {
			t.Errorf("got %q with no error; an absent version must be reported", got)
		}
	})

	t.Run("no config.json", func(t *testing.T) {
		if _, err := GetProfileClaudeVersion(t.TempDir()); err == nil {
			t.Error("a profile with no config.json must report an error")
		}
	})
}

func TestGetProfileClaudeCodeVersion(t *testing.T) {
	t.Run("the newest version directory wins", func(t *testing.T) {
		p := t.TempDir()
		for _, v := range []string{"2.1.9", "2.1.219", "2.0.1"} {
			if err := os.MkdirAll(filepath.Join(p, "claude-code", v), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		got, err := GetProfileClaudeCodeVersion(p)
		if err != nil {
			t.Fatalf("GetProfileClaudeCodeVersion: %v", err)
		}
		// Numeric, not lexical: "2.1.9" sorts after "2.1.219" as text.
		if got != "2.1.219" {
			t.Errorf("got %q, want 2.1.219", got)
		}
	})

	t.Run("no claude-code directory", func(t *testing.T) {
		if _, err := GetProfileClaudeCodeVersion(t.TempDir()); err == nil {
			t.Error("a profile with no CLI must report an error, not an empty version")
		}
	})
}
