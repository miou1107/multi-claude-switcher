package diagnostics

import "testing"

// TestMaskerCollapsesOneAccountToOnePseudonym pins the property the whole
// report is read through: the same account is the same name wherever it turns
// up, whether it appears as an address in the summary or as a bare UUID in a
// log line thirty lines further down.
func TestMaskerCollapsesOneAccountToOnePseudonym(t *testing.T) {
	m := NewMasker()
	m.RegisterAccount("035899b2-b130-40b6-aa9e-93cf208df7b7", "vincent@fontrip.com")
	m.RegisterAccount("ae543f88-0f24-4ae6-ae21-3033915bca76", "other@example.com")

	got := m.Apply("vincent@fontrip.com switched; bucket 035899b2-b130-40b6-aa9e-93cf208df7b7 has 91 files")
	want := "account-1 switched; bucket account-1 has 91 files"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if got := m.Apply("other@example.com"); got != "account-2" {
		t.Errorf("second account = %q, want account-2", got)
	}
}

// TestMaskerNumbersInFirstSeenOrder keeps two reports from the same machine
// comparable: registration order is the report's own order, not map order.
func TestMaskerNumbersInFirstSeenOrder(t *testing.T) {
	m := NewMasker()
	m.RegisterOrg("d129c8c1-7834-4e6c-84a4-dc19dfeedc8f")
	m.RegisterOrg("245fb00c-4b74-4d8d-9ba8-3580e216ff85")
	if got := m.Apply("d129c8c1-7834-4e6c-84a4-dc19dfeedc8f"); got != "org-A" {
		t.Errorf("first org = %q, want org-A", got)
	}
	if got := m.Apply("245fb00c-4b74-4d8d-9ba8-3580e216ff85"); got != "org-B" {
		t.Errorf("second org = %q, want org-B", got)
	}
}

// TestMaskerKeepsOnePseudonymPerValue guards the ordering trap: a UUID that
// arrives in two roles must not get two names depending on which call came
// first, or the report says two different things about one thing.
func TestMaskerKeepsOnePseudonymPerValue(t *testing.T) {
	shared := "d129c8c1-7834-4e6c-84a4-dc19dfeedc8f"

	m1 := NewMasker()
	m1.RegisterAccount(shared, "")
	m1.RegisterOrg(shared)

	m2 := NewMasker()
	m2.RegisterOrg(shared)
	m2.RegisterAccount(shared, "")

	if got := m1.Apply(shared); got != "account-1" {
		t.Errorf("account-first = %q, want account-1", got)
	}
	if got := m2.Apply(shared); got != "org-A" {
		t.Errorf("org-first = %q, want org-A", got)
	}
	if m1.Apply(shared+" "+shared) != "account-1 account-1" {
		t.Error("a value must mask to one name within one report")
	}
}

// TestMaskerIgnoresEmptyRegistrations stops an unsigned-in profile, whose email
// and uuid are both "", from turning every empty string in the report into
// account-1.
func TestMaskerIgnoresEmptyRegistrations(t *testing.T) {
	m := NewMasker()
	m.RegisterAccount("", "")
	m.RegisterOrg("")
	if got := m.Apply("nothing to mask here"); got != "nothing to mask here" {
		t.Errorf("got %q, want the input unchanged", got)
	}
}

// TestMaskerBoundedWordDoesNotEatLongerWords is the admin/administrator trap.
// A user name is a short ordinary word, so replacing it everywhere corrupts
// unrelated text, and corrupted text in a bug report is worse than absent text.
func TestMaskerBoundedWordDoesNotEatLongerWords(t *testing.T) {
	m := NewMasker()
	m.RegisterBoundedWord("admin", "user")

	cases := []struct{ in, want string }{
		{"administrator rights", "administrator rights"},
		{"the admin account", "the user account"},
		{"/Volumes/Data/admin/Claude", "/Volumes/Data/user/Claude"},
		{`D:\WorkData\admin\Claude`, `D:\WorkData\user\Claude`},
		{"admin", "user"},
		{"badmin", "badmin"},
		{"admin@example.com", "user@example.com"},
	}
	for _, c := range cases {
		if got := m.Apply(c.in); got != c.want {
			t.Errorf("Apply(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMaskerRewritesTheHomePrefix keeps the tail of a path, because which
// folder inside the profile a file landed in is usually the whole bug.
func TestMaskerRewritesTheHomePrefix(t *testing.T) {
	m := NewMasker()
	m.RegisterHome("/Users/vincentkao", "~")

	in := "backup /Users/vincentkao/Library/Application Support/Claude/config.json"
	want := "backup ~/Library/Application Support/Claude/config.json"
	if got := m.Apply(in); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestMaskerRewritesTheHomePrefixWithMixedSeparators covers what Windows
// actually emits: one string carrying both spellings, because a Go-built path
// and a command line reported by the OS meet in the same log line.
func TestMaskerRewritesTheHomePrefixWithMixedSeparators(t *testing.T) {
	m := NewMasker()
	m.RegisterHome(`C:\Users\Adam`, "%USERPROFILE%")

	cases := []struct{ in, want string }{
		{`C:\Users\Adam\AppData\Roaming\Claude`, `%USERPROFILE%\AppData\Roaming\Claude`},
		{`C:\Users\Adam/AppData/Roaming/Claude`, `%USERPROFILE%/AppData/Roaming/Claude`},
		{`c:\users\adam\AppData`, `%USERPROFILE%\AppData`},
	}
	for _, c := range cases {
		if got := m.Apply(c.in); got != c.want {
			t.Errorf("Apply(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMaskerHandlesWindowsHomeWithSpacedBoundedUserName closes a Task 3
// ledger gap: no test paired a Windows home directory with a bounded (Task 9)
// user-name replacement, which is the combination a real Windows machine
// produces whenever the account's display name contains a space —
// "C:\Users\Adam Smith" is what Windows actually creates, not "C:\Users\Adam".
// The home rule and the bounded-word rule both have to fire correctly in the
// same string: the home prefix consumes "C:\Users\Adam Smith" as one unit
// (RegisterHome matches literally, space included), and the bounded rule
// still has to find "Adam Smith" as a whole word elsewhere in the same log
// line, with either path separator, without either rule corrupting the
// other's output.
func TestMaskerHandlesWindowsHomeWithSpacedBoundedUserName(t *testing.T) {
	m := NewMasker()
	m.RegisterHome(`C:\Users\Adam Smith`, "%USERPROFILE%")
	m.RegisterBoundedWord("Adam Smith", "user")

	cases := []struct{ in, want string }{
		{`C:\Users\Adam Smith\AppData\Roaming\Claude`, `%USERPROFILE%\AppData\Roaming\Claude`},
		{`C:\Users\Adam Smith/AppData/Roaming/Claude`, `%USERPROFILE%/AppData/Roaming/Claude`},
		{`seen under D:\WorkData\Adam Smith\logs`, `seen under D:\WorkData\user\logs`},
	}
	for _, c := range cases {
		if got := m.Apply(c.in); got != c.want {
			t.Errorf("Apply(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMaskerBoundedWordDoesNotCorruptGeneratedPseudonyms is the hazard the
// Task 2 reviewer raised: Apply accumulates into one string, so a bounded-word
// rule running after pseudonyms have been inserted can match inside the
// masker's own output, not just inside the caller's text. A single-letter org
// pseudonym ("org-A") and a numbered account pseudonym ("account-1") both end
// in a character that a short OS user name can collide with at a real word
// boundary — a user named "A" or "1" would otherwise see the masker's own
// pseudonym torn open.
func TestMaskerBoundedWordDoesNotCorruptGeneratedPseudonyms(t *testing.T) {
	m := NewMasker()
	m.RegisterOrg("d129c8c1-7834-4e6c-84a4-dc19dfeedc8f") // becomes "org-A"
	m.RegisterAccount("035899b2-b130-40b6-aa9e-93cf208df7b7", "")
	m.RegisterBoundedWord("A", "user-a")
	m.RegisterBoundedWord("1", "user-1")

	got := m.Apply("org d129c8c1-7834-4e6c-84a4-dc19dfeedc8f, account 035899b2-b130-40b6-aa9e-93cf208df7b7")
	want := "org org-A, account account-1"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestMaskerBoundedWordDoesNotEatItsOwnReplacement is the double-pass hazard a
// code-review pass on Task 3 caught: the bounded pass runs its regex twice
// (to catch adjacent matches), and guards the value/home insertions from the
// earlier passes, but never guards its own output. So the second pass over a
// replacement that itself contains the registered word matches its own first
// pass's insertion, and two bounded rules can chain into each other through
// their own outputs.
func TestMaskerBoundedWordDoesNotEatItsOwnReplacement(t *testing.T) {
	m := NewMasker()
	m.RegisterBoundedWord("A", "user-a")

	got := m.Apply("Section A, item-A, Track A-1")
	want := "Section user-a, item-user-a, Track user-a-1"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestMaskerBoundedWordDoesNotChainIntoAnotherBoundedRule is the second half
// of the same hazard: one rule's replacement text can satisfy a later rule's
// own pattern, letting registrations mask each other's output instead of just
// the caller's text. "adam" is registered to become "user", and a later rule
// separately masks bare "user" to "host". The inserted "user" (from the first
// rule) must survive the second rule untouched; only the caller's own,
// pre-existing "user" is fair game for the second rule.
func TestMaskerBoundedWordDoesNotChainIntoAnotherBoundedRule(t *testing.T) {
	m := NewMasker()
	m.RegisterBoundedWord("adam", "user")
	m.RegisterBoundedWord("user", "host")

	got := m.Apply("login adam on host user")
	want := "login user on host host"
	if got != want {
		t.Errorf("got  %q\nwant %q (bounded rules must not chain through each other's output)", got, want)
	}
}

// TestMaskerBoundedWordInsertsReplacementLiterally is the ReplaceAllString
// hazard: Go's regexp treats "$" in the replacement template as an expansion
// operator ("$1", "$USER", "${1}"), not a literal character. A caller-supplied
// replacement like "$USER" is exactly the kind of value an OS environment
// variable convention would hand this function, and it must survive intact.
func TestMaskerBoundedWordInsertsReplacementLiterally(t *testing.T) {
	m := NewMasker()
	m.RegisterBoundedWord("adam", "$USER")

	got := m.Apply("hi adam ok")
	want := "hi $USER ok"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestMaskerHomePrefixDoesNotRewriteASiblingsHome is the missing trailing
// boundary: without one, RegisterHome("/Users/adam", "~") matches the prefix
// of "/Users/adamson" too, corrupting an unrelated sibling account's path and
// leaking the residue of its name. Covered on both platforms' spellings,
// since Windows reports the same hazard with its own separator and prefix.
func TestMaskerHomePrefixDoesNotRewriteASiblingsHome(t *testing.T) {
	unix := NewMasker()
	unix.RegisterHome("/Users/adam", "~")

	unixCases := []struct{ in, want string }{
		{"/Users/adamson/Library", "/Users/adamson/Library"},
		{"/Users/adam/Library", "~/Library"},
	}
	for _, c := range unixCases {
		if got := unix.Apply(c.in); got != c.want {
			t.Errorf("Apply(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	windows := NewMasker()
	windows.RegisterHome(`C:\Users\Adam`, "%USERPROFILE%")

	windowsCases := []struct{ in, want string }{
		{`C:\Users\Adamson\AppData`, `C:\Users\Adamson\AppData`},
		{`C:\Users\Adam\AppData`, `%USERPROFILE%\AppData`},
	}
	for _, c := range windowsCases {
		if got := windows.Apply(c.in); got != c.want {
			t.Errorf("Apply(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMaskerHomePrefixMatchesEndOfLineAndOtherRealBoundaries is the re-review
// finding: RegisterHome's trailing boundary was only "[\\/]" or "$", and Go's
// "$" does not match before a newline without "(?m)". A multi-line report —
// which is exactly what this package builds — puts a home path at the end of
// its own line ("Home: <path>\n...") far more often than it puts one before a
// literal path separator, so the narrow boundary silently let the single most
// common shape through unmasked. The fix widens the trailing boundary to
// whitespace/newline and quote-like punctuation, without going as far as "any
// non-alphanumeric" — that would reopen the sibling-home leak from the other
// side, since a folder name like "adam-work" continues past the home prefix
// with a hyphen, not a separator.
func TestMaskerHomePrefixMatchesEndOfLineAndOtherRealBoundaries(t *testing.T) {
	m := NewMasker()
	m.RegisterHome("/Users/adam", "~")

	cases := []struct{ in, want string }{
		{"Home: /Users/adam\nProfile: x", "Home: ~\nProfile: x"},
		{"home=/Users/adam;", "home=~;"},
		{`"/Users/adam"`, `"~"`},
		{"/Users/adam ", "~ "},
	}
	for _, c := range cases {
		if got := m.Apply(c.in); got != c.want {
			t.Errorf("Apply(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMaskerHomePrefixWidenedBoundaryStillProtectsSiblingHomes pins the
// counterexample the re-reviewer warned against: widening the trailing
// boundary to "any non-alphanumeric" would let "/Users/adam-work/x" mask to
// "~-work/x", leaking the residue of a different folder's name through the
// hyphen. Hyphen must stay outside the trailing boundary class.
func TestMaskerHomePrefixWidenedBoundaryStillProtectsSiblingHomes(t *testing.T) {
	m := NewMasker()
	m.RegisterHome("/Users/adam", "~")

	in := "/Users/adam-work/x"
	if got := m.Apply(in); got != in {
		t.Errorf("Apply(%q) = %q, want unchanged (hyphen must not be a trailing boundary)", in, got)
	}
}

// TestMaskerHomePrefixMasksBothOfTwoAdjacentMentions is the minor finding from
// the same re-review: the trailing separator is part of the match, so it gets
// consumed by the first occurrence and is no longer available as the leading
// character the second, immediately-adjacent occurrence's literal text starts
// with. The bounded-word pass runs its regex twice for exactly this reason;
// the home pass only ran once. Both mentions of the home must be masked, not
// just the first.
func TestMaskerHomePrefixMasksBothOfTwoAdjacentMentions(t *testing.T) {
	m := NewMasker()
	m.RegisterHome("/Users/adam", "~")

	got := m.Apply("/Users/adam/Users/adam/L")
	want := "~~/L"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
