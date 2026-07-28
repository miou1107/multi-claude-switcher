//go:build windows

package platform

// protocolhandler_windows.go points the `claude://` URL handler at whichever
// profile is currently active.
//
// Why this is needed: signing in opens a browser, and the browser hands the
// result back by launching `claude://login/...`. Windows resolves that through
// HKCU\Software\Classes\claude\shell\open\command, which Claude Desktop
// registers as:
//
//	"…\claude.exe" "%1"
//
// with no --user-data-dir. So the callback always opens the DEFAULT profile,
// whatever profile the user is actually switched to, and the sign-in lands in
// the wrong account. `--user-data-dir` only binds the process the switcher
// launches; it cannot bind one the shell launches.
//
// The fix is to rewrite that command to carry the profile's data directory:
//
//	"…\claude.exe" --user-data-dir="…\ClaudeWork" "%1"
//
// Timing is the whole problem. **Claude Desktop re-registers its own protocol
// handler about 825 ms after it starts** (measured), wiping anything written
// beforehand. Writing at launch time therefore never survives to the callback,
// which is exactly how this failed the first time round. The write has to come
// after Claude has registered, and has to be re-asserted if Claude clobbers it
// again.
//
// So the rewrite is held only for the window where it is actually needed:
// after switching to a profile that has **no account yet**, until that profile
// gains one (the sign-in landed) or the window expires. A profile that is
// already signed in needs no callback at all and is never touched.
//
// The rest is kept deliberately narrow. It is a per-user key (no admin), it
// belongs to Claude Desktop's own protocol, the exe path is always re-read from
// the current value rather than remembered (so a Claude update that moves the
// exe cannot leave a stale path behind), and Restore rewrites the pristine form
// rather than replaying a stored backup that could have gone out of date.

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows/registry"
)

// claudeProtocolKey is where Windows looks up `claude://` for the current user.
const claudeProtocolKey = `Software\Classes\claude\shell\open\command`

// dataDirFlag is the argument Claude Desktop takes for its profile directory.
const dataDirFlag = "--user-data-dir="

// readProtocolCommand returns the current `claude://` command line.
func readProtocolCommand() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, claudeProtocolKey, registry.QUERY_VALUE)
	if err != nil {
		return "", fmt.Errorf("claude:// handler is not registered: %w", err)
	}
	defer k.Close()
	cmd, _, err := k.GetStringValue("")
	if err != nil {
		return "", fmt.Errorf("read claude:// handler: %w", err)
	}
	return cmd, nil
}

// writeProtocolCommand replaces the `claude://` command line.
func writeProtocolCommand(cmd string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, claudeProtocolKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open claude:// handler for writing: %w", err)
	}
	defer k.Close()
	return k.SetStringValue("", cmd)
}

// exeFromProtocolCommand extracts the executable from a handler command line.
// The registered form quotes the path, which is what makes this reliable even
// though the path itself contains spaces.
func exeFromProtocolCommand(cmd string) (string, bool) {
	cmd = strings.TrimSpace(cmd)
	if !strings.HasPrefix(cmd, `"`) {
		// Unquoted: take everything up to the first space. Claude does not
		// register it this way, but a hand-edited value might look like it.
		exe, _, _ := strings.Cut(cmd, " ")
		return exe, exe != ""
	}
	rest := cmd[1:]
	exe, _, found := strings.Cut(rest, `"`)
	if !found || exe == "" {
		return "", false
	}
	return exe, true
}

// buildProtocolCommand renders the handler command line for a profile. Passing
// an empty profilePath renders the pristine form Claude Desktop itself
// registers.
func buildProtocolCommand(exe, profilePath string) string {
	if profilePath == "" {
		return fmt.Sprintf(`"%s" "%%1"`, exe)
	}
	return fmt.Sprintf(`"%s" %s"%s" "%%1"`, exe, dataDirFlag, profilePath)
}

// protocolCommandProfile reports the profile directory a handler command line
// currently carries, and whether it carries one at all.
func protocolCommandProfile(cmd string) (string, bool) {
	i := strings.Index(cmd, dataDirFlag)
	if i < 0 {
		return "", false
	}
	rest := cmd[i+len(dataDirFlag):]
	if !strings.HasPrefix(rest, `"`) {
		dir, _, _ := strings.Cut(rest, " ")
		return dir, dir != ""
	}
	dir, _, found := strings.Cut(rest[1:], `"`)
	return dir, found && dir != ""
}

// SetProtocolHandlerProfile points `claude://` at profilePath, so a sign-in
// callback opens the profile the user is actually on. It is a no-op when the
// handler already names that profile.
func SetProtocolHandlerProfile(profilePath string) error {
	cmd, err := readProtocolCommand()
	if err != nil {
		return err
	}
	if cur, ok := protocolCommandProfile(cmd); ok && strings.EqualFold(cur, profilePath) {
		return nil
	}
	exe, ok := exeFromProtocolCommand(cmd)
	if !ok {
		return fmt.Errorf("could not read Claude's path out of the claude:// handler (%q)", cmd)
	}
	if err := writeProtocolCommand(buildProtocolCommand(exe, profilePath)); err != nil {
		return err
	}
	log.Printf("claude:// handler now opens %s", profilePath)
	return nil
}

// RestoreProtocolHandler puts `claude://` back to the form Claude Desktop
// registers, so nothing is left behind once the switcher stops running. It is
// a no-op when the handler carries no profile.
func RestoreProtocolHandler() error {
	cmd, err := readProtocolCommand()
	if err != nil {
		return err
	}
	if _, ok := protocolCommandProfile(cmd); !ok {
		return nil
	}
	exe, ok := exeFromProtocolCommand(cmd)
	if !ok {
		return fmt.Errorf("could not read Claude's path out of the claude:// handler (%q)", cmd)
	}
	if err := writeProtocolCommand(buildProtocolCommand(exe, "")); err != nil {
		return err
	}
	log.Println("claude:// handler restored to Claude Desktop's own registration")
	return nil
}

const (
	// signInHoldWindow bounds how long the handler is held for a sign-in. Long
	// enough for a browser sign-in including two-factor, short enough that a
	// user who wandered off does not leave the handler pointed for a whole
	// session. The hold also ends as soon as the account appears.
	signInHoldWindow = 10 * time.Minute
	// signInPollInterval is how often the profile is checked for its new
	// account, and the handler re-asserted if Claude has clobbered it.
	signInPollInterval = time.Second
)

// signInHoldTarget names the profile currently being held for, so a second
// switch supersedes the first rather than the two fighting each other.
var signInHoldTarget atomic.Value // string

// HoldProtocolHandlerForSignIn keeps `claude://` pointed at profilePath until
// that profile has an account or the window expires, then restores the handler.
//
// It is only meaningful for a profile that is not signed in yet: that is the
// only case where a callback has to be steered, and confining it there is what
// keeps the registry write to the seconds it is genuinely needed.
//
// The re-assertion is not belt-and-braces. Claude Desktop rewrites this key
// shortly after every start, so the first write has to land after that and be
// repeated if it happens again.
func HoldProtocolHandlerForSignIn(profilePath string) {
	signInHoldTarget.Store(profilePath)
	go func() {
		defer func() {
			// Only the newest hold restores, or an old one would undo the
			// handler a newer switch just set up.
			if cur, _ := signInHoldTarget.Load().(string); cur != profilePath {
				return
			}
			if err := RestoreProtocolHandler(); err != nil {
				log.Printf("could not restore the claude:// handler: %v", err)
			}
		}()

		deadline := time.Now().Add(signInHoldWindow)
		for time.Now().Before(deadline) {
			time.Sleep(signInPollInterval)

			if cur, _ := signInHoldTarget.Load().(string); cur != profilePath {
				return // superseded by another switch
			}
			if _, err := GetProfileAccountUUID(profilePath); err == nil {
				log.Printf("sign-in to %s completed; releasing the claude:// handler", profilePath)
				return
			}
			// A no-op unless Claude has clobbered it, so this logs once in the
			// normal case rather than every second.
			if err := SetProtocolHandlerProfile(profilePath); err != nil {
				log.Printf("claude:// handler could not be held for %s: %v", profilePath, err)
				return
			}
		}
		log.Printf("no sign-in to %s within %s; releasing the claude:// handler", profilePath, signInHoldWindow)
	}()
}

// ReleaseProtocolHandlerHold cancels any hold in progress and restores the
// handler. Use this rather than RestoreProtocolHandler from outside this file:
// restoring while a hold is still running is undone by the hold's very next
// poll, which leaves the handler pointed at a profile after the switcher
// believed it had cleaned up.
func ReleaseProtocolHandlerHold() error {
	signInHoldTarget.Store("")
	return RestoreProtocolHandler()
}
