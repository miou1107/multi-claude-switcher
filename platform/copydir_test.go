package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCopyDirMergeNeverClobbersWhatIsAlreadyThere(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	for _, d := range []string{src, dst} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "shared.json"), []byte(`{"from":"src"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "new.json"), []byte(`{"n":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "shared.json"), []byte(`{"from":"dst"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	n, err := CopyDirMerge(src, dst)
	if err != nil {
		t.Fatalf("CopyDirMerge: %v", err)
	}
	if n != 1 {
		t.Fatalf("copied = %d, want only the file dst lacked", n)
	}
	b, err := os.ReadFile(filepath.Join(dst, "shared.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"from":"dst"}` {
		t.Fatalf("an existing conversation must not be overwritten, got %s", b)
	}
}

// TestCopyDirMergePreservesModeAndTime: session files are 0600 and sync decides
// which of two copies is current by comparing modification times. A copy that
// widened the mode or stamped "now" would leak conversations and then win the next
// comparison against its own source.
func TestCopyDirMergePreservesModeAndTime(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(src, "a.json")
	if err := os.WriteFile(f, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(f, old, old); err != nil {
		t.Fatal(err)
	}

	if _, err := CopyDirMerge(src, dst); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(filepath.Join(dst, "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !fi.ModTime().Truncate(time.Second).Equal(old) {
		t.Errorf("mtime = %v, want the source's %v", fi.ModTime(), old)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600 — a wider mode exposes the conversation", fi.Mode().Perm())
	}
}

// TestCopyDirMergeLeavesNoVisibleStagingFile: a staging file left by a killed
// process must not look like a conversation to anything that scans for *.json.
func TestCopyDirMergeLeavesNoVisibleStagingFile(t *testing.T) {
	if strings.HasSuffix(copyTmpSuffix, ".json") {
		t.Fatalf("staging suffix %q would be picked up as a session file", copyTmpSuffix)
	}
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CopyDirMerge(src, dst); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), copyTmpSuffix) {
			t.Fatalf("a successful copy left staging file %q behind", e.Name())
		}
	}
}

// TestCopyFileReplacesAnAbandonedStagingFile: a staging file from a killed run
// carries the source's mode, so a read-only one would block the file from ever
// being copied again if it were not cleared first.
func TestCopyFileReplacesAnAbandonedStagingFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.json")
	dst := filepath.Join(root, "b.json")
	if err := os.WriteFile(src, []byte(`{"v":1}`), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst+copyTmpSuffix, []byte("junk"), 0o400); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("an abandoned staging file must not block the copy: %v", err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"v":1}` {
		t.Fatalf("contents = %s", b)
	}
}
