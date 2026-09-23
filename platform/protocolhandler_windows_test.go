//go:build windows

package platform

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The registry write itself is not unit-testable without touching the machine,
// so the parsing and rendering that decide WHAT gets written are kept pure and
// covered here. Getting these wrong writes a broken command line into the
// handler that opens sign-in links, which is the worst failure this file has.

func TestExeFromProtocolCommand(t *testing.T) {
	tests := []struct {
		name   string
		cmd    string
		want   string
		wantOK bool
	}{
		{
			name:   "the form Claude Desktop registers",
			cmd:    `"C:\Users\Example\AppData\Local\AnthropicClaude\app-1.24012.9\claude.exe" "%1"`,
			want:   `C:\Users\Example\AppData\Local\AnthropicClaude\app-1.24012.9\claude.exe`,
			wantOK: true,
		},
		{
			name:   "already carrying a profile",
			cmd:    `"C:\App\claude.exe" --user-data-dir="C:\Users\Example\AppData\Roaming\ClaudeWork" "%1"`,
			want:   `C:\App\claude.exe`,
			wantOK: true,
		},
		{
			name:   "unquoted, no spaces in the path",
			cmd:    `C:\App\claude.exe "%1"`,
			want:   `C:\App\claude.exe`,
			wantOK: true,
		},
		{"empty", "", "", false},
		{"opening quote never closed", `"C:\App\claude.exe`, "", false},
		{"nothing between the quotes", `"" "%1"`, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := exeFromProtocolCommand(tt.cmd)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("exeFromProtocolCommand(%q) = %q, %v; want %q, %v", tt.cmd, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestBuildProtocolCommand(t *testing.T) {
	exe := `C:\Users\Example\AppData\Local\AnthropicClaude\app-1.24012.9\claude.exe`

	// The pristine form has to match what Claude Desktop registers, because
	// that is what Restore writes back.
	if got, want := buildProtocolCommand(exe, ""), `"`+exe+`" "%1"`; got != want {
		t.Errorf("pristine:\n got %q\nwant %q", got, want)
	}

	profile := `C:\Users\Example\AppData\Roaming\ClaudeWork`
	want := `"` + exe + `" --user-data-dir="` + profile + `" "%1"`
	if got := buildProtocolCommand(exe, profile); got != want {
		t.Errorf("with profile:\n got %q\nwant %q", got, want)
	}
}

func TestProtocolCommandProfile(t *testing.T) {
	exe := `C:\App\claude.exe`
	profile := `C:\Users\Example\AppData\Roaming\Claude Work` // a space, as real paths have

	got, ok := protocolCommandProfile(buildProtocolCommand(exe, profile))
	if !ok || got != profile {
		t.Errorf("round trip: got %q, %v; want %q, true", got, ok, profile)
	}

	if _, ok := protocolCommandProfile(buildProtocolCommand(exe, "")); ok {
		t.Error("the pristine form carries no profile")
	}
}

// TestProtocolCommandRoundTrip is the property that matters: whatever we write
// can be read back, and rewriting it for another profile does not accumulate
// arguments or lose the exe.
func TestProtocolCommandRoundTrip(t *testing.T) {
	exe := `C:\Program Files\Anthropic\claude.exe`
	first := buildProtocolCommand(exe, `C:\A`)

	gotExe, ok := exeFromProtocolCommand(first)
	if !ok || gotExe != exe {
		t.Fatalf("exe lost: %q", gotExe)
	}
	second := buildProtocolCommand(gotExe, `C:\B`)
	if dir, ok := protocolCommandProfile(second); !ok || dir != `C:\B` {
		t.Fatalf("second write: %q", second)
	}
	if pristine := buildProtocolCommand(gotExe, ""); pristine != `"`+exe+`" "%1"` {
		t.Fatalf("restore after two rewrites: %q", pristine)
	}
}

// The case this was written for: ClaudeWork had signed in once, so its
// config.json still named an account, but its session had lapsed and Claude
// asked for a Google sign-in. The old rule held the handler only for profiles
// without an account, so this one was skipped and the callback opened the
// default profile.
func TestHoldsProtocolHandlerForSignedInProfile(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "Claude")
	work := filepath.Join(root, "ClaudeWork")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"lastKnownAccountUuid": "035899b2-b130-40b6-aa9e-93cf208df7b7"}`
	if err := os.WriteFile(GetProfileConfigPath(work), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := GetProfileAccountUUID(work); err != nil {
		t.Fatalf("fixture should read as signed in: %v", err)
	}
	if !holdsProtocolHandler(work, defaultPath) {
		t.Error("a signed-in non-default profile must still hold the claude:// handler")
	}
}

func TestHoldsProtocolHandler(t *testing.T) {
	const def = `C:\Users\Example\AppData\Roaming\Claude`
	tests := []struct {
		name, profile, def string
		want               bool
	}{
		{"the default profile", def, def, false},
		{"the default profile, other case", `c:\users\example\appdata\roaming\CLAUDE`, def, false},
		{"another profile", `C:\Users\Example\AppData\Roaming\ClaudeWork`, def, true},
		{"default unknown", `C:\Users\Example\AppData\Roaming\Claude`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := holdsProtocolHandler(tt.profile, tt.def); got != tt.want {
				t.Errorf("holdsProtocolHandler(%q, %q) = %v; want %v", tt.profile, tt.def, got, tt.want)
			}
		})
	}
}

func TestHeldProfileInCmdLines(t *testing.T) {
	const def = `C:\Users\Example\AppData\Roaming\Claude`
	const work = `C:\Users\Example\AppData\Roaming\ClaudeWork`
	const exe = `"C:\Users\Example\AppData\Local\AnthropicClaude\app-2.7032.0\claude.exe"`
	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{"nothing running", nil, ""},
		{"default only", []string{exe + ` --user-data-dir=` + def}, ""},
		{"started from the Start menu, no flag", []string{exe}, ""},
		{"work profile", []string{exe + ` --user-data-dir=` + work}, work},
		{"work beside default", []string{exe + ` --user-data-dir=` + def, exe + ` --type=renderer --user-data-dir="` + work + `"`}, work},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := heldProfileInCmdLines(tt.lines, def); got != tt.want {
				t.Errorf("heldProfileInCmdLines(%q) = %q; want %q", tt.lines, got, tt.want)
			}
		})
	}
}

func TestHoldContinues(t *testing.T) {
	tests := []struct {
		name          string
		seen, running bool
		since         time.Duration
		wantSeen      bool
		wantKeep      bool
	}{
		{"still starting", false, false, time.Minute, false, true},
		{"never came up", false, false, holdStartupWindow, false, false},
		{"came up", false, true, time.Minute, true, true},
		{"still up long after start", true, true, time.Hour, true, true},
		{"closed after being up", true, false, 30 * time.Second, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen, keep := holdContinues(tt.seen, tt.running, tt.since)
			if seen != tt.wantSeen || keep != tt.wantKeep {
				t.Errorf("holdContinues(%v, %v, %s) = %v, %v; want %v, %v",
					tt.seen, tt.running, tt.since, seen, keep, tt.wantSeen, tt.wantKeep)
			}
		})
	}
}
