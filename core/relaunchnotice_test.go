package core

import "testing"

func TestRelaunchNoticeNamesTheAccountInThePanelLanguage(t *testing.T) {
	cases := []struct {
		lang, wantBody string
	}{
		{LanguageChineseTraditional, "Claude 更新後自己重開，已切回 FTData"},
		{LanguageChineseSimplified, "Claude 更新后自己重开，已切回 FTData"},
		{LanguageEnglish, "Claude Desktop relaunched itself after an update. Switched back to FTData."},
	}
	for _, c := range cases {
		title, body := RelaunchNotice(c.lang, "FTData")
		if title == "" {
			t.Errorf("%s: empty title", c.lang)
		}
		if body != c.wantBody {
			t.Errorf("%s: body = %q, want %q", c.lang, body, c.wantBody)
		}
	}
}

func TestRelaunchNoticeFallsBackToEnglishForAnUnknownLanguage(t *testing.T) {
	_, body := RelaunchNotice("fr", "FTData")
	if body != "Claude Desktop relaunched itself after an update. Switched back to FTData." {
		t.Fatalf("unknown language should read as English, got %q", body)
	}
}

// ResolveAutoLanguage mirrors what the panel's JS does with navigator.language
// when the preference is "auto", so a notification and the panel agree.
func TestResolveAutoLanguageFollowsTheSystemLanguageList(t *testing.T) {
	cases := []struct {
		pref   string
		system []string
		want   string
	}{
		{LanguageEnglish, []string{"zh-Hant-TW"}, LanguageEnglish}, // an explicit choice wins
		{LanguageAuto, []string{"zh-Hant-TW", "en-US"}, LanguageChineseTraditional},
		{LanguageAuto, []string{"zh-TW"}, LanguageChineseTraditional},
		{LanguageAuto, []string{"zh-HK"}, LanguageChineseTraditional},
		{LanguageAuto, []string{"zh-Hans-CN"}, LanguageChineseSimplified},
		{LanguageAuto, []string{"zh-CN"}, LanguageChineseSimplified},
		{LanguageAuto, []string{"ja-JP", "en-US"}, LanguageEnglish},
		{LanguageAuto, nil, LanguageEnglish},
	}
	for _, c := range cases {
		if got := ResolveAutoLanguage(c.pref, c.system); got != c.want {
			t.Errorf("ResolveAutoLanguage(%q, %v) = %q, want %q", c.pref, c.system, got, c.want)
		}
	}
}
