//go:build darwin

package main

import (
	webview "github.com/webview/webview_go"

	"github.com/miou1107/multi-claude-switcher/core"
)

// runPicker shows the native picker window and returns the user's choice.
// Confirm → {OK:true, Folders:[…]}; Cancel or closing the window → {OK:false}.
func runPicker(accounts []core.ScannedAccount, preselected map[string]bool) result {
	res := result{OK: false}

	w := webview.New(false)
	defer w.Destroy()
	w.SetTitle("Multi-Claude Switcher")
	w.SetSize(600, 700, webview.HintNone)

	_ = w.Bind("mcsSubmit", func(folders []string) {
		res = result{OK: true, Folders: folders}
		w.Terminate()
	})
	_ = w.Bind("mcsCancel", func() {
		res = result{OK: false}
		w.Terminate()
	})

	w.SetHtml(renderPicker(accounts, preselected))
	w.Run() // blocks until Terminate() or the window is closed
	return res
}
