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
		// Apply masks what was registered; Sweep catches what was not — a
		// foreign email or a bare UUID a user pastes in, which no registration
		// in this masker ever knew about. Without this the title carried
		// exactly the class of leak the sweep exists for, worse than the
		// clipboard body because a title is indexed and mailed to watchers.
		title = strings.TrimSpace(Sweep(m.Apply(comment)))
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

// AppendComment adds the user's comment to the report clipboard body, masked
// with the same masker that produced report and then swept.
//
// Exported so both hosts go through one implementation rather than each
// remembering the two steps separately. Build already sweeps, but it sweeps
// internally and returns; a caller appending the comment to that returned
// string afterwards puts the comment outside Build's sweep entirely — the gap
// this closes. m.Apply alone only masks what was registered (this machine's
// own accounts, home, user and host name); Sweep is what catches an address or
// a UUID belonging to someone else that a user's own pasted comment can carry,
// and which no registration here could ever have known about.
func AppendComment(report, comment string, m *Masker) string {
	if comment == "" {
		return report
	}
	return report + "\n---\n" + Sweep(m.Apply(comment)) + "\n"
}
