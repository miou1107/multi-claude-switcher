//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// osaQuote wraps a string as an AppleScript string literal, escaping backslashes
// and double quotes.
func osaQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", " ")
	return `"` + s + `"`
}

// notify shows a native macOS notification (best-effort).
func notify(title, text string) {
	script := fmt.Sprintf(`display notification %s with title %s`, osaQuote(text), osaQuote(title))
	_ = exec.Command("osascript", "-e", script).Run()
}

// openFolder reveals a directory in Finder.
func openFolder(path string) {
	_ = exec.Command("open", path).Run()
}

// fileManagerName is the OS file manager's name, for user-facing tooltips.
func fileManagerName() string { return "Finder" }

// chooseFromList shows a native macOS "choose from list" dialog and returns the
// selected item, or "" if cancelled.
func chooseFromList(options []string, prompt string) string {
	var quoted []string
	for _, o := range options {
		quoted = append(quoted, osaQuote(o))
	}
	script := fmt.Sprintf(`set sel to choose from list {%s} with prompt %s
if sel is false then
	return ""
end if
return item 1 of sel`, strings.Join(quoted, ", "), osaQuote(prompt))
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// askText shows a native text-input dialog and returns the entered string, or
// "" if cancelled.
func askText(prompt, defaultAnswer string) string {
	script := fmt.Sprintf(`set r to display dialog %s default answer %s buttons {"Cancel", "OK"} default button "OK" cancel button "Cancel" with title "Multi-Claude Switcher"
return text returned of r`, osaQuote(prompt), osaQuote(defaultAnswer))
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return "" // cancelled
	}
	return strings.TrimSpace(string(out))
}

// confirmDialog shows a native two-button confirmation (Cancel + confirmLabel).
// Returns true only if the user picks confirmLabel; the "Cancel" button makes
// osascript exit non-zero, which returns false.
func confirmDialog(message, confirmLabel string) bool {
	script := fmt.Sprintf(`display dialog %s buttons {"Cancel", %s} default button %s cancel button "Cancel" with title "Multi-Claude Switcher"`,
		osaQuote(message), osaQuote(confirmLabel), osaQuote(confirmLabel))
	return exec.Command("osascript", "-e", script).Run() == nil
}

// infoDialog shows an OK-only informational dialog. Newlines in message become
// separate AppleScript lines.
func infoDialog(title, message string) {
	var quoted []string
	for _, l := range strings.Split(message, "\n") {
		quoted = append(quoted, osaQuote(l))
	}
	script := "display dialog " + strings.Join(quoted, " & return & ") +
		fmt.Sprintf(` buttons {"OK"} default button "OK" with title %s`, osaQuote(title))
	_ = exec.Command("osascript", "-e", script).Run()
}

// askEnableAutoSyncChoice shows the auto-sync enable warning (three buttons) and
// returns the user's choice.
func askEnableAutoSyncChoice(message string) autoSyncChoice {
	script := "display dialog " + osaQuote(message) +
		` buttons {"Cancel", "Enable", "Enable, don't ask again"}` +
		` default button "Enable" cancel button "Cancel" with title "Multi-Claude Switcher"`
	out, err := exec.Command("osascript", "-e", script).Output()
	return parseAutoSyncChoice(string(out), err)
}

// parseAutoSyncChoice maps an osascript `display dialog` result to a choice.
// The cancel button makes osascript exit non-zero (runErr != nil); otherwise
// stdout is "button returned:<label>".
func parseAutoSyncChoice(out string, runErr error) autoSyncChoice {
	if runErr != nil {
		return choiceCancel
	}
	if strings.Contains(strings.ToLower(out), "don't ask") {
		return choiceEnableDontAsk
	}
	return choiceEnable
}
