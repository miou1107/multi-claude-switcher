package diagnostics

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fullInput(t *testing.T) Input {
	t.Helper()
	logDir := t.TempDir()
	body := "2026/08/04 10:50:12 [Safe Switch] from /Users/vincentkao/Library/Application Support/Claude\n" +
		"2026/08/04 10:50:13 account 035899b2-b130-40b6-aa9e-93cf208df7b7 (vincent@fontrip.com)\n"
	if err := os.WriteFile(filepath.Join(logDir, "mcs-tray.log"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Input{
		Version: "0.11.2", OS: "darwin", Arch: "arm64", OSVersion: "15.5", Install: "macos",
		ClaudeVer: "1.24012.11", ClaudeCodeVer: "2.1.219",
		AutoSync: true, LoginItem: true,
		Profiles: []Profile{
			{Folder: "Claude", AccountUUID: "035899b2-b130-40b6-aa9e-93cf208df7b7",
				Email: "vincent@fontrip.com", OrgUUID: "d129c8c1-7834-4e6c-84a4-dc19dfeedc8f",
				Path:     "/Users/vincentkao/Library/Application Support/Claude",
				SignedIn: true, Running: true, Convos: 252},
			{Folder: "Claude_Profile2", AccountUUID: "ae543f88-0f24-4ae6-ae21-3033915bca76",
				Email: "ft@example.com", OrgUUID: "245fb00c-4b74-4d8d-9ba8-3580e216ff85",
				Path:     "/Users/vincentkao/Library/Application Support/Claude_Profile2",
				SignedIn: true, Convos: 95},
			// Not signed in: exercises the branch fullInput otherwise never
			// reaches, where a profile has neither an account nor an org to mask.
			{Folder: "Claude_Profile3",
				Path:     "/Users/vincentkao/Library/Application Support/Claude_Profile3",
				SignedIn: false},
		},
		ActiveRecord:    "Claude",
		Home:            "/Users/vincentkao",
		HomeReplacement: "~",
		UserName:        "vincentkao",
		HostName:        "Vins-MacBook-Pro.local",
		LogDir:          logDir,
	}
}

// TestBuildLeavesNothingUnregistered catches two different ways a value can
// reach the report unmasked. UnregisteredMarker only ever shows up for a value
// that is email- or UUID-shaped, so a leak that looks like a home path, a user
// name, or a host name sails through with the marker check alone — which is
// how the log-file-name leak (report.go emitting the directory entry's name
// without routing it through Apply) survived this test unchanged. Writing a
// log file whose own name carries the fixture's user name reproduces that: the
// marker check stays green while the raw name leaks, so the test also asserts
// the fixture's raw home path, user name and host name never appear verbatim.
func TestBuildLeavesNothingUnregistered(t *testing.T) {
	in := fullInput(t)
	leakName := "mcs-" + in.UserName + "-" + in.Profiles[1].Folder + ".log"
	if err := os.WriteFile(filepath.Join(in.LogDir, leakName), []byte("line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Build(in)
	if strings.Contains(got, UnregisteredMarker) {
		t.Errorf("report carries an unregistered identifier:\n%s", got)
	}
	for _, raw := range []string{in.Home, in.UserName, in.HostName} {
		if raw != "" && strings.Contains(got, raw) {
			t.Errorf("%q reached the report unmasked:\n%s", raw, got)
		}
	}
}

// TestBuildMasksEverySurface walks the leaks found in review, one assertion
// each, because each was a place nobody had thought of rather than a rule
// written wrongly.
func TestBuildMasksEverySurface(t *testing.T) {
	in := fullInput(t)
	in.Profiles[1].Folder = "vincent@fontrip.com" // a folder named after an address
	// Cleared, not merely given an error alongside a value: orUnknown masks the
	// reason on both the value-present and value-empty branches, so leaving
	// ClaudeVer set only exercised the branch the gatherer never actually
	// produces (a version known by one route and a same-field error from
	// another). Empty is the reachable branch.
	in.ClaudeVer = ""
	in.ClaudeVerErr = "open /Users/vincentkao/Library/Application Support/Claude/config.json: permission denied"
	got := Build(in)

	for _, leak := range []string{
		"vincent@fontrip.com",
		"035899b2-b130-40b6-aa9e-93cf208df7b7",
		"d129c8c1-7834-4e6c-84a4-dc19dfeedc8f",
		"/Users/vincentkao",
		"vincentkao",
		"Vins-MacBook-Pro",
	} {
		if strings.Contains(got, leak) {
			t.Errorf("%q survived into the report:\n%s", leak, got)
		}
	}
	for _, keep := range []string{"account-1", "org-A", "0.11.2", "252"} {
		if !strings.Contains(got, keep) {
			t.Errorf("%q missing from the report:\n%s", keep, got)
		}
	}
	// The error still has to be readable as an error, and the empty-value
	// branch still has to say "unknown".
	if !strings.Contains(got, "unknown (") {
		t.Errorf("an empty version must say unknown:\n%s", got)
	}
	if !strings.Contains(got, "permission denied") {
		t.Errorf("the reason for an unknown field was dropped:\n%s", got)
	}
}

// TestBuildReportsPathShapeWithoutValues is what replaces an unmask switch: a
// bug caused by the shape of a path stays diagnosable without the path.
func TestBuildReportsPathShapeWithoutValues(t *testing.T) {
	in := fullInput(t)
	in.Home = "/Users/張小明"
	in.UserName = "張小明"
	got := Build(in)
	if !strings.Contains(got, "non-ASCII: yes") {
		t.Errorf("a non-ASCII home must be reported as a property:\n%s", got)
	}
	if strings.Contains(got, "張小明") {
		t.Errorf("the value leaked while reporting its shape:\n%s", got)
	}
}

// TestBuildAdmitsWhatItCouldNotRead keeps a gap visible instead of letting an
// absent field read as an absent problem.
func TestBuildAdmitsWhatItCouldNotRead(t *testing.T) {
	in := fullInput(t)
	in.ClaudeVer, in.ClaudeVerErr = "", "no updaterLastSeenVersion in config.json"
	in.ClaudeCodeVer, in.ClaudeCodeVerErr = "", "no version directory"
	got := Build(in)
	if !strings.Contains(got, "unknown (no updaterLastSeenVersion in config.json)") {
		t.Errorf("an unreadable field must say why:\n%s", got)
	}
}

// TestBuildTruncatesTheLogAndSaysSo stops a 40 MB log from becoming the report.
func TestBuildTruncatesTheLogAndSaysSo(t *testing.T) {
	in := fullInput(t)
	var b strings.Builder
	for i := 0; i < 500; i++ {
		b.WriteString("2026/08/04 10:50:12 line\n")
	}
	if err := os.WriteFile(filepath.Join(in.LogDir, "mcs-tray.log"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Build(in)
	if !strings.Contains(got, "mcs-tray.log (last 200 lines)") {
		t.Errorf("the log section must head each file and say it is truncated:\n%s", got)
	}
	if n := strings.Count(got, "10:50:12 line"); n != 200 {
		t.Errorf("kept %d lines, want 200", n)
	}
}

// TestBuildNamesAMissingLogRatherThanOmittingIt stops a report with no log from
// looking like a run with no activity.
func TestBuildNamesAMissingLogRatherThanOmittingIt(t *testing.T) {
	in := fullInput(t)
	in.LogDir = filepath.Join(t.TempDir(), "gone")
	got := Build(in)
	if !strings.Contains(got, "no log files") {
		t.Errorf("an absent log directory must be stated:\n%s", got)
	}
}

// TestBuildTailsLargeLogsWithoutReadingWhole pins the correctness of reading a
// bounded tail from disk instead of os.ReadFile-ing the whole file: the last
// line and the 200th-from-last line must both survive the seek, and an early
// line that falls outside both the byte bound and the 200-line bound must not.
func TestBuildTailsLargeLogsWithoutReadingWhole(t *testing.T) {
	in := fullInput(t)
	var b strings.Builder
	const total = 40000
	for i := 0; i < total; i++ {
		fmt.Fprintf(&b, "line-%05d\n", i)
	}
	if err := os.WriteFile(filepath.Join(in.LogDir, "mcs-tray.log"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Build(in)
	last := fmt.Sprintf("line-%05d", total-1)
	nearEdge := fmt.Sprintf("line-%05d", total-200)
	if !strings.Contains(got, last) {
		t.Errorf("the last line must survive tailing:\n%s", got)
	}
	if !strings.Contains(got, nearEdge) {
		t.Errorf("the 200th-from-last line must survive tailing:\n%s", got)
	}
	if strings.Contains(got, "line-00000\n") {
		t.Errorf("an early line must not survive the byte and line bound:\n%s", got)
	}
}

// TestBuildNamesAnUnreadableLogFile is the one branch of logSections that had
// no coverage: a log file that exists but cannot be opened must still be named,
// with the reason given, rather than silently dropped from the report.
func TestBuildNamesAnUnreadableLogFile(t *testing.T) {
	in := fullInput(t)
	badPath := filepath.Join(in.LogDir, "mcs-locked.log")
	if err := os.WriteFile(badPath, []byte("secret line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(badPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(badPath, 0o600) })
	got := Build(in)
	if !strings.Contains(got, "mcs-locked.log (last 200 lines)") {
		t.Errorf("an unreadable log must still be headed by its name:\n%s", got)
	}
	if !strings.Contains(got, "unreadable (") {
		t.Errorf("an unreadable log must say it could not be read:\n%s", got)
	}
	if strings.Contains(got, "secret line") {
		t.Errorf("the unreadable file's content must not appear:\n%s", got)
	}
}

// TestBuildMasksInstall keeps Install on the same invariant as every other
// field: everything that reaches the output goes through Apply. The gatherer
// only ever fills it with an enum today, so this cannot demonstrate a real
// leak, but routing it around Apply is exactly how finding 1 (the log file
// name) started: one field that seemed safe enough to skip.
func TestBuildMasksInstall(t *testing.T) {
	in := fullInput(t)
	in.Install = in.UserName
	got := Build(in)
	if strings.Contains(got, in.UserName) {
		t.Errorf("Install must be masked like every other surface:\n%s", got)
	}
}

// TestBuildReportsNoActiveRecordAsNone exercises orNone's empty branch:
// fullInput always sets an active record, so without this the branch that
// prints "none" never ran.
func TestBuildReportsNoActiveRecordAsNone(t *testing.T) {
	in := fullInput(t)
	in.ActiveRecord = ""
	got := Build(in)
	if !strings.Contains(got, "Active record: none") {
		t.Errorf("an empty active record must read as none:\n%s", got)
	}
}

// TestNewMaskerForMasksTheUsersOwnComment covers the case the exported
// constructor exists for: a user pastes the error they saw, and that error
// names their account.
func TestNewMaskerForMasksTheUsersOwnComment(t *testing.T) {
	m := NewMaskerFor(fullInput(t))
	got := m.Apply("it broke for vincent@fontrip.com in /Users/vincentkao/Library")
	if strings.Contains(got, "fontrip") || strings.Contains(got, "vincentkao") {
		t.Errorf("the comment kept an identifier: %q", got)
	}
}
