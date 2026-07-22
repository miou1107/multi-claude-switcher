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
