package main

import (
	"fmt"
	"log"
	"os/exec"
	"path/filepath"

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
	systray.AddMenuItem("Multi-Claude Switcher v0.2.0", "Seamless Multi-Account Switcher for Claude Desktop").Disable()
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

				// Find current running profile or default source
				srcPath := getSourceProfilePath(target.Path, profiles)
				err := switcher.SafeSwitch(srcPath, target.Path)
				if err != nil {
					log.Printf("Switch error: %v", err)
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

func getSourceProfilePath(targetPath string, profiles []*platform.ProfileInfo) string {
	for _, p := range profiles {
		if p.Path != targetPath && p.HasSessionsDir {
			return p.Path
		}
	}
	// Fallback to primary Claude profile
	if len(profiles) > 0 {
		return profiles[0].Path
	}
	return filepath.Join(plat.AppSupportDir(), "Claude")
}
