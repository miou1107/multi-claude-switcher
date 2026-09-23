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
// So the rewrite is held for as long as Claude is running on a profile other
// than the default one, and released once that Claude has closed. The default
// profile needs no rewrite: the pristine handler already opens it.
//
// An earlier version held only for a profile with **no account yet**, on the
// theory that a signed-in profile never needs a callback. That theory is wrong.
// `lastKnownAccountUuid` in config.json survives a sign-out and an expired
// session, so a profile that has to sign in again still "has an account", was
// never held, and its Google sign-in opened the default profile. Seen on a real
// machine: MCS launched ClaudeWork correctly, the user was asked to sign in,
// and the callback started a second claude.exe on %APPDATA%\Claude. Nothing on
// disk says in advance whether a sign-in is coming, so the hold cannot wait to
// find out.
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
	// holdPollInterval is how often the handler is re-asserted in case Claude
	// has clobbered it. A registry read, so it can afford to be frequent.
	holdPollInterval = time.Second
	// holdRunningCheckEvery is how many polls pass between checks that Claude
	// is still running on the profile. That check starts PowerShell, which is
	// far too heavy to run every second for a whole session.
	holdRunningCheckEvery = 15
	// holdStartupWindow is how long a launched Claude gets to show up before the
	// hold gives up on it. Generous, because Claude Desktop can install an
	// update before its window appears.
	holdStartupWindow = 3 * time.Minute
)

// holdTarget names the profile currently being held for, so a second switch
// supersedes the first rather than the two fighting each other.
var holdTarget atomic.Value // string

// holdContinues decides, at one running-check, whether the hold goes on.
// seen is whether Claude has been seen on the profile at any earlier check.
// It reports the updated seen alongside the verdict.
//
// A Claude that has come up and then gone away is the end of the hold: the
// user quit it, or switched, and the next Claude may well be the default
// profile started from the Start menu, whose own sign-in must not be steered.
// A Claude that has not come up yet gets holdStartupWindow to do so.
func holdContinues(seen, running bool, sinceStart time.Duration) (nowSeen, keep bool) {
	if running {
		return true, true
	}
	if seen {
		return true, false
	}
	return false, sinceStart < holdStartupWindow
}

// HoldProtocolHandler keeps `claude://` pointed at profilePath for as long as
// Claude runs on it, then restores the handler. isRunning reports whether
// Claude Desktop is currently running on that profile.
//
// The re-assertion is not belt-and-braces. Claude Desktop rewrites this key
// shortly after every start, so the first write has to land after that and be
// repeated if it happens again.
func HoldProtocolHandler(profilePath string, isRunning func() (bool, error)) {
	holdTarget.Store(profilePath)
	go func() {
		defer func() {
			// Only the newest hold restores, or an old one would undo the
			// handler a newer switch just set up.
			if cur, _ := holdTarget.Load().(string); cur != profilePath {
				return
			}
			if err := RestoreProtocolHandler(); err != nil {
				log.Printf("could not restore the claude:// handler: %v", err)
			}
		}()

		start := time.Now()
		seen := false
		for poll := 1; ; poll++ {
			time.Sleep(holdPollInterval)

			if cur, _ := holdTarget.Load().(string); cur != profilePath {
				return // superseded by another switch
			}
			// A no-op unless Claude has clobbered it, so this logs once in the
			// normal case rather than every second.
			if err := SetProtocolHandlerProfile(profilePath); err != nil {
				log.Printf("claude:// handler could not be held for %s: %v", profilePath, err)
				return
			}
			if poll%holdRunningCheckEvery != 0 {
				continue
			}
			running, err := isRunning()
			if err != nil {
				// Not knowing is not the same as closed. Keep holding; the
				// next check may do better.
				log.Printf("could not tell whether Claude is still on %s: %v", profilePath, err)
				continue
			}
			var keep bool
			if seen, keep = holdContinues(seen, running, time.Since(start)); !keep {
				log.Printf("Claude is no longer running on %s; releasing the claude:// handler", profilePath)
				return
			}
		}
	}()
}

// ReleaseProtocolHandlerHold cancels any hold in progress and restores the
// handler. Use this rather than RestoreProtocolHandler from outside this file:
// restoring while a hold is still running is undone by the hold's very next
// poll, which leaves the handler pointed at a profile after the switcher
// believed it had cleaned up.
func ReleaseProtocolHandlerHold() error {
	holdTarget.Store("")
	return RestoreProtocolHandler()
}
