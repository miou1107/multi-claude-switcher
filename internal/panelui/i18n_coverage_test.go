package panelui

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/miou1107/multi-claude-switcher/core"
)

// jsWhitespace is the character class JavaScript's \s stands for. Go's \s is
// the five ASCII ones and nothing else, so a pattern carried over verbatim
// would stop matching the moment a non-breaking space is involved — and the
// panel is full of them: every icon button is written ＋&nbsp; Add another
// account, so the icon-prefix rule below reads that space as part of the icon
// in a browser and, without this, as the start of the phrase here.
const jsWhitespace = `[` + jsTrimCutset + `]`

// jsTrimCutset is what String.prototype.trim removes, which is not what Go's
// strings.TrimSpace removes: Go also takes U+0085, JavaScript also takes the
// byte order mark. Trimming the wrong set either reports copy that is in fact
// translated, or passes copy that is not.
const jsTrimCutset = "\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005" +
	"\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"

// iconRuleInScript is the line in i18n.js that iconPrefix below is a port of,
// matched literally. The port cannot be derived from the line safely (see
// iconPrefix), so the next person to edit the rule is made to come here.
const iconRuleInScript = `const icon = s.match(/^([＋⟳⚙🏢✓]\s*)(.*)$/s);`

// iconPrefix is i18n.js's own escape hatch for buttons whose label is an icon
// followed by a phrase: the phrase alone is what the dictionary answers.
//
// 🏢 is deliberately NOT in this class, although it is in the script's. That
// class has no u flag, so JavaScript reads it as UTF-16 code units and 🏢
// (U+1F3E2) joins it as its two surrogate halves. Handed "🏢 Team", the script
// therefore takes \uD83C as the icon and looks up "\uDFE2 Team", which no
// dictionary answers — so the badge stays in English in a browser. A Go class
// is runes, so keeping 🏢 here would look up "Team" instead and call the badge
// translated when it is not. The Team plan's badge is left in English on
// purpose, and notCopy records that; this is about the two sides agreeing.
var iconPrefix = regexp.MustCompile(`(?s)^([＋⟳⚙✓]` + jsWhitespace + `*)(.*)$`)

// goPatterns reads one language's sentence patterns out of i18n.js and returns
// them as Go regexps. Reading them rather than keeping a second copy here is
// the point: a pattern added to i18n.js and not to this file would make the
// guard quietly stop covering the sentence it was added for, which is the very
// failure this test exists to prevent one level up.
func goPatterns(t *testing.T, language string) []*regexp.Regexp {
	t.Helper()
	start := strings.Index(i18nScript, `"`+language+`": [`)
	if start < 0 {
		t.Fatalf("i18n.js has no %q patterns", language)
	}
	rest := i18nScript[start:]
	end := strings.Index(rest, "\n    ]")
	if end < 0 {
		t.Fatalf("%q patterns are not closed", language)
	}
	block := rest[:end]

	var out []*regexp.Regexp
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "[/") {
			continue
		}
		source, flags := jsRegexLiteral(t, trimmed)
		out = append(out, compileJSRegex(t, source, flags))
	}
	// The count has to agree with what the simple scan in i18n_test.go sees.
	// If this parser ever walks off the end of the block, or stops recognizing
	// the shape of a line, it would return fewer patterns and the coverage
	// check would start reporting sentences that are in fact translated.
	if want := strings.Count(block, "\n      [/^"); len(out) != want {
		t.Fatalf("%q: parsed %d patterns, i18n.js has %d", language, len(out), want)
	}
	if len(out) == 0 {
		t.Fatalf("%q patterns parsed as empty; this test is reading the wrong thing", language)
	}
	return out
}

// jsRegexLiteral splits one `[/…/flags, m => …]` line into the regular
// expression and its flags. The closing slash cannot be found by searching for
// the next / or the next comma: these patterns contain both (/^Claude closes,
// (\d+) conversations?…/ has a comma in it, and a character class can hold a
// slash), so the literal is walked the way a JavaScript parser walks it —
// escapes consumed, character classes tracked.
func jsRegexLiteral(t *testing.T, line string) (source, flags string) {
	t.Helper()
	runes := []rune(line)
	open := 1 // line starts with "[/"
	inClass := false
	for i := open + 1; i < len(runes); i++ {
		switch runes[i] {
		case '\\':
			i++
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if inClass {
				continue
			}
			source = string(runes[open+1 : i])
			for j := i + 1; j < len(runes); j++ {
				if runes[j] < 'a' || runes[j] > 'z' {
					return source, string(runes[i+1 : j])
				}
			}
			t.Fatalf("regex literal has no end: %s", line)
		}
	}
	t.Fatalf("regex literal is not closed: %s", line)
	return "", ""
}

// compileJSRegex turns one JavaScript regular expression into the Go one that
// matches the same strings. Only the differences these patterns actually run
// into are handled, and anything else is a failure rather than a silent
// approximation: a pattern that compiles to something subtly wider here would
// mark a sentence covered that the panel shows in English.
func compileJSRegex(t *testing.T, source, flags string) *regexp.Regexp {
	t.Helper()
	for _, f := range flags {
		// s is the only flag in use. g, i, m and y would each change what the
		// pattern matches, and u is handled where the icon rule is read, so
		// none may pass unnoticed.
		if f != 's' {
			t.Fatalf("pattern uses the %q flag, which this test does not translate: /%s/%s", f, source, flags)
		}
	}
	dotAll := strings.Contains(flags, "s")

	// One pass, escape- and class-aware, because every one of these
	// substitutions is wrong if it fires inside a character class or on the
	// second half of an escape.
	var b strings.Builder
	inClass := false
	runes := []rune(source)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if c == '\\' {
			if i+1 >= len(runes) {
				t.Fatalf("pattern ends in a backslash: %s", source)
			}
			switch next := runes[i+1]; next {
			case 's':
				// JavaScript's \s covers a non-breaking space and the Unicode
				// spaces; Go's covers five ASCII characters. Inside a class the
				// substitution would nest one class in another, and no pattern
				// needs that today, so it is a failure rather than a guess.
				if inClass {
					t.Fatalf(`pattern uses \s inside a character class, which this translation cannot express: %s`, source)
				}
				b.WriteString(jsWhitespace)
			case 'S':
				// The negation cannot be expressed by substituting a class, and
				// Go's \S would match the very characters JavaScript's does not.
				t.Fatalf(`pattern uses \S, which this test does not translate: %s`, source)
			case '\\':
				t.Fatalf("pattern escapes a backslash, which this translation does not handle: %s", source)
			default:
				b.WriteRune(c)
				b.WriteRune(next)
			}
			i++
			continue
		}
		switch {
		case c == '[':
			inClass = true
			b.WriteRune(c)
		case c == ']':
			inClass = false
			b.WriteRune(c)
		case c == '.' && !inClass && !dotAll:
			// Without the s flag, JavaScript's . skips all four line
			// terminators where Go's skips only \n, so Go would match a string
			// JavaScript's pattern does not.
			b.WriteString(`[^\n\r\x{2028}\x{2029}]`)
		default:
			b.WriteRune(c)
		}
	}
	go_ := b.String()
	if dotAll {
		go_ = "(?s)" + go_
	}
	// Lookahead, lookbehind and back references have no RE2 equivalent, so
	// Compile rejects them here rather than this guard going quiet.
	re, err := regexp.Compile(go_)
	if err != nil {
		t.Fatalf("cannot translate /%s/%s: %v", source, flags, err)
	}
	return re
}

// translatedBy answers i18n.js's own question: handed this string, would the
// script have something to show instead? It mirrors translate() in i18n.js —
// whole phrase, then the icon-prefixed phrase, then the sentence patterns.
func translatedBy(s string, words map[string]bool, patterns []*regexp.Regexp) bool {
	s = strings.Trim(s, jsTrimCutset)
	if s == "" {
		return true
	}
	if words[s] {
		return true
	}
	if m := iconPrefix.FindStringSubmatch(s); m != nil && words[m[2]] {
		return true
	}
	for _, p := range patterns {
		if p.MatchString(s) {
			return true
		}
	}
	return false
}

// readable is text a person could read as a sentence. Everything the panel
// draws that holds no Latin letter at all — the × on the close button, the ⇄
// on a switch row, the · between two facts, a conversation count — is a glyph
// or a number, not copy, and no dictionary will ever answer it. Keeping that
// out by rule rather than by two dozen entries in the list below leaves that
// list for the strings a reader might reasonably expect to be translated.
func readable(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
	})
}

// notCopy is every readable string the panel draws that must stay in English,
// each with the reason it must. Anything else the panel draws and neither
// dictionary answers is a miss.
//
// Account names, folder names and email addresses are not in here: i18n.js
// already exempts the rows that carry them, so the walk never offers them up.
// That is the point of walking the DOM rather than the markup — the exemptions
// that are already expressed in the product do not have to be restated here,
// and this list holds only what is left over.
//
// It is a fixed list of strings rather than a set of shapes on purpose. A rule
// like "skip anything that looks like a name" would also skip the next
// sentence somebody forgets to translate.
func notCopy(t *testing.T) map[string]string {
	t.Helper()
	list := map[string]string{}
	add := func(s, why string) {
		// Two entries that collapse into one would take a reason with them and
		// leave the list a line short, silently. "v" + core.Version and the
		// Settings fixture's own version are one release apart from doing it.
		if had, ok := list[s]; ok {
			t.Fatalf("%q is listed twice, as %q and as %q", s, had, why)
		}
		list[s] = why
	}
	for s, why := range map[string]string{
		// The version in the corner of every screen, and the one the Settings
		// fixture hands the renderer. A release number is the same in every
		// language.
		"v" + core.Version: "the running version, printed in the corner of every screen",
		"v0.11.2":          "the version the Settings fixture hands the renderer",
		// The first half of an account's own UUID, shown while recovering it
		// so the user can tell two accounts apart in the Claude window.
		"abcd1234": "the start of an account ID, from the recover fixture",
		// The language picker names each language in that language, so that
		// somebody who cannot read the current one can still find their way
		// out. Only "Automatic" is translated, and the picker in render.go
		// says so where the list is built.
		"English": "a language's own name in the language picker",
		// Anthropic's own name for the plan, which is how it reads on the plan
		// itself in any language. "Free" is a word rather than a name, and the
		// dictionaries do answer that one.
		"Pro": "Anthropic's name for the plan",
		// The Team plan's badge, which stays in English the way Pro does. The
		// script leaves it alone too, for a reason worth reading: see
		// iconPrefix.
		"🏢 Team": "Anthropic's name for the plan, with its badge",
	} {
		add(s, why)
	}
	// core's errors reach a screen as they are, carrying file paths, folder
	// names and whatever the operating system said. Translating them means a
	// pattern per message against core's wording, which is work of its own and
	// has not been done for any of the 80-odd errors core can return — so the
	// four that reach a screen in the fixtures, one of them a joined error that
	// draws a line per complaint, are listed here rather than left looking like
	// an oversight.
	// The guard will flag the next core error that lands on a screen, which is
	// the point: somebody then decides whether to translate it.
	for _, s := range append([]string{errArchiveFailed, errSwitchFailed, errSyncSkipped}, errRegistryNote...) {
		add(s, "core's own error text, printed word for word")
	}
	return list
}

// visibleAttributes are the attributes i18n.js translates, and so the ones
// whose value has to be in the dictionary.
var visibleAttributes = []string{"title", "aria-label", "placeholder"}

// reachedAtDepth names, for four screens, copy that only a walk going all the
// way into the page can find: the sub-copy of a card inside the card list, the
// note on a Rescan row and that screen's own header line, a hint under a
// control on Settings, one line of a multi-line note, and a label that lives
// in an attribute rather than in text.
//
// A walk that quietly stops early is this guard's worst failure, because it
// reports nothing missing and passes. Counting how much it found does not
// catch it either: the window title alone kept an earlier version of this
// check satisfied on a page whose whole body was being skipped. Naming the
// copy does catch it.
var reachedAtDepth = map[string][]string{
	"list":                  {"Not signed in yet. Switch here, then sign in.", "What's new"},
	"rescan":                {core.SignedOutNote, "Which accounts should be managed?"},
	"settings":              {"Which language the panel is shown in"},
	"removed_registry_note": {errRegistryNote[2]},
}

// panelStrings walks one rendered screen the way i18n.js walks it in the
// browser and returns every string the script would try to translate.
//
// The walk is the whole difficulty of this guard. What the panel draws is not
// all copy: an account name, a plan, an email address, a version number and
// the whole diagnostic report are the user's own data, and i18n.js leaves them
// alone by asking the DOM — a .chip is skipped, a .name inside a row is
// skipped. A pattern over the HTML cannot ask that question, so the HTML is
// parsed and the same questions are asked of the same tree.
func panelStrings(t *testing.T, page string) map[string]string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatalf("cannot parse the rendered page: %v", err)
	}
	found := map[string]string{}
	var ancestors []*html.Node

	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			// x/net/html has already turned &nbsp; and friends back into the
			// characters a browser would hand the script.
			if !protectedText(ancestors) {
				found[n.Data] = "text"
			}
			return
		case html.ElementNode:
		default:
			return
		}
		// Attributes are translated before localize() decides whether to walk
		// into an element, so a title on a .chip is translated even though the
		// text inside it is not.
		for _, attr := range visibleAttributes {
			if v, ok := attribute(n, attr); ok {
				found[v] = "the " + attr + " attribute"
			}
		}
		if skipsSubtree(n) {
			return
		}
		ancestors = append(ancestors, n)
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		ancestors = ancestors[:len(ancestors)-1]
	}

	// i18n.js localizes document.body, and the window title separately. The
	// parent has to be head: an SVG can carry a <title> of its own, and
	// document.title is not that one.
	if title := find(doc, func(n *html.Node) bool {
		return n.Data == "title" && n.Parent != nil && n.Parent.Data == "head"
	}); title != nil && title.FirstChild != nil {
		found[title.FirstChild.Data] = "the window title"
	}
	body := find(doc, func(n *html.Node) bool { return n.Data == "body" })
	if body == nil {
		t.Fatal("the rendered page has no body")
	}
	walk(body)
	return found
}

// skipsSubtree is localize()'s own early return: these elements are not copy
// at all (a stylesheet, a script), or they are the user's own text (the
// diagnostic report in .dbgbox, a plan or folder .chip).
func skipsSubtree(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.Data {
	case "script", "style", "textarea", "code", "pre":
		return true
	}
	return hasClass(n, "dbgbox") || hasClass(n, "chip")
}

// protectedText is protectedText() in i18n.js: a text node is left alone when
// it sits in one of the elements above, or when it is the .name of a row that
// carries the account's own identity. closest() looks at the element itself
// first and then up, which is what this ancestor list is.
func protectedText(ancestors []*html.Node) bool {
	if closest(ancestors, skipsSubtree) != nil {
		return true
	}
	name := closest(ancestors, func(n *html.Node) bool { return hasClass(n, "name") })
	row := closest(ancestors, func(n *html.Node) bool {
		if _, ok := attribute(n, "data-name"); ok {
			return true
		}
		if _, ok := attribute(n, "data-from"); ok {
			return true
		}
		return hasClass(n, "card") && hasClass(n, "selectable")
	})
	return name != nil && row != nil
}

func closest(ancestors []*html.Node, match func(*html.Node) bool) *html.Node {
	for i := len(ancestors) - 1; i >= 0; i-- {
		if match(ancestors[i]) {
			return ancestors[i]
		}
	}
	return nil
}

func find(n *html.Node, match func(*html.Node) bool) *html.Node {
	if n.Type == html.ElementNode && match(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if hit := find(c, match); hit != nil {
			return hit
		}
	}
	return nil
}

func attribute(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}

func hasClass(n *html.Node, class string) bool {
	v, ok := attribute(n, "class")
	if !ok {
		return false
	}
	for _, f := range strings.Fields(v) {
		if f == class {
			return true
		}
	}
	return false
}

// Every sentence the panel draws has to be in both dictionaries. The two
// checks already here compare the dictionaries against each other, which says
// nothing about the panel: copy added to a screen and to neither dictionary
// passes both of them, passes the build, and is read in English by everyone
// who asked for Chinese. That is not hypothetical — the message shown when
// removing an account fails shipped untranslated and was found by a person
// reading the screen.
//
// Two kinds of copy are outside this check, and a sentence that goes missing
// is likeliest to be in one of them:
//
//   - The dialogs' copy, which the shell's own JavaScript concatenates at
//     click time and writes in with textContent. It is in no screen's HTML, so
//     no walk of a rendered screen can see it. About ten dictionary entries
//     exist only to answer it; all ten are answered today, by hand.
//   - Copy on states no fixture in views_test.go renders — the status lines
//     while something is running, mostly. Roughly a quarter of the dictionary
//     is reachable only that way. The fixtures are the contract for what this
//     check covers, which is the argument for adding a fixture when a screen
//     grows a state, rather than for trusting a green run here.
func TestEveryStringOnScreenIsTranslated(t *testing.T) {
	allowed := notCopy(t)
	used := map[string]bool{}
	views := renderedViews(t)
	onScreen := map[string]map[string]string{}
	for name, page := range views {
		onScreen[name] = panelStrings(t, page)
	}

	if !strings.Contains(i18nScript, iconRuleInScript) {
		t.Fatalf("i18n.js no longer carries the icon rule this test ports:\n  %s", iconRuleInScript)
	}

	// Before reading anything into what the walk did not find, check that it
	// got to the bottom of the page at all.
	for name, landmarks := range reachedAtDepth {
		if onScreen[name] == nil {
			t.Errorf("%s is no longer one of the rendered screens", name)
			continue
		}
		for _, want := range landmarks {
			if _, ok := onScreen[name][want]; !ok {
				t.Errorf("the walk did not reach %q on %s, so what it reports about that screen means nothing",
					want, name)
			}
		}
	}

	for _, language := range []string{core.LanguageChineseTraditional, core.LanguageChineseSimplified} {
		words := dictionaryKeys(t, language)
		patterns := goPatterns(t, language)

		for _, name := range sortedKeys(views) {
			for _, s := range sortedKeys(onScreen[name]) {
				trimmed := strings.Trim(s, jsTrimCutset)
				if _, ok := allowed[trimmed]; ok {
					used[trimmed] = true
					continue
				}
				if translatedBy(s, words, patterns) || !readable(trimmed) {
					continue
				}
				t.Errorf("%s/%s is shown in English, and %s does not answer it — %s: %s",
					name, language, language, onScreen[name][s], trimmed)
			}
		}
	}

	for _, s := range sortedKeys(allowed) {
		if !used[s] {
			t.Errorf("%q is listed as not needing translation (%s), but no screen shows it any more", s, allowed[s])
		}
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
