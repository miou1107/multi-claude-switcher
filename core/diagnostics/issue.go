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
//
// The placeholder line sits inside a fenced code block on purpose. GitHub
// renders an issue body as Markdown, and the report is multi-line, indented,
// `·`-separated and ends in up to 200 log lines; pasted into plain prose that
// reflows every line, drops the indentation and reads underscores as
// emphasis markers. A fence tells GitHub (and every renderer downstream —
// notifications, the mobile app, search) to print the report exactly as it
// was copied. The instruction asks the user to replace the placeholder line
// rather than paste after it, so the fence stays intact around whatever they
// paste over it.
const issueBody = "Paste the report here (Cmd+V / Ctrl+V), replacing the line below:\n\n```\nPASTE THE REPORT HERE\n```\n"

// IssueURL builds the prefilled new-issue link.
//
// The title is masked before anything else: a user describing their problem
// tends to paste the error they saw, and the error they saw has their path in
// it. It is then flattened to one line, capped, and escaped — a comment
// containing & or # must not be able to truncate the URL or reach a shell.
//
// NewMaskerFor is the only thing that produces the masker in the hosts today,
// but the debug report cache (see debugReportCache in cmd/mcs-menubar and its
// Windows twin) makes a nil m reachable in practice: the cache starts empty,
// and reportProblem is not gated on the debug view actually being on screen —
// only on the gather having completed at least once. Before that first
// gather, or if reportProblem is ever reached without one, m is nil. IssueURL
// is exported besides, and its signature promises nothing about m being
// non-nil, so a nil m is guarded here rather than left to panic inside
// Masker.Apply. The guard drops the comment entirely instead of falling back
// to it verbatim: with no
// masker there is no way to know the comment is safe to publish, and a
// quietly unmasked title is worse than the generic fallback title below.
func IssueURL(comment string, m *Masker) string {
	title := ""
	if m != nil {
		// Apply masks what was registered; Sweep catches what was not — a
		// foreign email or a bare UUID a user pastes in, which no registration
		// in this masker ever knew about. Without this the title carried
		// exactly the class of leak the sweep exists for, worse than the
		// clipboard body because a title is indexed and mailed to watchers.
		// SweepFreeText, not Sweep: a comment is typed, not assembled from
		// registered fields, so an identifier in it says nothing about this
		// report's own registration — and this string becomes the title of a
		// public issue, where "unregistered" would read as a claim about the
		// app the issue is being filed against.
		title = strings.TrimSpace(SweepFreeText(m.Apply(comment)))
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
// with the same masker that produced report and then swept as free text.
//
// Exported so both hosts go through one implementation rather than each
// remembering the two steps separately. Build already sweeps, but it sweeps
// internally and returns; a caller appending the comment to that returned
// string afterwards puts the comment outside Build's sweep entirely — the gap
// this closes. m.Apply alone only masks what was registered (this machine's
// own accounts, home, user and host name); SweepFreeText is what catches an
// address or a UUID belonging to someone else that a user's own pasted comment
// can carry, and which no registration here could ever have known about. Free
// text rather than Sweep: what a user types is not assembled from this
// report's fields, so a hit there says nothing about whether one was missed.
//
// A nil m is guarded the same way IssueURL guards it, and for the same
// reason: the debug report cache makes nil reachable here (see the doc
// comment on IssueURL), and with no masker there is no way to know the
// comment is safe to publish. The guard drops the comment rather than publish
// it unmasked; the report itself is returned unchanged since it was already
// masked and swept by Build before it reached the cache.
func AppendComment(report, comment string, m *Masker) string {
	if comment == "" || m == nil {
		return report
	}
	return report + "\n---\n" + SweepFreeText(m.Apply(comment)) + "\n"
}
