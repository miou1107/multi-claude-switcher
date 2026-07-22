package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withStubbedLoginItem redirects the LaunchAgents dir to a temp dir so tests
// never touch the real ~/Library/LaunchAgents.
func withStubbedLoginItem(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := launchAgentsDir
	launchAgentsDir = func() string { return dir }
	t.Cleanup(func() { launchAgentsDir = orig })
	return dir
}

func TestLoginItemEnableDisable(t *testing.T) {
	dir := withStubbedLoginItem(t)

	if LoginItemEnabled() {
		t.Fatal("expected login item disabled initially")
	}

	exe := "/Applications/Multi-Claude Switcher.app/Contents/MacOS/mcs-tray"
	if err := EnableLoginItem(exe); err != nil {
		t.Fatalf("EnableLoginItem: %v", err)
	}
	if !LoginItemEnabled() {
		t.Fatal("expected login item enabled after EnableLoginItem")
	}

	plist := filepath.Join(dir, loginItemLabel+".plist")
	data, err := os.ReadFile(plist)
	if err != nil {
		t.Fatalf("reading plist: %v", err)
	}
	body := string(data)
	for _, want := range []string{
		"<string>" + loginItemLabel + "</string>",
		"<string>" + exe + "</string>",
		"<key>RunAtLoad</key>",
		"<true/>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("plist missing %q\n---\n%s", want, body)
		}
	}

	if err := DisableLoginItem(); err != nil {
		t.Fatalf("DisableLoginItem: %v", err)
	}
	if LoginItemEnabled() {
		t.Fatal("expected login item disabled after DisableLoginItem")
	}
	if _, err := os.Stat(plist); !os.IsNotExist(err) {
		t.Errorf("plist should be gone, stat err = %v", err)
	}
}

func TestDisableLoginItemWhenAbsentIsNoError(t *testing.T) {
	withStubbedLoginItem(t)
	if err := DisableLoginItem(); err != nil {
		t.Fatalf("disabling an absent login item should be a no-op, got %v", err)
	}
}

func TestEnableLoginItemEmptyPathErrors(t *testing.T) {
	withStubbedLoginItem(t)
	if err := EnableLoginItem(""); err == nil {
		t.Fatal("expected error for empty executable path")
	}
}

func TestXMLEscape(t *testing.T) {
	got := xmlEscape(`/Users/a & b/<x>"'`)
	want := `/Users/a &amp; b/&lt;x&gt;&quot;&apos;`
	if got != want {
		t.Errorf("xmlEscape = %q want %q", got, want)
	}
}
