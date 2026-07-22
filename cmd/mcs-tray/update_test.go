package main

import "testing"

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
