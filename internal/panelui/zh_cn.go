package panelui

import (
	_ "embed"
	"encoding/base64"
)

//go:embed zh_cn.js
var chineseScript string

// Keep localization code separate from the page markup. This also keeps
// renderer checks of the visible markup independent of the word dictionary.
func chineseScriptTag() string {
	return `<script src="data:text/javascript;base64,` + base64.StdEncoding.EncodeToString([]byte(chineseScript)) + `"></script>`
}
