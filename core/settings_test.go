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
	if AutoSyncOnSwitch() {
		t.Error("autoSyncOnSwitch should default false when no file exists")
	}
	if AutoSyncWarningDismissed() {
		t.Error("autoSyncWarningDismissed should default false when no file exists")
	}
}

func TestSettingsRoundTripAndNoClobber(t *testing.T) {
	withStubbedSettings(t)
	if err := SetAutoSyncOnSwitch(true); err != nil {
		t.Fatal(err)
	}
	if !AutoSyncOnSwitch() {
		t.Error("expected autoSyncOnSwitch true after set")
	}
	// Writing the second flag must not clobber the first.
	if err := SetAutoSyncWarningDismissed(true); err != nil {
		t.Fatal(err)
	}
	if !AutoSyncOnSwitch() {
		t.Error("setting warning-dismissed clobbered autoSyncOnSwitch")
	}
	if !AutoSyncWarningDismissed() {
		t.Error("expected autoSyncWarningDismissed true after set")
	}
}

func TestPanelLanguageDefaultsToAuto(t *testing.T) {
	withStubbedSettings(t)
	if got := PanelLanguage(); got != LanguageAuto {
		t.Errorf("panel language with no file = %q, want %q", got, LanguageAuto)
	}
}

// A settings file written before this setting existed has no entry at all. It
// must read as auto, which is how that build behaved, rather than as an empty
// language the panel would have to guess at.
func TestPanelLanguageOlderFileReadsAsAuto(t *testing.T) {
	withStubbedSettings(t)
	if err := SetAutoSyncOnSwitch(true); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	if got := PanelLanguage(); got != LanguageAuto {
		t.Errorf("panel language from a file without one = %q, want %q", got, LanguageAuto)
	}
}

func TestSetPanelLanguageRoundTrip(t *testing.T) {
	withStubbedSettings(t)
	for _, lang := range []string{LanguageChineseTraditional, LanguageChineseSimplified, LanguageEnglish, LanguageAuto} {
		if err := SetPanelLanguage(lang); err != nil {
			t.Fatalf("SetPanelLanguage(%q): %v", lang, err)
		}
		if got := PanelLanguage(); got != lang {
			t.Errorf("after SetPanelLanguage(%q), PanelLanguage() = %q", lang, got)
		}
	}
}

// An unknown language is refused at the door. If it were stored instead, every
// later render would read it back and leave the panel untranslated.
func TestSetPanelLanguageRefusesUnknown(t *testing.T) {
	withStubbedSettings(t)
	if err := SetPanelLanguage(LanguageChineseTraditional); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := SetPanelLanguage("klingon"); err == nil {
		t.Fatal("SetPanelLanguage(\"klingon\") = nil, want an error")
	}
	if got := PanelLanguage(); got != LanguageChineseTraditional {
		t.Errorf("a refused language changed the stored one to %q", got)
	}
}

// Setting the language must not wipe the toggles sharing the file.
func TestSetPanelLanguageKeepsOtherSettings(t *testing.T) {
	withStubbedSettings(t)
	if err := SetAutoSyncOnSwitch(true); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := SetPanelLanguage(LanguageEnglish); err != nil {
		t.Fatalf("SetPanelLanguage: %v", err)
	}
	if !AutoSyncOnSwitch() {
		t.Error("setting the language cleared autoSyncOnSwitch")
	}
}
