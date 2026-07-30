package platform

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestPrepareRecoveryCopiesTheBucket(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "Claude")
	dst := filepath.Join(root, "Claude_Recovered")
	bucket := filepath.Join(src, "claude-code-sessions", "orphan-uuid")
	if err := os.MkdirAll(bucket, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucket, "local_1.json"), []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := prepareRecoveryByCopy(dst, []RecoverySource{{Path: src, UUID: "orphan-uuid"}}); err != nil {
		t.Fatalf("prepareRecoveryByCopy: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(dst, "claude-code-sessions", "orphan-uuid", "local_1.json"))
	if err != nil {
		t.Fatalf("session not copied into the new profile: %v", err)
	}
	if string(b) != `{"a":1}` {
		t.Fatalf("contents = %q", b)
	}
	// The source is the only copy that matters until sign-in succeeds, so it
	// must be left exactly as it was.
	if _, err := os.Stat(filepath.Join(bucket, "local_1.json")); err != nil {
		t.Fatalf("source bucket was disturbed: %v", err)
	}
}

func TestPrepareRecoveryMergesEverySource(t *testing.T) {
	// An orphan split across two profiles. Recovering one share and dropping the
	// other would deliver less than the row's count promised.
	root := t.TempDir()
	dst := filepath.Join(root, "Claude_Recovered")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []RecoverySource
	for i, name := range []string{"Claude", "Claude_Two"} {
		src := filepath.Join(root, name)
		bucket := filepath.Join(src, "claude-code-sessions", "orphan-uuid")
		if err := os.MkdirAll(bucket, 0o755); err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(bucket, "local_"+strconv.Itoa(i)+".json")
		if err := os.WriteFile(f, []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, RecoverySource{Path: src, UUID: "orphan-uuid"})
	}

	if err := prepareRecoveryByCopy(dst, sources); err != nil {
		t.Fatalf("prepareRecoveryByCopy: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dst, "claude-code-sessions", "orphan-uuid"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want both shares copied, got %d entries", len(entries))
	}
}

func TestPrepareRecoveryMissingSourceBucketIsAnError(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "Claude")
	dst := filepath.Join(root, "Claude_Recovered")
	for _, d := range []string{src, dst} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := prepareRecoveryByCopy(dst, []RecoverySource{{Path: src, UUID: "not-there"}}); err == nil {
		t.Fatal("want an error when there is nothing to recover")
	}
}

func TestPrepareRecoveryNoSourcesIsAnError(t *testing.T) {
	if err := prepareRecoveryByCopy(t.TempDir(), nil); err == nil {
		t.Fatal("a recovery with nothing to recover from must not report success")
	}
}
