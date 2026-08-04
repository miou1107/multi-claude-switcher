package diagnostics

import (
	"net/url"
	"strings"
)

// issueBase is where a problem report goes. A public repository, which is why
// the confirm step says so in those words.
const issueBase = "https://github.com/miou1107/multi-claude-switcher/issues/new"

// maxTitleRunes keeps the title readable in a list and the URL well short of the
// roughly 8 KB a prefilled issue link tolerates.
const maxTitleRunes = 80

// issueBody is all the URL carries. The report itself goes by clipboard: 200
// log lines do not fit in a link, and truncating them to fit would ship a report
// that is missing exactly the part someone asked for.
const issueBody = "Paste the report here (Cmd+V / Ctrl+V).\n"

// IssueURL builds the prefilled new-issue link.
//
// The title is masked before anything else: a user describing their problem
// tends to paste the error they saw, and the error they saw has their path in
// it. It is then flattened to one line, capped, and escaped — a comment
// containing & or # must not be able to truncate the URL or reach a shell.
//
// Only NewMaskerFor produces the masker today, so m is never nil in practice.
// But IssueURL is exported and its signature promises nothing about that, so a
// nil m is guarded here rather than left to panic inside Masker.Apply. The
// guard drops the comment entirely instead of falling back to it verbatim:
// with no masker there is no way to know the comment is safe to publish, and
// a quietly unmasked title is worse than the generic fallback title below.
func IssueURL(comment string, m *Masker) string {
	title := ""
	if m != nil {
		title = strings.TrimSpace(m.Apply(comment))
	}
	if i := strings.IndexAny(title, "\r\n"); i >= 0 {
		title = strings.TrimSpace(title[:i])
	}
	if title == "" {
		title = "Problem report"
	}
	if r := []rune(title); len(r) > maxTitleRunes {
		title = strings.TrimSpace(string(r[:maxTitleRunes]))
	}
	q := url.Values{}
	q.Set("title", title)
	q.Set("body", issueBody)
	return issueBase + "?" + q.Encode()
}
