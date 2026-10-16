package panelui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/miou1107/multi-claude-switcher/core"
)

// The dialogs are the one part of the panel no walk of a rendered screen can
// see. Their copy is in no page's HTML: the shell's own JavaScript builds each
// sentence when somebody clicks, and writes it in with textContent, so the
// localization coverage check in i18n_coverage_test.go never meets it. Ten
// dictionary entries exist only to answer these sentences, and an eleventh
// added to askRemove or askReport without touching both dictionaries would be
// read in English by everyone who asked for Chinese, with every test green.
//
// So this file reads the shell instead of a rendered screen, and it reads it
// where the copy is rather than hunting for sentence-shaped strings: every
// sentence goes through askConfirm, the only code that writes text into a
// dialog, and each piece of copy is one of its arguments. An argument's
// position says what it is, so nothing has to be guessed about which literals
// in that script are copy and which are selectors or action names.
//
// Reading JavaScript from Go is only safe while the JavaScript stays inside
// what this reading covers, so every assumption it makes is asserted rather
// than trusted: the signature it takes positions from, the four assignments
// that put text on screen, the absence of any other way to write text, and
// the number of calls found against two independent counts of the name. A
// shape it cannot read fails the test; it never passes quietly.

// The holes in a sentence, and which kind of value the panel drops into each.
// The patterns in i18n.js are particular about that — one takes (\d+) where
// another takes (.*) — so filling a count's hole with an account name would
// call a sentence covered that the panel can never show.
const (
	namePlaceholder  = "⟦name⟧"
	countPlaceholder = "⟦count⟧"
	nameSample       = "Work"
	countSample      = "2"
)

// countExpressions are the interpolations that yield a number rather than a
// name: the shell has one, n, which both counting helpers assign from
// parseInt. Anything else is read as a name, so a count passed where a name
// belongs reports a sentence neither dictionary answers, which is the loud
// direction.
var countExpressions = map[string]bool{"n": true}

// askConfirmSignature is the line this file takes argument positions from,
// matched literally. A new parameter, or two swapped, would move the copy
// without moving anything here, and copyArguments would then read the wrong
// values with no sign that it had.
const askConfirmSignature = `function askConfirm(action, arg, title, body, okLabel, warn, kind){`

// copyArguments are the positions in that signature whose values reach the
// screen. action, arg and kind drive behaviour and are read by no one.
var copyArguments = map[int]string{2: "title", 3: "body", 4: "okLabel", 5: "warn"}

// dialogCopySinks is every assignment that puts text on a dialog. This check
// is only as complete as that list, so the test also refuses every other way
// of writing text into the DOM: a sixth parameter written in with innerText
// would otherwise be copy nothing here reads.
var dialogCopySinks = []string{
	`document.getElementById('mcsModalTitle').textContent=title;`,
	`document.getElementById('mcsModalBody').textContent=body;`,
	`warnEl.textContent = warnText;`,
	`document.getElementById('mcsModalOk').textContent=okLabel;`,
}

// otherWaysToWriteText are the other ways JavaScript can put text on screen.
// The shell uses none of them, and this reading cannot follow them, so it
// fails when one appears rather than going quietly blind.
var otherWaysToWriteText = []string{
	".innerText", ".innerHTML", ".outerHTML",
	"insertAdjacentText", "insertAdjacentHTML", "createTextNode", "replaceChildren",
}

var (
	scriptBlock    = regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`)
	askConfirmName = regexp.MustCompile(`\baskConfirm\b`)
	askConfirmCall = regexp.MustCompile(`\baskConfirm\s*\(`)
	// The warning askConfirm falls back to when a caller leaves warn off, and
	// the same line as it sits in the modal's own markup.
	defaultWarnInScript = regexp.MustCompile(`arguments\.length >= 6 \? warn : '([^']*)'`)
	defaultWarnInMarkup = regexp.MustCompile(`<div class="warn">([^<]*)</div>`)
	localAssignment     = regexp.MustCompile(`\b(?:var|let|const)\s+([A-Za-z_$][\w$]*)\s*=`)
)

// jsToken is a run of an expression: one string literal's contents, or a
// stretch of everything else.
type jsToken struct {
	text     string
	isString bool
}

// jsTokens splits a JavaScript expression into string literals and the code
// between them. It is the reading jsStringLiterals does, kept apart because
// this one has to know where the literals sat: 'Switch to '+name+'?' is three
// tokens, and losing the order would lose the sentence.
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
			// A quote or a backslash escapes to itself, which is what stops \'
			// from ending the literal early. Anything else stands for a
			// different character, and taking the letter after the backslash
			// would put that letter into the sentence.
			if next := b[i+1]; next != '\'' && next != '"' && next != '`' && next != '\\' {
				t.Fatalf("a string literal escapes %q, which this reading would get wrong: %s", string(next), expr)
			}
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
			t.Fatalf("a template literal interpolates, which this reading does not follow: %s", expr)
		}
	}
	return out
}

// splitTopLevel cuts an expression at the given separators, ignoring any that
// sit inside a string, a call or a bracket. Argument lists, the ternaries that
// pick one sentence of three, and the end of a statement are all found with it.
func splitTopLevel(expr string, seps string) []string {
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
	return append(out, cur.String())
}

// ternaryValues keeps the answers of a ternary chain and drops its tests.
// Flattened, a ? b : c ? d : e reads as a, b, c, d, e: the tests sit at the
// even positions, the answers at the odd ones, and the last piece is always an
// answer. Reading a test as an answer would check a condition as if it were
// copy; reading an answer as a test would leave a sentence unchecked, which is
// how a branch holding no literal of its own went missing before.
func ternaryValues(expr string) []string {
	pieces := splitTopLevel(expr, "?:")
	if len(pieces) == 1 {
		return pieces
	}
	var out []string
	for i := 1; i < len(pieces); i++ {
		if i%2 == 1 || i == len(pieces)-1 {
			out = append(out, pieces[i])
		}
	}
	return out
}

// sentencesFrom turns one expression into every sentence it can put on screen.
// A string literal contributes its own text; an interpolation contributes a
// hole, unless it names a local this helper assigned sentences to, in which
// case each of those is substituted — which is how askSync's body,
// 'Claude closes, '+what+' from '+…, is read as the sentence a user sees
// rather than as one with a hole in the middle.
func sentencesFrom(t *testing.T, expr string, locals map[string][]string) []string {
	t.Helper()
	if branches := ternaryValues(expr); len(branches) > 1 {
		// A ternary written straight into an argument reads as its answers,
		// the same way one assigned to a local does.
		var out []string
		for _, b := range branches {
			out = append(out, sentencesFrom(t, b, locals)...)
		}
		return out
	}
	out := []string{""}
	for _, tok := range jsTokens(t, expr) {
		var parts []string
		if tok.isString {
			parts = []string{tok.text}
		} else {
			// What is left once the concatenation itself is taken out: the +
			// signs and the line breaks between pieces are not values, and
			// reading them as one would put a hole in front of every sentence.
			name := strings.TrimSpace(strings.Trim(strings.TrimSpace(tok.text), "+"))
			switch vals, ok := locals[name]; {
			case name == "":
				continue
			case ok:
				parts = vals
			case countExpressions[name]:
				parts = []string{countPlaceholder}
			default:
				parts = []string{namePlaceholder}
			}
		}
		var next []string
		for _, have := range out {
			for _, p := range parts {
				next = append(next, have+p)
			}
		}
		// One argument that reads more than a handful of ways means this
		// reading has lost the thread, not that the panel has that many
		// dialogs.
		if len(next) > 24 {
			t.Fatalf("argument reads %d ways, which cannot be right: %s", len(next), expr)
		}
		out = next
	}
	return out
}

// localSentences reads one helper's assignments, so a sentence given a name
// before it is passed on is still read as a sentence. They are read in order,
// so a branch naming an earlier local is substituted too.
func localSentences(t *testing.T, body string) map[string][]string {
	t.Helper()
	locals := map[string][]string{}
	for _, m := range localAssignment.FindAllStringSubmatchIndex(body, -1) {
		name := body[m[2]:m[3]]
		// The end of the statement, found the same way as everything else: a
		// semicolon inside a string literal does not end it.
		expr := splitTopLevel(body[m[1]:], ";")[0]
		if !strings.ContainsAny(expr, "'\"`") {
			continue // a count or a folder, not copy
		}
		var vals []string
		for _, branch := range ternaryValues(expr) {
			vals = append(vals, sentencesFrom(t, branch, locals)...)
		}
		if len(vals) == 0 {
			t.Fatalf("%s holds copy this reading could not follow: %s", name, expr)
		}
		locals[name] = vals
	}
	return locals
}

// enclosing returns the name and body of the function the byte at i sits in.
// The name is what a failure message needs: askRemove is where somebody looks.
// A call this cannot place fails the test rather than being guessed at, since
// reading it against the wrong function would substitute another helper's
// sentences and leave this one's unchecked.
func enclosing(t *testing.T, script string, i int) (string, string) {
	t.Helper()
	start := strings.LastIndex(script[:i], "function ")
	if start < 0 {
		t.Fatalf("an askConfirm call at %d sits outside any function", i)
	}
	paren := strings.Index(script[start:], "(")
	if paren < 0 {
		t.Fatalf("the function before %d has no parameter list", i)
	}
	name := strings.TrimSpace(script[start+len("function ") : start+paren])
	open := strings.Index(script[start:], "{")
	if open < 0 {
		t.Fatalf("%s has no body", name)
	}
	open += start

	depth := 0
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
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				if i < open || i > j {
					// An arrow function, or anything else this does not read
					// as a function, would otherwise be attributed to
					// whichever plain function came before it, and be checked
					// with that one's locals.
					t.Fatalf("the askConfirm call at %d is not inside %s, the nearest function this reading found", i, name)
				}
				return name, script[open+1 : j]
			}
		}
	}
	t.Fatalf("%s is not closed", name)
	return "", ""
}

// blankComments replaces every comment with spaces of the same length, so the
// scan reads only code while every offset still points where it did. Comments
// sit inside askConfirm's own argument lists, carrying commas, brackets and
// apostrophes that would otherwise be read as part of a sentence.
//
// It cannot tell a regular expression literal from a division, which is the
// blind spot jsStringLiterals records for the same reason: /\/\// reads as a
// line comment, and the code after it on that line would be blanked away. The
// counts in dialogSentences are what stop that from hiding a call — they are
// taken before blanking as well as after.
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

// dialogSentences reads one page's scripts and returns every sentence the
// dialogs can show, mapped to the helper that builds it.
func dialogSentences(t *testing.T, page string) map[string]string {
	t.Helper()
	found := map[string]string{}
	for _, raw := range scripts(page) {
		if !strings.Contains(raw, "askConfirm") {
			continue
		}
		script := blankComments(raw)

		// The name appears once per call, plus once in the definition. Finding
		// fewer calls than names means this reading walked past one: written
		// askConfirm (…), reached through an alias, or hidden behind a regular
		// expression literal that blankComments mistook for a comment. The
		// script is counted before blanking as well, since blanking is what
		// could have removed it.
		calls := askConfirmCall.FindAllStringIndex(script, -1)
		definitions := strings.Count(script, "function askConfirm")
		if definitions != 1 {
			t.Fatalf("the shell defines askConfirm %d times", definitions)
		}
		// Every mention of the name is either the definition or a call this
		// reading found. One more means a call it cannot read: reached through
		// an alias, or through askConfirm.call, or written askConfirm?.(…).
		if got, want := len(askConfirmName.FindAllString(script, -1)), len(calls); got != want {
			t.Fatalf("askConfirm is named %d times in the shell, and this reading found %d calls: one it cannot read is copy nothing checks",
				got, want)
		}
		// And blanking the comments removed none of them. blankComments cannot
		// tell a regular expression literal from a division, so /\/\// would
		// read as a line comment and take the rest of that line with it —
		// including a call. Counting before and after is what catches that; a
		// commented-out call trips it too, and has to be reworded.
		if got, want := len(askConfirmCall.FindAllStringIndex(raw, -1)), len(calls); got != want {
			t.Fatalf("the shell calls askConfirm %d times, and %d survive blanking its comments: one is hidden behind a comment or a regular expression literal",
				got, want)
		}
		if !strings.Contains(script, askConfirmSignature) {
			t.Fatalf("askConfirm's signature has changed, so the positions this test takes copy from have to be read again:\n  want %s", askConfirmSignature)
		}

		for _, at := range calls {
			i := at[0]
			if strings.HasSuffix(script[:i], "function ") {
				continue // the definition, not a call
			}
			name, body := enclosing(t, script, i)
			locals := localSentences(t, body)

			open := at[1] - 1 // the ( this call's match ended on
			depth, end := 0, -1
			var quote byte
			inString := false
			for j := open; j < len(script) && end < 0; j++ {
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
			}
			if end < 0 {
				t.Fatalf("%s: an askConfirm call is not closed", name)
			}
			args := splitTopLevel(script[open+1:end], ",")
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
	if len(found) == 0 {
		t.Fatal("no dialog copy was found at all, so what this test reports means nothing")
	}
	return found
}

// Every sentence a dialog can show has to be in both dictionaries, the same
// way every sentence on a screen does. Nothing else checks these: they are
// built in JavaScript at click time and appear in no page's HTML, so the
// rendered-screen check cannot see them.
func TestEveryDialogSentenceIsTranslated(t *testing.T) {
	views := renderedViews(t)
	sentences := map[string]string{}
	// Every screen, not one of them: the shell is the same on all of them, but
	// RenderNewProfile adds a second script of its own, and an earlier guard
	// here read only the first script on a page and so covered the dialogs
	// everywhere except the two screens that carry that one.
	for _, name := range sortedKeys(views) {
		for sentence, where := range dialogSentences(t, views[name]) {
			sentences[sentence] = where
		}
	}
	checkDialogCopySinks(t, views)

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

// checkDialogCopySinks holds the assumption this whole file rests on:
// askConfirm writes the dialog's text in four places, and the shell has no
// other way of putting text on screen. Every page is checked, since each
// carries its own copy of the shell, and each complaint is made once rather
// than once per page.
func checkDialogCopySinks(t *testing.T, views map[string]string) {
	t.Helper()
	said := map[string]bool{}
	complain := func(format string, args ...any) {
		if said[format] {
			return
		}
		said[format] = true
		t.Errorf(format, args...)
	}
	for _, name := range sortedKeys(views) {
		page := views[name]
		script := blankComments(strings.Join(scripts(page), "\n"))
		for _, sink := range dialogCopySinks {
			if !strings.Contains(script, sink) {
				complain("the shell no longer writes dialog text with %q, so this test is reading the wrong thing", sink)
			}
		}
		// Counted on the code rather than the page: a comment, or a status
		// line that mentions textContent, is not an assignment.
		if got, want := strings.Count(script, ".textContent"), len(dialogCopySinks); got != want {
			complain("the shell writes textContent %d times, and this test accounts for %d", got, want)
		}
		for _, other := range otherWaysToWriteText {
			if strings.Contains(script, other) {
				complain("the shell now writes text with %s, which this reading cannot follow", other)
			}
		}
		// askConfirm's own fallback warning is copy too, and the only piece
		// not passed in by a caller. The rendered-screen check covers it
		// because the same line sits in the modal's markup — which is true
		// only while the two match, and that is what this asserts.
		inScript := defaultWarnInScript.FindStringSubmatch(script)
		inMarkup := defaultWarnInMarkup.FindStringSubmatch(page)
		switch {
		case inScript == nil:
			complain("askConfirm no longer falls back to a default warning this test can find")
		case inMarkup == nil:
			complain("the modal's markup no longer carries the default warning")
		case inScript[1] != inMarkup[1]:
			complain("the default warning differs between the shell and the markup, so neither check covers it: %q against %q",
				inScript[1], inMarkup[1])
		}
	}
}

func scripts(page string) []string {
	var out []string
	for _, block := range scriptBlock.FindAllStringSubmatch(page, -1) {
		out = append(out, block[1])
	}
	return out
}

// answered asks whether the script could translate this sentence. One with
// nothing interpolated has to be in the dictionary as it stands. One with a
// hole is a sentence the patterns answer, and a pattern only matches a
// finished sentence, so the holes are filled first — each with the kind of
// value the panel puts there.
func answered(sentence string, words map[string]bool, patterns []*regexp.Regexp) bool {
	return translatedBy(fills(sentence), words, patterns)
}

func fills(sentence string) string {
	sentence = strings.ReplaceAll(sentence, namePlaceholder, nameSample)
	return strings.ReplaceAll(sentence, countPlaceholder, countSample)
}
