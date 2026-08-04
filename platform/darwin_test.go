//go:build darwin

package platform

import (
	"path/filepath"
	"testing"
)

func TestMatchProfileInProcs(t *testing.T) {
	// Real ps output space-joins args without quoting; the default profile path
	// contains spaces and is followed by more flags.
	claude := "/Users/x/Library/Application Support/Claude"
	profile2 := "/Users/x/Library/Application Support/Claude_Profile2"

	cases := []struct {
		name  string
		procs []string
		paths []string
		want  string
	}{
		{
			name:  "path with spaces followed by more args",
			procs: []string{"501 123 Claude.app --user-data-dir=" + claude + " --standard-schemes=app --lang=zh-TW"},
			paths: []string{claude, profile2},
			want:  claude,
		},
		{
			name:  "path at end of line (no trailing space)",
			procs: []string{"501 123 Claude.app --user-data-dir=" + claude},
			paths: []string{claude, profile2},
			want:  claude,
		},
		{
			name:  "Claude must not match Claude_Profile2",
			procs: []string{"501 9 Claude.app --user-data-dir=" + profile2 + " --lang=en"},
			paths: []string{claude, profile2}, // claude listed first on purpose
			want:  profile2,
		},
		{
			name:  "no user-data-dir",
			procs: []string{"501 5 Claude.app --lang=en"},
			paths: []string{claude},
			want:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchProfileInProcs(tc.procs, tc.paths); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRunningProfilesInProcs covers what matchProfileInProcs cannot: which
// profiles are running when more than one is, and the default profile, which
// Claude Desktop runs with no --user-data-dir at all whenever it was opened by
// anything other than MCS (the Dock, Spotlight, a login item, its own updater).
//
// The process lines are the shape real `ps aux` output has on this platform:
// one main process per profile plus a crowd of Electron helpers, some of which
// repeat --user-data-dir and some of which carry no path at all.
func TestRunningProfilesInProcs(t *testing.T) {
	const (
		claude   = "/Users/x/Library/Application Support/Claude"
		profile2 = "/Users/x/Library/Application Support/Claude_Profile2"

		mainProc   = "/Applications/Claude.app/Contents/MacOS/Claude"
		helperProc = "/Applications/Claude.app/Contents/Frameworks/Claude Helper.app/Contents/MacOS/Claude Helper"
		crashpad   = "/Applications/Claude.app/Contents/Frameworks/Electron Framework.framework/Helpers/chrome_crashpad_handler --no-rate-limit"
	)
	paths := []string{claude, profile2}

	cases := []struct {
		name  string
		procs []string
		want  []string
	}{
		{
			name:  "default profile runs without --user-data-dir",
			procs: []string{"501 41981 " + mainProc},
			want:  []string{claude},
		},
		{
			name: "both profiles running are both reported",
			procs: []string{
				"501 42727 " + mainProc + " --user-data-dir=" + profile2,
				"501 42735 " + helperProc + " --type=gpu-process --user-data-dir=" + profile2 + " --gpu-preferences=x",
				"501 56075 " + mainProc + " --user-data-dir=" + claude,
				"501 56080 " + helperProc + " --type=gpu-process --user-data-dir=" + claude + " --gpu-preferences=x",
			},
			want: []string{profile2, claude},
		},
		{
			name: "the default profile is seen alongside a flagged one",
			procs: []string{
				"501 41981 " + mainProc,
				"501 42727 " + mainProc + " --user-data-dir=" + profile2,
			},
			want: []string{claude, profile2},
		},
		{
			name: "helpers without a path do not count as the default profile",
			procs: []string{
				"501 42727 " + mainProc + " --user-data-dir=" + profile2,
				"501 42737 " + helperProc + " --type=utility --utility-sub-type=network.mojom.NetworkService",
				"501 42734 " + crashpad,
			},
			want: []string{profile2},
		},
		{
			name:  "nothing running",
			procs: nil,
			want:  nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runningProfilesInProcs(tc.procs, paths, claude)
			if len(got) != len(tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %q, want %q", got, tc.want)
				}
			}
		})
	}
}

// TestPrepareRemoveResolvesUnderAppSupport pins PrepareRemove's resolution to
// the same directory FindProfiles scans, so a removal always archives the
// directory the user actually sees listed.
func TestPrepareRemoveResolvesUnderAppSupport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	d := &DarwinPlatform{}
	got, err := d.PrepareRemove("Claude_Work")
	if err != nil {
		t.Fatalf("PrepareRemove: %v", err)
	}
	want := filepath.Join(home, "Library", "Application Support", "Claude_Work")
	if got != want {
		t.Fatalf("PrepareRemove = %q, want %q", got, want)
	}
}
