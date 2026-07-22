//go:build windows

package main

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
	"unicode/utf16"
)

// psEnc encodes a PowerShell script as base64/UTF-16LE for -EncodedCommand,
// sidestepping all command-line quoting issues.
func psEnc(script string) string {
	u16 := utf16.Encode([]rune(script))
	buf := make([]byte, 0, len(u16)*2)
	for _, c := range u16 {
		buf = append(buf, byte(c), byte(c>>8))
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// runPS runs a PowerShell script (STA, for WinForms) and returns trimmed stdout
// plus the run error (a non-zero exit surfaces as *exec.ExitError).
func runPS(script string) (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA", "-EncodedCommand", psEnc(script))
	hideConsole(cmd)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// psQuote wraps a string as a single-quoted PowerShell literal.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// notify shows a best-effort Windows toast notification. Fired detached so it
// never blocks the caller. A toast (rather than a NotifyIcon balloon) avoids
// adding a second tray icon and displays reliably on modern Windows; if the user
// has notifications disabled it simply does nothing. The toast is attributed to
// the built-in PowerShell app id so it has a valid, always-present source.
func notify(title, text string) {
	script := fmt.Sprintf(`$ErrorActionPreference = "SilentlyContinue"
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType=WindowsRuntime] > $null
$t = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$n = $t.GetElementsByTagName("text")
$n.Item(0).AppendChild($t.CreateTextNode(%s)) > $null
$n.Item(1).AppendChild($t.CreateTextNode(%s)) > $null
$toast = [Windows.UI.Notifications.ToastNotification]::new($t)
$appId = "{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe"
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($appId).Show($toast)`, psQuote(title), psQuote(text))
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-EncodedCommand", psEnc(script))
	hideConsole(cmd)
	_ = cmd.Start()
}

// openFolder reveals a directory in File Explorer. explorer.exe returns a
// non-zero exit code even on success, so Start (fire-and-forget) is used.
func openFolder(path string) {
	_ = exec.Command("explorer", path).Start()
}

// confirmDialog shows a Yes/No message box; Yes = confirm. The action verb is
// carried in the message text (a standard box cannot relabel its buttons).
func confirmDialog(message, confirmLabel string) bool {
	_ = confirmLabel
	script := `Add-Type -AssemblyName System.Windows.Forms
$r = [System.Windows.Forms.MessageBox]::Show(` + psQuote(message) + `, 'Multi-Claude Switcher', 'YesNo', 'Question')
if ($r -eq 'Yes') { exit 0 } else { exit 1 }`
	_, err := runPS(script)
	return err == nil
}

// infoDialog shows an OK-only information box.
func infoDialog(title, message string) {
	script := `Add-Type -AssemblyName System.Windows.Forms
[void][System.Windows.Forms.MessageBox]::Show(` + psQuote(message) + `, ` + psQuote(title) + `, 'OK', 'Information')`
	_, _ = runPS(script)
}

// askText shows a text-input box and returns the entered string, or "" if
// cancelled (InputBox also returns "" for empty input).
func askText(prompt, defaultAnswer string) string {
	script := `Add-Type -AssemblyName Microsoft.VisualBasic
[Microsoft.VisualBasic.Interaction]::InputBox(` + psQuote(prompt) + `, 'Multi-Claude Switcher', ` + psQuote(defaultAnswer) + `)`
	out, err := runPS(script)
	if err != nil {
		return ""
	}
	return out
}

// chooseFromList shows a single-select list dialog and returns the chosen item,
// or "" if cancelled.
func chooseFromList(options []string, prompt string) string {
	var b strings.Builder
	b.WriteString(`Add-Type -AssemblyName System.Windows.Forms
$form = New-Object System.Windows.Forms.Form
$form.Text = 'Multi-Claude Switcher'
$form.Width = 440; $form.Height = 340; $form.StartPosition = 'CenterScreen'
$label = New-Object System.Windows.Forms.Label
$label.Text = ` + psQuote(prompt) + `
$label.AutoSize = $true; $label.Top = 10; $label.Left = 12
$form.Controls.Add($label)
$list = New-Object System.Windows.Forms.ListBox
$list.Top = 36; $list.Left = 12; $list.Width = 400; $list.Height = 210
`)
	for _, o := range options {
		b.WriteString("[void]$list.Items.Add(" + psQuote(o) + ")\n")
	}
	b.WriteString(`$form.Controls.Add($list)
$ok = New-Object System.Windows.Forms.Button
$ok.Text = 'OK'; $ok.Top = 258; $ok.Left = 250; $ok.DialogResult = 'OK'
$cancel = New-Object System.Windows.Forms.Button
$cancel.Text = 'Cancel'; $cancel.Top = 258; $cancel.Left = 335; $cancel.DialogResult = 'Cancel'
$form.Controls.Add($ok); $form.Controls.Add($cancel)
$form.AcceptButton = $ok; $form.CancelButton = $cancel
if (($form.ShowDialog() -eq 'OK') -and ($null -ne $list.SelectedItem)) { $list.SelectedItem }`)
	out, err := runPS(b.String())
	if err != nil {
		return ""
	}
	return out
}

// askEnableAutoSyncChoice shows the auto-sync enable warning with three buttons
// and maps the result to a choice. The form exits with 1 (Enable), 2 (Enable,
// don't ask again), or 0 (Cancel / closed).
func askEnableAutoSyncChoice(message string) autoSyncChoice {
	script := `Add-Type -AssemblyName System.Windows.Forms
$form = New-Object System.Windows.Forms.Form
$form.Text = 'Multi-Claude Switcher'
$form.Width = 470; $form.Height = 190; $form.StartPosition = 'CenterScreen'
$form.FormBorderStyle = 'FixedDialog'; $form.MinimizeBox = $false; $form.MaximizeBox = $false
$label = New-Object System.Windows.Forms.Label
$label.Text = ` + psQuote(message) + `
$label.Left = 14; $label.Top = 14; $label.Width = 430; $label.Height = 70
$form.Controls.Add($label)
$form.Tag = 0
$cancel = New-Object System.Windows.Forms.Button
$cancel.Text = 'Cancel'; $cancel.Left = 14; $cancel.Top = 100; $cancel.Width = 90
$cancel.Add_Click({ $form.Tag = 0; $form.Close() })
$enable = New-Object System.Windows.Forms.Button
$enable.Text = 'Enable'; $enable.Left = 150; $enable.Top = 100; $enable.Width = 120
$enable.Add_Click({ $form.Tag = 1; $form.Close() })
$dont = New-Object System.Windows.Forms.Button
$dont.Text = "Enable, don't ask again"; $dont.Left = 280; $dont.Top = 100; $dont.Width = 160
$dont.Add_Click({ $form.Tag = 2; $form.Close() })
$form.Controls.Add($cancel); $form.Controls.Add($enable); $form.Controls.Add($dont)
[void]$form.ShowDialog()
exit [int]$form.Tag`
	_, err := runPS(script)
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	switch code {
	case 1:
		return choiceEnable
	case 2:
		return choiceEnableDontAsk
	default:
		return choiceCancel
	}
}
