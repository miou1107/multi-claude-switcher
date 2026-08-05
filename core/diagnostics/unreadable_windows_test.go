//go:build windows

package diagnostics

import (
	"testing"

	"golang.org/x/sys/windows"
)

// makeUnreadable makes path fail to open, by holding it with no sharing.
//
// os.Chmod cannot do this on Windows: there are no POSIX permission bits, and
// Chmod drives only the read-only attribute, which does not stop a read. A
// 0o000 file stays perfectly readable, so a test that relies on Chmod to
// produce an unreadable file is testing the readable path and asserting the
// unreadable one.
//
// An exclusive handle is also the realistic shape of the failure being guarded
// against: a log the report cannot read on Windows is one another process has
// open, not one with the wrong mode.
func makeUnreadable(t *testing.T, path string) {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(
		p,
		windows.GENERIC_READ,
		0, // no FILE_SHARE_* at all: every later open fails
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatalf("could not take an exclusive handle on %s: %v", path, err)
	}
	// Registered after the temp directory's own cleanup, so it runs first and the
	// directory is removable.
	t.Cleanup(func() { _ = windows.CloseHandle(h) })
}
