//go:build windows

package core

import (
	"os"
	"os/exec"
	"testing"
)

// denyDirWrites makes dir reject creation of new children so a staged write into
// it fails. Windows ignores POSIX mode bits for access control, so this adds an
// explicit deny ACE for the current user blocking "add file" (WD) and "add
// subdirectory" (AD). The deny ACE is removed on cleanup so t.TempDir removal
// succeeds. A deny ACE is evaluated before allows and applies even to the
// directory owner, which a read-only attribute does not.
func denyDirWrites(t *testing.T, dir string) {
	t.Helper()
	user := os.Getenv("USERNAME")
	if user == "" {
		t.Skip("USERNAME not set; cannot apply a deny ACE")
	}
	if out, err := exec.Command("icacls", dir, "/deny", user+":(WD,AD)").CombinedOutput(); err != nil {
		t.Fatalf("icacls deny on %s: %v: %s", dir, err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("icacls", dir, "/remove:d", user).Run()
	})
}
