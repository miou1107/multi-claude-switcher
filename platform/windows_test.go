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
			in:   `"C:\Program Files\WindowsApps\Claude_x\app\Claude.exe" --type=gpu-process --user-data-dir="C:\Users\Vin\AppData\Roaming\Claude" --lang=zh-TW`,
			want: `C:\Users\Vin\AppData\Roaming\Claude`,
		},
		{
			name: "bare path followed by another arg (crashpad style)",
			in:   `"...Claude.exe" --type=crashpad-handler --user-data-dir=C:\Users\Vin\AppData\Roaming\Claude /prefetch:4`,
			want: `C:\Users\Vin\AppData\Roaming\Claude`,
		},
		{
			name: "bare path at end of line",
			in:   `"...Claude.exe" --user-data-dir=C:\Users\Vin\AppData\Roaming\Claude`,
			want: `C:\Users\Vin\AppData\Roaming\Claude`,
		},
		{
			name: "flag absent",
			in:   `"C:\Program Files\WindowsApps\Claude_x\app\Claude.exe"`,
			want: ``,
		},
		{
			name: "quoted but unterminated",
			in:   `--user-data-dir="C:\Users\Vin\AppData\Roaming\Claude`,
			want: `C:\Users\Vin\AppData\Roaming\Claude`,
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
				exePath: `C:\Users\Vin\AppData\Local\AnthropicClaude\app-1.24012.1\claude.exe`,
				cmdLine: `"C:\Users\Vin\AppData\Local\AnthropicClaude\app-1.24012.1\claude.exe" --user-data-dir=C:\Users\Vin\AppData\Roaming\ClaudeWork`,
			},
			want: true,
		},
		{
			name: "Claude Code CLI must NOT count (same image name, under claude-code)",
			p: procInfo{
				exePath: `C:\Users\Vin\AppData\Roaming\Claude\claude-code\2.1.217\claude.exe`,
				cmdLine: `C:\Users\Vin\AppData\Roaming\Claude\claude-code\2.1.217\claude.exe --model claude-opus-4-8`,
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
			a:    `C:\Users\Vin\AppData\Roaming\Claude`,
			b:    `C:\Users\Vin\AppData\Roaming\Claude`,
			want: true,
		},
		{
			name: "case-insensitive (NTFS)",
			a:    `C:\Users\Vin\AppData\Roaming\Claude`,
			b:    `c:\users\vin\appdata\roaming\claude`,
			want: true,
		},
		{
			name: "trailing separator / redundant segment",
			a:    `C:\Users\Vin\AppData\Roaming\Claude\`,
			b:    `C:\Users\Vin\AppData\Roaming\.\Claude`,
			want: true,
		},
		{
			name: "different profile must not match",
			a:    `C:\Users\Vin\AppData\Roaming\Claude`,
			b:    `C:\Users\Vin\AppData\Roaming\ClaudeWork`,
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
