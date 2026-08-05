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
			// Claude Desktop opened from the Start menu passes no flag and runs on
			// %APPDATA%\Claude. Reading that as "nothing is running" is what left the
			// removal guard with nothing to guard on a standalone install.
			name:  "a flagless main process is the default profile",
			procs: []string{`"C:\Program Files\Claude\Claude.exe"`},
			want:  []string{work},
		},
		{
			// The children of a flagged parent do not all repeat the flag, so counting
			// them would report the default profile as running whenever any profile was.
			name: "a flagless child process is not the default profile",
			procs: []string{
				`"C:\Program Files\Claude\Claude.exe" --type=utility --lang=en-GB`,
				`"C:\Program Files\Claude\Claude.exe" --type=crashpad-handler /prefetch:4`,
			},
			want: nil,
		},
		{
			// Both at once: one account opened by MCS, another opened by hand.
			name: "a flagged process and a flagless main process are both reported",
			procs: []string{
				`"C:\Program Files\Claude\Claude.exe" --user-data-dir="` + personal + `"`,
				`"C:\Program Files\Claude\Claude.exe"`,
			},
			want: []string{personal, work},
		},
		{
			// Verbatim shapes from a live standalone install (Claude Desktop
			// 1.25927.0, Windows 10 19045), only the user name replaced. The
			// cases above were written from what these were expected to look
			// like; these are what they are. Two differences the idealized
			// fixtures did not have, both harmless to the parser but neither
			// previously covered:
			//
			//   - the crashpad handler's own exe path is NOT quoted, while
			//     every other helper's is,
			//   - the main process line ends in a trailing space.
			//
			// The main process here carries no --user-data-dir even though MCS
			// launched that profile, because Claude Desktop's own updater had
			// since relaunched it. That is the flagless case arriving from a
			// cause other than the Start menu, and the only reason this profile
			// is detected at all.
			name: "verbatim command lines from a live standalone install",
			procs: []string{
				`"C:\Users\Example\AppData\Local\AnthropicClaude\app-1.25927.0\claude.exe" `,
				`C:\Users\Example\AppData\Local\AnthropicClaude\app-1.25927.0\claude.exe --type=crashpad-handler --user-data-dir=` + work +
					` /prefetch:4 --no-rate-limit --monitor-self-annotation=ptype=crashpad-handler --database=` + work + `\Crashpad`,
				`"C:\Users\Example\AppData\Local\AnthropicClaude\app-1.25927.0\claude.exe" --type=gpu-process --user-data-dir="` + work +
					`" --gpu-preferences=SAAAAAAAAADgAAAEAAAAAAAAAAAAAGAAAQAAAAAAAAAAAAAAAAAAAAIAAAAAAAAA`,
				`"C:\Users\Example\AppData\Local\AnthropicClaude\app-1.25927.0\claude.exe" --type=renderer --user-data-dir="` + work +
					`" --app-user-model-id=com.squirrel.AnthropicClaude.claude --lang=zh-TW /prefetch:1`,
			},
			want: []string{work},
		},
		{
			name:  "nothing running",
			procs: nil,
			want:  nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runningProfilesInProcsWindows(tc.procs, profiles, work)
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
