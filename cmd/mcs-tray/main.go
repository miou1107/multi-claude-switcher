package main

import (
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/getlantern/systray"
	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
)

var (
	plat     platform.Platform
	bm       *core.BackupManager
	switcher *core.Switcher
)

func main() {
	plat = platform.New()
	bm = core.NewBackupManager("")
	switcher = core.NewSwitcher(plat, bm)

	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetTitle(" Claude")
	systray.SetTooltip("Multi-Claude Switcher")

	// Header item
	systray.AddMenuItem(fmt.Sprintf("Multi-Claude Switcher v%s", core.Version), "Seamless Multi-Account Switcher for Claude Desktop").Disable()
	systray.AddSeparator()

	// Profiles section
	mProfilesHeader := systray.AddMenuItem("Available Profiles:", "")
	mProfilesHeader.Disable()

	profiles, err := plat.FindProfiles()
	if err != nil {
		log.Printf("Error finding profiles: %v", err)
	}

	profileItems := make(map[*systray.MenuItem]*platform.ProfileInfo)
	for _, p := range profiles {
		if !p.HasSessionsDir && p.Name != "Claude" && p.Name != "Claude_Profile2" {
			continue
		}
		item := systray.AddMenuItem(fmt.Sprintf("Switch to: %s", p.Name), fmt.Sprintf("Switch active profile to %s", p.Name))
		profileItems[item] = p
	}

	systray.AddSeparator()

	// Actions section
	mBackup := systray.AddMenuItem("Backup All Profiles", "Take a snapshot backup of all profiles")
	mOpenBackups := systray.AddMenuItem("Open Backup Directory", "Open backup folder in Finder")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Quit Multi-Claude Switcher Tray")

	// Handle menu item events in goroutines
	for item, prof := range profileItems {
		go func(m *systray.MenuItem, target *platform.ProfileInfo) {
			for range m.ClickedCh {
				log.Printf("User selected switch to profile: %s", target.Name)

				// Confirm before switching: the switch closes Claude Desktop, so
				// a mis-click should not silently kill a running session.
				if !confirmSwitch(target.Name) {
					log.Printf("Switch to %s cancelled by user.", target.Name)
					continue
				}

				// Find current running profile or default source
				srcPath := getSourceProfilePath(target.Path, profiles)
				err := switcher.SafeSwitch(srcPath, target.Path)
				if err != nil {
					log.Printf("Switch error: %v", err)
					notify("Switch failed", err.Error())
				}
			}
		}(item, prof)
	}

	go func() {
		for range mBackup.ClickedCh {
			log.Println("User clicked Backup All Profiles")
			for _, p := range profiles {
				if p.HasSessionsDir {
					backupPath, err := bm.CreateBackup(p.Path)
					if err == nil {
						log.Printf("Backed up %s to %s", p.Name, backupPath)
					}
				}
			}
		}
	}()

	go func() {
		for range mOpenBackups.ClickedCh {
			_ = exec.Command("open", bm.BackupRootDir).Run()
		}
	}()

	go func() {
		<-mQuit.ClickedCh
		systray.Quit()
	}()
}

func onExit() {
	log.Println("Multi-Claude Switcher Tray exited cleanly.")
}

// confirmSwitch shows a native macOS confirmation dialog. Returns true only if
// the user explicitly confirms; the "Cancel" button (osascript non-zero exit)
// returns false, so a mis-click never kills a running Claude session.
func confirmSwitch(targetName string) bool {
	msg := fmt.Sprintf("Switch to %q? Claude Desktop will be closed and reopened with this profile.", targetName)
	script := fmt.Sprintf(`display dialog %s buttons {"Cancel", "Switch"} default button "Switch" cancel button "Cancel" with title "Multi-Claude Switcher"`, osaQuote(msg))
	return exec.Command("osascript", "-e", script).Run() == nil
}

// notify shows a native macOS notification (best-effort).
func notify(title, text string) {
	script := fmt.Sprintf(`display notification %s with title %s`, osaQuote(text), osaQuote(title))
	_ = exec.Command("osascript", "-e", script).Run()
}

// osaQuote wraps a string as an AppleScript string literal, escaping backslashes
// and double quotes.
func osaQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", " ")
	return `"` + s + `"`
}

func getSourceProfilePath(targetPath string, profiles []*platform.ProfileInfo) string {
	// Prefer the profile the user is actually running right now: that is the
	// account being left behind, whose sessions should flow into the target.
	if running, err := plat.DetectRunningProfile(); err == nil && running != "" && running != targetPath {
		return running
	}

	// Otherwise fall back to the first other profile that has sessions.
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
