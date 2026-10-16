package panelui

import (
	_ "embed"
	"encoding/base64"
	"strconv"
	"sync"

	"github.com/miou1107/multi-claude-switcher/core"
)

//go:embed i18n.js
var i18nScript string

// panelLanguage is where the renderer reads the stored preference. It is a var
// so a test can pin it: otherwise rendering any screen would read the settings
// file of whoever is running the tests, and this package would stop being a
// function of what it is handed.
var panelLanguage = core.PanelLanguage

// The dictionary is the same bytes on every render, and it is 40 KB of them,
// so encode it once rather than on each redraw.
var encodedDictionary = sync.OnceValue(func() string {
	return base64.StdEncoding.EncodeToString([]byte(i18nScript))
})

// Keep localization out of the page markup. The dictionary holds every English
// string the panel can show, so a renderer check that counts a phrase in the
// HTML would find it in the dictionary too; base64 keeps the two apart.
//
// The chosen language rides in its own inline tag ahead of it, so the encoded
// dictionary stays identical between renders and only the preference changes.
func i18nScriptTag() string {
	return `<script>window.__mcsLang=` + strconv.Quote(panelLanguage()) + `;</script>` +
		`<script src="data:text/javascript;base64,` + encodedDictionary() + `"></script>`
}
