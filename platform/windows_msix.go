//go:build windows

package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	// PendingMigrateFrom names the just-parked profile whose saved sessions should
	// be brought into the fresh profile once the user signs into their other
	// account. Empty when there is nothing to migrate. Cleared after the copy (or
	// once the user switches away).
	PendingMigrateFrom string `json:"pending_migrate_from,omitempty"`
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

// renameWithRetry retries a directory rename to ride out the window after
// TerminateApp where the just-closed multi-process Claude still holds handles
// into the dir (Windows refuses to rename a directory with any open handle
// inside it). Up to ~20s, since a 12-process Electron app's handles can take
// several seconds to release after the processes exit.
func renameWithRetry(from, to string) error {
	var err error
	for i := 0; i < 40; i++ {
		if err = os.Rename(from, to); err == nil {
			if i > 0 {
				log.Printf("[msix] rename %q -> %q succeeded after %d retries", filepath.Base(from), filepath.Base(to), i)
			}
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	log.Printf("[msix] rename %q -> %q FAILED after retries: %v", filepath.Base(from), filepath.Base(to), err)
	return err
}

// removeIfEmpty deletes dir only if it is empty (best effort). Used to clean up a
// container dir created just before a swap that then failed.
func removeIfEmpty(dir string) {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
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

	log.Printf("[msix] switch: %q -> %q", current, targetName)
	// 1. Park the current slot (it may be absent if the user removed it).
	slotParked := false
	if _, err := os.Stat(slot); err == nil {
		if err := renameWithRetry(slot, parked); err != nil {
			return fmt.Errorf("couldn't switch — Claude is still holding its files. Fully quit Claude (check the tray / Task Manager) and try again. (%w)", err)
		}
		slotParked = true
	}
	// 2. Activate the target into the slot; roll back the parking on failure.
	if err := renameWithRetry(targetDir, slot); err != nil {
		if slotParked {
			_ = renameWithRetry(parked, slot)
		}
		return fmt.Errorf("couldn't switch to %q — Claude is still holding its files. Fully quit Claude and try again. (%w)", targetName, err)
	}
	// 3. Record the new occupant. Dirs are already swapped, so a write failure
	//    only mislabels the slot (no data loss); surface it so the user knows.
	//    Switching to an existing profile abandons any pending first-login migration.
	st.Current = targetName
	st.PendingMigrateFrom = ""
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
	log.Printf("[msix] new profile %q: parking current %q, roaming=%q", newName, current, roaming)

	if err := os.MkdirAll(container, 0o755); err != nil {
		return err
	}
	parked := filepath.Join(container, current)
	if _, err := os.Stat(parked); err == nil {
		removeIfEmpty(container)
		return fmt.Errorf("cannot save current profile %q: %q already exists (unexpected)", current, parked)
	}
	didPark := false
	if _, err := os.Stat(slot); err == nil {
		if err := renameWithRetry(slot, parked); err != nil {
			removeIfEmpty(container)
			return fmt.Errorf("couldn't save your current account — Claude is still holding its files. Fully quit Claude (check the tray / Task Manager) and try again. (%w)", err)
		}
		didPark = true
	}
	st.Current = newName
	if didPark {
		// After the user signs into the new account, bring that account's saved
		// sessions (if any) over from the profile we just parked.
		st.PendingMigrateFrom = current
	}
	if err := writeMSIXStateIn(roaming, st); err != nil {
		if didPark {
			_ = renameWithRetry(parked, slot) // roll back the parking
		}
		return fmt.Errorf("save state: %w", err)
	}
	return nil
}

// msixAttemptMigrationIn checks whether the freshly created profile has been
// signed in yet; if so it copies that account's previously saved sessions from
// the parked source profile into the new slot and clears the pending flag. It
// returns (filesCopied, done) — done is false only while the user has not signed
// in yet, so the caller keeps polling.
func msixAttemptMigrationIn(roaming string) (copied int, done bool) {
	st := readMSIXStateIn(roaming)
	from := st.PendingMigrateFrom
	if from == "" {
		return 0, true
	}
	// Not signed in yet? Keep waiting.
	uuid, err := GetProfileAccountUUID(msixSlotDir(roaming))
	if err != nil || uuid == "" {
		return 0, false
	}
	// Copy that account's bucket from the parked source, if it has one.
	fromBucket := filepath.Join(GetProfileSessionsDir(filepath.Join(msixContainerDir(roaming), from)), uuid)
	if fi, e := os.Stat(fromBucket); e == nil && fi.IsDir() {
		dstBucket := filepath.Join(GetProfileSessionsDir(msixSlotDir(roaming)), uuid)
		copied, _ = copyDirMerge(fromBucket, dstBucket)
	}
	st.PendingMigrateFrom = ""
	_ = writeMSIXStateIn(roaming, st)
	return copied, true
}

// copyDirMerge recursively copies files from src into dst (creating dst), skipping
// any file that already exists in dst, and returns the number of files copied.
func copyDirMerge(src, dst string) (int, error) {
	copied := 0
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if _, e := os.Stat(target); e == nil {
			return nil // don't clobber anything already there
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := copyFile(path, target); err != nil {
			return err
		}
		copied++
		return nil
	})
	return copied, err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, cerr := io.Copy(out, in)
	if closeErr := out.Close(); cerr == nil {
		cerr = closeErr
	}
	return cerr
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

// MSIXPendingMigration reports whether a first-login session migration is queued
// (i.e. a profile was just created and we are waiting for the user to sign in).
func MSIXPendingMigration() bool {
	roaming := msixRoamingDir()
	if roaming == "" {
		return false
	}
	return readMSIXStateIn(roaming).PendingMigrateFrom != ""
}

// MSIXAttemptMigration tries to complete a queued migration; see
// msixAttemptMigrationIn. done is false while the user has not yet signed in.
func MSIXAttemptMigration() (copied int, done bool) {
	roaming := msixRoamingDir()
	if roaming == "" {
		return 0, true
	}
	return msixAttemptMigrationIn(roaming)
}

// MSIXCancelMigration clears a queued migration (used when the poller gives up).
func MSIXCancelMigration() {
	roaming := msixRoamingDir()
	if roaming == "" {
		return
	}
	st := readMSIXStateIn(roaming)
	if st.PendingMigrateFrom != "" {
		st.PendingMigrateFrom = ""
		_ = writeMSIXStateIn(roaming, st)
	}
}

// MSIXUnconfiguredMultiAccount reports the "you've used more than one account in a
// single install, but haven't set up switching yet" state: the active profile has
// two or more account buckets and no profile has been parked yet. Used to nudge
// the user toward setting up their other account.
func MSIXUnconfiguredMultiAccount() bool {
	roaming := msixRoamingDir()
	if roaming == "" {
		return false
	}
	if entries, err := os.ReadDir(msixContainerDir(roaming)); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				return false // already has a parked profile → already configured
			}
		}
	}
	n := 0
	if entries, err := os.ReadDir(GetProfileSessionsDir(msixSlotDir(roaming))); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				n++
			}
		}
	}
	return n >= 2
}
