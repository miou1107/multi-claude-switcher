//go:build windows

package clip

import (
	"encoding/base64"
	"os/exec"
	"syscall"
	"unicode/utf16"
)

// Set writes text to the clipboard, returning only once it is there.
//
// The text is passed base64-encoded and decoded inside PowerShell rather than
// quoted into the script: a report contains quotes, backticks, dollar signs and
// newlines, all of which are PowerShell syntax, and single-quote escaping only
// handles the first of them.
//
// cmd.Run, not Start. Launching PowerShell costs several hundred milliseconds,
// and the caller opens a browser next; losing that race means the user pastes
// their previous clipboard into a public issue.
func Set(text string) error {
	script := `Set-Clipboard -Value ([System.Text.Encoding]::Unicode.GetString(` +
		`[System.Convert]::FromBase64String('` +
		base64.StdEncoding.EncodeToString(utf16le(text)) + `')))`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(utf16le(script)))
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
