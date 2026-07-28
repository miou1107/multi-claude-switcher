//go:build windows

package platform

import "testing"

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
