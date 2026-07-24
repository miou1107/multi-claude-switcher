//go:build !darwin && !windows

package main

// Dialogs are only implemented for macOS and Windows. On other operating systems
// (e.g. Linux, used by CI/dev builds) these are safe no-ops so the package still
// builds; a headless build never shows UI.

func notify(title, text string) {}

func openFolder(path string) {}

func fileManagerName() string { return "your file manager" }

func chooseFromList(options []string, prompt string) string { return "" }

func askText(prompt, defaultAnswer string) string { return "" }

func confirmDialog(message, confirmLabel string) bool { return false }

func confirmDialogMultiline(message, confirmLabel string) bool { return false }

func chooseMultipleFromList(options, preselected []string, prompt string) ([]string, bool) {
	return nil, false
}

func infoDialog(title, message string) {}

func askEnableAutoSyncChoice(message string) autoSyncChoice { return choiceCancel }
