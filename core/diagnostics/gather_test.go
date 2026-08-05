package diagnostics

import (
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// fakeGatherPlatform is the minimal platform.Platform double Gather actually
// calls: DetectRunningProfile and InstallKind. Everything else panics if
// touched, which is itself a useful assertion — Gather must not reach for
// anything beyond those two.
type fakeGatherPlatform struct {
	platform.Platform
	running string
	install string
}

func (f *fakeGatherPlatform) DetectRunningProfile() (string, error) { return f.running, nil }
func (f *fakeGatherPlatform) InstallKind() string                   { return f.install }

// TestGatherPassesThroughHostSpecificValues pins the extraction's contract:
// the three values that generately differ between the two hosts — the OS
// version string, the home-replacement token, and the resolved user-name env
// var — must land in Input unchanged, and OS/Arch must reflect the process
// actually running, not a hardcoded platform. Before this extraction, each
// host set these itself; a copy/paste mistake moving them into the shared
// function (e.g. swapping OSVersion and HomeReplacement) would silently
// produce a report that names the wrong thing, on both hosts at once.
// The OS version is whatever the OS handed back, and on a Windows whose console
// code page is not UTF-8 that is not valid UTF-8. A Traditional Chinese machine
// put the CP950 bytes for "版本" into the report's first line, which is the line
// a user pastes into a GitHub issue. The host helpers no longer produce those,
// so this is the backstop that stops any future source doing it again.
func TestGatherSanitizesAnInvalidUTF8OSVersion(t *testing.T) {
	plat := &fakeGatherPlatform{}
	// Exactly what `cmd /c ver` returned on that machine: CP950 for 版本.
	raw := "Microsoft Windows [\xaa\xa9\xa5\xbb 10.0.19045.7548]"

	in := Gather(nil, plat, raw, "%USERPROFILE%", "adam")

	if !utf8.ValidString(in.OSVersion) {
		t.Errorf("OSVersion is not valid UTF-8: %q", in.OSVersion)
	}
	// The readable half has to survive; sanitizing must not drop the version.
	if !strings.Contains(in.OSVersion, "10.0.19045.7548") {
		t.Errorf("OSVersion = %q, want the version number kept", in.OSVersion)
	}
}

func TestGatherPassesThroughHostSpecificValues(t *testing.T) {
	plat := &fakeGatherPlatform{running: "", install: "store"}
	in := Gather(nil, plat, "15.5", "%USERPROFILE%", "adam")

	if in.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", in.OS, runtime.GOOS)
	}
	if in.Arch != runtime.GOARCH {
		t.Errorf("Arch = %q, want %q", in.Arch, runtime.GOARCH)
	}
	if in.OSVersion != "15.5" {
		t.Errorf("OSVersion = %q, want the value passed in", in.OSVersion)
	}
	if in.HomeReplacement != "%USERPROFILE%" {
		t.Errorf("HomeReplacement = %q, want the value passed in", in.HomeReplacement)
	}
	if in.UserName != "adam" {
		t.Errorf("UserName = %q, want the value passed in", in.UserName)
	}
	if in.Install != "store" {
		t.Errorf("Install = %q, want the platform's InstallKind()", in.Install)
	}
	if len(in.Profiles) != 0 {
		t.Errorf("no profiles were given; want an empty slice, got %d", len(in.Profiles))
	}
	// Home and HostName come from the process environment, not the fake
	// platform — just pin that Gather actually tried to fill them rather than
	// leaving the zero value on every path.
	if in.Home == "" {
		t.Error("Home must be populated from os.UserHomeDir")
	}
}
