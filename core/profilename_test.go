package core

import "testing"

func TestValidateProfileName(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantClean string // ignored when wantErr
		wantErr   bool
	}{
		{"plain", "Personal", "Personal", false},
		{"with space", "Work Team", "Work Team", false},
		{"with dash and underscore", "work-2_b", "work-2_b", false},
		{"digits", "Acct2", "Acct2", false},
		{"trims to valid", "  Personal  ", "Personal", false},
		{"empty", "", "", true},
		{"whitespace only", "   ", "", true},
		{"forward slash", "a/b", "", true},
		{"backslash", `a\b`, "", true},
		{"dot dot", "..", "", true},
		{"leading dot", ".hidden", "", true},
		{"colon", "a:b", "", true},
		{"asterisk", "a*b", "", true},
		{"question mark", "a?b", "", true},
		{"quote", `a"b`, "", true},
		{"angle brackets", "a<b>c", "", true},
		{"pipe", "a|b", "", true},
		{"newline", "a\nb", "", true},
		{"reserved bare Claude", "Claude", "", true},
		{"reserved, untrimmed", "  claude ", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clean, err := ValidateProfileName(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ValidateProfileName(%q) = %q, nil; want an error", c.in, clean)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateProfileName(%q) = %v, want nil", c.in, err)
			}
			// The cleaned name is what becomes the identity and every registry
			// key, so it has to come back from here rather than be re-derived.
			if clean != c.wantClean {
				t.Fatalf("ValidateProfileName(%q) cleaned to %q, want %q", c.in, clean, c.wantClean)
			}
		})
	}
}

func TestProfileFolderName(t *testing.T) {
	clean, err := ValidateProfileName("  Work Team  ")
	if err != nil {
		t.Fatal(err)
	}
	if got := ProfileFolderName(clean); got != "Claude_Work Team" {
		t.Fatalf("got %q", got)
	}
}
