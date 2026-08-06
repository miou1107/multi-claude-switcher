package diagnostics

import (
	"strings"
	"testing"
)

// TestSweepCatchesWhatRegistrationMissed is the backstop's whole point: a value
// nobody registered still must not reach a public issue.
func TestSweepCatchesWhatRegistrationMissed(t *testing.T) {
	cases := []struct{ name, in string }{
		{"an address in a log line", "2026/08/04 10:50 signed in as stranger@example.com"},
		{"a bare uuid", "bucket 6c7b2c78-0d0a-4ab6-bffa-e9e6fe671d61 has 12 files"},
		{"a uuid inside a path", "open ~/sessions/6c7b2c78-0d0a-4ab6-bffa-e9e6fe671d61/x.json"},
		{"an uppercase uuid", "ORG 6C7B2C78-0D0A-4AB6-BFFA-E9E6FE671D61"},
		// Go's \b treats "_" as a word character, so it never fires between "_"
		// and a hex digit. local_<uuid>.json is the real filename shape every
		// Claude Code session file on this repo's disk uses (see core/*_test.go
		// writeSessionFile calls), and that path shows up in sync/conflict/skip
		// log lines a diagnostics report carries verbatim.
		{"a uuid preceded by an underscore, the real session filename shape", "synced local_6c7b2c78-0d0a-4ab6-bffa-e9e6fe671d61.json"},
		{"a uuid flanked by alphanumeric characters on both sides", "id=x6c7b2c78-0d0a-4ab6-bffa-e9e6fe671d61x done"},
		// An unhyphenated 32-hex-digit run is not something a report has any
		// other reason to contain, so it must be caught even though this app
		// only ever writes canonically hyphenated UUIDs.
		{"an unhyphenated uuid", "raw id 6c7b2c780d0a4ab6bffae9e6fe671d61 seen"},
	}
	for _, c := range cases {
		got := Sweep(c.in)
		if !strings.Contains(got, UnregisteredMarker) {
			t.Errorf("%s: Sweep(%q) = %q, want it redacted", c.name, c.in, got)
		}
		if strings.Contains(got, "stranger@example.com") ||
			strings.Contains(strings.ToLower(got), "6c7b2c78-0d0a-4ab6-bffa-e9e6fe671d61") ||
			strings.Contains(strings.ToLower(got), "6c7b2c780d0a4ab6bffae9e6fe671d61") {
			t.Errorf("%s: the value survived: %q", c.name, got)
		}
	}
}

// TestSweepFreeTextRedactsWithoutClaimingADefect is the split the first real
// Windows session forced. Sweep's marker names a defect: a field the gatherer
// forgot to register. On a machine whose logs mention session filenames the
// same marker fired 27 times over 9 lines for session IDs, which belong to no
// field and never will — so the check "the report must not contain the marker"
// could never pass there again, and a check that is permanently red stops
// being read. Free text gets a marker that says only what happened.
func TestSweepFreeTextRedactsWithoutClaimingADefect(t *testing.T) {
	in := "[Safe Switch] skipped local_6c7b2c78-0d0a-4ab6-bffa-e9e6fe671d61.json: rename"
	got := SweepFreeText(in)

	if strings.Contains(strings.ToLower(got), "6c7b2c78") {
		t.Errorf("the identifier survived: %q", got)
	}
	if !strings.Contains(got, RedactedMarker) {
		t.Errorf("SweepFreeText(%q) = %q, want it to carry %q", in, got, RedactedMarker)
	}
	if strings.Contains(got, UnregisteredMarker) {
		t.Errorf("an identifier in free text is not an unregistered field, so it must not claim to be one: %q", got)
	}
}

// TestSweepDoesNotOverreachOnBareHex guards the false-positive side of the
// unhyphenated-uuid rule: a 32-hex-digit run with hex-character neighbours on
// either side is a longer, unrelated hex string, not a standalone identifier,
// and must not be redacted.
func TestSweepDoesNotOverreachOnBareHex(t *testing.T) {
	in := "checksum a6c7b2c780d0a4ab6bffae9e6fe671d61b ok"
	got := Sweep(in)
	if got != in {
		t.Errorf("Sweep redacted a hex run that was not a standalone uuid: %q -> %q", in, got)
	}
}

// TestSweepLeavesTheReportAlone guards against the backstop eating the report.
// Pseudonyms, versions, paths and counts must all survive it.
func TestSweepLeavesTheReportAlone(t *testing.T) {
	in := `MCS 0.11.2 · macOS 15.5 · arm64
Claude Desktop 1.24012.11 · standalone
claude-code 2.1.219
  Claude_Profile2 — account-2
    org-B · 95 convos
2026/08/04 10:50:12 [Safe Switch] ~/…/Claude to ~/…/Claude_test`
	if got := Sweep(in); got != in {
		t.Errorf("sweep changed a clean report:\n%q", got)
	}
}
