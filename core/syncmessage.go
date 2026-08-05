package core

import (
	"errors"
	"fmt"
)

// SyncResultMessage is the sentence the panel shows after a sync.
//
// It lives here, not in each host, because both the macOS menu bar and the
// Windows tray show it and they must not word it differently. They share a
// renderer for the same reason. Keeping it in core also makes it testable: the
// cmd packages have no tests, and this string is the only place a clash is ever
// reported to a user of the panel.
func SyncResultMessage(rep *SyncReport, targetDisplay string) string {
	summary, skipped := SyncResultParts(rep, targetDisplay)
	if skipped == "" {
		return summary
	}
	return summary + " " + skipped
}

// SyncResultParts splits that sentence in two: what the sync did, and what it
// could not read.
//
// The panel needs them apart. Files that could not be read are a warning, and
// the progress card puts a warning in its own box and then waits to be closed,
// rather than clearing itself after two seconds like a clean result does. Both
// halves still come from here so the card and the one-line message cannot end
// up wording the same fact differently.
func SyncResultParts(rep *SyncReport, targetDisplay string) (summary, skipped string) {
	if rep == nil {
		return "Sync finished.", ""
	}
	summary = fmt.Sprintf("✓ Copied %s into %s.", pluralConversations(rep.CopiedCount), targetDisplay)
	if rep.ConflictCount > 0 {
		// The target already had a newer version of these, so they were left
		// alone. Worth saying, because otherwise a sync that copied little looks
		// like a sync that did nothing.
		tail := "was already newer here and left alone."
		if rep.ConflictCount != 1 {
			tail = "were already newer here and left alone."
		}
		summary += " " + pluralConversations(rep.ConflictCount) + " " + tail
	}
	if n := len(rep.SkipErrors); n > 0 {
		// These could not be read or written at all. A count with a pointer to the
		// log beats silence: the run continued past them on purpose, so nothing
		// else would ever mention them.
		tail := "file could not be read and was skipped (see the log)."
		if n != 1 {
			tail = "files could not be read and were skipped (see the log)."
		}
		skipped = fmt.Sprintf("%d %s", n, tail)
	}
	return summary, skipped
}

// pluralConversations renders a count with its noun, so "1 conversation" never
// comes out as "1 conversations".
func pluralConversations(n int) string {
	if n == 1 {
		return "1 conversation"
	}
	return fmt.Sprintf("%d conversations", n)
}

// SyncFailureMessage turns a sync error into something a non-technical user can
// act on. Only the cases a user can actually do something about are translated;
// anything else is passed through so a real fault is never hidden.
func SyncFailureMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrRunningProfileUnknown) {
		// Reached by anyone who launched Claude Desktop themselves instead of
		// through MCS. The underlying reason (no --user-data-dir to match on) is
		// not something to put in front of a user; the action is.
		return "Quit Claude Desktop first, then try Sync again."
	}
	return "Sync failed: " + err.Error()
}
