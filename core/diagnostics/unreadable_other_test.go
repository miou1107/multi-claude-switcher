//go:build !windows

package diagnostics

import (
	"os"
	"testing"
)

// makeUnreadable makes path fail to open. Where permission bits are real, the
// mode is the direct way to say it; see the Windows file for why that does not
// carry across.
func makeUnreadable(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
}
