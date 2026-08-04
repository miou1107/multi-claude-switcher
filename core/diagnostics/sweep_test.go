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
	}
	for _, c := range cases {
		got := Sweep(c.in)
		if !strings.Contains(got, UnregisteredMarker) {
			t.Errorf("%s: Sweep(%q) = %q, want it redacted", c.name, c.in, got)
		}
		if strings.Contains(got, "stranger@example.com") || strings.Contains(strings.ToLower(got), "6c7b2c78-0d0a-4ab6-bffa-e9e6fe671d61") {
			t.Errorf("%s: the value survived: %q", c.name, got)
		}
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
