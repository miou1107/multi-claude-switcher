package diagnostics

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// logTailLines is how much of each log file the report carries. Enough to hold
// a switch, its sync and whatever went wrong after; short enough that the report
// still fits in a clipboard and a reader's patience.
const logTailLines = 200

// Profile is one account's worth of what a report needs. Raw values: masking
// happens here, once, rather than at every call site that fills this in.
type Profile struct {
	Folder      string
	AccountUUID string
	Email       string
	OrgUUID     string
	Path        string
	SignedIn    bool
	Running     bool
	Convos      int
}

// Input is everything a host gathers. Deliberately plain data: the hosts differ
// in how they find it, and none of that difference belongs in here.
type Input struct {
	Version   string
	OS        string
	Arch      string
	OSVersion string
	Install   string

	ClaudeVer        string
	ClaudeVerErr     string
	ClaudeCodeVer    string
	ClaudeCodeVerErr string

	AutoSync  bool
	LoginItem bool

	Profiles     []Profile
	ActiveRecord string

	Home            string
	HomeReplacement string
	UserName        string
	HostName        string

	LogDir string
}

// NewMaskerFor builds the masker Build uses.
//
// Exported because the user's own comment and the issue title have to be masked
// with the same registrations: a user pastes the error they saw, and the error
// they saw names their account. A fresh masker there would know nothing and mask
// nothing.
func NewMaskerFor(in Input) *Masker {
	m := NewMasker()
	m.RegisterHome(in.Home, in.HomeReplacement)
	for _, p := range in.Profiles {
		m.RegisterAccount(p.AccountUUID, p.Email)
		m.RegisterOrg(p.OrgUUID)
	}
	// After the accounts, so an address that is also a user name reads as the
	// account it belongs to rather than as "user".
	m.RegisterBoundedWord(in.UserName, "user")
	m.RegisterBoundedWord(in.HostName, "host")
	return m
}

// Build renders the report. Every string that reaches the output goes through
// the masker, and the whole thing goes through the sweep last.
func Build(in Input) string {
	m := NewMaskerFor(in)

	var b strings.Builder
	w := func(format string, args ...any) {
		fmt.Fprintf(&b, format+"\n", args...)
	}

	w("MCS %s · %s %s · %s", in.Version, in.OS, in.OSVersion, in.Arch)
	w("Claude Desktop %s · %s", orUnknown(in.ClaudeVer, in.ClaudeVerErr, m), in.Install)
	w("claude-code %s", orUnknown(in.ClaudeCodeVer, in.ClaudeCodeVerErr, m))
	w("Auto sync on switch: %s · Login item: %s", onOff(in.AutoSync), onOff(in.LoginItem))
	w("%s", pathShape(in.Home))
	w("")

	w("Profiles (%d)", len(in.Profiles))
	for _, p := range in.Profiles {
		state := ""
		switch {
		case !p.SignedIn:
			state = " — not signed in"
		case p.Running:
			state = " — " + m.Apply(p.Email) + " — running"
		default:
			state = " — " + m.Apply(p.Email)
		}
		w("  %s%s", m.Apply(p.Folder), state)
		if p.SignedIn {
			w("    %s · %d convos", m.Apply(p.OrgUUID), p.Convos)
		}
	}
	w("Active record: %s", orNone(m.Apply(in.ActiveRecord)))
	w("")

	b.WriteString(logSections(in.LogDir, m))

	return Sweep(b.String())
}

func orUnknown(value, reason string, m *Masker) string {
	if reason == "" {
		if value != "" {
			return value
		}
		return "unknown"
	}
	// The reason is masked like everything else: *os.PathError prints the
	// absolute path it failed on, so an unmasked reason reintroduces exactly what
	// the field beside it removed.
	reason = m.Apply(reason)
	if value != "" {
		// A version can be known by one route (e.g. a directory listing) while
		// another route that was also tried failed; the failure is still worth
		// keeping next to the value it did not prevent us from finding.
		return value + " (" + reason + ")"
	}
	return "unknown (" + reason + ")"
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// pathShape states what a path is like without saying what it is.
//
// This is what stands in for an unmask switch. A bug caused by the shape of a
// path — a non-ASCII user name, a space, an unusual length — is invisible once
// the path is a pseudonym, and those bugs are common. Nearly all of them are
// breaking on a property that can simply be stated.
func pathShape(home string) string {
	nonASCII := false
	for _, r := range home {
		if r > unicode.MaxASCII {
			nonASCII = true
			break
		}
	}
	return fmt.Sprintf("Home path: %d chars, non-ASCII: %s, spaces: %s",
		len([]rune(home)), yesNo(nonASCII), yesNo(strings.ContainsRune(home, ' ')))
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// logSections renders the tail of every log file, one headed section each, so
// two components' lines are never read as one stream.
func logSections(dir string, m *Masker) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "Logs: no log files (" + m.Apply(err.Error()) + ")\n"
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return "Logs: no log files in " + m.Apply(dir) + "\n"
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "%s (last %d lines)\n", name, logTailLines)
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			// Named rather than omitted, so a report never silently looks like a
			// run with no activity.
			fmt.Fprintf(&b, "  unreadable (%s)\n\n", m.Apply(err.Error()))
			continue
		}
		for _, line := range tail(string(data), logTailLines) {
			b.WriteString(m.Apply(line) + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func tail(s string, n int) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
