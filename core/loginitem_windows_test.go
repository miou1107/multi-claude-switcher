//go:build windows

package core

import (
	"os/exec"
	"strings"
	"testing"
)

// withStubbedRunKey redirects the Run key to a throwaway HKCU\Software subtree so
// tests never touch the real autostart key, and deletes it afterwards.
func withStubbedRunKey(t *testing.T) {
	t.Helper()
	orig := loginRunKey
	loginRunKey = `HKCU\Software\Multi-Claude-Switcher-Test\Run`
	t.Cleanup(func() {
		_ = exec.Command("reg", "delete", `HKCU\Software\Multi-Claude-Switcher-Test`, "/f").Run()
		loginRunKey = orig
	})
}

func TestLoginItemEnableDisable(t *testing.T) {
	withStubbedRunKey(t)

	if LoginItemEnabled() {
		t.Fatal("expected login item disabled initially")
	}

	exe := `C:\Program Files\Multi-Claude-Switcher\mcs-tray.exe`
	if err := EnableLoginItem(exe); err != nil {
		t.Fatalf("EnableLoginItem: %v", err)
	}
	if !LoginItemEnabled() {
		t.Fatal("expected login item enabled after EnableLoginItem")
	}

	// The stored Run value must carry the executable path (with spaces intact),
	// which validates the reg.exe / exec argument quoting round-trip.
	out, err := exec.Command("reg", "query", loginRunKey, "/v", loginRunValue).CombinedOutput()
	if err != nil {
		t.Fatalf("reg query: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), exe) {
		t.Errorf("Run value missing exe path %q\n---\n%s", exe, out)
	}

	if err := DisableLoginItem(); err != nil {
		t.Fatalf("DisableLoginItem: %v", err)
	}
	if LoginItemEnabled() {
		t.Fatal("expected login item disabled after DisableLoginItem")
	}
}

func TestDisableLoginItemWhenAbsentIsNoError(t *testing.T) {
	withStubbedRunKey(t)
	if err := DisableLoginItem(); err != nil {
		t.Fatalf("disabling an absent login item should be a no-op, got %v", err)
	}
}

func TestEnableLoginItemEmptyPathErrors(t *testing.T) {
	withStubbedRunKey(t)
	if err := EnableLoginItem(""); err == nil {
		t.Fatal("expected error for empty executable path")
	}
}
