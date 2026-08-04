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
