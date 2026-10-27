package platform

import (
	"strings"
	"testing"
)

func TestClaudeLinkStatusFor(t *testing.T) {
	cases := []struct {
		name       string
		progID     string
		userChoice string
		storeBuild bool
		slotBuild  bool
		want       ClaudeLinkStatus
	}{
		// What Windows answers on a machine with Claude from the Store
		// (measured, Windows 10, Claude 3.6556.2): the package's own handler.
		{"store app owns the links", "AppX8fe9ajw7dbkq7xe010tqgz6zzabny2ka", "", true, false, ClaudeLinksNeedSwitcher},
		{"user chose the switcher", ClaudeLinkProgID, ClaudeLinkProgID, true, false, ClaudeLinksViaSwitcher},
		{"chosen, matched without case", strings.ToLower(ClaudeLinkProgID), ClaudeLinkProgID, true, false, ClaudeLinksViaSwitcher},
		// Picking another app in Windows' window still leaves the links on the
		// default account, so the notice stays, and Windows stops asking.
		{"store app picked", "AppX8fe9ajw7dbkq7xe010tqgz6zzabny2ka", "AppX8fe9ajw7dbkq7xe010tqgz6zzabny2ka", true, false, ClaudeLinksOtherAppPicked},
		{"store installed, another app picked", "claude", "claude", true, false, ClaudeLinksOtherAppPicked},
		// The claude.com build alone: the handler the switcher holds on the
		// account in use is what Windows reads.
		{"claude.com build only", "claude", "", false, false, ClaudeLinksFine},
		{"nothing registered", "", "", false, false, ClaudeLinksFine},
		// A packaged app that is not the Store build the switcher drives still
		// sends links past it.
		{"some other package", "AppXabc", "", false, false, ClaudeLinksNeedSwitcher},
		// The older Store build keeps every account in one folder, so the
		// default account is always the account in use.
		{"older store build", "AppXdef", "", false, true, ClaudeLinksFine},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := claudeLinkStatusFor(c.progID, c.userChoice, c.storeBuild, c.slotBuild)
			if got != c.want {
				t.Errorf("claudeLinkStatusFor(%q, %q, store=%v, slot=%v) = %v, want %v", c.progID, c.userChoice, c.storeBuild, c.slotBuild, got, c.want)
			}
			if wantNeeds := c.want == ClaudeLinksNeedSwitcher || c.want == ClaudeLinksOtherAppPicked; got.NeedsSwitcher() != wantNeeds {
				t.Errorf("%v.NeedsSwitcher() = %v, want %v", got, got.NeedsSwitcher(), wantNeeds)
			}
		})
	}
}

func TestClaudeLinkRegistration(t *testing.T) {
	const exe = `C:\Users\someone\AppData\Local\Programs\Multi-Claude Switcher\mcs-tray.exe`
	got := map[string]string{}
	for _, v := range claudeLinkRegistration(exe, 0x0409) {
		got[v.key+"|"+v.name] = v.value
	}
	want := map[string]string{
		// What Windows runs for a link once the user has chosen the switcher.
		`Software\Classes\MultiClaudeSwitcher.ClaudeLink\shell\open\command|`: `"` + exe + `" --open-claude-link "%1"`,
		// What puts the switcher in Windows' list of apps for claude://.
		`Software\RegisteredApplications|Multi-Claude Switcher`:                          `Software\Multi-Claude Switcher\Capabilities`,
		`Software\Multi-Claude Switcher\Capabilities\URLAssociations|claude`:             ClaudeLinkProgID,
		`Software\Multi-Claude Switcher\Capabilities|ApplicationName`:                    "Multi-Claude Switcher",
		`Software\Classes\MultiClaudeSwitcher.ClaudeLink\Application|ApplicationName`:    "Multi-Claude Switcher (recommended)",
		`Software\Classes\MultiClaudeSwitcher.ClaudeLink\DefaultIcon|`:                   `"` + exe + `",0`,
		`Software\Classes\MultiClaudeSwitcher.ClaudeLink\Application|ApplicationIcon`:    `"` + exe + `",0`,
		`Software\Classes\MultiClaudeSwitcher.ClaudeLink|`:                               "Claude sign-in link",
		`Software\Multi-Claude Switcher\Capabilities|ApplicationDescription`:             "Sends Claude sign-in links to the account you are on.",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("registration writes %d values, want %d: %q", len(got), len(want), got)
	}
}

func TestPackageVersion(t *testing.T) {
	for fullName, want := range map[string]string{
		"AnthropicPBC.Claude_3.6556.2.0_x64__fnn82j28hfe8t": "3.6556.2",
		"Claude_1.24012.9.0_x64__pzs8sxrjxfjjc":             "1.24012.9",
		"Claude_1.2.3.4_arm64__pzs8sxrjxfjjc":               "1.2.3.4",
		"not a package name":                                "",
	} {
		if got := packageVersion(fullName); got != want {
			t.Errorf("packageVersion(%q) = %q, want %q", fullName, got, want)
		}
	}
}

func TestNewestAppVersion(t *testing.T) {
	// Squirrel keeps the previous version's folder next to the new one, and
	// 3.10 is newer than 3.9 although it sorts first as text.
	got := newestAppVersion([]string{"app-3.9.1", "packages", "app-3.10.0", "app.ico", "app-3.1623.1"})
	if got != "3.1623.1" {
		t.Errorf("newestAppVersion = %q, want 3.1623.1", got)
	}
	if got := newestAppVersion([]string{"packages"}); got != "" {
		t.Errorf("newestAppVersion with no app folder = %q, want empty", got)
	}
}

func TestRecommendedMark(t *testing.T) {
	for lang, want := range map[uint16]string{
		0x0409: " (recommended)", // English (United States)
		0x0411: " (recommended)", // Japanese
		0x0404: "（推薦）",           // Chinese (Taiwan), measured on Windows 10
		0x0c04: "（推薦）",           // Chinese (Hong Kong)
		0x1404: "（推薦）",           // Chinese (Macau)
		0x0804: "（推荐）",           // Chinese (China)
		0x1004: "（推荐）",           // Chinese (Singapore)
	} {
		if got := recommendedMark(lang); got != want {
			t.Errorf("recommendedMark(%#04x) = %q, want %q", lang, got, want)
		}
	}
}

func TestIsClaudeLinkChooser(t *testing.T) {
	for link, want := range map[string]bool{
		ClaudeLinkChooser: true,
		"claude://hotkey/?from=multi-claude-switcher":           true,
		"CLAUDE://HOTKEY?from=multi-claude-switcher":            true,
		"claude://hotkey":                                       false,
		"claude://login/google-auth?from=multi-claude-switcher": false,
		"claude://hotkey/x?from=multi-claude-switcher":          false,
		"%%%":                                                   false,
	} {
		if got := IsClaudeLinkChooser(link); got != want {
			t.Errorf("IsClaudeLinkChooser(%q) = %v, want %v", link, got, want)
		}
	}
}

func TestPlistShortVersion(t *testing.T) {
	// Info.plist as Claude.app ships it: XML, the key on one line and its value
	// on the next.
	plist := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>CFBundleVersion</key>
	<string>1.5354.0.1</string>
	<key>CFBundleShortVersionString</key>
	<string>1.5354.0</string>
</dict>
</plist>`)
	if got := plistShortVersion(plist); got != "1.5354.0" {
		t.Errorf("plistShortVersion = %q, want 1.5354.0", got)
	}
	if got := plistShortVersion([]byte("bplist00 binary")); got != "" {
		t.Errorf("plistShortVersion of a binary plist = %q, want empty", got)
	}
}
