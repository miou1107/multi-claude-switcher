package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// mockPlatform is a test double for platform.Platform.
type mockPlatform struct {
	running      bool
	launched     bool
	launchedPath string
	terminated   bool
	detected     string // DetectRunningProfile result
}

func (m *mockPlatform) AppSupportDir() string                          { return "" }
func (m *mockPlatform) FindProfiles() ([]*platform.ProfileInfo, error) { return nil, nil }
func (m *mockPlatform) IsAppRunning() (bool, []string, error)          { return m.running, nil, nil }
func (m *mockPlatform) TerminateApp() error {
	m.terminated = true
	m.running = false
	return nil
}
func (m *mockPlatform) DetectRunningProfile() (string, error) { return m.detected, nil }
func (m *mockPlatform) LaunchProfile(profilePath string) error {
	m.launched = true
	m.launchedPath = profilePath
	return nil
}

// TestSafeSwitchLaunchesWhenTargetNotLoggedIn verifies that switching to a
// fresh profile with no account yet (no config.json) skips the sync but still
// launches it — so `switch` can be used to open a profile in order to log in.
func TestSafeSwitchLaunchesWhenTargetNotLoggedIn(t *testing.T) {
	withStubbedSettings(t)
	tempDir := t.TempDir()

	src := filepath.Join(tempDir, "Src")
	writeAccountConfig(t, src, "uuid1")
	srcSessions := filepath.Join(platform.GetProfileSessionsDir(src), "uuid1")
	if err := os.MkdirAll(srcSessions, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcSessions, "local_src.json"), []byte(`{"src":1}`), 0644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(tempDir, "Dst") // no config.json -> not logged in
	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	mp := &mockPlatform{}
	s := NewSwitcher(mp, bm)

	if err := s.SafeSwitch(src, dst); err != nil {
		t.Fatalf("expected switch to a not-logged-in target to succeed (skip sync, still launch), got %v", err)
	}
	if !mp.launched {
		t.Error("target profile must still be launched even though sync was skipped")
	}
}

// TestSafeSwitchAbortsWhenBackupFails verifies that if a profile has data but
// the backup step fails, SafeSwitch aborts BEFORE aligning (never destroy data
// without a backup). Backup only runs when auto sync is ON and both profiles
// are logged in, so this test turns auto sync ON.
func TestSafeSwitchAbortsWhenBackupFails(t *testing.T) {
	withStubbedSettings(t)
	if err := SetAutoSyncOnSwitch(true); err != nil { // ON so the backup step runs
		t.Fatal(err)
	}
	tempDir := t.TempDir()

	src := filepath.Join(tempDir, "Src")
	writeAccountConfig(t, src, "uuid1")
	srcSessions := filepath.Join(platform.GetProfileSessionsDir(src), "uuid1")
	if err := os.MkdirAll(srcSessions, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcSessions, "local_src.json"), []byte(`{"src":1}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Target has real data we must not lose.
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, dst, "uuid2")
	dstSessions := filepath.Join(platform.GetProfileSessionsDir(dst), "uuid2")
	if err := os.MkdirAll(dstSessions, 0755); err != nil {
		t.Fatal(err)
	}
	dstFile := filepath.Join(dstSessions, "local_dst.json")
	original := []byte(`{"dst":"precious"}`)
	if err := os.WriteFile(dstFile, original, 0644); err != nil {
		t.Fatal(err)
	}

	// Force backup to fail: a regular file where the backup root needs a dir.
	blocker := filepath.Join(tempDir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	bm := NewBackupManager(filepath.Join(blocker, "backups"))

	mp := &mockPlatform{}
	s := NewSwitcher(mp, bm)

	if err := s.SafeSwitch(src, dst); err == nil {
		t.Fatal("expected SafeSwitch to abort when backup fails, got nil error")
	}
	if mp.launched {
		t.Error("target profile must NOT be launched after a failed backup")
	}
	got, readErr := os.ReadFile(dstFile)
	if readErr != nil {
		t.Fatalf("target file disappeared: %v", readErr)
	}
	if string(got) != string(original) {
		t.Errorf("target file was overwritten despite backup failure: got %q", got)
	}
}

// TestSafeSwitchOffMovesNoData verifies that with auto sync OFF (the
// default), SafeSwitch is a pure account switch — no session data moves.
func TestSafeSwitchOffMovesNoData(t *testing.T) {
	withStubbedSettings(t) // default OFF
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "src-uuid")
	writeAccountConfig(t, dst, "dst-uuid")
	writeSessionFile(t, src, filepath.Join("src-uuid", "local_a.json"), `{"v":"A"}`, time.Now())

	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	mp := &mockPlatform{}
	s := NewSwitcher(mp, bm)

	if err := s.SafeSwitch(src, dst); err != nil {
		t.Fatalf("pure switch should succeed: %v", err)
	}
	if !mp.launched {
		t.Error("target must still be launched on a pure switch")
	}
	// The source session must NOT have been copied into the target.
	if _, err := os.Stat(filepath.Join(platformSessions(dst), "dst-uuid", "local_a.json")); err == nil {
		t.Error("OFF switch moved session data — it must be a pure switch")
	}
}

// TestSafeSwitchOnUnionsBothAccounts verifies that with auto sync ON,
// SafeSwitch backs up and unions both accounts' sessions bidirectionally.
func TestSafeSwitchOnUnionsBothAccounts(t *testing.T) {
	withStubbedSettings(t)
	if err := SetAutoSyncOnSwitch(true); err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "src-uuid")
	writeAccountConfig(t, dst, "dst-uuid")
	writeSessionFile(t, src, filepath.Join("src-uuid", "local_a.json"), `{"v":"A"}`, time.Now())
	writeSessionFile(t, dst, filepath.Join("dst-uuid", "local_b.json"), `{"v":"B"}`, time.Now())

	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	mp := &mockPlatform{}
	s := NewSwitcher(mp, bm)

	if err := s.SafeSwitch(src, dst); err != nil {
		t.Fatalf("ON switch failed: %v", err)
	}
	if !mp.launched {
		t.Error("target must be launched")
	}
	for _, want := range []string{
		filepath.Join(platformSessions(dst), "dst-uuid", "local_a.json"),
		filepath.Join(platformSessions(src), "src-uuid", "local_b.json"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected union file %s: %v", want, err)
		}
	}
}

// TestSafeSwitchProceedsWhenTargetIsEmpty verifies a brand-new target profile
// (no sessions dir, nothing to lose) does not block the switch.
func TestSafeSwitchProceedsWhenTargetIsEmpty(t *testing.T) {
	withStubbedSettings(t)
	tempDir := t.TempDir()

	src := filepath.Join(tempDir, "Src")
	writeAccountConfig(t, src, "uuid1")
	srcSessions := filepath.Join(platform.GetProfileSessionsDir(src), "uuid1")
	if err := os.MkdirAll(srcSessions, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcSessions, "local_src.json"), []byte(`{"src":1}`), 0644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(tempDir, "Dst") // no sessions dir at all
	writeAccountConfig(t, dst, "uuid1")
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
