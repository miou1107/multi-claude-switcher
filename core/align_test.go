package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManualAlignReturnsToRunningProfile(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "src-uuid")
	writeAccountConfig(t, dst, "dst-uuid")
	writeSessionFile(t, src, filepath.Join("src-uuid", "local_a.json"), `{"v":"work"}`, time.Now())

	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	mp := &mockPlatform{running: true, detected: src} // user is on src
	s := NewSwitcher(mp, bm)

	report, err := s.ManualAlign(src, dst)
	if err != nil {
		t.Fatalf("ManualAlign failed: %v", err)
	}
	if !mp.terminated {
		t.Error("expected Claude Desktop to be closed before writing")
	}
	if mp.launchedPath != src {
		t.Errorf("expected to reopen the running profile %q, got %q", src, mp.launchedPath)
	}
	if report.CopiedCount != 1 {
		t.Errorf("expected 1 session copied, got %d", report.CopiedCount)
	}
	// Session must be re-homed under the TARGET account bucket.
	want := filepath.Join(platformSessions(dst), "dst-uuid", "local_a.json")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected session at %s: %v", want, err)
	}
}

// TestManualAlignReopensEveryRunningProfile pins the behaviour that makes a
// sync safe when the user has more than one account open at once. Closing Claude
// Desktop closes ALL of them (there is one process per profile and MCS kills them
// together), so reopening only the one MCS happened to detect silently takes an
// account away from the user. An align changes no account, so it owes them every
// window it closed.
func TestManualAlignReopensEveryRunningProfile(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "src-uuid")
	writeAccountConfig(t, dst, "dst-uuid")
	writeSessionFile(t, src, filepath.Join("src-uuid", "local_a.json"), `{"v":"work"}`, time.Now())

	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	// Both accounts are open, which is normal: MCS launches each profile as its
	// own instance and nothing closes the previous one.
	mp := &mockPlatform{running: true, detectedAll: []string{src, dst}}
	s := NewSwitcher(mp, bm)

	if _, err := s.ManualAlign(src, dst); err != nil {
		t.Fatalf("ManualAlign failed: %v", err)
	}
	assertLaunched(t, mp.launchedPaths, src, dst)
}

// assertLaunched fails unless exactly the wanted profiles were launched, in any
// order and without repeats (a repeat leaves the user with two windows on one
// account).
func assertLaunched(t *testing.T, got []string, want ...string) {
	t.Helper()
	seen := make(map[string]int, len(got))
	for _, p := range got {
		seen[p]++
	}
	for _, w := range want {
		switch seen[w] {
		case 1:
			delete(seen, w)
		case 0:
			t.Errorf("profile %q was closed but never reopened (launched: %q)", w, got)
		default:
			t.Errorf("profile %q was launched %d times, want once (launched: %q)", w, seen[w], got)
			delete(seen, w)
		}
	}
	for extra := range seen {
		t.Errorf("profile %q was launched but was not running and is not the target (launched: %q)", extra, got)
	}
}

func TestManualAlignNoRelaunchWhenNothingRunning(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "src-uuid")
	writeAccountConfig(t, dst, "dst-uuid")
	writeSessionFile(t, src, filepath.Join("src-uuid", "local_a.json"), `{"v":"x"}`, time.Now())

	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	mp := &mockPlatform{running: false, detected: ""}
	s := NewSwitcher(mp, bm)

	if _, err := s.ManualAlign(src, dst); err != nil {
		t.Fatalf("ManualAlign failed: %v", err)
	}
	if mp.launched {
		t.Error("must not launch anything when nothing was running")
	}
}

func TestManualAlignAbortsWhenRunningProfileUnknown(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "src-uuid")
	writeAccountConfig(t, dst, "dst-uuid")
	writeSessionFile(t, src, filepath.Join("src-uuid", "local_a.json"), `{"v":"x"}`, time.Now())

	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	// App is running but its profile can't be identified (detected == "").
	mp := &mockPlatform{running: true, detected: ""}
	s := NewSwitcher(mp, bm)

	if _, err := s.ManualAlign(src, dst); err == nil {
		t.Fatal("expected ManualAlign to abort when the running profile can't be identified")
	}
	if mp.terminated {
		t.Error("must not close Claude Desktop when it cannot be reopened")
	}
	// Sync must not have run: the target must not have received the source session.
	if _, err := os.Stat(filepath.Join(platformSessions(dst), "dst-uuid", "local_a.json")); err == nil {
		t.Error("align wrote data despite aborting before close")
	}
}

func TestManualAlignReopensAfterSyncError(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "src-uuid")
	// dst has NO config.json -> SyncSessions errors at the account-UUID lookup.
	writeSessionFile(t, src, filepath.Join("src-uuid", "local_a.json"), `{"v":"x"}`, time.Now())

	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	mp := &mockPlatform{running: true, detected: src}
	s := NewSwitcher(mp, bm)

	if _, err := s.ManualAlign(src, dst); err == nil {
		t.Fatal("expected an error when the target isn't logged in")
	}
	if !mp.terminated {
		t.Error("app should have been terminated before the failing sync")
	}
	if mp.launchedPath != src {
		t.Errorf("must reopen the original profile %q even when the sync fails, got %q", src, mp.launchedPath)
	}
}
