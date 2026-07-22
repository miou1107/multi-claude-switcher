package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// mockPlatform is a test double for platform.Platform.
type mockPlatform struct {
	running      bool
	launched     bool
	launchedPath string
	terminated   bool
}

func (m *mockPlatform) AppSupportDir() string                          { return "" }
func (m *mockPlatform) FindProfiles() ([]*platform.ProfileInfo, error) { return nil, nil }
func (m *mockPlatform) IsAppRunning() (bool, []string, error)          { return m.running, nil, nil }
func (m *mockPlatform) TerminateApp() error {
	m.terminated = true
	m.running = false
	return nil
}
func (m *mockPlatform) DetectRunningProfile() (string, error) { return "", nil }
func (m *mockPlatform) LaunchProfile(profilePath string) error {
	m.launched = true
	m.launchedPath = profilePath
	return nil
}

// TestSafeSwitchAbortsWhenBackupFails verifies that if the target profile has
// data but the backup step fails, SafeSwitch aborts BEFORE overwriting the
// target (never destroy data without a backup).
func TestSafeSwitchAbortsWhenBackupFails(t *testing.T) {
	tempDir := t.TempDir()

	// Source profile with one session file.
	src := filepath.Join(tempDir, "Src")
	srcSessions := filepath.Join(platform.GetProfileSessionsDir(src), "uuid1")
	if err := os.MkdirAll(srcSessions, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcSessions, "local_src.json"), []byte(`{"src":1}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Target profile that ALREADY has real data we must not lose.
	dst := filepath.Join(tempDir, "Dst")
	dstSessions := filepath.Join(platform.GetProfileSessionsDir(dst), "uuid1")
	if err := os.MkdirAll(dstSessions, 0755); err != nil {
		t.Fatal(err)
	}
	dstFile := filepath.Join(dstSessions, "local_dst.json")
	original := []byte(`{"dst":"precious"}`)
	if err := os.WriteFile(dstFile, original, 0644); err != nil {
		t.Fatal(err)
	}

	// Force backup to fail: place a regular file where the backup root needs a dir.
	blocker := filepath.Join(tempDir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	bm := NewBackupManager(filepath.Join(blocker, "backups")) // MkdirAll under a file -> fails

	mp := &mockPlatform{}
	s := NewSwitcher(mp, bm)

	err := s.SafeSwitch(src, dst)
	if err == nil {
		t.Fatal("expected SafeSwitch to abort when backup fails, got nil error")
	}
	if mp.launched {
		t.Error("target profile must NOT be launched after a failed backup")
	}
	// Target data must be untouched (sync must not have run).
	got, readErr := os.ReadFile(dstFile)
	if readErr != nil {
		t.Fatalf("target file disappeared: %v", readErr)
	}
	if string(got) != string(original) {
		t.Errorf("target file was overwritten despite backup failure: got %q", got)
	}
}

// TestSafeSwitchProceedsWhenTargetIsEmpty verifies a brand-new target profile
// (no sessions dir, nothing to lose) does not block the switch.
func TestSafeSwitchProceedsWhenTargetIsEmpty(t *testing.T) {
	tempDir := t.TempDir()

	src := filepath.Join(tempDir, "Src")
	srcSessions := filepath.Join(platform.GetProfileSessionsDir(src), "uuid1")
	if err := os.MkdirAll(srcSessions, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcSessions, "local_src.json"), []byte(`{"src":1}`), 0644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(tempDir, "Dst") // no sessions dir at all
	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	mp := &mockPlatform{}
	s := NewSwitcher(mp, bm)

	if err := s.SafeSwitch(src, dst); err != nil {
		t.Fatalf("expected switch to succeed for empty target, got %v", err)
	}
	if !mp.launched {
		t.Error("expected target profile to be launched")
	}
}
