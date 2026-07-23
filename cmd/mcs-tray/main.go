package main

import (
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

	// Refuse to start a second menu-bar instance (two icons / two auto-updaters).
	// The self-updater relaunch is exempt via the skip flag, since the old
	// instance is still quitting when the new one starts.
	if !hasSkipInstanceFlag(os.Args) && anotherInstanceRunning() {
		log.Printf("Another Multi-Claude Switcher tray is already running; exiting.")
		notify("Multi-Claude Switcher is already running", "It's already in your menu bar — this extra copy will close.")
		return
	}

	plat = platform.New()
	bm = core.NewBackupManager("")
	switcher = core.NewSwitcher(plat, bm)

	systray.Run(onReady, onExit)
}

func onReady() {
	// Set the menu-bar / tray icon (per-OS: a macOS template PNG that the system
	// recolors for light/dark, a Windows .ico via SetIcon).
	setTrayIcon()
	systray.SetTooltip("Multi-Claude Switcher")

	// Profiles section
	mProfilesHeader := systray.AddMenuItem("Available Profiles:", "")
	mProfilesHeader.Disable()

	profiles, err := plat.FindProfiles()
	if err != nil {
		log.Printf("Error finding profiles: %v", err)
	}

	// Each profile is its own submenu with two items: "Switch to this profile"
	// and "Rename…". The parent item shows the account's display name and carries
	// the active marker. On macOS, clicking a parent that has a submenu only opens
	// the submenu, so the switch action has to live in the child.
	type profileMenu struct {
		info    *platform.ProfileInfo
		parent  *systray.MenuItem
		mSwitch *systray.MenuItem
		mRename *systray.MenuItem
	}
	var profileMenus []profileMenu
	profileItems := make(map[*systray.MenuItem]*platform.ProfileInfo) // parent item -> info, for markActive
	for _, p := range profiles {
		if !p.HasSessionsDir && !p.Managed && p.Name != "Claude" && p.Name != "Claude_Profile2" {
			continue
		}
		// Empty tooltip on the parent: on macOS the parent's tooltip pops up next
		// to it when hovered and visually overlaps the first submenu item.
		parent := systray.AddMenuItem(profileTitle(core.DisplayName(p.Name), getAcctType(p.Path), false), "")
		mSwitchTo := parent.AddSubMenuItem("Switch to this profile", fmt.Sprintf("Switch the active profile to %s", p.Name))
		mRenameTo := parent.AddSubMenuItem("Rename…", "Give this profile a friendlier display name")
		profileItems[parent] = p
		profileMenus = append(profileMenus, profileMenu{info: p, parent: parent, mSwitch: mSwitchTo, mRename: mRenameTo})
	}

	// Store/MSIX build only: let the user add another account as a new profile
	// (the standalone build's profiles are ordinary sibling data dirs).
	var mNewProfile *systray.MenuItem
	if newProfileSupported() {
		mNewProfile = systray.AddMenuItem("New account profile…", "Add another Claude account you can switch to")
	}

	systray.AddSeparator()

	// Manual sync + its auto-mode toggle live in one submenu. Auto Sync sits at
	// the top of the submenu, above the manual "From … → To …" directions.
	mSync := systray.AddMenuItem("Sync sessions", "Copy one account's sessions into another (without switching accounts)")
	mAutoSync := mSync.AddSubMenuItemCheckbox("Auto Sync on Switch", "Sync both accounts automatically on every switch", core.AutoSyncOnSwitch())
	type alignPair struct{ src, dst *platform.ProfileInfo }
	alignItems := map[*systray.MenuItem]alignPair{}
	var shown []*platform.ProfileInfo
	for _, p := range profiles {
		if p.HasSessionsDir || p.Managed || p.Name == "Claude" || p.Name == "Claude_Profile2" {
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
	// While Auto Sync is on, the manual directions are redundant, so disable them.
	// The Auto Sync checkbox and the parent submenu stay usable so it can be
	// turned back off.
	setManualDirectionsEnabled := func(enabled bool) {
		for child := range alignItems {
			if enabled {
				child.Enable()
			} else {
				child.Disable()
			}
		}
	}
	setManualDirectionsEnabled(!core.AutoSyncOnSwitch())

	systray.AddSeparator()

	// Settings submenu
	mSettings := systray.AddMenuItem("Settings", "Preferences")
	mLogin := mSettings.AddSubMenuItemCheckbox("Start at Login", "Launch automatically when you log in", core.LoginItemEnabled())

	// Maintenance submenu
	mMaint := systray.AddMenuItem("Maintenance", "Backups, logs, updates")
	mBackup := mMaint.AddSubMenuItem("Backup All Profiles", "Take a snapshot backup of all profiles")
	mOpenBackups := mMaint.AddSubMenuItem("Open Backup Directory", "Open backup folder in Finder")
	mOpenLogs := mMaint.AddSubMenuItem("Open Log Folder", "Open the log folder in Finder")
	mUpdate := mMaint.AddSubMenuItem("Check for Updates…", "Check GitHub for a newer version and update")

	systray.AddSeparator()
	mAbout := systray.AddMenuItem("About", "About Multi-Claude Switcher")
	mQuit := systray.AddMenuItem("Quit", "Quit Multi-Claude Switcher Tray")

	// Handle menu item events in goroutines
	for _, pm := range profileMenus {
		// Switch to this profile.
		go func(pm profileMenu) {
			target := pm.info
			for range pm.mSwitch.ClickedCh {
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
					go func() { detectAccountTypes(profiles); markActive(profileItems, target.Path) }()
				}
			}
		}(pm)

		// Rename this profile (acts directly on it — no picker needed).
		go func(pm profileMenu) {
			for range pm.mRename.ClickedCh {
				renameProfile(pm.info, profileItems)
			}
		}(pm)
	}

	if mNewProfile != nil {
		go func() {
			for range mNewProfile.ClickedCh {
				runNewProfileFlow()
			}
		}()
	}

	for item, pair := range alignItems {
		go func(m *systray.MenuItem, pr alignPair) {
			for range m.ClickedCh {
				dstName := core.DisplayName(pr.dst.Name)
				confirmed := false
				if importTargetIsTeam(pr.dst.Path) {
					confirmed = confirmImportIntoTeam(dstName)
				} else {
					confirmed = confirmAlign(core.DisplayName(pr.src.Name), dstName)
				}
				if !confirmed {
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
				go func() { detectAccountTypes(profiles); markActive(profileItems, "") }()
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

	// Detect account types in the background (copies + reads each profile's Local
	// Storage), then refresh titles so the "🏢 Team" tag appears.
	go func() {
		detectAccountTypes(profiles)
		active, _ := plat.DetectRunningProfile()
		markActive(profileItems, active)
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
			openFolder(bm.BackupRootDir)
		}
	}()

	go func() {
		for range mOpenLogs.ClickedCh {
			openFolder(core.LogDir())
		}
	}()

	go func() {
		for range mUpdate.ClickedCh {
			go checkForUpdate(false)
		}
	}()

	go func() {
		for range mLogin.ClickedCh {
			toggleLoginItem(mLogin)
		}
	}()

	go func() {
		for range mAutoSync.ClickedCh {
			toggleAutoSync(mAutoSync, teamProfileNames(shown))
			// Reflect the new state on the manual directions. Read the persisted
			// value so a cancelled enable (warning dismissed) leaves it correct.
			setManualDirectionsEnabled(!core.AutoSyncOnSwitch())
		}
	}()

	// Auto-update: check on startup and periodically.
	startUpdateChecker()

	go func() {
		for range mAbout.ClickedCh {
			showAbout()
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

// markActive relabels the profile menu items so the one matching activePath is
// shown as the current profile (checkmark + "(current)"), and the rest as just
// their display name (clicking one switches to it; the tooltip says so). Called
// at startup, after a switch, and by the background poller so the marker stays
// correct however the profile changed.
// markMu serializes menu-item relabeling, which is driven concurrently by the
// switch handler, the rename handler, and the background poller.
var markMu sync.Mutex

func markActive(items map[*systray.MenuItem]*platform.ProfileInfo, activePath string) {
	markMu.Lock()
	defer markMu.Unlock()
	for item, p := range items {
		current := samePath(p.Path, activePath)
		item.SetTitle(profileTitle(core.DisplayName(p.Name), getAcctType(p.Path), current))
		if current {
			item.Check()
		} else {
			item.Uncheck()
		}
	}
}

// samePath reports whether two profile paths refer to the same directory.
func samePath(a, b string) bool {
	return a != "" && b != "" && filepath.Clean(a) == filepath.Clean(b)
}

// relaunchSelf starts a fresh copy of the tray (detached, with the instance-check
// skip flag) and quits this one. Used after an action that changes the profile
// set, since the systray menu is built once at startup and cannot grow new items
// afterward.
func relaunchSelf() {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("relaunch: cannot find own executable: %v", err)
		return
	}
	args := append([]string{}, os.Args[1:]...)
	if !hasSkipInstanceFlag(args) {
		args = append(args, relaunchSkipInstanceCheckFlag)
	}
	cmd := exec.Command(exe, args...)
	detachRelaunch(cmd) // detach so it outlives us
	if err := cmd.Start(); err != nil {
		log.Printf("relaunch failed: %v", err)
		return
	}
	systray.Quit()
}

// renameProfile prompts for a new display name for the given profile, persists
// it, and refreshes the menu labels (preserving the current-profile marker).
// It acts directly on the passed-in profile, so no picker dialog is needed.
func renameProfile(p *platform.ProfileInfo, items map[*systray.MenuItem]*platform.ProfileInfo) {
	newName := askText(fmt.Sprintf("New display name for %q:", p.Name), core.DisplayName(p.Name))
	if newName == "" {
		return // cancelled or empty
	}
	if err := core.SetProfileName(p.Name, newName); err != nil {
		log.Printf("Rename failed: %v", err)
		notify("Rename failed", err.Error())
		return
	}
	log.Printf("Renamed profile %s -> %q", p.Name, newName)

	// Refresh labels (preserve the current-profile marker).
	active, _ := plat.DetectRunningProfile()
	markActive(items, active)
}

// toggleLoginItem flips the start-at-login setting and syncs the menu checkbox.
// When enabling, it registers the resolved path of the running executable.
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

// confirmSwitch asks the user to confirm a switch. Returns true only on explicit
// confirmation, so a mis-click never kills a running Claude session.
func confirmSwitch(targetName string) bool {
	msg := fmt.Sprintf("Switch to %q? Claude Desktop will be closed and reopened with this profile.", targetName)
	return confirmDialog(msg, "Switch")
}

// confirmAlign asks before a manual align, which closes and reopens Claude
// Desktop on the SAME account (it copies data, it does not switch accounts).
func confirmAlign(src, dst string) bool {
	msg := fmt.Sprintf("Copy %q's sessions into %q? Claude Desktop will be closed, synced, and reopened on the account you're using now.", src, dst)
	return confirmDialog(msg, "Sync")
}

// importTargetIsTeam reports whether the sync destination is a Team account,
// whose Code sidebar is server-authoritative so a local import is a no-op.
func importTargetIsTeam(dstPath string) bool {
	return getAcctType(dstPath) == core.AccountTeam
}

// confirmImportIntoTeam warns that copying sessions into a Team account does
// nothing (the import half is a no-op), and asks whether to continue.
func confirmImportIntoTeam(dst string) bool {
	msg := fmt.Sprintf("%q is a Team account — Code conversations cannot be imported into it, so this sync's import half will do nothing. Continue anyway?", dst)
	return confirmDialog(msg, "Continue")
}

// showAbout displays a small About dialog with the app name, version, and link.
func showAbout() {
	lines := []string{
		"Multi-Claude Switcher",
		"Version " + core.Version,
		"",
		"Seamless multi-account switcher for Claude Desktop.",
		"github.com/miou1107/multi-claude-switcher",
	}
	infoDialog("About Multi-Claude Switcher", strings.Join(lines, "\n"))
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
