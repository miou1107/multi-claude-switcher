//go:build darwin

package platform

import "testing"

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
