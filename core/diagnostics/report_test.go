package diagnostics

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
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
	makeUnreadable(t, badPath)
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

// TestAppendCommentSweepsWhatRegistrationMissed is round-2 finding 1. Build
// sweeps internally and returns, so a caller appending the comment to that
// returned string afterwards put the comment outside Build's sweep entirely —
// a foreign email or a bare UUID the masker never registered survived
// verbatim, right beside the user's own account, which correctly collapsed to
// its pseudonym and made the gap easy to miss. AppendComment is the one place
// both hosts now go through, so this pins that it masks the registered value
// and sweeps everything registration does not know about.
func TestAppendCommentSweepsWhatRegistrationMissed(t *testing.T) {
	in := fullInput(t)
	m := NewMaskerFor(in)
	comment := "crashed for someone@example.com session 11112222-3333-4444-5555-666677778888 and " + in.Profiles[0].Email
	got := AppendComment("MCS report body", comment, m)

	for _, leak := range []string{"someone@example.com", "11112222-3333-4444-5555-666677778888"} {
		if strings.Contains(got, leak) {
			t.Errorf("%q, an unregistered identifier, survived AppendComment:\n%s", leak, got)
		}
	}
	if !strings.Contains(got, UnregisteredMarker) {
		t.Errorf("AppendComment must carry the sweep marker for what registration missed:\n%s", got)
	}
	if !strings.Contains(got, "account-1") {
		t.Errorf("the user's own account must still collapse to its pseudonym:\n%s", got)
	}
}

// TestNewMaskerForFallsBackToHomeBasenameWhenUserNameIsEmpty is round-2
// finding 4. os.Getenv("USER")/"USERNAME" can come back empty from a launch
// environment that never set it — internal/clip/clip_darwin.go documents the
// same class of bug for a GUI-launched bundle. Unlike an email or a UUID, a
// bare OS user name has no shape Sweep can catch, so with UserName empty
// RegisterBoundedWord returns immediately and the name would flow through
// every log line unmasked. filepath.Base(in.Home) recovers the same name from
// the one field that stays populated.
func TestNewMaskerForFallsBackToHomeBasenameWhenUserNameIsEmpty(t *testing.T) {
	in := fullInput(t)
	in.UserName = ""
	base := filepath.Base(in.Home)
	m := NewMaskerFor(in)
	got := m.Apply("seen under /Volumes/Backup/" + base + "/data")
	if strings.Contains(got, base) {
		t.Errorf("an empty UserName must still be masked via the home directory's basename: %q", got)
	}
}

// TestBuildFallsBackToAccountUUIDWhenEmailIsEmpty is the finding-1 fix.
// core.ScanAccounts copies a live LevelDB and swallows its own read error
// (core/scan.go), so a signed-in profile reaches Build with Email empty while
// AccountUUID is not. Before the fix, m.Apply(p.Email) on an empty string
// returned "", so the summary line read "Claude_work — " with a dangling
// separator, while AccountUUID was still registered and the org line below it
// (and any log line quoting the bare UUID) read as "account-2" — a pseudonym
// nothing in the summary ever named. The fallback must produce the same
// pseudonym from either field, and the dangling separator must be gone.
func TestBuildFallsBackToAccountUUIDWhenEmailIsEmpty(t *testing.T) {
	in := fullInput(t)
	in.Profiles[0].Email = ""
	got := Build(in)

	if strings.Contains(got, "Claude ()") || strings.Contains(got, "Claude (\n") {
		t.Errorf("a signed-in profile with no readable email must not leave an empty bracket:\n%s", got)
	}
	// AccountUUID still ties this profile to account-1 (fullInput registers
	// email and UUID together), so falling back to the UUID must produce the
	// very same pseudonym the org line and the log already use for it.
	if !strings.Contains(got, "Claude (account-1, running)") {
		t.Errorf("falling back to AccountUUID must still surface the account's pseudonym:\n%s", got)
	}
}

// TestBuildSuppressesDanglingOrgSeparator covers the other half of the same
// finding: a signed-in profile with no org UUID must not print
// "    · N convos" with nothing before the separator either.
func TestBuildSuppressesDanglingOrgSeparator(t *testing.T) {
	in := fullInput(t)
	in.Profiles[0].OrgUUID = ""
	got := Build(in)
	if strings.Contains(got, "  · 252 convos") {
		t.Errorf("an empty org must not leave a dangling separator before the convo count:\n%s", got)
	}
	if !strings.Contains(got, "252 convos") {
		t.Errorf("the convo count must still be printed:\n%s", got)
	}
}

// TestBuildReportsProfilePathDepthWithoutTheValue pins the emitted
// "Profile path: under home, depth N" line: the design specified it, it was
// never implemented, and Input.Path was being gathered by both hosts for
// nothing. The value itself must never appear.
func TestBuildReportsProfilePathDepthWithoutTheValue(t *testing.T) {
	in := fullInput(t)
	got := Build(in)
	// fullInput's profile path is <home>/Library/Application Support/Claude —
	// three segments below home.
	if !strings.Contains(got, "Profile path: under home, depth 3") {
		t.Errorf("want a profile-path depth line for a profile under home:\n%s", got)
	}
	if strings.Contains(got, in.Profiles[0].Path) {
		t.Errorf("the raw profile path must not appear, only its shape:\n%s", got)
	}
}

// TestBuildLogHeaderStatesTheRealLineCount is the finding-5 fix: the header
// used to print the logTailLines constant regardless of how many lines the
// file actually had, so a 3-line log read "(last 200 lines)" — misreading an
// ordinary short log as truncated, which is exactly the misreading this
// section exists to prevent.
func TestBuildLogHeaderStatesTheRealLineCount(t *testing.T) {
	in := fullInput(t)
	if err := os.WriteFile(filepath.Join(in.LogDir, "mcs-tray.log"), []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Build(in)
	if !strings.Contains(got, "mcs-tray.log (last 3 lines)") {
		t.Errorf("the header must state the real line count, not the 200-line ceiling:\n%s", got)
	}
	if strings.Contains(got, "(last 200 lines)") {
		t.Errorf("a short log must not claim to be truncated:\n%s", got)
	}
}

// TestBuildSanitizesInvalidUTF8InLogLines is the finding-6 fix. A log line is
// whatever the app being debugged wrote, not necessarily valid UTF-8, and
// invalid bytes reaching pbcopy are silently discarded along with everything
// else on the clipboard write (see internal/clip/clip_darwin.go). The report
// itself must already be valid UTF-8 by the time it gets there.
func TestBuildSanitizesInvalidUTF8InLogLines(t *testing.T) {
	in := fullInput(t)
	bad := append([]byte("valid line\nbroken "), 0xff, 0xfe)
	bad = append(bad, []byte(" tail\n")...)
	if err := os.WriteFile(filepath.Join(in.LogDir, "mcs-tray.log"), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	got := Build(in)
	if !utf8.ValidString(got) {
		t.Errorf("the report must be valid UTF-8 even when a log file is not")
	}
	if !strings.Contains(got, "valid line") || !strings.Contains(got, "tail") {
		t.Errorf("the rest of the corrupted line must survive:\n%s", got)
	}
}

func TestBackupsLine(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   Input
		want string
	}{
		{"nothing yet", Input{}, "Backups: none"},
		{"one snapshot", Input{BackupCount: 1, BackupBytes: 1536}, "Backups: 1 snapshot, 1.5 KB"},
		{"several", Input{BackupCount: 10, BackupBytes: 241172480}, "Backups: 10 snapshots, 230.0 MB"},
		{"with staged", Input{BackupCount: 5, BackupBytes: 1024, BackupStaged: 3}, "Backups: 5 snapshots, 1.0 KB · 3 awaiting deletion"},
		// "none" and "could not look" are different facts and must read
		// differently, or a reader takes an unreadable folder for an empty one.
		{"unreadable", Input{BackupReadErr: "permission denied"}, "Backups: could not read (permission denied)"},
		{"staged only", Input{BackupStaged: 2, BackupBytes: 2048}, "Backups: 0 snapshots, 2.0 KB · 2 awaiting deletion"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := backupsLine(tc.in); got != tc.want {
				t.Errorf("backupsLine = %q, want %q", got, tc.want)
			}
		})
	}
}

// The line has to actually reach the report, not just exist as a function.
func TestReportCarriesTheBackupsLine(t *testing.T) {
	in := fullInput(t)
	in.BackupCount, in.BackupBytes = 7, 1048576
	got := Build(in)
	if !strings.Contains(got, "Backups: 7 snapshots, 1.0 MB") {
		t.Errorf("the report does not carry the backups line:\n%s", got)
	}
}
