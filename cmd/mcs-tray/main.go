package main

import (
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/getlantern/systray"
	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
)

//go:embed assets/icon.png
var trayIcon []byte

var (
	plat     platform.Platform
	bm       *core.BackupManager
	switcher *core.Switcher
)

func main() {
	if closer, _, err := core.SetupLogging("mcs-tray"); err == nil {
		defer closer.Close()
	} else {
		log.Printf("Warning: could not open log file, logging to stderr only: %v", err)
	}

	plat = platform.New()
	bm = core.NewBackupManager("")
	switcher = core.NewSwitcher(plat, bm)

	systray.Run(onReady, onExit)
}

func onReady() {
	// Template icon: black-on-transparent glyph that macOS recolors to match a
	// light or dark menu bar automatically. Icon only, no text title.
	systray.SetTemplateIcon(trayIcon, trayIcon)
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
		item := systray.AddMenuItem(fmt.Sprintf("Switch to: %s", core.DisplayName(p.Name)), fmt.Sprintf("Switch active profile to %s", p.Name))
		profileItems[item] = p
	}

	systray.AddSeparator()

	// Manual align: copy one account's sessions into another WITHOUT switching.
	mSync := systray.AddMenuItem("Sync sessions →", "Copy one account's sessions into another (without switching accounts)")
	type alignPair struct{ src, dst *platform.ProfileInfo }
	alignItems := map[*systray.MenuItem]alignPair{}
	var shown []*platform.ProfileInfo
	for _, p := range profiles {
		if p.HasSessionsDir || p.Name == "Claude" || p.Name == "Claude_Profile2" {
			shown = append(shown, p)
		}
	}
	for _, a := range shown {
		for _, b := range shown {
			if a.Path == b.Path {
				continue
			}
			label := fmt.Sprintf("From %s → To %s", core.DisplayName(a.Name), core.DisplayName(b.Name))
			child := mSync.AddSubMenuItem(label, "Copy the first account's sessions into the second")
			alignItems[child] = alignPair{src: a, dst: b}
		}
	}

	// Actions section
	mUpdate := systray.AddMenuItem("Check for Updates…", "Check GitHub for a newer version and update")
	mRename := systray.AddMenuItem("Rename a Profile…", "Give a profile a friendlier display name")
	mLogin := systray.AddMenuItemCheckbox("Start at Login", "Launch automatically when you log in", core.LoginItemEnabled())
	mAutoAlign := systray.AddMenuItemCheckbox("Auto-Align on Switch", "Keep both accounts' sessions identical on every switch", core.AutoAlignOnSwitch())
	mBackup := systray.AddMenuItem("Backup All Profiles", "Take a snapshot backup of all profiles")
	mOpenBackups := systray.AddMenuItem("Open Backup Directory", "Open backup folder in Finder")
	mOpenLogs := systray.AddMenuItem("Open Log Folder", "Open the log folder in Finder")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Quit Multi-Claude Switcher Tray")

	// Handle menu item events in goroutines
	for item, prof := range profileItems {
		go func(m *systray.MenuItem, target *platform.ProfileInfo) {
			for range m.ClickedCh {
				log.Printf("User selected switch to profile: %s", target.Name)

				// Confirm before switching: the switch closes Claude Desktop, so
				// a mis-click should not silently kill a running session.
				if !confirmSwitch(core.DisplayName(target.Name)) {
					log.Printf("Switch to %s cancelled by user.", target.Name)
					continue
				}

				// Find current running profile or default source
				srcPath := getSourceProfilePath(target.Path, profiles)
				err := switcher.SafeSwitch(srcPath, target.Path)
				if err != nil {
					log.Printf("Switch error: %v", err)
					notify("Switch failed", err.Error())
				} else {
					// We just launched the target, so mark it active immediately
					// (before the new process is even detectable by `ps`).
					markActive(profileItems, target.Path)
				}
			}
		}(item, prof)
	}

	for item, pair := range alignItems {
		go func(m *systray.MenuItem, pr alignPair) {
			for range m.ClickedCh {
				if !confirmAlign(core.DisplayName(pr.src.Name), core.DisplayName(pr.dst.Name)) {
					log.Printf("Align %s -> %s cancelled by user.", pr.src.Name, pr.dst.Name)
					continue
				}
				report, err := switcher.ManualAlign(pr.src.Path, pr.dst.Path)
				if err != nil {
					log.Printf("Manual align error: %v", err)
					notify("Align failed", err.Error())
					continue
				}
				log.Printf("Align %s -> %s: %d copied, %d skipped, %d conflict(s).", pr.src.Name, pr.dst.Name, report.CopiedCount, report.SkippedCount, report.ConflictCount)
				notify("Align complete", fmt.Sprintf("%d copied, %d skipped, %d conflict(s).", report.CopiedCount, report.SkippedCount, report.ConflictCount))
			}
		}(item, pair)
	}

	// Mark the currently-active profile now, and keep the marker fresh even if
	// the profile is changed outside the tray (e.g. opening Claude directly).
	go func() {
		last := "\x00" // sentinel so the first real detection always applies
		for {
			active, _ := plat.DetectRunningProfile()
			// Only act on a positive detection. Ignore "" (no process visible):
			// right after a switch the freshly launched app isn't in `ps` yet, and
			// unmarking here would blink off the marker the switch handler just set.
			if active != "" && active != last {
				markActive(profileItems, active)
				last = active
			}
			time.Sleep(4 * time.Second)
		}
	}()

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
		for range mOpenLogs.ClickedCh {
			_ = exec.Command("open", core.LogDir()).Run()
		}
	}()

	go func() {
		for range mUpdate.ClickedCh {
			go checkForUpdate(false)
		}
	}()

	go func() {
		for range mRename.ClickedCh {
			renameFlow(profileItems)
		}
	}()

	go func() {
		for range mLogin.ClickedCh {
			toggleLoginItem(mLogin)
		}
	}()

	go func() {
		for range mAutoAlign.ClickedCh {
			toggleAutoAlign(mAutoAlign)
		}
	}()

	// Auto-update: check on startup and periodically.
	startUpdateChecker()

	go func() {
		<-mQuit.ClickedCh
		systray.Quit()
	}()
}

func onExit() {
	log.Println("Multi-Claude Switcher Tray exited cleanly.")
}

// markActive relabels the profile menu items so the one matching activePath is
// shown as the current profile (checkmark + "(current)"), and the rest as
// "Switch to: …". Called at startup, after a switch, and by the background
// poller so the marker stays correct however the profile changed.
// markMu serializes menu-item relabeling, which is driven concurrently by the
// switch handler, the rename handler, and the background poller.
var markMu sync.Mutex

func markActive(items map[*systray.MenuItem]*platform.ProfileInfo, activePath string) {
	markMu.Lock()
	defer markMu.Unlock()
	for item, p := range items {
		name := core.DisplayName(p.Name)
		if samePath(p.Path, activePath) {
			item.SetTitle(fmt.Sprintf("✓ %s  (current)", name))
			item.Check()
		} else {
			item.SetTitle(fmt.Sprintf("Switch to: %s", name))
			item.Uncheck()
		}
	}
}

// samePath reports whether two profile paths refer to the same directory.
func samePath(a, b string) bool {
	return a != "" && b != "" && filepath.Clean(a) == filepath.Clean(b)
}

// renameFlow asks the user (via native dialogs) which profile to rename and the
// new display name, persists it, and refreshes the menu labels.
func renameFlow(items map[*systray.MenuItem]*platform.ProfileInfo) {
	// Collect the folder names currently shown, each labeled with its display name.
	var labels []string
	labelToFolder := map[string]string{}
	for _, p := range items {
		label := fmt.Sprintf("%s  (%s)", core.DisplayName(p.Name), p.Name)
		labels = append(labels, label)
		labelToFolder[label] = p.Name
	}

	chosenLabel := chooseFromList(labels, "Which profile do you want to rename?")
	if chosenLabel == "" {
		return // cancelled
	}
	folder := labelToFolder[chosenLabel]

	newName := askText(fmt.Sprintf("New display name for %q:", folder), core.DisplayName(folder))
	if newName == "" {
		return // cancelled or empty
	}
	if err := core.SetProfileName(folder, newName); err != nil {
		log.Printf("Rename failed: %v", err)
		notify("Rename failed", err.Error())
		return
	}
	log.Printf("Renamed profile %s -> %q", folder, newName)

	// Refresh labels (preserve the current-profile marker).
	active, _ := plat.DetectRunningProfile()
	markActive(items, active)
}

// toggleLoginItem flips the start-at-login LaunchAgent and syncs the menu
// checkbox. When enabling, it registers the resolved path of the running
// executable (the binary inside the .app bundle once packaged).
func toggleLoginItem(m *systray.MenuItem) {
	if core.LoginItemEnabled() {
		if err := core.DisableLoginItem(); err != nil {
			log.Printf("Disable login item failed: %v", err)
			notify("Couldn't update Start at Login", err.Error())
			return
		}
		m.Uncheck()
		log.Println("Start at Login disabled")
		return
	}

	exe, err := os.Executable()
	if err != nil {
		log.Printf("Cannot resolve executable for login item: %v", err)
		notify("Couldn't update Start at Login", err.Error())
		return
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	if err := core.EnableLoginItem(exe); err != nil {
		log.Printf("Enable login item failed: %v", err)
		notify("Couldn't update Start at Login", err.Error())
		return
	}
	m.Check()
	log.Printf("Start at Login enabled (%s)", exe)
}

// chooseFromList shows a native macOS "choose from list" dialog and returns the
// selected item, or "" if cancelled.
func chooseFromList(options []string, prompt string) string {
	var quoted []string
	for _, o := range options {
		quoted = append(quoted, osaQuote(o))
	}
	script := fmt.Sprintf(`set sel to choose from list {%s} with prompt %s
if sel is false then
	return ""
end if
return item 1 of sel`, strings.Join(quoted, ", "), osaQuote(prompt))
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// askText shows a native text-input dialog and returns the entered string, or
// "" if cancelled.
func askText(prompt, defaultAnswer string) string {
	script := fmt.Sprintf(`set r to display dialog %s default answer %s buttons {"Cancel", "OK"} default button "OK" cancel button "Cancel" with title "Multi-Claude Switcher"
return text returned of r`, osaQuote(prompt), osaQuote(defaultAnswer))
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return "" // cancelled
	}
	return strings.TrimSpace(string(out))
}

// confirmSwitch shows a native macOS confirmation dialog. Returns true only if
// the user explicitly confirms; the "Cancel" button (osascript non-zero exit)
// returns false, so a mis-click never kills a running Claude session.
func confirmSwitch(targetName string) bool {
	msg := fmt.Sprintf("Switch to %q? Claude Desktop will be closed and reopened with this profile.", targetName)
	script := fmt.Sprintf(`display dialog %s buttons {"Cancel", "Switch"} default button "Switch" cancel button "Cancel" with title "Multi-Claude Switcher"`, osaQuote(msg))
	return exec.Command("osascript", "-e", script).Run() == nil
}

// confirmAlign asks before a manual align, which closes and reopens Claude
// Desktop on the SAME account (it copies data, it does not switch accounts).
func confirmAlign(src, dst string) bool {
	msg := fmt.Sprintf("Copy %q's sessions into %q? Claude Desktop will be closed, synced, and reopened on the account you're using now.", src, dst)
	script := fmt.Sprintf(`display dialog %s buttons {"Cancel", "Sync"} default button "Sync" cancel button "Cancel" with title "Multi-Claude Switcher"`, osaQuote(msg))
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
