//go:build windows

package platform

// claudelink_windows.go is the Windows side of claude:// link handling (issue
// #60): asking Windows which app opens the links, offering the switcher as one
// that can, showing Windows' own window where the user picks it, and passing a
// link on to the Claude of the right account. claudelink.go says why.

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	modShlwapi                     = windows.NewLazySystemDLL("shlwapi.dll")
	procAssocQueryStringW          = modShlwapi.NewProc("AssocQueryStringW")
	modShell32                     = windows.NewLazySystemDLL("shell32.dll")
	procSHChangeNotify             = modShell32.NewProc("SHChangeNotify")
	modKernel32                    = windows.NewLazySystemDLL("kernel32.dll")
	procGetPackagesByPackageFamily = modKernel32.NewProc("GetPackagesByPackageFamily")
	procGetUserDefaultUILanguage   = modKernel32.NewProc("GetUserDefaultUILanguage")
)

const (
	assocfIsProtocol = 0x00001000 // ASSOCF_IS_PROTOCOL
	assocstrProgID   = 20         // ASSOCSTR_PROGID

	shcneAssocChanged = 0x08000000 // SHCNE_ASSOCCHANGED
	shcnfIDList       = 0x0000     // SHCNF_IDLIST
)

// claudeLinkUserChoiceKey is where Windows keeps the app the user picked for
// claude:// links. Windows writes it, with a hash it checks; the switcher only
// ever reads or removes it.
const claudeLinkUserChoiceKey = `Software\Microsoft\Windows\Shell\Associations\UrlAssociations\claude\UserChoice`

// storePackageName is the package name of the Store build the switcher drives
// through its alias (see findClaudeExecutable).
const storePackageName = "AnthropicPBC.Claude"

// claudeLinkProgIDNow returns the ProgID Windows resolves claude:// to for the
// current user, or "" when nothing is registered for it. A packaged app's
// handler reads as AppX followed by a hash.
func claudeLinkProgIDNow() string {
	scheme, _ := windows.UTF16PtrFromString("claude")
	verb, _ := windows.UTF16PtrFromString("open")
	buf := make([]uint16, 512)
	n := uint32(len(buf))
	hr, _, _ := procAssocQueryStringW.Call(assocfIsProtocol, assocstrProgID,
		uintptr(unsafe.Pointer(scheme)), uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if hr != 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// claudeLinkUserChoice returns the ProgID the user picked for claude:// links,
// or "" when they have not picked one.
func claudeLinkUserChoice() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, claudeLinkUserChoiceKey, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, _ := k.GetStringValue("ProgId")
	return v
}

// ClaudeLinkState reports whether claude:// links reach the account in use.
func ClaudeLinkState() ClaudeLinkStatus {
	return claudeLinkStatusFor(claudeLinkProgIDNow(), claudeLinkUserChoice(),
		isLaunchableFile(storeAliasPath()), (&WindowsPlatform{}).isMSIX())
}

// RegisterClaudeLinkHandler offers the running switcher to Windows as an app
// that can open claude:// links. It changes nothing about which app does: that
// is the user's pick in the window AskWindowsForClaudeLinkApp brings up. Run
// each time before asking, so the entry always names the switcher's current
// location.
func RegisterClaudeLinkHandler() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find the switcher's own path: %w", err)
	}
	for _, v := range claudeLinkRegistration(exe, windowsUILanguage()) {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, v.key, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("register the claude:// handler (%s): %w", v.key, err)
		}
		err = k.SetStringValue(v.name, v.value)
		k.Close()
		if err != nil {
			return fmt.Errorf("register the claude:// handler (%s %s): %w", v.key, v.name, err)
		}
	}
	procSHChangeNotify.Call(shcneAssocChanged, shcnfIDList, 0, 0)
	log.Printf("offered %s to Windows as a claude:// handler", exe)
	return nil
}

// RefreshClaudeLinkHandler re-registers the handler when it was registered
// before, so it names the running switcher. It registers nothing new: offering
// the switcher to Windows is for the moment the user asks for it.
func RefreshClaudeLinkHandler() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\`+ClaudeLinkProgID, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	k.Close()
	return RegisterClaudeLinkHandler()
}

// windowsUILanguage is Windows' display language, a LANGID.
func windowsUILanguage() uint16 {
	lang, _, _ := procGetUserDefaultUILanguage.Call()
	return uint16(lang)
}

// ClaudeLinkDisplayName is how the switcher is named in Windows' "How do you
// want to open this?" window, so the guide can show the same words.
func ClaudeLinkDisplayName() string {
	return claudeLinkDisplayName(windowsUILanguage())
}

// UnregisterClaudeLinkHandler removes what RegisterClaudeLinkHandler wrote. The
// uninstaller runs it, so nothing is left offering an exe that is gone.
//
// The user's pick goes too when it is the switcher. Left behind, it names an
// app that no longer exists, and Windows answers the next link with a notice
// that an app default was reset.
func UnregisterClaudeLinkHandler() error {
	var errs []error
	if strings.EqualFold(claudeLinkUserChoice(), ClaudeLinkProgID) {
		if err := registry.DeleteKey(registry.CURRENT_USER, claudeLinkUserChoiceKey); err != nil {
			errs = append(errs, err)
		}
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\RegisteredApplications`, registry.SET_VALUE); err == nil {
		if err := k.DeleteValue(claudeLinkAppName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			errs = append(errs, err)
		}
		k.Close()
	}
	for _, key := range []string{`Software\Classes\` + ClaudeLinkProgID, `Software\Multi-Claude Switcher`} {
		if err := deleteRegistryTree(registry.CURRENT_USER, key); err != nil {
			errs = append(errs, err)
		}
	}
	procSHChangeNotify.Call(shcneAssocChanged, shcnfIDList, 0, 0)
	return errors.Join(errs...)
}

// deleteRegistryTree deletes key and everything under it. A key that is not
// there is not an error.
func deleteRegistryTree(root registry.Key, key string) error {
	k, err := registry.OpenKey(root, key, registry.ENUMERATE_SUB_KEYS)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	subs, err := k.ReadSubKeyNames(-1)
	k.Close()
	if err != nil {
		return err
	}
	for _, s := range subs {
		if err := deleteRegistryTree(root, key+`\`+s); err != nil {
			return err
		}
	}
	return registry.DeleteKey(root, key)
}

// AskWindowsForClaudeLinkApp makes Windows show its "How do you want to open
// this?" window for claude:// links, where the user picks the switcher. It
// returns once the link is handed to Windows; the window is Windows' own, so
// ClaudeLinkState is what tells whether the user picked the switcher. See
// claudelink.go for why it is done by opening a link.
//
// Windows asks only while the user has picked no app. When they picked another
// one before, that pick is removed first so Windows asks again: clicking the
// switcher's button is the user asking for exactly that, and the new pick is
// still theirs, made in Windows' window.
func AskWindowsForClaudeLinkApp() error {
	if err := RegisterClaudeLinkHandler(); err != nil {
		return err
	}
	if prev := claudeLinkUserChoice(); prev != "" && !strings.EqualFold(prev, ClaudeLinkProgID) {
		if err := registry.DeleteKey(registry.CURRENT_USER, claudeLinkUserChoiceKey); err != nil {
			return fmt.Errorf("remove the earlier pick for claude:// links (%s): %w", prev, err)
		}
		procSHChangeNotify.Call(shcneAssocChanged, shcnfIDList, 0, 0)
		log.Printf("removed the earlier pick for claude:// links (%s) so Windows asks again", prev)
	}
	verb, _ := windows.UTF16PtrFromString("open")
	link, _ := windows.UTF16PtrFromString(ClaudeLinkChooser)
	if err := windows.ShellExecute(0, verb, link, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("open %s: %w", ClaudeLinkChooser, err)
	}
	// Windows' window is OpenWith.exe (measured, Windows 10). When it has not
	// come up and nothing was picked, Windows gave the link straight to an app,
	// and only Settings is left to change it.
	for deadline := time.Now().Add(chooserAppearsWithin); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		if ClaudeLinkChooserOpen() || ClaudeLinkState() == ClaudeLinksViaSwitcher {
			return nil
		}
	}
	return ErrNoClaudeLinkChooser
}

// ClaudeLinkChooserOpen reports whether Windows' "How do you want to open
// this?" window is up: it is OpenWith.exe (measured, Windows 10).
func ClaudeLinkChooserOpen() bool {
	return processRunning("OpenWith.exe")
}

// chooserAppearsWithin is how long Windows gets to show its window.
const chooserAppearsWithin = 5 * time.Second

// ErrNoClaudeLinkChooser means Windows opened ClaudeLinkChooser without asking
// which app to use.
var ErrNoClaudeLinkChooser = errors.New("Windows opened the claude:// link without asking which app to use")

// processRunning reports whether a process with this executable name runs.
func processRunning(exe string) bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), exe) {
			return true
		}
	}
	return false
}

// OpenClaudeLink hands link to the Claude running on profilePath, starting it
// there if it is not running. An empty profilePath, or the default profile,
// means Claude's own default: the Store build keeps that somewhere other than
// %APPDATA%\Claude, and only a start with no --user-data-dir reaches it.
//
// It starts Claude itself rather than opening the link through the shell,
// which would hand it straight back to the switcher.
func (w *WindowsPlatform) OpenClaudeLink(profilePath, link string) error {
	exe, err := findClaudeExecutable()
	if err != nil {
		return err
	}
	return exec.Command(exe, claudeLinkArgs(profilePath, w.defaultProfilePath(), link)...).Start()
}

// claudeLinkArgs is Claude's command line for OpenClaudeLink. A second Claude
// started on a folder that a running Claude holds passes its arguments to that
// one and exits, which is how the link reaches an account already open.
func claudeLinkArgs(profilePath, defaultPath, link string) []string {
	if profilePath == "" || (defaultPath != "" && sameWindowsPath(profilePath, defaultPath)) {
		return []string{link}
	}
	return []string{dataDirFlag + profilePath, link}
}

// ClaudeApp names the Claude Desktop the switcher drives: its version, or ""
// when it cannot be read, and its edition, one of the Edition constants, or ""
// when there is no Claude Desktop.
func ClaudeApp() (version, edition string) {
	exe, err := findClaudeExecutable()
	switch {
	case err == nil && !sameWindowsPath(exe, storeAliasPath()):
		return claudeComVersion(), EditionClaudeCom
	case err == nil:
		return packageFamilyVersion(storePackageFamily()), EditionStore
	case msixPackageDir() != "":
		return packageFamilyVersion(filepath.Base(msixPackageDir())), EditionStore
	}
	return "", ""
}

// claudeComVersion is the newest app-<version> folder the claude.com installer
// keeps under %LOCALAPPDATA%\AnthropicClaude.
func claudeComVersion() string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return ""
	}
	entries, err := os.ReadDir(filepath.Join(local, "AnthropicClaude"))
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return newestAppVersion(names)
}

// storePackageFamily returns the Store build's package family name, read from
// its data folder under %LOCALAPPDATA%\Packages, or "".
func storePackageFamily() string {
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		if matches, _ := filepath.Glob(filepath.Join(local, "Packages", storePackageName+"_*")); len(matches) > 0 {
			return filepath.Base(matches[0])
		}
	}
	// The data folder appears only once Claude has run. The family is fixed by
	// the publisher, so it is known without it.
	return storePackageFamilyName
}

// storePackageFamilyName is the Store build's package family name (measured,
// Claude 3.6556.2 from the Store).
const storePackageFamilyName = storePackageName + "_fnn82j28hfe8t"

// packageFamilyVersion returns the version of the installed package in a
// package family, or "". The install folder that carries it in its name is
// under Program Files\WindowsApps, which a user cannot list.
func packageFamilyVersion(family string) string {
	if family == "" {
		return ""
	}
	name, err := windows.UTF16PtrFromString(family)
	if err != nil {
		return ""
	}
	var count, bufLen uint32
	procGetPackagesByPackageFamily.Call(uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&count)), 0, uintptr(unsafe.Pointer(&bufLen)), 0)
	if count == 0 || bufLen == 0 {
		return ""
	}
	names := make([]*uint16, count)
	buf := make([]uint16, bufLen)
	if r, _, _ := procGetPackagesByPackageFamily.Call(uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&names[0])),
		uintptr(unsafe.Pointer(&bufLen)), uintptr(unsafe.Pointer(&buf[0]))); r != 0 {
		return ""
	}
	newest := ""
	for _, p := range names[:count] {
		if v := packageVersion(windows.UTF16PtrToString(p)); v != "" && (newest == "" || lessVersion(newest, v)) {
			newest = v
		}
	}
	return newest
}
