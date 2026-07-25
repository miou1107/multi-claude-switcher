// Command mcs-menubar is the menu-bar app for Multi-Claude Switcher: a native
// NSStatusItem whose click shows an NSPopover hosting a WKWebView — the styled
// account panel, rendered from Go. Written in direct CGO Objective-C (menubar.m)
// because it is compatible with current macOS, unlike darwinkit. Must run inside
// a .app bundle. The page calls back via window.webkit.messageHandlers.mcs.
package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#include <stdlib.h>
#include "menubar.h"
*/
import "C"

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
)

var (
	plat     platform.Platform
	switcher *core.Switcher
)

//export goPanelReady
func goPanelReady() { reloadPanel() }

//export goPanelWillOpen
func goPanelWillOpen() { reloadPanel() }

//export goPanelAction
func goPanelAction(caction, cfolder *C.char) {
	action := C.GoString(caction)
	folder := C.GoString(cfolder)
	switch action {
	case "switch":
		go func() {
			doSwitch(folder)
			reloadPanel()
		}()
	case "rescan":
		go func() {
			doRescan()
			reloadPanel()
		}()
	case "quit":
		C.TerminateApp()
	}
}

// reloadPanel re-renders the panel HTML and pushes it into the popover's webview.
func reloadPanel() {
	c := C.CString(renderPanel(buildProfiles()))
	defer C.free(unsafe.Pointer(c))
	C.LoadPanelHTML(c)
}

func main() {
	plat = platform.New()
	switcher = core.NewSwitcher(plat, core.NewBackupManager(""))
	C.RunMenuBar()
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

// panelIncludes mirrors the tray's managed-registry filter.
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
