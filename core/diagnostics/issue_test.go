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
		if len(u) > 8000 {
			t.Errorf("url is %d bytes, want under 8000", len(u))
		}
		if n := len([]rune(titleOf(t, u))); n > 80 {
			t.Errorf("title is %d runes, want at most 80", n)
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

func titleOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u.Query().Get("title")
}
