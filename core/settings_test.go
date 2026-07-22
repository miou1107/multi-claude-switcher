// core/settings_test.go
package core

import (
	"path/filepath"
	"testing"
)

// withStubbedSettings redirects the settings file to a temp dir so tests never
// touch the real ~/.multi-claude-switcher/settings.json (same idea as
// loginitem_test.go's stubbed dir).
func withStubbedSettings(t *testing.T) {
	t.Helper()
	orig := settingsPath
	dir := t.TempDir()
	settingsPath = func() string { return filepath.Join(dir, "settings.json") }
	t.Cleanup(func() { settingsPath = orig })
}

func TestSettingsDefaultFalse(t *testing.T) {
	withStubbedSettings(t)
	if AutoAlignOnSwitch() {
		t.Error("autoAlignOnSwitch should default false when no file exists")
	}
	if AutoAlignWarningDismissed() {
		t.Error("autoAlignWarningDismissed should default false when no file exists")
	}
}

func TestSettingsRoundTripAndNoClobber(t *testing.T) {
	withStubbedSettings(t)
	if err := SetAutoAlignOnSwitch(true); err != nil {
		t.Fatal(err)
	}
	if !AutoAlignOnSwitch() {
		t.Error("expected autoAlignOnSwitch true after set")
	}
	// Writing the second flag must not clobber the first.
	if err := SetAutoAlignWarningDismissed(true); err != nil {
		t.Fatal(err)
	}
	if !AutoAlignOnSwitch() {
		t.Error("setting warning-dismissed clobbered autoAlignOnSwitch")
	}
	if !AutoAlignWarningDismissed() {
		t.Error("expected autoAlignWarningDismissed true after set")
	}
}
