package diagnostics

import (
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
		},
		ActiveRecord:    "Claude",
		Home:            "/Users/vincentkao",
		HomeReplacement: "~",
		UserName:        "vincentkao",
		HostName:        "Vins-MacBook-Pro.local",
		LogDir:          logDir,
	}
}

// TestBuildLeavesNothingUnregistered is the regression test the sweep exists
// for: add a field to the report, forget to register its identifiers, and this
// goes red rather than a user's address turning up in a public issue.
func TestBuildLeavesNothingUnregistered(t *testing.T) {
	got := Build(fullInput(t))
	if strings.Contains(got, UnregisteredMarker) {
		t.Errorf("report carries an unregistered identifier:\n%s", got)
	}
}

// TestBuildMasksEverySurface walks the leaks found in review, one assertion
// each, because each was a place nobody had thought of rather than a rule
// written wrongly.
func TestBuildMasksEverySurface(t *testing.T) {
	in := fullInput(t)
	in.Profiles[1].Folder = "vincent@fontrip.com" // a folder named after an address
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
	for _, keep := range []string{"account-1", "org-A", "0.11.2", "1.24012.11", "252"} {
		if !strings.Contains(got, keep) {
			t.Errorf("%q missing from the report:\n%s", keep, got)
		}
	}
	// The error still has to be readable as an error.
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
