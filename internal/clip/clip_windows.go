//go:build windows

package clip

import (
	"encoding/base64"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
)

// Set writes text to the clipboard, returning only once it is there.
//
// The payload travels on stdin, not embedded in the script: CreateProcess
// caps a command line at 32,767 characters, and a script that carries the
// payload inline (base64 of UTF-16, doubling the size twice over) blows past
// that at a few thousand characters of report -- well within what a real
// debug report holds once log tails are attached. Stdin has no such limit.
//
// The payload is still base64-encoded UTF-16LE, now read back with
// ReadToEnd() and decoded inside PowerShell, rather than handed over as raw
// text: a report contains quotes, backticks, dollar signs and newlines, all
// of which are PowerShell syntax if they ever reach script text, and the
// console's input encoding cannot be trusted to preserve non-ASCII bytes
// read as text. Base64 keeps the wire format to a fixed ASCII alphabet
// immune to both problems; only where it lives changed, not why it exists.
//
// $ErrorActionPreference = 'Stop' makes a non-terminating Set-Clipboard
// failure -- another process holding the clipboard, for instance -- actually
// fail the script and surface a non-zero exit code, instead of silently
// leaving Set returning nil over a stale clipboard.
//
// cmd.Run, not Start. Launching PowerShell costs several hundred milliseconds,
// and the caller opens a browser next; losing that race means the user pastes
// their previous clipboard into a public issue.
func Set(text string) error {
	script := `$ErrorActionPreference = 'Stop'; ` +
		`$b64 = [Console]::In.ReadToEnd(); ` +
		`Set-Clipboard -Value ([System.Text.Encoding]::Unicode.GetString(` +
		`[System.Convert]::FromBase64String($b64)))`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(utf16le(script)))
	cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString(utf16le(text)))
	// CREATE_NO_WINDOW: the hosts are background processes, and a console
	// flashing up while copying a bug report is the kind of thing users report
	// as a bug of its own.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	return cmd.Run()
}

// utf16le encodes a string the way PowerShell's -EncodedCommand and
// System.Text.Encoding.Unicode both expect. Used for the script and the payload
// alike, which is why it lives here rather than being borrowed from the tray's
// dialog helpers.
func utf16le(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, 0, len(u)*2)
	for _, c := range u {
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}
