package panelui

import (
	_ "embed"
	"encoding/base64"
	"strconv"

	"github.com/miou1107/multi-claude-switcher/core"
)

//go:embed i18n.js
var i18nScript string

// Keep localization out of the page markup. The dictionary holds every English
// string the panel can show, so a renderer check that counts a phrase in the
// HTML would find it in the dictionary too; base64 keeps the two apart.
//
// The chosen language rides in a separate inline tag rather than inside the
// dictionary, so the same dictionary bytes serve every render and only four
// characters change when someone picks a different language.
func i18nScriptTag() string {
	return `<script>window.__mcsLang=` + strconv.Quote(core.PanelLanguage()) + `;</script>` +
		`<script src="data:text/javascript;base64,` +
		base64.StdEncoding.EncodeToString([]byte(i18nScript)) + `"></script>`
}
