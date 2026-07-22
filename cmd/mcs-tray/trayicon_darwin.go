//go:build darwin

package main

import (
	_ "embed"

	"github.com/getlantern/systray"
)

//go:embed assets/icon.png
var trayIconPNG []byte

// setTrayIcon installs the menu-bar icon. A template image (black on
// transparent) lets macOS recolor it to match a light or dark menu bar.
func setTrayIcon() {
	systray.SetTemplateIcon(trayIconPNG, trayIconPNG)
}
