package diagnostics

import (
	"net/url"
	"strings"
	"testing"
)

func TestIssueURL(t *testing.T) {
	m := NewMasker()
	m.RegisterAccount("", "vincent@fontrip.com")

	t.Run("the first line of the comment becomes the title", func(t *testing.T) {
		u := IssueURL("Switching closed my other account\nIt happened twice", m)
		if got := titleOf(t, u); got != "Switching closed my other account" {
			t.Errorf("title = %q", got)
		}
	})

	t.Run("an empty comment still has a title", func(t *testing.T) {
		if got := titleOf(t, IssueURL("   \n  ", m)); got != "Problem report" {
			t.Errorf("title = %q, want Problem report", got)
		}
	})

	t.Run("the title is masked", func(t *testing.T) {
		u := IssueURL("fails for vincent@fontrip.com", m)
		if strings.Contains(u, "fontrip") {
			t.Errorf("an address reached the url: %s", u)
		}
		if got := titleOf(t, u); got != "fails for account-1" {
			t.Errorf("title = %q", got)
		}
	})

	t.Run("a long comment cannot run away with the url", func(t *testing.T) {
		u := IssueURL(strings.Repeat("very long ", 500), m)
		// Measured worst case with the 80-rune cap in place: an 80-rune title
		// made entirely of 4-byte UTF-8 characters encodes to about 1085 bytes
		// for the whole url. 2000 leaves headroom above that measured worst
		// case while staying far under what this subtest's own input encodes
		// to (about 5124 bytes) if the truncation were removed — so this
		// assertion actually fails without it, unlike a bound like 8000 that
		// neither the truncated nor the untruncated case ever reaches.
		if len(u) > 2000 {
			t.Errorf("url is %d bytes, want under 2000", len(u))
		}
		if n := len([]rune(titleOf(t, u))); n > 80 {
			t.Errorf("title is %d runes, want at most 80", n)
		}
	})

	t.Run("a nil masker does not panic and does not leak the comment", func(t *testing.T) {
		u := IssueURL("hello vincent@fontrip.com", nil)
		if got := titleOf(t, u); got != "Problem report" {
			t.Errorf("title = %q, want Problem report", got)
		}
		if strings.Contains(u, "hello") || strings.Contains(u, "fontrip") {
			t.Errorf("unmasked comment reached the url: %s", u)
		}
	})

	t.Run("an unregistered address and uuid are swept from the title", func(t *testing.T) {
		// Round-2 finding 1: IssueURL only ran the comment through m.Apply, which
		// masks registered values, never through Sweep, which catches what
		// registration missed. A foreign email or a bare UUID in the comment
		// reached the title verbatim, unmasked, and titles are indexed and
		// mailed to watchers, worse than the clipboard body.
		u := IssueURL("crashed for someone@example.com session 11112222-3333-4444-5555-666677778888", m)
		got := titleOf(t, u)
		if strings.Contains(got, "someone@example.com") || strings.Contains(got, "11112222-3333-4444-5555-666677778888") {
			t.Errorf("an unregistered identifier reached the title: %q", got)
		}
		if !strings.Contains(got, UnregisteredMarker) {
			t.Errorf("the title should carry the sweep marker: %q", got)
		}
	})

	t.Run("punctuation cannot break the url", func(t *testing.T) {
		u := IssueURL(`sync & switch: "why?" #3`, m)
		parsed, err := url.Parse(u)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if got := parsed.Query().Get("title"); got != `sync & switch: "why?" #3` {
			t.Errorf("title round-tripped as %q", got)
		}
	})
}

// TestAppendCommentNilMasker is a round-3 finding: IssueURL guards m == nil
// but AppendComment did not, even though both are exported and the cache
// added in the previous round makes nil reachable in practice — the cache
// starts empty, and reportProblem is not gated on the debug view actually
// being on screen, only on the gather having completed at least once, so a
// Report-a-problem call reached before that first gather calls AppendComment
// with a nil masker. With no masker there is no way to know the comment is
// safe to publish, so the guard has to drop it, exactly like IssueURL does,
// rather than publish it unmasked or panic.
func TestAppendCommentNilMasker(t *testing.T) {
	got := AppendComment("MCS report body", "hello vincent@fontrip.com", nil)
	if got != "MCS report body" {
		t.Errorf("AppendComment(nil masker) = %q, want the report unchanged", got)
	}
}

// TestIssueBodyIsAFencedCodeBlock is the finding-4 fix. GitHub renders an
// issue body as Markdown, and the report is multi-line, `·`-separated,
// indented and can end in up to 200 log lines — pasted into plain prose that
// gets reflowed into paragraphs, with underscores read as emphasis markers,
// every report this feature produces arrived unreadable. The body carried in
// the prefilled URL must open a fence for the user to paste inside, so
// GitHub renders whatever they paste verbatim.
func TestIssueBodyIsAFencedCodeBlock(t *testing.T) {
	if !strings.Contains(issueBody, "```") {
		t.Fatalf("the issue body must contain a fenced code block:\n%s", issueBody)
	}
	if strings.Count(issueBody, "```") != 2 {
		t.Fatalf("the fence must be opened and closed exactly once:\n%s", issueBody)
	}
}

func titleOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u.Query().Get("title")
}
