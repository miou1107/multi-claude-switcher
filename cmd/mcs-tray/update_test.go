package main

import "testing"

func TestFindAppZip(t *testing.T) {
	assets := map[string]string{
		"mcs-macos-universal":                             "https://x/cli",
		"Multi-Claude-Switcher_0.6.1_macos.zip":           "https://x/app",
		"multi-claude-switcher_0.6.1_macos-universal.zip": "https://x/raw",
	}
	url, ok := findAppZip(assets)
	if !ok || url != "https://x/app" {
		t.Fatalf("findAppZip = (%q,%v), want the app zip URL", url, ok)
	}

	// A release with no packaged app must report not-found (not match the
	// lowercase raw-binary zip).
	if _, ok := findAppZip(map[string]string{
		"multi-claude-switcher_0.6.1_macos-universal.zip": "https://x/raw",
		"mcs-tray-macos-universal":                        "https://x/tray",
	}); ok {
		t.Fatal("findAppZip matched a non-app asset")
	}
}

func TestIsInsideAppBundle(t *testing.T) {
	cases := []struct {
		exe        string
		wantBundle string
		wantOK     bool
	}{
		{
			"/Applications/Multi-Claude Switcher.app/Contents/MacOS/mcs-tray",
			"/Applications/Multi-Claude Switcher.app",
			true,
		},
		{
			"/Users/vin/Downloads/Foo.app/Contents/MacOS/bin",
			"/Users/vin/Downloads/Foo.app",
			true,
		},
		{"/usr/local/bin/mcs-tray", "", false},
		{"/Users/vin/mcs-tray-macos-universal", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		gotBundle, gotOK := isInsideAppBundle(c.exe)
		if gotOK != c.wantOK || gotBundle != c.wantBundle {
			t.Errorf("isInsideAppBundle(%q) = (%q,%v) want (%q,%v)",
				c.exe, gotBundle, gotOK, c.wantBundle, c.wantOK)
		}
	}
}
