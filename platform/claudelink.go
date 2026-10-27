package platform

// claudelink.go holds the parts of the claude:// link handling (issue #60) that
// are plain data, so they are tested on every platform. The registry and shell
// calls are in claudelink_windows.go.
//
// Why the switcher opens claude:// links at all: Claude from the Microsoft
// Store owns the protocol through its package, and Windows then ignores the
// HKCU\Software\Classes\claude command the switcher holds on the account in use
// (protocolhandler_windows.go). The Store app opens every link on its default
// account, so signing in to any other account either never finishes (Google)
// or signs the default account in as the other one (email link, SSO). Which app
// opens a protocol is the user's choice, kept under a hash Windows checks, so
// the switcher registers itself as one of the apps that can and the user picks
// it once in Windows' own window.
//
// Windows 10 does not let a program show that window: SHOpenWithDialog for a
// protocol only says to go to Settings, Apps, Default apps (measured, Windows 10
// 22H2, zh-TW). What does show it is opening a claude:// link while two apps can
// open one and the user has picked neither, so that is how the switcher asks:
// it opens ClaudeLinkChooser. Windows keeps asking on every link until the user
// picks, and stops once they have.

import (
	"net/url"
	"strings"
)

// ClaudeLinkChooser is the link the switcher opens to make Windows ask which app
// opens claude:// links. Claude treats claude://hotkey as nothing to do (read
// from Claude 3.6556.2), so it is harmless whichever app the user picks, and the
// switcher recognises it and passes it nowhere.
const ClaudeLinkChooser = "claude://hotkey?from=multi-claude-switcher"

// IsClaudeLinkChooser reports whether link is ClaudeLinkChooser as Windows
// hands it back, which may add a slash after the host.
func IsClaudeLinkChooser(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "claude") && strings.EqualFold(u.Host, "hotkey") &&
		strings.Trim(u.Path, "/") == "" && u.Query().Get("from") == "multi-claude-switcher"
}

// ClaudeLinkProgID is the name the switcher's claude:// handler is registered
// under in HKCU\Software\Classes. Windows records it as the user's choice.
const ClaudeLinkProgID = "MultiClaudeSwitcher.ClaudeLink"

// claudeLinkAppName is how the switcher appears in Windows' list of apps. It is
// the installer's AppName, which is also the Start menu entry, so the guide that
// tells the user what to click can name it exactly.
const claudeLinkAppName = "Multi-Claude Switcher"

// claudeLinkCapabilities is where the switcher declares what it can open.
const claudeLinkCapabilities = `Software\Multi-Claude Switcher\Capabilities`

// ClaudeLinkStatus says whether claude:// links reach the account in use.
type ClaudeLinkStatus int

const (
	// ClaudeLinksFine: nothing is in the way. The claude.com build reads the
	// handler the switcher holds, and the older Store build keeps every
	// account in one folder, so its default account is the one in use.
	ClaudeLinksFine ClaudeLinkStatus = iota
	// ClaudeLinksNeedSwitcher: a packaged app opens the links on its default
	// account, and the user has not picked an app yet.
	ClaudeLinksNeedSwitcher
	// ClaudeLinksOtherAppPicked: as ClaudeLinksNeedSwitcher, but the user picked
	// another app in Windows' window, so Windows no longer asks.
	ClaudeLinksOtherAppPicked
	// ClaudeLinksViaSwitcher: the user chose the switcher, which passes each
	// link to the account in use.
	ClaudeLinksViaSwitcher
)

// NeedsSwitcher reports whether sign-in to any account but the default one is
// broken until the user chooses the switcher.
func (s ClaudeLinkStatus) NeedsSwitcher() bool {
	return s == ClaudeLinksNeedSwitcher || s == ClaudeLinksOtherAppPicked
}

func (s ClaudeLinkStatus) String() string {
	switch s {
	case ClaudeLinksNeedSwitcher:
		return "a packaged app opens them on its default account"
	case ClaudeLinksOtherAppPicked:
		return "the user picked another app, which opens them on its default account"
	case ClaudeLinksViaSwitcher:
		return "the switcher opens them"
	default:
		return "Claude opens them on the account in use"
	}
}

// claudeLinkStatusFor decides the status from the ProgID Windows resolves
// claude:// to, the ProgID the user picked in Windows' window ("" when they
// have not), whether the Store build the switcher drives is installed, and
// whether the older Store build is the one in use.
//
// With the Store build installed, any answer but the switcher leaves the links
// on the default account: the package's own handler, and equally the
// claude.com build's or anything else the user picks in Windows' window. That
// is why picking another app keeps the notice up.
func claudeLinkStatusFor(progID, userChoice string, storeBuild, slotBuild bool) ClaudeLinkStatus {
	switch {
	case strings.EqualFold(progID, ClaudeLinkProgID):
		return ClaudeLinksViaSwitcher
	case slotBuild:
		return ClaudeLinksFine
	case storeBuild, strings.HasPrefix(strings.ToLower(progID), "appx"):
		if userChoice != "" {
			return ClaudeLinksOtherAppPicked
		}
		return ClaudeLinksNeedSwitcher
	default:
		return ClaudeLinksFine
	}
}

// regValue is one registry string value under HKEY_CURRENT_USER. An empty name
// is the key's default value.
type regValue struct{ key, name, value string }

// claudeLinkRegistration is what registers exe as an app that can open
// claude:// links. It only offers the switcher to Windows: nothing here changes
// which app opens the links, which stays the user's choice. uiLanguage is
// Windows' display language, a LANGID.
//
// The name in Windows' window carries a "recommended" mark, in Windows' own
// language, so the user can tell which of the two apps to click. That name is
// the ProgID's; the one under Capabilities, which Settings lists among all the
// user's apps, stays plain.
func claudeLinkRegistration(exe string, uiLanguage uint16) []regValue {
	prog := `Software\Classes\` + ClaudeLinkProgID
	icon := `"` + exe + `",0`
	return []regValue{
		{prog, "", "Claude sign-in link"},
		{prog + `\DefaultIcon`, "", icon},
		{prog + `\Application`, "ApplicationName", claudeLinkAppName + recommendedMark(uiLanguage)},
		{prog + `\Application`, "ApplicationIcon", icon},
		{prog + `\shell\open\command`, "", `"` + exe + `" --open-claude-link "%1"`},
		{claudeLinkCapabilities, "ApplicationName", claudeLinkAppName},
		{claudeLinkCapabilities, "ApplicationDescription", "Sends Claude sign-in links to the account you are on."},
		{claudeLinkCapabilities + `\URLAssociations`, "claude", ClaudeLinkProgID},
		{`Software\RegisteredApplications`, claudeLinkAppName, claudeLinkCapabilities},
	}
}

// recommendedMark is the "recommended" after the switcher's name in Windows'
// window, in the language of a LANGID. The primary language is the low ten
// bits; for Chinese the sublanguage tells Taiwan, Hong Kong and Macau
// (traditional) from China and Singapore (simplified).
func recommendedMark(langID uint16) string {
	const langChinese = 0x04
	if langID&0x3ff != langChinese {
		return " (recommended)"
	}
	switch langID >> 10 {
	case 0x02, 0x04: // China, Singapore
		return "（推荐）"
	default: // Taiwan, Hong Kong, Macau
		return "（推薦）"
	}
}

// Editions name which Claude Desktop the switcher drives, for the panel's
// footer.
const (
	EditionStore     = "store"      // Microsoft Store, either package
	EditionClaudeCom = "claude.com" // the installer from claude.com
	EditionMac       = "mac"
)

// packageVersion returns the version in a package full name, which is
// Name_Version_Architecture_ResourceId_PublisherId. A Store version always has
// four parts; a last part of 0 is dropped so it reads as Claude itself shows
// its version.
func packageVersion(fullName string) string {
	parts := strings.Split(fullName, "_")
	if len(parts) != 5 {
		return ""
	}
	return strings.TrimSuffix(parts[1], ".0")
}

// newestAppVersion returns the newest version among the app-<version> folders
// the claude.com installer keeps, or "" when there is none.
func newestAppVersion(names []string) string {
	newest := ""
	for _, n := range names {
		v, ok := strings.CutPrefix(n, "app-")
		if !ok || v == "" {
			continue
		}
		if newest == "" || lessVersion(newest, v) {
			newest = v
		}
	}
	return newest
}

// plistShortVersion reads CFBundleShortVersionString out of an XML Info.plist,
// or returns "" for anything else, such as a binary plist.
func plistShortVersion(data []byte) string {
	s := string(data)
	_, rest, ok := strings.Cut(s, "<key>CFBundleShortVersionString</key>")
	if !ok {
		return ""
	}
	_, rest, ok = strings.Cut(rest, "<string>")
	if !ok {
		return ""
	}
	v, _, ok := strings.Cut(rest, "</string>")
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}
