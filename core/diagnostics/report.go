package diagnostics

import (
	"bytes"
	"fmt"
	"io"
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

// logTailBytes bounds how much of a log file is ever read off disk, so a
// large or runaway log costs a bounded read rather than a full-file
// allocation in a long-running tray process. Comfortably larger than
// logTailLines could ever need at realistic line lengths, so the 200-line
// limit — not this byte limit — is what actually decides the content on any
// log this app itself writes.
const logTailBytes = 256 * 1024

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

	// Backups describes what the backups folder is holding. It is here because
	// this report is what somebody pastes into an issue, and "how much disk is
	// this thing using" is the first question anyone asks about a tool that
	// copies a session tree before every switch. Zero snapshots renders as
	// "none", not as a missing line, so its absence is never ambiguous.
	BackupCount       int
	BackupBytes       int64
	BackupStaged      int
	BackupStagedBytes int64
	BackupOther       int
	BackupOtherBytes  int64
	BackupReadErr     string

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
	userName := in.UserName
	if userName == "" {
		// os.Getenv("USER")/"USERNAME") can come back empty from a launch
		// environment that never set it — internal/clip/clip_darwin.go documents
		// the same class of bug for a GUI-launched bundle. Unlike an email or a
		// UUID, a bare OS user name has no shape Sweep can catch, so with
		// UserName empty RegisterBoundedWord would return immediately below and
		// the name would flow through every log line unmasked.
		// filepath.Base(in.Home) recovers the same name from the one field that
		// stays populated.
		userName = filepath.Base(in.Home)
	}
	// After the accounts, so an address that is also a user name reads as the
	// account it belongs to rather than as "user".
	m.RegisterBoundedWord(userName, "user")
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
	w("Claude Desktop %s · %s", orUnknown(in.ClaudeVer, in.ClaudeVerErr, m), m.Apply(in.Install))
	w("claude-code %s", orUnknown(in.ClaudeCodeVer, in.ClaudeCodeVerErr, m))
	w("Auto sync on switch: %s · Login item: %s", onOff(in.AutoSync), onOff(in.LoginItem))
	w("%s", pathShape(in.Home))
	w("")

	w("Profiles (%d)", len(in.Profiles))
	for _, p := range in.Profiles {
		state := ""
		if !p.SignedIn {
			state = " (not signed in)"
		} else {
			// Email is best-effort: core.ScanAccounts copies a live LevelDB and
			// swallows its own read error (core/scan.go), so a signed-in profile
			// can reach here with Email empty while AccountUUID is not. Falling
			// back to the UUID keeps the pseudonym on screen at all — without
			// this, the summary line had nothing after its separator while the
			// log section below still called the same profile "account-2", with
			// no line anywhere saying the two were the same account.
			identity := p.Email
			if identity == "" {
				identity = p.AccountUUID
			}
			var parts []string
			if masked := m.Apply(identity); masked != "" {
				parts = append(parts, masked)
			}
			if p.Running {
				parts = append(parts, "running")
			}
			if len(parts) > 0 {
				state = " (" + strings.Join(parts, ", ") + ")"
			}
		}
		w("  %s%s", m.Apply(p.Folder), state)
		if p.SignedIn {
			// Same reasoning as above: an empty OrgUUID must not leave a bare
			// " · N convos" with nothing before the separator.
			if org := m.Apply(p.OrgUUID); org != "" {
				w("    %s · %d convos", org, p.Convos)
			} else {
				w("    %d convos", p.Convos)
			}
			w("    %s", profilePathShape(p.Path, in.Home))
		}
	}
	w("Active record: %s", orNone(m.Apply(in.ActiveRecord)))
	w("%s", backupsLine(in))
	w("")

	// The two halves are swept apart because a hit means different things in
	// each. Everything above is assembled from registered fields, so an
	// identifier surviving there is a field somebody forgot and the marker says
	// so. A log line is whatever the app wrote, and the identifiers in it —
	// session filenames, most of them — belong to no field at all, so the same
	// marker there would be reporting a defect that does not exist. See
	// RedactedMarker.
	return Sweep(b.String()) + SweepFreeText(logSections(in.LogDir, m))
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

// profilePathShape states where a profile folder sits relative to home,
// without saying what either path is — the same reasoning as pathShape, and
// what Input.Path exists for. The design called for a
// "Profile path: under home, depth 4" line and no such line was ever emitted;
// this is that line. Deleting the field instead was the other option, but a
// profile living somewhere unexpected (a synced folder, a second drive) is
// exactly the kind of shape bug pathShape already carves out an exception
// for, and the value was already being gathered by both hosts for nothing.
func profilePathShape(profilePath, home string) string {
	if profilePath == "" {
		return "Profile path: unknown"
	}
	rel, err := filepath.Rel(home, profilePath)
	if err != nil || strings.HasPrefix(rel, "..") || strings.HasPrefix(filepath.ToSlash(rel), "/") {
		depth := strings.Count(strings.Trim(filepath.ToSlash(profilePath), "/"), "/") + 1
		return fmt.Sprintf("Profile path: outside home, depth %d", depth)
	}
	depth := len(strings.Split(filepath.ToSlash(rel), "/"))
	return fmt.Sprintf("Profile path: under home, depth %d", depth)
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
		// The directory entry's name is a raw filesystem value like anything
		// else the gatherer hands in — a user's own log naming convention can
		// carry an account folder or a user name — so it goes through Apply
		// the same as every line of the file's content does below.
		maskedName := m.Apply(name)
		data, err := readTail(filepath.Join(dir, name), logTailBytes)
		if err != nil {
			// The count in the header is a best guess here: the file could not
			// be opened at all, so there is no line count to report honestly.
			// Named rather than omitted, so a report never silently looks like a
			// run with no activity.
			fmt.Fprintf(&b, "%s (last %d lines)\n", maskedName, logTailLines)
			fmt.Fprintf(&b, "  unreadable (%s)\n\n", m.Apply(err.Error()))
			continue
		}
		// The header states how many lines are actually kept, not the
		// logTailLines ceiling — a 3-line log said "(last 200 lines)"
		// otherwise, which misreads a short, ordinary log as truncated and is
		// exactly the kind of misreading this section exists to prevent.
		lines := tail(data, logTailLines)
		fmt.Fprintf(&b, "%s (last %d lines)\n", maskedName, len(lines))
		for _, line := range lines {
			b.WriteString(m.Apply(line) + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// readTail reads at most the last maxBytes of path, rather than the whole
// file, so a log that has grown large costs a bounded read instead of a
// full-file allocation in a long-running tray process that may build this
// report repeatedly. When the read starts mid-file, the first line read is
// almost certainly a partial line — cut off at an arbitrary byte, not a line
// boundary — so it is dropped; logTailLines then bounds the line count same
// as before, on whatever full lines remain.
func readTail(path string, maxBytes int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}

	var start int64
	if info.Size() > maxBytes {
		start = info.Size() - maxBytes
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return "", err
		}
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	if start > 0 {
		if idx := bytes.IndexByte(data, '\n'); idx >= 0 {
			data = data[idx+1:]
		} else {
			// No newline in the tail at all: the whole read is one partial
			// line with nothing complete to keep.
			data = nil
		}
	}
	// A log line is whatever the app being debugged wrote, not necessarily
	// valid UTF-8 — a crash mid-write, a foreign encoding, a corrupted
	// buffer. Invalid bytes reaching pbcopy get silently discarded along with
	// everything else on the clipboard write; internal/clip/clip_darwin.go's
	// doc comment already documents pbcopy doing exactly that over an
	// encoding problem elsewhere in the C locale. Sanitizing here is cheaper
	// and closer to the source than trying to catch it at the clipboard.
	return strings.ToValidUTF8(string(data), string(unicode.ReplacementChar)), nil
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

// backupsLine summarises the backups folder for the report.
//
// It never masks anything: the numbers carry no identity, and the folder is
// MCS's own. An unreadable folder says so rather than reporting nothing, since
// "0 snapshots" and "could not look" mean very different things to whoever is
// reading the report.
func backupsLine(in Input) string {
	if in.BackupReadErr != "" {
		return "Backups: could not read (" + in.BackupReadErr + ")"
	}
	if in.BackupCount == 0 && in.BackupStaged == 0 && in.BackupOther == 0 {
		return "Backups: none"
	}
	// The two sizes are reported apart. Pooling them made a reader attribute
	// bytes that are already on their way out to the snapshots being kept.
	line := fmt.Sprintf("Backups: %s, %s", plural(in.BackupCount, "snapshot", "snapshots"), humanBytes(in.BackupBytes))
	if in.BackupStaged > 0 {
		line += fmt.Sprintf(" · %d awaiting deletion, %s", in.BackupStaged, humanBytes(in.BackupStagedBytes))
	}
	if in.BackupOther > 0 {
		line += fmt.Sprintf(" · %d other folder(s), %s", in.BackupOther, humanBytes(in.BackupOtherBytes))
	}
	return line
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// humanBytes renders a size the way somebody reading a bug report wants it,
// not the way a machine wants it.
func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit && exp < 3; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGT"[exp])
}
