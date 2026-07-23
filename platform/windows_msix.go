//go:build windows

package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Store / MSIX support.
//
// The Microsoft-Store (MSIX) build of Claude Desktop cannot be launched with a
// custom --user-data-dir the way the standalone build can (it has no directly
// invocable claude.exe on PATH, and its data dir is virtualized). So instead of
// "launch with a different data dir", MCS switches accounts by SWAPPING the
// single live data directory in place:
//
//	<roaming>\Claude              -> the ACTIVE profile's data ("the slot")
//	<roaming>\.mcs-profiles\<name> -> each INACTIVE profile, parked here
//	<roaming>\.mcs-profiles\state.json -> { "current": "<name of slot occupant>" }
//
// where <roaming> is …\Packages\Claude_<hash>\LocalCache\Roaming (the real
// backing store the MSIX runtime redirects %APPDATA%\Claude to). A switch closes
// Claude, renames the slot aside to .mcs-profiles\<current>, renames
// .mcs-profiles\<target> into the slot, then relaunches the packaged app via its
// AppUserModelID. All moves are same-volume directory renames — atomic, fast, and
// reversible; no data is ever deleted, and a failed activation rolls the parking
// back. The core move logic below takes an explicit roaming dir so it is unit
// tested without a real Claude install.

const (
	msixSlotName      = "Claude"        // the active-profile dir name inside <roaming>
	msixContainerName = ".mcs-profiles" // holds parked (inactive) profiles + state
	msixStateName     = "state.json"
	msixDefaultName   = "Claude" // implied name of the pre-existing bare slot
	msixAppID         = "Claude" // Application Id in the package manifest
)

// msixPackageDir returns the installed Store-build package directory
// (…\Packages\Claude_<hash>) that actually holds a LocalCache\Roaming, or "".
func msixPackageDir() string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(local, "Packages", "Claude_*"))
	for _, m := range matches {
		if fi, err := os.Stat(filepath.Join(m, "LocalCache", "Roaming")); err == nil && fi.IsDir() {
			return m
		}
	}
	return ""
}

// msixRoamingDir returns the Store build's real roaming data root, or "".
func msixRoamingDir() string {
	pkg := msixPackageDir()
	if pkg == "" {
		return ""
	}
	return filepath.Join(pkg, "LocalCache", "Roaming")
}

// msixAUMID returns the AppUserModelID used to launch the packaged app, or "".
func msixAUMID() string {
	pkg := msixPackageDir()
	if pkg == "" {
		return ""
	}
	return filepath.Base(pkg) + "!" + msixAppID // e.g. Claude_pzs8sxrjxfjjc!Claude
}

func msixSlotDir(roaming string) string      { return filepath.Join(roaming, msixSlotName) }
func msixContainerDir(roaming string) string { return filepath.Join(roaming, msixContainerName) }
func msixStatePath(roaming string) string {
	return filepath.Join(msixContainerDir(roaming), msixStateName)
}

type msixState struct {
	Current string `json:"current"`
}

// readMSIXStateIn reads the current-profile marker, defaulting to the bare-slot
// name when no state has been written yet.
func readMSIXStateIn(roaming string) msixState {
	var s msixState
	if b, err := os.ReadFile(msixStatePath(roaming)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if strings.TrimSpace(s.Current) == "" {
		s.Current = msixDefaultName
	}
	return s
}

func writeMSIXStateIn(roaming string, s msixState) error {
	if err := os.MkdirAll(msixContainerDir(roaming), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(msixStatePath(roaming), b, 0o644)
}

// renameWithRetry retries a directory rename to ride out the brief window after
// TerminateApp where a just-closed process still holds a handle into the dir.
func renameWithRetry(from, to string) error {
	var err error
	for i := 0; i < 20; i++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return err
}

// msixValidateNameIn checks that name is usable as a new profile folder: non-empty,
// filesystem-safe, not the reserved slot name, and not colliding with the current
// profile or an existing parked one.
func msixValidateNameIn(roaming, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("profile name is empty")
	}
	if strings.EqualFold(name, msixSlotName) {
		return fmt.Errorf("%q is a reserved name, pick another", msixSlotName)
	}
	if strings.ContainsAny(name, `\/:*?"<>|`) || strings.HasPrefix(name, ".") {
		return errors.New(`profile name can't contain \ / : * ? " < > | or start with a dot`)
	}
	if strings.EqualFold(name, readMSIXStateIn(roaming).Current) {
		return fmt.Errorf("%q is already the current profile", name)
	}
	if _, err := os.Stat(filepath.Join(msixContainerDir(roaming), name)); err == nil {
		return fmt.Errorf("a profile named %q already exists", name)
	}
	return nil
}

// msixSwapToIn makes the parked profile targetName the active one: it parks the
// current slot into .mcs-profiles\<current> and moves .mcs-profiles\<targetName>
// into the slot, updating state. On any failure it rolls the parking back so the
// slot is never left empty. It does NOT launch the app. Caller must have stopped
// Claude first.
func msixSwapToIn(roaming, targetName string) error {
	slot := msixSlotDir(roaming)
	container := msixContainerDir(roaming)
	st := readMSIXStateIn(roaming)
	current := st.Current

	if strings.EqualFold(targetName, current) {
		return nil // already active
	}
	targetDir := filepath.Join(container, targetName)
	if fi, err := os.Stat(targetDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("profile %q not found", targetName)
	}
	if err := os.MkdirAll(container, 0o755); err != nil {
		return err
	}
	parked := filepath.Join(container, current)
	if _, err := os.Stat(parked); err == nil {
		return fmt.Errorf("cannot save current profile %q: %q already exists (unexpected)", current, parked)
	}

	// 1. Park the current slot (it may be absent if the user removed it).
	slotParked := false
	if _, err := os.Stat(slot); err == nil {
		if err := renameWithRetry(slot, parked); err != nil {
			return fmt.Errorf("save current profile (is Claude fully closed?): %w", err)
		}
		slotParked = true
	}
	// 2. Activate the target into the slot; roll back the parking on failure.
	if err := renameWithRetry(targetDir, slot); err != nil {
		if slotParked {
			_ = renameWithRetry(parked, slot)
		}
		return fmt.Errorf("activate profile %q (is Claude fully closed?): %w", targetName, err)
	}
	// 3. Record the new occupant. Dirs are already swapped, so a write failure
	//    only mislabels the slot (no data loss); surface it so the user knows.
	st.Current = targetName
	if err := writeMSIXStateIn(roaming, st); err != nil {
		return fmt.Errorf("profiles swapped but saving state failed: %w", err)
	}
	return nil
}

// msixParkForNewIn parks the current slot under its name and points state at
// newName, leaving the slot absent so the packaged app creates a fresh, signed-out
// data dir on next launch. It does NOT launch the app. Caller must have stopped
// Claude first.
func msixParkForNewIn(roaming, newName string) error {
	if err := msixValidateNameIn(roaming, newName); err != nil {
		return err
	}
	slot := msixSlotDir(roaming)
	container := msixContainerDir(roaming)
	st := readMSIXStateIn(roaming)
	current := st.Current

	if err := os.MkdirAll(container, 0o755); err != nil {
		return err
	}
	parked := filepath.Join(container, current)
	if _, err := os.Stat(parked); err == nil {
		return fmt.Errorf("cannot save current profile %q: %q already exists (unexpected)", current, parked)
	}
	if _, err := os.Stat(slot); err == nil {
		if err := renameWithRetry(slot, parked); err != nil {
			return fmt.Errorf("save current profile (is Claude fully closed?): %w", err)
		}
	}
	st.Current = newName
	if err := writeMSIXStateIn(roaming, st); err != nil {
		_ = renameWithRetry(parked, slot) // roll back the parking
		return fmt.Errorf("save state: %w", err)
	}
	return nil
}

// msixLaunch reopens the packaged Claude Desktop via its AppUserModelID. explorer
// launches the Store app (a GUI process), so there is no console window.
func msixLaunch() error {
	aumid := msixAUMID()
	if aumid == "" {
		return errors.New("could not locate the Store Claude Desktop package to launch")
	}
	return exec.Command("explorer.exe", `shell:AppsFolder\`+aumid).Start()
}

// --- Exported entry points used by the tray (Store build only) ---

// MSIXAvailable reports whether the Store build is the active target: the
// standalone build is preferred when both are present.
func MSIXAvailable() bool {
	if _, err := findClaudeExecutable(); err == nil {
		return false // standalone present, use the --user-data-dir path instead
	}
	return msixRoamingDir() != ""
}

// MSIXCurrentName returns the display/folder name of the currently active Store
// profile, or "" if the Store build is not present.
func MSIXCurrentName() string {
	roaming := msixRoamingDir()
	if roaming == "" {
		return ""
	}
	return readMSIXStateIn(roaming).Current
}

// MSIXNewProfile saves the current account as a parked profile and opens a fresh,
// signed-out Claude under newName so the user can log into another account. The
// caller must have terminated Claude Desktop first.
func MSIXNewProfile(newName string) error {
	roaming := msixRoamingDir()
	if roaming == "" {
		return errors.New("no Store Claude Desktop found")
	}
	if err := msixParkForNewIn(roaming, strings.TrimSpace(newName)); err != nil {
		return err
	}
	return msixLaunch()
}
