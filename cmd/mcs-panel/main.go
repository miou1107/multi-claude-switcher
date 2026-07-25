// Command mcs-panel is the account panel window for Multi-Claude Switcher,
// rendered in a native WKWebView (via webview_go, which is compatible with
// current macOS — unlike darwinkit). It lists the managed accounts as styled
// cards; the page calls back into Go via window.mcsAct(action, folder) to switch
// accounts, run Rescan, or quit. Launched as a separate process by the tray so
// it doesn't fight systray for the macOS main run loop.
package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	webview "github.com/webview/webview_go"

	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
)

var (
	plat     platform.Platform
	switcher *core.Switcher
	win      webview.WebView
)

func main() {
	plat = platform.New()
	switcher = core.NewSwitcher(plat, core.NewBackupManager(""))

	win = webview.New(false)
	defer win.Destroy()
	win.SetTitle("Multi-Claude Switcher")
	win.SetSize(412, 560, webview.HintNone)

	win.Bind("mcsAct", func(action, folder string) {
		switch action {
		case "switch":
			go func() {
				doSwitch(folder)
				reload()
			}()
		case "rescan":
			go func() {
				doRescan()
				reload()
			}()
		case "quit":
			win.Terminate()
		}
	})

	reload()
	win.Run()
}

// reload re-renders the panel from current state, on the UI thread.
func reload() {
	win.Dispatch(func() {
		win.SetHtml(renderPanel(buildProfiles()))
	})
}

func doSwitch(folder string) {
	if folder == "" {
		return
	}
	profiles, err := plat.FindProfiles()
	if err != nil {
		return
	}
	var target *platform.ProfileInfo
	for _, p := range profiles {
		if p.Name == folder {
			target = p
			break
		}
	}
	if target == nil {
		return
	}
	_ = switcher.SafeSwitch(sourceProfilePath(target.Path, profiles), target.Path)
}

func doRescan() {
	folders, ok := runPicker()
	if ok && len(folders) > 0 {
		_ = core.SetManaged(folders)
	}
}

// buildProfiles lists the managed accounts for the panel.
func buildProfiles() []profileVM {
	profiles, err := plat.FindProfiles()
	if err != nil {
		return nil
	}
	managed := core.LoadManaged()
	running, _ := plat.DetectRunningProfile()
	var out []profileVM
	for _, p := range profiles {
		_, uErr := platform.GetProfileAccountUUID(p.Path)
		if !panelIncludes(managed, p.Name, uErr == nil, p.Managed) {
			continue
		}
		vm := profileVM{Folder: p.Name, Name: core.DisplayName(p.Name), Current: p.Path == running}
		if at, err := core.DetectAccountType(p.Path); err == nil {
			switch at {
			case core.AccountTeam:
				vm.Team = "Team"
			case core.AccountPersonal:
				vm.Team = "Personal"
			}
		}
		out = append(out, vm)
	}
	return out
}

// panelIncludes mirrors the tray's managed-registry filter: the registry is
// authoritative when present; on first run (nil) show any dir with a live login.
func panelIncludes(managed []string, folder string, hasLiveLogin, managedFlag bool) bool {
	if managed != nil {
		for _, m := range managed {
			if m == folder {
				return true
			}
		}
		return false
	}
	return hasLiveLogin || managedFlag
}

// sourceProfilePath picks the "from" profile for a switch: the running one, else
// the first other profile with sessions.
func sourceProfilePath(targetPath string, profiles []*platform.ProfileInfo) string {
	if running, err := plat.DetectRunningProfile(); err == nil && running != "" && running != targetPath {
		return running
	}
	for _, p := range profiles {
		if p.Path != targetPath && p.HasSessionsDir {
			return p.Path
		}
	}
	if len(profiles) > 0 {
		return profiles[0].Path
	}
	return filepath.Join(plat.AppSupportDir(), "Claude")
}

// runPicker launches the sibling mcs-picker binary and parses its result line.
func runPicker() ([]string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return nil, false
	}
	out, err := exec.Command(filepath.Join(filepath.Dir(exe), "mcs-picker")).Output()
	if err != nil {
		return nil, false
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "MCS_RESULT ") {
			continue
		}
		var r struct {
			OK      bool     `json:"ok"`
			Folders []string `json:"folders"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "MCS_RESULT ")), &r) == nil {
			return r.Folders, r.OK
		}
	}
	return nil, false
}
