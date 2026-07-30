package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSyncResultMessage(t *testing.T) {
	cases := []struct {
		name string
		rep  *SyncReport
		want string
	}{
		{
			name: "nothing to do",
			rep:  &SyncReport{},
			want: "✓ Copied 0 conversations into Work.",
		},
		{
			name: "one copied reads as singular",
			rep:  &SyncReport{CopiedCount: 1},
			want: "✓ Copied 1 conversation into Work.",
		},
		{
			name: "several copied",
			rep:  &SyncReport{CopiedCount: 12},
			want: "✓ Copied 12 conversations into Work.",
		},
		{
			// A sync that copied little because the target was already ahead would
			// otherwise read as a sync that did nothing.
			name: "one already-newer reads as singular throughout",
			rep:  &SyncReport{CopiedCount: 0, ConflictCount: 1},
			want: "✓ Copied 0 conversations into Work. 1 conversation was already newer here and left alone.",
		},
		{
			name: "several already-newer",
			rep:  &SyncReport{CopiedCount: 3, ConflictCount: 16},
			want: "✓ Copied 3 conversations into Work. 16 conversations were already newer here and left alone.",
		},
		{
			// The walk continues past a file it cannot read, so this count is the
			// only thing that would ever mention it.
			name: "one skipped file",
			rep:  &SyncReport{CopiedCount: 2, SkipErrors: []string{"a.json: boom"}},
			want: "✓ Copied 2 conversations into Work. 1 file could not be read and was skipped (see the log).",
		},
		{
			name: "clashes and skips together",
			rep:  &SyncReport{CopiedCount: 1, ConflictCount: 2, SkipErrors: []string{"a: x", "b: y"}},
			want: "✓ Copied 1 conversation into Work. 2 conversations were already newer here and left alone. 2 files could not be read and were skipped (see the log).",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SyncResultMessage(c.rep, "Work"); got != c.want {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestSyncResultMessageNilReport(t *testing.T) {
	// A nil report must not panic: the hosts pass whatever ManualAlign returned,
	// and an error path can return a nil report alongside a nil error only by
	// mistake — crashing the panel is a worse response than a bland sentence.
	if got := SyncResultMessage(nil, "Work"); got == "" || strings.Contains(got, "%!") {
		t.Errorf("got %q", got)
	}
}

func TestSyncFailureMessageTranslatesTheUnknownProfileAbort(t *testing.T) {
	// Anyone who opened Claude Desktop themselves rather than through MCS lands
	// here, because profile detection matches the --user-data-dir argument MCS
	// passes. They need an action, not the reason.
	got := SyncFailureMessage(fmt.Errorf("%w: ps output had no match", ErrRunningProfileUnknown))
	if got != "Quit Claude Desktop first, then try Sync again." {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "--user-data-dir") || strings.Contains(got, "ps output") {
		t.Errorf("the underlying reason must not reach the user: %q", got)
	}
}

func TestSyncFailureMessagePassesOtherErrorsThrough(t *testing.T) {
	// A real fault must never be swallowed by a friendly message.
	got := SyncFailureMessage(errors.New("disk full"))
	if !strings.Contains(got, "disk full") {
		t.Errorf("got %q, want the underlying error preserved", got)
	}
}

func TestSyncFailureMessageNil(t *testing.T) {
	if got := SyncFailureMessage(nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
