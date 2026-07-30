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

// failingSource returns a path that opens but cannot be read, which is what a
// copy interrupted partway looks like from copyFile's point of view: the
// destination has already been opened for writing by then.
//
// A directory is used because it is the one portable way to get that behaviour
// without a test-only hook in the production path — open(2) succeeds on a
// directory and read(2) then fails.
func failingSource(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "unreadable")
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(p)
	if err != nil {
		t.Skipf("this platform will not open a directory, so the failure cannot be staged: %v", err)
	}
	defer in.Close()
	if _, err := in.Read(make([]byte, 1)); err == nil {
		t.Skip("this platform reads directories without error, so the failure cannot be staged")
	}
	return p
}

// This is the test the staging mechanism exists for. A copy that dies partway
// must leave the destination exactly as it was.
//
// Without staging, the destination is opened O_TRUNC before the first byte is
// read, so an interrupted copy left a truncated file stamped with the time of
// the write — newer than its source, and sync keeps whichever copy is newer.
// The truncated file therefore became the current version of that conversation
// and the real one was overwritten on the next run.
//
// The assertion is deliberately on the destination's bytes rather than on the
// absence of a staging file: "no staging file" is trivially true of an
// implementation that has no staging at all.
func TestCopyFileLeavesTheDestinationIntactWhenTheCopyFails(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "conversation.json")
	const original = `{"messages":["the user's real conversation"]}`
	if err := os.WriteFile(dst, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(failingSource(t), dst); err == nil {
		t.Fatal("want an error from a source that cannot be read, got nil")
	}

	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("the destination was destroyed by a failed copy: %v", err)
	}
	if string(b) != original {
		t.Fatalf("a failed copy changed the destination:\nwant %s\ngot  %s", original, b)
	}
}

// And the failed attempt must not leave its staging file behind either, or a
// profile accumulates one dead file per interrupted copy.
func TestCopyFileCleansUpItsStagingFileWhenTheCopyFails(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "conversation.json")
	if err := os.WriteFile(dst, []byte(`{"v":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(failingSource(t), dst); err == nil {
		t.Fatal("want an error, got nil")
	}

	if _, err := os.Lstat(dst + copyTmpSuffix); err == nil {
		t.Fatal("a failed copy left its staging file behind")
	}
}

// The staged file must be swapped in by a rename rather than written through, so
// a reader either sees the old file or the new one and never a half-written mix.
// Checked by watching the destination's inode change: a rename replaces the file,
// a write in place does not.
func TestCopyFileSwapsTheDestinationRatherThanWritingThroughIt(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.json")
	dst := filepath.Join(root, "b.json")
	if err := os.WriteFile(src, []byte(`{"v":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte(`{"v":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}

	after, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("the destination was written through rather than replaced, so a reader could see a half-written file")
	}
}
