package platform

import (
	"path/filepath"
	"runtime"
	"testing"
)

// TestSamePath pins the spellings the same profile directory actually arrives in:
// the platform reports a canonical path, a caller passes what the user typed.
func TestSamePath(t *testing.T) {
	base := filepath.Join("Users", "x", "Library", "Application Support", "Claude")

	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", base, base, true},
		{"trailing separator", base, base + string(filepath.Separator), true},
		{"dot segment", base, filepath.Join(base, "."), true},
		{"different profiles", base, base + "_Profile2", false},
		{"one empty", base, "", false},
		{"both empty", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SamePath(tc.a, tc.b); got != tc.want {
				t.Errorf("SamePath(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestSamePathCasing asserts the host's own rule rather than one fixed answer:
// Windows spells the same directory in whatever case it likes, and elsewhere two
// directories differing only in case are two directories.
func TestSamePathCasing(t *testing.T) {
	lower := filepath.Join("users", "x", "claude")
	upper := filepath.Join("Users", "X", "Claude")

	want := runtime.GOOS == "windows"
	if got := SamePath(lower, upper); got != want {
		t.Errorf("SamePath(%q, %q) = %v, want %v on %s", lower, upper, got, want, runtime.GOOS)
	}
}
