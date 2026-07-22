//go:build windows

package main

import (
	_ "embed"

	"github.com/getlantern/systray"
)

//go:embed assets/icon.ico
var trayIconICO []byte

// setTrayIcon installs the tray icon. Windows needs a real .ico; a PNG template
// makes systray's SetIcon fail ("Unable to set icon"), so ship the
// multi-resolution icon.ico and use SetIcon (SetTemplateIcon is a macOS notion).
func setTrayIcon() {
	systray.SetIcon(trayIconICO)
}
