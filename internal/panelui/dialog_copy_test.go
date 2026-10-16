package panelui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/miou1107/multi-claude-switcher/core"
)

// The dialogs are the one part of the panel no walk of a rendered screen can
// see. Their copy does not exist in any page's HTML: the shell's own
// JavaScript builds each sentence when somebody clicks, and writes it in with
// textContent, so the localization coverage check in i18n_coverage_test.go
// never meets it. Ten dictionary entries exist only to answer these
// sentences, and the next one somebody adds to askRemove or askReport without
// touching both dictionaries would be read in English by everyone who asked
// for Chinese, with every test here still green.
//
// So this file reads the shell instead of a rendered page, and it reads it
// where the copy is, rather than hunting for sentence-shaped strings: every
// sentence goes through askConfirm, which is the only code that writes text
// into the dialog, and each piece of copy is one of its arguments. An
// argument's position says what it is, so there is no guessing about which
// literals in the script are copy and which are selectors or action names.

// dialogPlaceholder stands in for whatever the panel interpolates — an account
// name, a count, a folder. It is a character no copy contains, so filling it
// back in cannot collide with the sentence around it.
const dialogPlaceholder = "⟦⟧"

// copyArguments are the positions of askConfirm's parameters that reach the
// screen, by the order of its own signature:
//
//	askConfirm(action, arg, title, body, okLabel, warn, kind)
//
// action, arg and kind drive behaviour and are never read by anyone. The
// fifth, warn, is often left off, and then askConfirm falls back to the line
// already sitting in the modal's markup, which the rendered-screen check
// covers.
var copyArguments = map[int]string{2: "title", 3: "body", 4: "okLabel", 5: "warn"}

// dialogCopySinks is every place the shell writes text into the dialog. The
// check below is only as complete as this list is short: a fifth assignment
// would be copy nothing here reads, so the test fails when the set changes
// rather than carrying on against a stale assumption.
var dialogCopySinks = []string{
	`document.getElementById('mcsModalTitle').textContent=title;`,
	`document.getElementById('mcsModalBody').textContent=body;`,
	`warnEl.textContent = warnText;`,
	`document.getElementById('mcsModalOk').textContent=okLabel;`,
}

var scriptBlock = regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`)

// blankComments replaces every comment with spaces of the same length, so the
// scan below reads only code while every offset still points where it did.
// The shell's comments are long and full of prose about the dialogs — one of
// them spells out an askConfirm call — and a scan that reads them would
// report sentences no dialog shows.
func blankComments(script string) string {
	b := []byte(script)
	out := make([]byte, len(b))
	copy(out, b)
	const (
		code = iota
		lineComment
		blockComment
		quoted
	)
	state := code
	var quote byte
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch state {
		case code:
			switch {
			case c == '/' && i+1 < len(b) && b[i+1] == '/':
				state = lineComment
				out[i], out[i+1] = ' ', ' '
				i++
			case c == '/' && i+1 < len(b) && b[i+1] == '*':
				state = blockComment
				out[i], out[i+1] = ' ', ' '
				i++
			case c == '\'' || c == '"' || c == '`':
				state, quote = quoted, c
			}
		case lineComment:
			if c == '\n' {
				state = code
			} else {
				out[i] = ' '
			}
		case blockComment:
			if c == '*' && i+1 < len(b) && b[i+1] == '/' {
				out[i], out[i+1] = ' ', ' '
				state = code
				i++
			} else if c != '\n' {
				out[i] = ' '
			}
		case quoted:
			if c == '\\' && i+1 < len(b) {
				i++
			} else if c == quote {
				state = code
			}
		}
	}
	return string(out)
}

// jsToken is a run of an expression: either one string literal's contents, or
// a stretch of everything else.
type jsToken struct {
	text     string
	isString bool
}

// jsTokens splits a JavaScript expression into string literals and the code
// between them. It is the same reading jsStringLiterals does, kept separate
// because this one has to know where the literals sat: 'Switch to '+name+'?'
// is three tokens, and losing the order would lose the sentence.
func jsTokens(t *testing.T, expr string) []jsToken {
	t.Helper()
	var out []jsToken
	var lit, code strings.Builder
	var quote byte
	inString := false

	flushCode := func() {
		if code.Len() > 0 {
			out = append(out, jsToken{text: code.String()})
			code.Reset()
		}
	}
	b := []byte(expr)
	for i := 0; i < len(b); i++ {
		c := b[i]
		if !inString {
			if c == '\'' || c == '"' || c == '`' {
				flushCode()
				inString, quote = true, c
				lit.Reset()
				continue
			}
			code.WriteByte(c)
			continue
		}
		switch {
		case c == '\\' && i+1 < len(b):
			lit.WriteByte(b[i+1])
			i++
		case c == quote:
			out = append(out, jsToken{text: lit.String(), isString: true})
			inString = false
		default:
			lit.WriteByte(c)
		}
	}
	if inString {
		t.Fatalf("a string literal is not closed in: %s", expr)
	}
	flushCode()
	for _, tok := range out {
		if tok.isString && strings.Contains(tok.text, "${") {
			t.Fatalf("a template literal interpolates, which this test does not read: %s", expr)
		}
	}
	return out
}

// splitTopLevel cuts an expression at the given separators, ignoring any that
// sit inside a string, a call or a bracket. Argument lists and the ternaries
// that pick one sentence of three are both read with it.
func splitTopLevel(t *testing.T, expr string, seps string) []string {
	t.Helper()
	var out []string
	var cur strings.Builder
	depth := 0
	var quote byte
	inString := false

	b := []byte(expr)
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inString {
			cur.WriteByte(c)
			if c == '\\' && i+1 < len(b) {
				cur.WriteByte(b[i+1])
				i++
				continue
			}
			if c == quote {
				inString = false
			}
			continue
		}
		switch {
		case c == '\'' || c == '"' || c == '`':
			inString, quote = true, c
			cur.WriteByte(c)
		case c == '(' || c == '[' || c == '{':
			depth++
			cur.WriteByte(c)
		case c == ')' || c == ']' || c == '}':
			depth--
			cur.WriteByte(c)
		case depth == 0 && strings.IndexByte(seps, c) >= 0:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	out = append(out, cur.String())
	return out
}

// sentencesFrom turns one argument expression into every sentence it can put
// on screen. A string literal contributes its own text; anything else
// contributes a placeholder, unless it names a local this function assigned a
// sentence to, in which case each of that local's sentences is substituted —
// which is how askSync's body, 'Claude closes, '+what+' from '+…, is read as
// the sentence a user sees rather than as a fragment with a hole in it.
func sentencesFrom(t *testing.T, expr string, locals map[string][]string) []string {
	t.Helper()
	out := []string{""}
	for _, tok := range jsTokens(t, expr) {
		var parts []string
		switch {
		case tok.isString:
			parts = []string{tok.text}
		default:
			// What is left once the concatenation itself is taken out: the
			// + signs and the line breaks between the pieces are not values,
			// and reading them as one would put a placeholder in front of
			// every sentence in the file.
			name := strings.TrimSpace(strings.Trim(strings.TrimSpace(tok.text), "+"))
			switch vals, ok := locals[name]; {
			case name == "":
				continue
			case ok:
				parts = vals
			default:
				parts = []string{dialogPlaceholder}
			}
		}
		var next []string
		for _, have := range out {
			for _, p := range parts {
				next = append(next, have+p)
			}
		}
		// A single argument that can read more than a handful of ways means
		// this reading has lost the thread, not that the panel has that many
		// dialogs.
		if len(next) > 24 {
			t.Fatalf("argument reads %d ways, which cannot be right: %s", len(next), expr)
		}
		out = next
	}
	return out
}

// localSentences reads the `var x = …` lines of one helper, so a sentence
// assigned to a name before it is passed on is still read as a sentence.
// Only the branches that hold a string literal are copy; a count or a folder
// assigned the same way is not, and falls through to a placeholder.
func localSentences(t *testing.T, body string) map[string][]string {
	t.Helper()
	locals := map[string][]string{}
	for _, line := range regexp.MustCompile(`(?s)\bvar\s+([A-Za-z_$][\w$]*)\s*=(.*?);`).FindAllStringSubmatch(body, -1) {
		name, expr := line[1], line[2]
		if !strings.ContainsAny(expr, "'\"`") {
			continue
		}
		var vals []string
		for _, branch := range splitTopLevel(t, expr, "?:") {
			if !strings.ContainsAny(branch, "'\"`") {
				continue // the test of a ternary, not one of its answers
			}
			vals = append(vals, sentencesFrom(t, branch, nil)...)
		}
		if len(vals) == 0 {
			t.Fatalf("var %s holds copy this test could not read: %s", name, expr)
		}
		locals[name] = vals
	}
	return locals
}

// enclosing returns the name and body of the function the byte at i sits in.
// The name is what the failure message needs: "askRemove" is where somebody
// looks, and the dialogs' copy is spread over four helpers that all end up in
// the same call.
func enclosing(t *testing.T, script string, i int) (string, string) {
	t.Helper()
	start := strings.LastIndex(script[:i], "function ")
	if start < 0 {
		t.Fatalf("an askConfirm call at %d sits outside any function", i)
	}
	open := strings.Index(script[start:], "{")
	if open < 0 {
		t.Fatalf("the function around %d has no body", i)
	}
	open += start
	depth := 0
	for j := open; j < len(script); j++ {
		switch script[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				name := strings.TrimSpace(script[start+len("function ") : strings.Index(script[start:], "(")+start])
				return name, script[open+1 : j]
			}
		}
	}
	t.Fatalf("the function around %d is not closed", i)
	return "", ""
}

// dialogSentences reads one page's scripts and returns every sentence the
// dialogs can show, mapped to the helper that builds it.
func dialogSentences(t *testing.T, page string) map[string]string {
	t.Helper()
	found := map[string]string{}
	calls := 0
	code := ""
	for _, block := range scriptBlock.FindAllStringSubmatch(page, -1) {
		script := blankComments(block[1])
		code += script
		for i := 0; i+len("askConfirm(") <= len(script); i++ {
			if !strings.HasPrefix(script[i:], "askConfirm(") {
				continue
			}
			if strings.HasSuffix(script[:i], "function ") {
				continue // the definition, not a call
			}
			calls++
			name, body := enclosing(t, script, i)
			locals := localSentences(t, body)

			open := i + len("askConfirm(") - 1
			depth, end := 0, -1
			var quote byte
			inString := false
			for j := open; j < len(script); j++ {
				c := script[j]
				if inString {
					if c == '\\' {
						j++
					} else if c == quote {
						inString = false
					}
					continue
				}
				switch c {
				case '\'', '"', '`':
					inString, quote = true, c
				case '(':
					depth++
				case ')':
					depth--
					if depth == 0 {
						end = j
					}
				}
				if end >= 0 {
					break
				}
			}
			if end < 0 {
				t.Fatalf("%s: an askConfirm call is not closed", name)
			}
			args := splitTopLevel(t, script[open+1:end], ",")
			// action, arg, title, body and okLabel are always passed; warn and
			// kind are not. Fewer than five means this reading has gone wrong,
			// or a dialog is being built without a button.
			if len(args) < 5 {
				t.Fatalf("%s: askConfirm called with %d arguments: %q", name, len(args), args)
			}
			for pos, what := range copyArguments {
				if pos >= len(args) {
					continue
				}
				for _, s := range sentencesFrom(t, args[pos], locals) {
					if strings.TrimSpace(s) == "" {
						continue // an explicitly empty warn hides the block
					}
					found[s] = name + "'s " + what
				}
			}
		}
	}
	// A reading that finds no calls would report nothing missing and pass. The
	// count has to match what a plain text search sees.
	if want := strings.Count(code, "askConfirm(") - strings.Count(code, "function askConfirm("); calls != want {
		t.Fatalf("read %d askConfirm calls, the page's scripts have %d", calls, want)
	}
	return found
}

// Every sentence a dialog can show has to be in both dictionaries, the same
// way every sentence on a screen does. Nothing else checks these: they are
// built in JavaScript at click time and appear in no page's HTML, so the
// rendered-screen check cannot see them.
func TestEveryDialogSentenceIsTranslated(t *testing.T) {
	// Every screen, not one of them: the shell is the same on all of them, but
	// RenderNewProfile adds a second script of its own, and an earlier guard
	// here read only the first script on a page and so covered the dialogs
	// everywhere except the two screens that carry that one.
	sentences := map[string]string{}
	for _, name := range sortedKeys(renderedViews(t)) {
		page := renderedViews(t)[name]
		for _, sink := range dialogCopySinks {
			if !strings.Contains(page, sink) {
				t.Fatalf("%s: the shell no longer writes dialog text with %q, so this test is reading the wrong thing", name, sink)
			}
		}
		// One more assignment than the four above means copy this test never sees.
		if got, want := strings.Count(page, ".textContent"), len(dialogCopySinks); got != want {
			t.Errorf("%s: the shell writes textContent %d times, and this test accounts for %d", name, got, want)
		}
		for sentence, where := range dialogSentences(t, page) {
			sentences[sentence] = where
		}
	}
	// The sentences are spread over four helpers; finding only some of them
	// means the reading stopped early.
	for _, want := range []string{"Open a GitHub issue?", "Got it", "Switch", "Sync"} {
		if _, ok := sentences[want]; !ok {
			t.Errorf("the dialogs' copy no longer includes %q, so what this test reports means nothing", want)
		}
	}

	for _, language := range []string{core.LanguageChineseTraditional, core.LanguageChineseSimplified} {
		words := dictionaryKeys(t, language)
		patterns := goPatterns(t, language)
		for _, s := range sortedKeys(sentences) {
			if answered(s, words, patterns) {
				continue
			}
			t.Errorf("%s is shown in English, and %s does not answer it — %s: %s",
				language, language, sentences[s], s)
		}
	}
}

// answered asks whether the script could translate this sentence. A sentence
// with nothing interpolated has to be in the dictionary as it stands. One with
// a placeholder is a sentence the patterns answer, and a pattern only matches
// a filled-in sentence, so the placeholders are filled before asking — with a
// name and with a count, in every combination, since one sentence can carry
// both and the patterns are particular about which is which.
func answered(sentence string, words map[string]bool, patterns []*regexp.Regexp) bool {
	for _, filled := range fills(sentence) {
		if translatedBy(filled, words, patterns) {
			return true
		}
	}
	return false
}

func fills(sentence string) []string {
	n := strings.Count(sentence, dialogPlaceholder)
	if n == 0 {
		return []string{sentence}
	}
	out := []string{sentence}
	for i := 0; i < n; i++ {
		var next []string
		for _, s := range out {
			for _, sample := range []string{"Work", "2"} {
				next = append(next, strings.Replace(s, dialogPlaceholder, sample, 1))
			}
		}
		out = next
	}
	return out
}
