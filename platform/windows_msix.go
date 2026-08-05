//go:build windows

package platform

import (
	"encoding/json"
	"errors"
	"fmt"
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

func msixSlotDir(roaming string) string { return filepath.Join(roaming, msixSlotName) }

// msixProfilePath returns where the profile called identity keeps its data: the
// shared slot if it is the current profile, otherwise its parked directory.
//
// This is the only correct way to get a Store profile's path, and the inverse does
// not exist: filepath.Base of the slot is always "Claude", whatever the profile is
// called.
//
// The comparison folds case, matching msixSwapToIn and msixValidateNameIn. Two
// Store profiles cannot differ only in case — validation rejects that, and the
// filesystem is case-insensitive anyway — so folding cannot pick the wrong one,
// while exact matching would resolve a differently-cased current name to a parked
// path that does not exist.
func msixProfilePath(roaming, identity string) string {
	if strings.EqualFold(readMSIXStateIn(roaming).Current, identity) {
		return msixSlotDir(roaming)
	}
	return filepath.Join(msixContainerDir(roaming), identity)
}

// msixIsSlotOccupant reports whether identity is the profile currently living in
// the shared slot. Extracted so PrepareArchive and PrepareRemove cannot come to
// disagree about what "the occupant" means.
//
// Case-folded for the same reason msixProfilePath folds: Store profiles cannot
// differ only in case, and exact matching would treat a differently-cased current
// name as a parked profile.
func msixIsSlotOccupant(roaming, identity string) bool {
	return strings.EqualFold(readMSIXStateIn(roaming).Current, identity)
}

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

// msixStateRecorded reports whether MCS has ever written its slot state for this
// install. It is distinct from reading the state: readMSIXStateIn substitutes the
// default name when there is no file, so a caller that has to tell "MCS parked
// the slot and this profile belongs in it" apart from "MCS has never run here"
// cannot get that from the returned value.
func msixStateRecorded(roaming string) bool {
	_, err := os.Stat(msixStatePath(roaming))
	return err == nil
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
	// "Access is denied" says nothing about who is holding the directory, and the
	// answer is nearly always a program loaded from inside it. Name them in the
	// error, not just the log: this text is what msixSwapToIn wraps into the
	// notification the user sees, and "quit chrome-native-host.exe" is something
	// they can act on where "Access is denied" is not.
	if holders := msixDescribeHolders(from); holders != "" {
		log.Printf("[msix] still running from inside %q: %s", filepath.Base(from), holders)
		return fmt.Errorf("%w. Still running from inside it: %s", err, holders)
	}
	return err
}

// msixStrayPrefix names directories moved out of the slot's way. They sit in
// <roaming>, deliberately not in the profile container: msixFindProfilesIn reads
// only the slot and the container, so a stray is preserved without turning up in
// the account list as a profile the user never made.
const msixStrayPrefix = ".mcs-stray-"

// msixStrayDir returns an unused <roaming>\.mcs-stray-N. Numbered rather than
// timestamped so the name is reproducible in tests.
func msixStrayDir(roaming string) (string, error) {
	for n := 1; n <= 100; n++ {
		p := filepath.Join(roaming, fmt.Sprintf("%s%d", msixStrayPrefix, n))
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p, nil
		}
	}
	return "", fmt.Errorf("too many %s* directories left in %s; move or remove them", msixStrayPrefix, roaming)
}

// msixClearSlot frees the slot path so a profile can be renamed into it.
//
// The slot is supposed to be free by now: parking moved the live profile out.
// Anything sitting there appeared DURING the swap, and in practice only one
// thing does that — Claude, which recreates its data directory within seconds of
// starting. Windows fails a rename onto an existing directory with a bare
// "Access is denied", which is exactly what made this read as a permissions
// problem when it is really a race.
//
// The intruder is moved aside, never deleted. The user may have completed a
// sign-in in that window, and that is real data.
func msixClearSlot(roaming string) error {
	slot := msixSlotDir(roaming)
	if _, err := os.Stat(slot); err != nil {
		if os.IsNotExist(err) {
			return nil // nothing in the way, which is the normal case
		}
		// A permission or path error is not "the slot is free". Treating it as one
		// hands the caller a rename onto a directory that does exist, and the error
		// it fails with is worse than this one — it is the bare "Access is denied"
		// this function exists to prevent.
		return fmt.Errorf("couldn't check whether the live slot at %s is free: %w", slot, err)
	}
	stray, err := msixStrayDir(roaming)
	if err != nil {
		return err
	}
	if err := renameWithRetry(slot, stray); err != nil {
		return fmt.Errorf("Claude recreated its data folder during the switch and it could not be moved out of the way. Fully quit Claude and try again (%w)", err)
	}
	log.Printf("[msix] Claude recreated the slot during the switch; moved it aside to %q (kept, not deleted)", filepath.Base(stray))
	return nil
}

// msixActivate renames dir into the slot, clearing whatever has appeared there
// first. Every rename INTO the slot goes through here, rollbacks included: a
// rollback races Claude for the same directory the activation just lost to it.
func msixActivate(roaming, dir string) error {
	if err := msixClearSlot(roaming); err != nil {
		return err
	}
	return renameWithRetry(dir, msixSlotDir(roaming))
}

// msixRecreatedName is what the slot is called once a switch has failed in both
// directions and the directory sitting there is one Claude made, not a profile
// the user ever named.
const msixRecreatedName = "Recreated by Claude"

// msixUnusedProfileName returns base, or base with a number appended, so that it
// does not collide with a parked profile.
func msixUnusedProfileName(roaming, base string) string {
	container := msixContainerDir(roaming)
	for n := 1; n <= 100; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s %d", base, n)
		}
		if _, err := os.Stat(filepath.Join(container, name)); os.IsNotExist(err) {
			return name
		}
	}
	return base
}

// msixRecordStrandedSlot makes state.json describe the disk after BOTH the move
// into the slot and its rollback failed, and returns the error the user is shown.
// what names the operation that failed ("switch to \"Work\"") and only appears in
// that message, since both callers arrive at the same disk state by different
// routes.
//
// At that point the slot holds whatever Claude recreated — that is why both
// renames failed — while the profile that used to be in it is parked under its
// own name. Leaving state.json naming that parked profile as the slot occupant is
// what turns a bad switch into a dead end:
//
//   - msixFindProfilesIn lists the slot as st.Current AND every directory in the
//     container, so the one profile appears twice under the same name;
//   - a switch back to it takes msixSwapToIn's `targetName == current` early
//     return and reports success without moving anything;
//   - every later sync or backup then works on Claude's empty directory while the
//     real data sits unreachable through the UI.
//
// Naming the slot for what it actually holds costs no new field and no new state
// machine. The parked profile becomes an ordinary inactive profile, listed once,
// and switching to it does the real work — which is exactly the recovery the user
// needs and can reach from the account list.
func msixRecordStrandedSlot(roaming string, st msixState, what, current, parked string, rollbackErr error) error {
	slot := msixSlotDir(roaming)
	stranded := msixUnusedProfileName(roaming, msixRecreatedName)
	st.Current = stranded
	st.PendingMigrateFrom = ""
	if werr := writeMSIXStateIn(roaming, st); werr != nil {
		// Nothing left to record with. Say plainly what is where, including the
		// directory in the slot: "move your folder back" cannot be followed while
		// something else is sitting in the destination.
		return fmt.Errorf("couldn't %[1]s, %[2]q could not be put back (%[3]v), and the record of which profile is live could not be updated either (%[4]v). Nothing is lost: %[2]q is at %[5]s, and the live slot at %[6]s holds a folder Claude recreated. Fully quit Claude, move that folder out of the way, then move %[2]q's folder into its place",
			what, current, rollbackErr, werr, parked, slot)
	}
	log.Printf("[msix] %s failed and the rollback failed too; the slot holds a directory Claude recreated, now recorded as %q so %q stays listed and switchable", what, stranded, current)
	return fmt.Errorf("couldn't %[1]s, and %[2]q could not be put back into the live slot: %[3]w. Nothing is lost: %[2]q is still listed, so fully quit Claude and switch to it to put it back. The live slot currently holds a folder Claude recreated, listed as %[4]q",
		what, current, rollbackErr, stranded)
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
			return fmt.Errorf("couldn't switch: Claude is still holding its files. Fully quit Claude (check the tray / Task Manager) and try again. (%w)", err)
		}
		slotParked = true
	}
	// 2. Activate the target into the slot; roll back the parking on failure.
	//
	// The rollback's own failure used to be discarded, which is the worst outcome
	// this code can produce: the slot is gone, the user's profile is parked under
	// a name they never chose, and nothing says so. Say so.
	if err := msixActivate(roaming, targetDir); err != nil {
		if slotParked {
			if rb := msixActivate(roaming, parked); rb != nil {
				return msixRecordStrandedSlot(roaming, st, fmt.Sprintf("switch to %q", targetName), current, parked, rb)
			}
		}
		return fmt.Errorf("couldn't switch to %q: %w", targetName, err)
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
	// Validation trims a local copy; this function stores its argument. Without
	// this, " Work " becomes a profile identity with spaces around it.
	newName = strings.TrimSpace(newName)
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
			return fmt.Errorf("couldn't save your current account: Claude is still holding its files. Fully quit Claude (check the tray / Task Manager) and try again. (%w)", err)
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
			if rb := msixActivate(roaming, parked); rb != nil {
				// The same dead end msixSwapToIn avoids, reached from the create path
				// instead: state.json still names current as the slot occupant while the
				// slot holds whatever Claude recreated, so current is listed twice and a
				// switch back to it short-circuits on "already active".
				//
				// Recording it means writing state again right after a write failed. That
				// is worth doing anyway: the first failure can be transient — a lock, a
				// momentarily full disk — and the retry then leaves the user switchable
				// rather than stranded. When it fails again, msixRecordStrandedSlot's own
				// write-failure branch produces the message this used to hand-roll, and
				// that one names all three locations including the folder in the slot.
				return msixRecordStrandedSlot(roaming, st, fmt.Sprintf("set up the new profile %q", newName), current, parked, rb)
			}
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
		copied, _ = CopyDirMerge(fromBucket, dstBucket)
	}
	st.PendingMigrateFrom = ""
	_ = writeMSIXStateIn(roaming, st)
	return copied, true
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
