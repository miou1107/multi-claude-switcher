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
func IssueURL(comment string, m *Masker) string {
	title := strings.TrimSpace(m.Apply(comment))
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
