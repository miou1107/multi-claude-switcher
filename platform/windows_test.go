//go:build windows

package platform

import "testing"

func TestExtractUserDataDir(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "quoted path (renderer/gpu style)",
			in:   `"C:\Program Files\WindowsApps\Claude_x\app\Claude.exe" --type=gpu-process --user-data-dir="C:\Users\Example\AppData\Roaming\Claude" --lang=zh-TW`,
			want: `C:\Users\Example\AppData\Roaming\Claude`,
		},
		{
			name: "bare path followed by another arg (crashpad style)",
			in:   `"...Claude.exe" --type=crashpad-handler --user-data-dir=C:\Users\Example\AppData\Roaming\Claude /prefetch:4`,
			want: `C:\Users\Example\AppData\Roaming\Claude`,
		},
		{
			name: "bare path at end of line",
			in:   `"...Claude.exe" --user-data-dir=C:\Users\Example\AppData\Roaming\Claude`,
			want: `C:\Users\Example\AppData\Roaming\Claude`,
		},
		{
			name: "flag absent",
			in:   `"C:\Program Files\WindowsApps\Claude_x\app\Claude.exe"`,
			want: ``,
		},
		{
			name: "quoted but unterminated",
			in:   `--user-data-dir="C:\Users\Example\AppData\Roaming\Claude`,
			want: `C:\Users\Example\AppData\Roaming\Claude`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractUserDataDir(tc.in); got != tc.want {
				t.Errorf("extractUserDataDir() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsDesktopProcess(t *testing.T) {
	cases := []struct {
		name string
		p    procInfo
		want bool
	}{
		{
			name: "MSIX/Store desktop app",
			p: procInfo{
				exePath: `C:\Program Files\WindowsApps\Claude_1.24012.1.0_x64__pzs8sxrjxfjjc\app\Claude.exe`,
				cmdLine: `"C:\Program Files\WindowsApps\Claude_1.24012.1.0_x64__pzs8sxrjxfjjc\app\Claude.exe"`,
			},
			want: true,
		},
		{
			name: "standalone desktop app",
			p: procInfo{
				exePath: `C:\Users\Example\AppData\Local\AnthropicClaude\app-1.24012.1\claude.exe`,
				cmdLine: `"C:\Users\Example\AppData\Local\AnthropicClaude\app-1.24012.1\claude.exe" --user-data-dir=C:\Users\Example\AppData\Roaming\ClaudeWork`,
			},
			want: true,
		},
		{
			name: "Claude Code CLI must NOT count (same image name, under claude-code)",
			p: procInfo{
				exePath: `C:\Users\Example\AppData\Roaming\Claude\claude-code\2.1.217\claude.exe`,
				cmdLine: `C:\Users\Example\AppData\Roaming\Claude\claude-code\2.1.217\claude.exe --model claude-opus-4-8`,
			},
			want: false,
		},
		{
			name: "unrelated process",
			p: procInfo{
				exePath: `C:\Windows\System32\notepad.exe`,
				cmdLine: `notepad.exe`,
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDesktopProcess(tc.p); got != tc.want {
				t.Errorf("isDesktopProcess() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSameWindowsPath(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{
			name: "identical",
			a:    `C:\Users\Example\AppData\Roaming\Claude`,
			b:    `C:\Users\Example\AppData\Roaming\Claude`,
			want: true,
		},
		{
			name: "case-insensitive (NTFS)",
			a:    `C:\Users\Example\AppData\Roaming\Claude`,
			b:    `c:\users\example\appdata\roaming\claude`,
			want: true,
		},
		{
			name: "trailing separator / redundant segment",
			a:    `C:\Users\Example\AppData\Roaming\Claude\`,
			b:    `C:\Users\Example\AppData\Roaming\.\Claude`,
			want: true,
		},
		{
			name: "different profile must not match",
			a:    `C:\Users\Example\AppData\Roaming\Claude`,
			b:    `C:\Users\Example\AppData\Roaming\ClaudeWork`,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameWindowsPath(tc.a, tc.b); got != tc.want {
				t.Errorf("sameWindowsPath(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestRunningProfilesInProcsWindows covers the standalone build's answer to
// "which accounts are open", which decides what gets reopened after MCS closes
// Claude Desktop. Command lines are the shape wmic/tasklist report: one main
// process per profile plus Electron helpers repeating the same path.
func TestRunningProfilesInProcsWindows(t *testing.T) {
	const (
		work     = `C:\Users\Example\AppData\Roaming\Claude`
		personal = `C:\Users\Example\AppData\Roaming\ClaudePersonal`
	)
	profiles := []*ProfileInfo{
		{Name: "Claude", Path: work},
		{Name: "ClaudePersonal", Path: personal},
	}

	cases := []struct {
		name  string
		procs []string
		want  []string
	}{
		{
			name: "one profile, counted once despite its helpers",
			procs: []string{
				`"C:\Program Files\Claude\Claude.exe" --user-data-dir="` + work + `"`,
				`"C:\Program Files\Claude\Claude.exe" --type=gpu-process --user-data-dir="` + work + `"`,
			},
			want: []string{work},
		},
		{
			name: "both profiles are reported",
			procs: []string{
				`"C:\Program Files\Claude\Claude.exe" --user-data-dir="` + personal + `"`,
				`"C:\Program Files\Claude\Claude.exe" --user-data-dir="` + work + `"`,
			},
			want: []string{personal, work},
		},
		{
			name: "casing and trailing separator still name the same profile",
			procs: []string{
				`"C:\Program Files\Claude\Claude.exe" --user-data-dir="c:\users\example\appdata\roaming\claude\"`,
			},
			want: []string{work},
		},
		{
			// Electron does not quote the value consistently. Taken from a real
			// Windows command line: the crashpad handler passes it bare with more
			// flags after it, while every other helper quotes it. Matching only the
			// quoted form would miss a running profile whose only surviving process
			// is the crashpad handler.
			name: "unquoted value, as the crashpad handler passes it",
			procs: []string{
				`"C:\Program Files\Claude\Claude.exe" --type=crashpad-handler --user-data-dir=` + work +
					` /prefetch:4 --no-rate-limit --database=` + work + `\Crashpad`,
			},
			want: []string{work},
		},
		{
			name:  "a process with no profile path is not attributed to one",
			procs: []string{`"C:\Program Files\Claude\Claude.exe"`},
			want:  nil,
		},
		{
			name:  "nothing running",
			procs: nil,
			want:  nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runningProfilesInProcsWindows(tc.procs, profiles)
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
