package core

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		remote, local string
		want          bool
	}{
		{"v0.5.0", "0.4.0", true},
		{"0.5.0", "v0.5.0", false}, // equal (leading v ignored)
		{"v0.4.9", "0.5.0", false},
		{"v0.5.1", "0.5.0", true},
		{"v1.0.0", "0.9.9", true},
		{"v0.5.0", "0.5.0", false},
		{"v0.10.0", "0.9.0", true},        // numeric, not lexical
		{"v0.5.0-rc1", "0.5.0", false},    // pre-release suffix stripped -> equal
		{"v0.5.1-beta", "0.5.0", true},    // still newer on patch
		{"0.6", "0.5.0", true},            // short form
	}
	for _, c := range cases {
		if got := IsNewer(c.remote, c.local); got != c.want {
			t.Errorf("IsNewer(%q,%q)=%v want %v", c.remote, c.local, got, c.want)
		}
	}
}

func TestParseRelease(t *testing.T) {
	body := []byte(`{
		"tag_name": "v0.5.0",
		"assets": [
			{"name": "mcs-tray-macos-universal", "browser_download_url": "https://example.com/tray"},
			{"name": "multi-claude-switcher_0.5.0_macos-universal.zip", "browser_download_url": "https://example.com/zip"}
		]
	}`)
	tag, assets, err := parseRelease(body)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v0.5.0" {
		t.Errorf("tag = %q", tag)
	}
	if assets["mcs-tray-macos-universal"] != "https://example.com/tray" {
		t.Errorf("tray asset URL wrong: %q", assets["mcs-tray-macos-universal"])
	}
	if len(assets) != 2 {
		t.Errorf("expected 2 assets, got %d", len(assets))
	}
}
