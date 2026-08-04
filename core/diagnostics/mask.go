// Package diagnostics turns what MCS knows about a machine into a report a user
// can publish. Its job is not gathering — each host does that — but masking and
// formatting, which is the part worth testing and the part that must not depend
// on which platform it runs on.
package diagnostics

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Masker replaces identifying values with stable pseudonyms.
//
// Pseudonyms rather than asterisks, because "vin***@fontrip.com" still gives up
// a name and an employer, and two occurrences of it cannot be told apart. A
// report is made of relationships — this account's conversations turned up in
// that account's folder — and a pseudonym keeps every relationship while giving
// up none of the values.
type Masker struct {
	// byValue maps a raw value to its pseudonym. One table, keyed by value and
	// never by role: a UUID that arrives as both an account and an organization
	// must not answer to two names depending on which was registered first.
	byValue  map[string]string
	bounded  []boundedRule
	homes    []homeRule
	accounts int
	orgs     int
}

type boundedRule struct {
	re          *regexp.Regexp
	replacement string
}

type homeRule struct {
	re   *regexp.Regexp
	with string
}

// guardRune brackets a pseudonym the moment Apply inserts it, so a bounded-word
// rule running later cannot mistake the pseudonym's own edge for a word
// boundary. It is a Private Use Area code point, chosen only because real
// diagnostics text never contains one; it never survives past Apply's return.
//
// Without this, a single-letter org ("org-A") or a numbered account
// ("account-1") is one boundary away from colliding with a short OS user name
// registered through RegisterBoundedWord — a user named "A" would otherwise
// see the masker's own output torn open, not just their own name masked.
const guardRune = '\ue000'

var guardString = string(guardRune)

// guard wraps a just-inserted replacement so later bounded-word rules see a
// non-boundary character at both of its edges instead of the real boundary
// that would otherwise sit there.
func guard(s string) string {
	return guardString + s + guardString
}

func NewMasker() *Masker {
	return &Masker{byValue: map[string]string{}}
}

// RegisterAccount ties a UUID and an email to one pseudonym, so a log line that
// only ever mentions the UUID still reads as the same account as the summary
// line that mentions the address. Either may be empty; a profile that is not
// signed in has both.
func (m *Masker) RegisterAccount(uuid, email string) {
	name := ""
	for _, v := range []string{uuid, email} {
		if v == "" {
			continue
		}
		if existing, ok := m.byValue[v]; ok {
			name = existing
			break
		}
	}
	if name == "" {
		if uuid == "" && email == "" {
			return
		}
		m.accounts++
		name = fmt.Sprintf("account-%d", m.accounts)
	}
	m.put(uuid, name)
	m.put(email, name)
}

// RegisterOrg gives an organization UUID a letter. Letters rather than numbers
// so an org is never mistaken for an account at a glance.
func (m *Masker) RegisterOrg(uuid string) {
	if uuid == "" {
		return
	}
	if _, ok := m.byValue[uuid]; ok {
		return
	}
	m.orgs++
	m.put(uuid, "org-"+orgLetter(m.orgs))
}

// RegisterWord registers a value with a caller-chosen replacement, for the
// values that are not identifiers in their own right but give a person away all
// the same: the OS user name, the host name.
func (m *Masker) RegisterWord(value, replacement string) {
	if value == "" {
		return
	}
	m.put(value, replacement)
}

// RegisterBoundedWord replaces value only where it stands on its own — bounded
// by a separator, not by a letter or digit.
//
// The OS user name has to be masked as a value, because the home-prefix rule
// cannot reach /Volumes/…/<user>/… or D:\WorkData\<user>\…. But user names are
// short ordinary words, and replacing "admin" everywhere turns "administrator"
// into "useristrator". A boundary is the difference between a masked report and
// a corrupted one.
//
// guardRune joins the set of characters that do NOT count as a boundary, so
// this rule also refuses to match against the outer edge of a pseudonym Apply
// has already inserted in this same pass.
func (m *Masker) RegisterBoundedWord(value, replacement string) {
	if value == "" {
		return
	}
	sep := `[^\p{L}\p{N}` + regexp.QuoteMeta(guardString) + `]`
	m.bounded = append(m.bounded, boundedRule{
		re:          regexp.MustCompile(`(?i)(^|` + sep + `)` + regexp.QuoteMeta(value) + `($|` + sep + `)`),
		replacement: replacement,
	})
}

// homeSepInQuoted finds a separator inside an already-QuoteMeta'd path: either
// a quoted backslash (which QuoteMeta always renders as two backslash bytes)
// or a bare forward slash (which QuoteMeta leaves untouched, since it is not a
// regex metacharacter). Matched in one alternation, not two sequential
// ReplaceAll passes: the class we substitute in, "[\\/]", itself contains a
// forward slash, and a second pass over already-substituted text would find
// that slash and mangle its own output.
var homeSepInQuoted = regexp.MustCompile(`\\\\|/`)

// homeTrailingBoundary is what may follow a home prefix for it to count as
// the whole path rather than just a prefix of a longer, unrelated name: a
// path separator, the end of the string, whitespace (which in Go's regexp
// already includes "\n", so this doubles as the end-of-line case a bare "$"
// misses without "(?m)"), or quote-like/clause-closing punctuation that a
// report's prose puts right after a path — a closing quote, a trailing
// comma or semicolon, a closing bracket.
//
// This deliberately stops short of "any non-alphanumeric": a folder name can
// continue past the home prefix with a hyphen or a dot ("/Users/adam-work",
// "/Users/adam.old"), and treating those as boundaries would reopen the same
// leak RegisterHome's trailing boundary exists to close, just from the
// far side — masking "/Users/adam-work/x" down to "~-work/x" and leaking the
// residue of a different folder's name.
const homeTrailingBoundary = `([\\/]|$|[\s"'` + "`" + `,;:)\]}!?])`

// RegisterHome rewrites a home directory prefix, keeping everything after it.
// Case-insensitive and separator-blind, because Windows reports both spellings
// and mixes them inside one string.
//
// The prefix must be followed by a separator, the end of the string,
// whitespace, or quote-like punctuation (see homeTrailingBoundary). Without
// that trailing boundary, "/Users/adam" also matches the start of
// "/Users/adamson", rewriting a sibling account's home and leaking the
// residue of its name ("~son") into the output — corruption plus a privacy
// leak, on any machine where one login name happens to prefix another's.
func (m *Masker) RegisterHome(home, replacement string) {
	if home == "" {
		return
	}
	pat := regexp.QuoteMeta(home)
	pat = homeSepInQuoted.ReplaceAllString(pat, `[\\/]`)
	m.homes = append(m.homes, homeRule{
		re:   regexp.MustCompile(`(?i)` + pat + homeTrailingBoundary),
		with: replacement,
	})
}

func (m *Masker) put(value, name string) {
	if value == "" {
		return
	}
	if _, ok := m.byValue[value]; ok {
		return
	}
	m.byValue[value] = name
}

// orgLetter numbers organizations A, B, … Z, AA, AB. Sequential rather than
// hashed so two reports from one machine line up.
func orgLetter(n int) string {
	out := ""
	for n > 0 {
		n--
		out = string(rune('A'+n%26)) + out
		n /= 26
	}
	return out
}

// Apply replaces every registered value in s.
//
// Ordering is explicit: exact values first (they are the most specific), then
// home prefixes, then bounded words (the least specific, and the ones most
// likely to fire inside a path the earlier rules already handled). Apply
// accumulates into one string across all three passes, so a later rule can
// see — and mismask — an earlier rule's output; exact values and home
// prefixes are inserted wrapped in guardRune specifically so the bounded pass
// cannot mistake their edges for a word boundary. See guard's doc comment.
//
// Longest first among exact values: an email and a UUID can share a prefix
// with something else registered, and replacing the shorter one first would
// leave a fragment of the longer one behind.
func (m *Masker) Apply(s string) string {
	if s == "" {
		return s
	}
	for _, v := range m.sortedValues() {
		s = strings.ReplaceAll(s, v, guard(m.byValue[v]))
	}
	for _, h := range m.homes {
		replaceHome := func(match string) string {
			// The trailing separator (or end-of-string empty match) is part of
			// the match so the boundary check applies, but it belongs to
			// whatever comes after the home, not to the home itself — put it
			// back after the replacement instead of consuming it.
			groups := h.re.FindStringSubmatch(match)
			return guard(h.with) + groups[1]
		}
		// Twice, for the same reason the bounded pass below runs twice: the
		// trailing separator is part of the match, so two adjacent, abutting
		// occurrences of the same home ("/Users/adam/Users/adam/L") share the
		// one separator between them. The first pass consumes it as the first
		// occurrence's trailing boundary and reinserts it right after the
		// replacement, which puts it back in front of the second occurrence —
		// available as that occurrence's own leading text only on a second
		// pass over the result, not within the first pass's single left-to-
		// right scan.
		s = h.re.ReplaceAllStringFunc(s, replaceHome)
		s = h.re.ReplaceAllStringFunc(s, replaceHome)
	}
	for _, b := range m.bounded {
		// Twice: adjacent matches share the separator the pattern consumes, so a
		// single pass leaves the second of "…/admin/admin/…" behind. Each pass
		// is done with ReplaceAllStringFunc rather than the "${1}"+with+"${2}"
		// template ReplaceAllString would need, for two reasons: the
		// replacement text must be inserted literally (a caller-supplied
		// replacement like "$USER" is data, not a "$1"-style expansion
		// operator), and the inserted text must be guarded so the second pass
		// — looking for the very same bounded word — cannot mistake its own
		// first pass's output for a fresh match, the same hazard the value and
		// home passes are already guarded against.
		replaceBounded := func(match string) string {
			groups := b.re.FindStringSubmatch(match)
			return groups[1] + guard(b.replacement) + groups[2]
		}
		s = b.re.ReplaceAllStringFunc(s, replaceBounded)
		s = b.re.ReplaceAllStringFunc(s, replaceBounded)
	}
	if strings.Contains(s, guardString) {
		s = strings.ReplaceAll(s, guardString, "")
	}
	return s
}

func (m *Masker) sortedValues() []string {
	out := make([]string, 0, len(m.byValue))
	for v := range m.byValue {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}
