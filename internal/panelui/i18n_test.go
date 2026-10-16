package panelui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/miou1107/multi-claude-switcher/core"
)

// dictionaryKeys returns the English strings one language block answers.
func dictionaryKeys(t *testing.T, language string) map[string]bool {
	t.Helper()
	start := strings.Index(i18nScript, `"`+language+`": {`)
	if start < 0 {
		t.Fatalf("i18n.js has no %q dictionary", language)
	}
	rest := i18nScript[start:]
	end := strings.Index(rest, "\n    }")
	if end < 0 {
		t.Fatalf("%q dictionary is not closed", language)
	}
	keys := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s+"((?:[^"\\]|\\.)*)":`).FindAllStringSubmatch(rest[:end], -1) {
		keys[m[1]] = true
	}
	if len(keys) == 0 {
		t.Fatalf("%q dictionary parsed as empty", language)
	}
	return keys
}

// Every English string one language answers, the other must answer too. A
// phrase translated into one Chinese and not the other shows up in English for
// half the people reading the panel, and nothing else would catch it: the page
// still renders, the build still passes.
func TestBothChineseDictionariesAnswerTheSameStrings(t *testing.T) {
	hant := dictionaryKeys(t, core.LanguageChineseTraditional)
	hans := dictionaryKeys(t, core.LanguageChineseSimplified)
	for k := range hant {
		if !hans[k] {
			t.Errorf("%q is in the Traditional dictionary but not the Simplified one", k)
		}
	}
	for k := range hans {
		if !hant[k] {
			t.Errorf("%q is in the Simplified dictionary but not the Traditional one", k)
		}
	}
}

// The same applies to the sentence patterns, which carry the messages that
// quote a number or an account name.
func TestBothChineseLanguagesHaveTheSamePatterns(t *testing.T) {
	count := func(language string) int {
		start := strings.Index(i18nScript, `"`+language+`": [`)
		if start < 0 {
			t.Fatalf("i18n.js has no %q patterns", language)
		}
		rest := i18nScript[start:]
		end := strings.Index(rest, "\n    ]")
		if end < 0 {
			t.Fatalf("%q patterns are not closed", language)
		}
		return strings.Count(rest[:end], "\n      [/^")
	}
	hant, hans := count(core.LanguageChineseTraditional), count(core.LanguageChineseSimplified)
	if hant != hans {
		t.Errorf("Traditional has %d patterns, Simplified has %d", hant, hans)
	}
	if hant == 0 {
		t.Fatal("no patterns parsed; the test is reading the wrong thing")
	}
}

// The picker has to offer a way out of every language it can put someone in,
// including back to automatic.
func TestSettingsOffersEveryLanguage(t *testing.T) {
	html := RenderSettings(SettingsVM{Language: core.LanguageChineseTraditional})
	for _, want := range []string{core.LanguageAuto, core.LanguageChineseTraditional,
		core.LanguageChineseSimplified, core.LanguageEnglish} {
		if !strings.Contains(html, `value="`+want+`"`) {
			t.Errorf("the language picker does not offer %q", want)
		}
	}
	if !strings.Contains(html, `value="`+core.LanguageChineseTraditional+`" selected`) {
		t.Error("the current language is not the selected option")
	}
	if !strings.Contains(html, `send('setLanguage',this.value)`) {
		t.Error("choosing a language sends nothing to the host")
	}
}

// A settings file written before the picker existed has no language in it. The
// panel must come up on Automatic rather than on an empty option.
func TestSettingsLanguageDefaultsToAutomatic(t *testing.T) {
	html := RenderSettings(SettingsVM{})
	if !strings.Contains(html, `value="`+core.LanguageAuto+`" selected`) {
		t.Error("with no stored language, Automatic is not the selected option")
	}
}

// The page tells the script which language to use before the script runs. If
// this tag went missing, or carried something other than what was stored, the
// panel would fall back to the computer's own language and ignore the choice.
func TestPanelTellsTheScriptWhichLanguage(t *testing.T) {
	orig := panelLanguage
	t.Cleanup(func() { panelLanguage = orig })
	panelLanguage = func() string { return core.LanguageChineseTraditional }

	html := RenderSettings(SettingsVM{})
	if !strings.Contains(html, `window.__mcsLang="`+core.LanguageChineseTraditional+`"`) {
		t.Error("the page does not carry the stored language")
	}
	if !strings.Contains(html, "<script src=\"data:text/javascript;base64,") {
		t.Error("the localization script is not attached to the page")
	}
}

// Every screen carries the script, not just Settings: a progress card or a
// confirmation shown over the account list has to be readable too.
func TestEveryScreenCarriesTheScript(t *testing.T) {
	orig := panelLanguage
	t.Cleanup(func() { panelLanguage = orig })
	panelLanguage = func() string { return core.LanguageEnglish }

	for name, html := range renderedViews(t) {
		if !strings.Contains(html, "window.__mcsLang=") {
			t.Errorf("%s does not carry a language preference", name)
		}
	}
}
