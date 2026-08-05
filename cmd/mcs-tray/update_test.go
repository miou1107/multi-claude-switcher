package main

import "testing"

func TestFindAppZip(t *testing.T) {
	// The packaged-app asset is matched by prefix + the OS-specific suffix, so
	// build the expected name from appZipSuffix to stay correct on every OS.
	appAsset := "Multi-Claude-Switcher_0.6.1" + appZipSuffix
	assets := map[string]string{
		"mcs-cli-binary":                      "https://x/cli",
		appAsset:                              "https://x/app",
		"multi-claude-switcher_0.6.1_raw.zip": "https://x/raw", // lowercase prefix: not a match
	}
	url, ok := findAppZip(assets)
	if !ok || url != "https://x/app" {
		t.Fatalf("findAppZip = (%q,%v), want the app zip URL", url, ok)
	}

	// A release with no packaged app (only the lowercase raw-binary zip and a
	// bare binary) must report not-found.
	if _, ok := findAppZip(map[string]string{
		"multi-claude-switcher_0.6.1_raw.zip": "https://x/raw",
		"mcs-tray-binary":                     "https://x/tray",
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
			"/Users/example/Downloads/Foo.app/Contents/MacOS/bin",
			"/Users/example/Downloads/Foo.app",
			true,
		},
		{"/usr/local/bin/mcs-tray", "", false},
		{"/Users/example/mcs-tray-macos-universal", "", false},
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

// A background check stays quiet about a transient failure but must not stay
// quiet forever. Observed on a real machine: seven consecutive DNS failures
// over two days, the app two minor versions behind, and nothing on screen ever
// saying so. The rule is one notice per run of failures — hence equality, not
// >=, which would toast every 6 hours for as long as the network stayed down.
func TestShouldWarnAboutFailedChecks(t *testing.T) {
	cases := []struct {
		name        string
		consecutive int
		want        bool
	}{
		{name: "first failure stays quiet", consecutive: 1, want: false},
		{name: "second failure stays quiet", consecutive: 2, want: false},
		{name: "third failure is the notice", consecutive: checkFailuresBeforeNotifying, want: true},
		{name: "fourth does not repeat it", consecutive: checkFailuresBeforeNotifying + 1, want: false},
		{name: "nor does the twentieth", consecutive: 20, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldWarnAboutFailedChecks(tc.consecutive); got != tc.want {
				t.Errorf("shouldWarnAboutFailedChecks(%d) = %v, want %v", tc.consecutive, got, tc.want)
			}
		})
	}
}
