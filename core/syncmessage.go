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
	if rep == nil {
		return "Sync finished."
	}
	msg := fmt.Sprintf("✓ Copied %s into %s.", pluralConversations(rep.CopiedCount), targetDisplay)
	if rep.ConflictCount > 0 {
		// Sync never replaces a conversation the target already has, so a clash
		// has to be said out loud. Silence would read as "nothing needed doing"
		// when the truth is "some conversations differ on both sides and both
		// copies were kept".
		tail := "differed on both sides and was left as it is."
		if rep.ConflictCount != 1 {
			tail = "differed on both sides and were left as they are."
		}
		msg += " " + pluralConversations(rep.ConflictCount) + " " + tail
	}
	return msg
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
