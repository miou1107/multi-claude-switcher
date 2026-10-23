package core

import (
	"regexp"
	"strings"
)

// RelaunchNotice is the system notification shown after RestoreAfterSelfRelaunch
// has put the user back: the title and the one-line body, in lang (one of the
// panel language constants; anything else reads as English). account is the
// display name of the account the user is back on.
//
// The panel's own strings live in internal/panelui/i18n.js and are translated
// in the page; a system notification is drawn by macOS, not by the page, so its
// words have to be in Go. Three languages, one sentence each: this is the whole
// dictionary the notification needs.
func RelaunchNotice(lang, account string) (title, body string) {
	switch lang {
	case LanguageChineseTraditional:
		return "Multi-Claude Switcher", "Claude 更新後自己重開，已切回 " + account
	case LanguageChineseSimplified:
		return "Multi-Claude Switcher", "Claude 更新后自己重开，已切回 " + account
	default:
		return "Multi-Claude Switcher", "Claude Desktop relaunched itself after an update. Switched back to " + account + "."
	}
}

var (
	autoTraditional = regexp.MustCompile(`^zh-hant(?:-|$)|^zh-(?:tw|hk|mo)(?:-|$)`)
	autoSimplified  = regexp.MustCompile(`^zh-hans(?:-|$)|^(?:zh|zh-cn|zh-sg)$`)
)

// ResolveAutoLanguage turns the stored panel-language preference into a
// concrete language. An explicit choice is returned as is; LanguageAuto is
// decided from the computer's preferred languages, most preferred first, by
// the same rule the panel applies to navigator.language (see systemLanguage in
// internal/panelui/i18n.js): Traditional for Taiwan, Hong Kong and Macau,
// Simplified for the mainland and Singapore, English otherwise.
//
// Shared so the notification and the panel never disagree about which
// language "auto" means on one machine.
func ResolveAutoLanguage(pref string, systemLanguages []string) string {
	if pref != LanguageAuto {
		return pref
	}
	l := "en"
	if len(systemLanguages) > 0 && systemLanguages[0] != "" {
		l = systemLanguages[0]
	}
	l = strings.ToLower(strings.ReplaceAll(l, "_", "-"))
	switch {
	case autoTraditional.MatchString(l):
		return LanguageChineseTraditional
	case autoSimplified.MatchString(l):
		return LanguageChineseSimplified
	default:
		return LanguageEnglish
	}
}
