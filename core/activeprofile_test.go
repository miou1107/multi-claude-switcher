package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// withStubbedActiveProfile redirects the record away from the user's real one.
func withStubbedActiveProfile(t *testing.T) {
	t.Helper()
	orig := activeProfilePath
	dir := t.TempDir()
	activeProfilePath = func() string { return filepath.Join(dir, "active.json") }
	t.Cleanup(func() { activeProfilePath = orig })
}

func TestActiveProfileRoundTrip(t *testing.T) {
	withStubbedActiveProfile(t)

	if got := LoadActiveProfile(); got != "" {
		t.Fatalf("nothing recorded yet, got %q", got)
	}
	if err := SaveActiveProfile("Claude_Work"); err != nil {
		t.Fatalf("SaveActiveProfile: %v", err)
	}
	if got := LoadActiveProfile(); got != "Claude_Work" {
		t.Errorf("LoadActiveProfile = %q, want %q", got, "Claude_Work")
	}
	// The last switch wins; this is a record of one thing, not a history.
	if err := SaveActiveProfile("Claude_Personal"); err != nil {
		t.Fatalf("SaveActiveProfile: %v", err)
	}
	if got := LoadActiveProfile(); got != "Claude_Personal" {
		t.Errorf("LoadActiveProfile = %q, want %q", got, "Claude_Personal")
	}
}

// TestLoadActiveProfileSurvivesADamagedRecord: this record is a convenience, not
// data the user cannot lose. A damaged file must read as "unknown" so the caller
// falls back to looking at what is running, never as a hard failure.
func TestLoadActiveProfileSurvivesADamagedRecord(t *testing.T) {
	withStubbedActiveProfile(t)
	if err := os.WriteFile(activeProfilePath(), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := LoadActiveProfile(); got != "" {
		t.Errorf("a damaged record must read as unknown, got %q", got)
	}
}

// TestCurrentProfilePath covers which account MCS believes the user is on, which
// decides the one profile a switch deliberately leaves closed.
func TestCurrentProfilePath(t *testing.T) {
	const (
		work     = "/Users/x/Library/Application Support/Claude"
		personal = "/Users/x/Library/Application Support/Claude_Personal"
		side     = "/Users/x/Library/Application Support/Claude_Side"
	)

	cases := []struct {
		name          string
		running       []string
		lastActivated string
		want          string
	}{
		{
			name:          "the account last switched to, when it is still open",
			running:       []string{personal, work}, // process order puts the other one first
			lastActivated: work,
			want:          work,
		},
		{
			name:          "spelling differences do not lose the record",
			running:       []string{personal, work},
			lastActivated: work + "/",
			want:          work,
		},
		{
			name:          "a stale record is ignored in favour of what is open",
			running:       []string{personal},
			lastActivated: side, // switched to, then closed by hand
			want:          personal,
		},
		{
			name:          "no record yet falls back to what is running",
			running:       []string{personal, work},
			lastActivated: "",
			want:          personal,
		},
		{
			name:          "nothing running",
			running:       nil,
			lastActivated: work,
			want:          "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CurrentProfilePath(tc.running, tc.lastActivated); got != tc.want {
				t.Errorf("CurrentProfilePath(%q, %q) = %q, want %q", tc.running, tc.lastActivated, got, tc.want)
			}
		})
	}
}

// TestSourceProfilePathPrefersTheAccountTheUserWasPutOn is the point of the whole
// record: with two accounts open, the one a switch leaves closed must be the one
// MCS last put the user on, not whichever the process list named first.
func TestSourceProfilePathPrefersTheAccountTheUserWasPutOn(t *testing.T) {
	withStubbedActiveProfile(t)

	const (
		work     = "/Users/x/Library/Application Support/Claude"
		personal = "/Users/x/Library/Application Support/Claude_Personal"
		side     = "/Users/x/Library/Application Support/Claude_Side"
	)
	profiles := []*platform.ProfileInfo{
		{Name: "Claude", Path: work, HasSessionsDir: true},
		{Name: "Claude_Personal", Path: personal, HasSessionsDir: true},
		{Name: "Claude_Side", Path: side, HasSessionsDir: true},
	}
	// Both open, and the process list happens to name Personal first.
	mp := &mockPlatform{running: true, detectedAll: []string{personal, work}}

	if err := SaveActiveProfile("Claude"); err != nil {
		t.Fatal(err)
	}
	if got := SourceProfilePath(mp, side, profiles); got != work {
		t.Errorf("source = %q, want the account last switched to (%q)", got, work)
	}
}

// TestSourceProfilePathIgnoresAStaleRecord: the record names an account that is
// no longer open, so what is running is the better evidence.
func TestSourceProfilePathIgnoresAStaleRecord(t *testing.T) {
	withStubbedActiveProfile(t)

	const (
		work     = "/Users/x/Library/Application Support/Claude"
		personal = "/Users/x/Library/Application Support/Claude_Personal"
		side     = "/Users/x/Library/Application Support/Claude_Side"
	)
	profiles := []*platform.ProfileInfo{
		{Name: "Claude", Path: work, HasSessionsDir: true},
		{Name: "Claude_Personal", Path: personal, HasSessionsDir: true},
	}
	mp := &mockPlatform{running: true, detectedAll: []string{personal}}

	if err := SaveActiveProfile("Claude"); err != nil { // closed by hand since
		t.Fatal(err)
	}
	if got := SourceProfilePath(mp, side, profiles); got != personal {
		t.Errorf("source = %q, want the account that is actually open (%q)", got, personal)
	}
}

// TestSourceProfilePathFallsBackWhenNothingIsRunning keeps the old behaviour: with
// no evidence at all, the first other profile holding sessions is the source.
func TestSourceProfilePathFallsBackWhenNothingIsRunning(t *testing.T) {
	withStubbedActiveProfile(t)

	const (
		work     = "/Users/x/Library/Application Support/Claude"
		personal = "/Users/x/Library/Application Support/Claude_Personal"
	)
	profiles := []*platform.ProfileInfo{
		{Name: "Claude", Path: work, HasSessionsDir: true},
		{Name: "Claude_Personal", Path: personal, HasSessionsDir: true},
	}
	mp := &mockPlatform{running: false}

	if got := SourceProfilePath(mp, personal, profiles); got != work {
		t.Errorf("source = %q, want %q", got, work)
	}
}

// TestSourceProfilePathPrefersARunningProfileOverAnIdleOne: re-switching to the
// account you are already on, with a second account open. The target cannot be
// its own source, and the answer must still be something that is RUNNING — with
// auto sync on, the source's sessions flow into the target, so naming an idle
// account merges history the user never had open.
func TestSourceProfilePathPrefersARunningProfileOverAnIdleOne(t *testing.T) {
	withStubbedActiveProfile(t)

	const (
		work     = "/Users/x/Library/Application Support/Claude"
		personal = "/Users/x/Library/Application Support/Claude_Personal"
		idle     = "/Users/x/Library/Application Support/Claude_Idle"
	)
	profiles := []*platform.ProfileInfo{
		// Listed first, so the static fallback would pick it.
		{Name: "Claude_Idle", Path: idle, HasSessionsDir: true},
		{Name: "Claude", Path: work, HasSessionsDir: true},
		{Name: "Claude_Personal", Path: personal, HasSessionsDir: true},
	}
	mp := &mockPlatform{running: true, detectedAll: []string{work, personal}}
	if err := SaveActiveProfile("Claude"); err != nil {
		t.Fatal(err)
	}

	if got := SourceProfilePath(mp, work, profiles); got != personal {
		t.Errorf("source = %q, want the other RUNNING account (%q)", got, personal)
	}
}

// TestSourceProfilePathRejectsTheTargetSpeltDifferently: the target arrives as the
// user typed it and the running profile as the platform reports it. Returning the
// target as its own source would sync a profile with itself.
func TestSourceProfilePathRejectsTheTargetSpeltDifferently(t *testing.T) {
	withStubbedActiveProfile(t)

	const (
		work = "/Users/x/Library/Application Support/Claude"
		idle = "/Users/x/Library/Application Support/Claude_Idle"
	)
	profiles := []*platform.ProfileInfo{
		{Name: "Claude", Path: work, HasSessionsDir: true},
		{Name: "Claude_Idle", Path: idle, HasSessionsDir: true},
	}
	mp := &mockPlatform{running: true, detectedAll: []string{work}}

	if got := SourceProfilePath(mp, work+"/", profiles); got != idle {
		t.Errorf("source = %q, want %q — the target is not its own source", got, idle)
	}
}

// TestSourceProfilePathFallsBackToTheDataRoot covers the last rung: no profiles at
// all to choose from.
func TestSourceProfilePathFallsBackToTheDataRoot(t *testing.T) {
	withStubbedActiveProfile(t)
	mp := &mockPlatform{running: false, appSupport: "/Users/x/Library/Application Support"}

	want := "/Users/x/Library/Application Support/Claude"
	if got := SourceProfilePath(mp, "/somewhere/Dst", nil); got != want {
		t.Errorf("source = %q, want %q", got, want)
	}
}

// TestSourceProfilePathIgnoresARecordForAProfileThatIsGone: an archived or deleted
// account can still be named by the record, and it must resolve to nothing rather
// than to some other profile.
func TestSourceProfilePathIgnoresARecordForAProfileThatIsGone(t *testing.T) {
	withStubbedActiveProfile(t)

	const (
		work     = "/Users/x/Library/Application Support/Claude"
		personal = "/Users/x/Library/Application Support/Claude_Personal"
	)
	profiles := []*platform.ProfileInfo{
		{Name: "Claude", Path: work, HasSessionsDir: true},
		{Name: "Claude_Personal", Path: personal, HasSessionsDir: true},
	}
	mp := &mockPlatform{running: true, detectedAll: []string{personal, work}}
	if err := SaveActiveProfile("Claude_Archived"); err != nil {
		t.Fatal(err)
	}

	if got := SourceProfilePath(mp, "/somewhere/Dst", profiles); got != personal {
		t.Errorf("source = %q, want the first running profile (%q)", got, personal)
	}
}
