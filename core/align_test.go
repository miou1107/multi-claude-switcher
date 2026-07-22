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
