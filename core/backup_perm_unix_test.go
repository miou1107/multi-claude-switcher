//go:build !windows

package core

import (
	"os"
	"testing"
)

// denyDirWrites makes dir reject creation of new children so a staged write into
// it fails. On Unix a read-only mode (0555) is sufficient. Access is restored on
// cleanup so t.TempDir removal succeeds.
func denyDirWrites(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("chmod %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
}
