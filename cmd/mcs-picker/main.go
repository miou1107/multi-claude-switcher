// Command mcs-picker is the standalone native picker window for "Rescan
// accounts". The tray launches it as a separate process (so it doesn't fight
// systray for the macOS main run loop); it scans the machine, shows a native
// window, and prints the chosen folders as JSON to stdout, prefixed with a
// sentinel so the tray can find it amid any webview noise:
//
//	MCS_RESULT {"ok":true,"folders":["Claude","Claude_Profile2"]}
//
// Cancel or closing the window prints {"ok":false}.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
)

// resultSentinel prefixes the one stdout line the tray parses.
const resultSentinel = "MCS_RESULT "

type result struct {
	OK      bool     `json:"ok"`
	Folders []string `json:"folders,omitempty"`
}

func main() {
	profiles, err := platform.New().FindProfiles()
	if err != nil {
		fmt.Fprintln(os.Stderr, "picker: find profiles:", err)
		emit(result{OK: false})
		return
	}
	accounts := core.ScanAccounts(profiles, core.LoadPending())
	preselected := computePreselect(accounts, core.LoadManaged())

	// runPicker is platform-specific: a native webview window on macOS, a no-op
	// on other platforms (where the tray uses a different flow).
	emit(runPicker(accounts, preselected))
}

func emit(r result) {
	b, _ := json.Marshal(r)
	fmt.Println(resultSentinel + string(b))
}
