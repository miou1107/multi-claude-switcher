package panelui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/miou1107/multi-claude-switcher/platform"
)

func noPlan(string) string { return "" }

func profile(name, path string) *platform.ProfileInfo {
	return &platform.ProfileInfo{Name: name, Path: path, Exists: true}
}

// TestBuildProfilesMarksSignedIn is the regression test for a dead Sync screen.
//
// ProfileVM.SignedIn was set by the Windows host and not by the macOS one, so on
// macOS every account came through as not signed in. RenderSync offers a sync pair
// only between two signed-in accounts, so it offered none and reported "2 accounts
// not signed in yet" for two accounts that were both signed in. The screen was
// unusable from the day the field was added, and nothing failed: the renderer's own
// tests supply their view models directly, so they never exercised the host that
// builds them.
//
// The two hosts now share this one builder, which is why the test can exist at all.
func TestBuildProfilesMarksSignedIn(t *testing.T) {
	root := t.TempDir()
	in := filepath.Join(root, "Claude")
	out := filepath.Join(root, "Claude_Fresh")
	writeAccount(t, in, "uuid-a")
	writeConfigWithoutAccount(t, out)

	got := BuildProfiles(
		[]*platform.ProfileInfo{profile("Claude", in), profile("Claude_Fresh", out)},
		[]string{"Claude", "Claude_Fresh"}, nil, "", noPlan)

	if len(got) != 2 {
		t.Fatalf("want both managed profiles, got %+v", got)
	}
	if !got[0].SignedIn {
		t.Errorf("a profile with an account must be signed in: %+v", got[0])
	}
	if got[1].SignedIn {
		t.Errorf("a profile with no account must not be: %+v", got[1])
	}
}

func TestBuildProfilesMarksTheRunningProfile(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "Claude")
	b := filepath.Join(root, "Claude_Work")
	writeAccount(t, a, "uuid-a")
	writeAccount(t, b, "uuid-b")

	got := BuildProfiles(
		[]*platform.ProfileInfo{profile("Claude", a), profile("Claude_Work", b)},
		[]string{"Claude", "Claude_Work"}, nil, b, noPlan)

	if got[0].Current {
		t.Errorf("Claude is not the running profile: %+v", got[0])
	}
	if !got[1].Current {
		t.Errorf("Claude_Work is running and must be marked: %+v", got[1])
	}
}

// TestBuildProfilesCarriesTheAccountUUID is the regression test for a dead
// duplicate-account warning. The account list groups profiles by ProfileVM.UUID to
// spot two folders holding one account, but the shared builder never set the field,
// so in the real app every VM arrived with an empty UUID, nothing ever grouped, and
// the warning could not appear — while the renderer's own tests, which hand-build
// VMs with UUID set, stayed green. A profile with no account keeps the empty UUID
// that means exactly that.
func TestBuildProfilesCarriesTheAccountUUID(t *testing.T) {
	root := t.TempDir()
	in := filepath.Join(root, "Claude")
	fresh := filepath.Join(root, "Claude_Fresh")
	writeAccount(t, in, "uuid-a")
	writeConfigWithoutAccount(t, fresh)

	got := BuildProfiles(
		[]*platform.ProfileInfo{profile("Claude", in), profile("Claude_Fresh", fresh)},
		[]string{"Claude", "Claude_Fresh"}, nil, "", noPlan)

	if len(got) != 2 {
		t.Fatalf("want both managed profiles, got %+v", got)
	}
	if got[0].UUID != "uuid-a" {
		t.Errorf("the signed-in profile must carry its account UUID so duplicates can be found: %+v", got[0])
	}
	if got[1].UUID != "" {
		t.Errorf("a profile with no account must have an empty UUID, not something to be a duplicate of: %+v", got[1])
	}
}

// TestBuildProfilesHonoursTheManagedList: once the user has curated the list, only
// what they chose is shown — including a folder with no account in it, which is how
// they reach it to sign in.
func TestBuildProfilesHonoursTheManagedList(t *testing.T) {
	root := t.TempDir()
	kept := filepath.Join(root, "Claude")
	other := filepath.Join(root, "Claude_Other")
	writeAccount(t, kept, "uuid-a")
	writeAccount(t, other, "uuid-b")

	got := BuildProfiles(
		[]*platform.ProfileInfo{profile("Claude", kept), profile("Claude_Other", other)},
		[]string{"Claude"}, nil, "", noPlan)

	if len(got) != 1 || got[0].Folder != "Claude" {
		t.Fatalf("only the managed folder should appear, got %+v", got)
	}
}

// TestBuildProfilesFirstRunFallsBackToWhatIsUsable: with no managed list yet (nil,
// not empty), show anything signed in or MCS-created, so a first-run panel is not
// blank.
func TestBuildProfilesFirstRunFallsBackToWhatIsUsable(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, "Claude")
	stray := filepath.Join(root, "ClaudeBar")
	writeAccount(t, live, "uuid-a")

	got := BuildProfiles(
		[]*platform.ProfileInfo{profile("Claude", live), profile("ClaudeBar", stray)},
		nil, nil, "", noPlan)

	if len(got) != 1 || got[0].Folder != "Claude" {
		t.Fatalf("first run shows what is usable, got %+v", got)
	}
}

// TestBuildProfilesFirstRunKeepsAnMCSCreatedProfile: a profile MCS made has no
// account until the user signs in, and dropping it is what makes a freshly created
// account invisible in the panel it was created from.
func TestBuildProfilesFirstRunKeepsAnMCSCreatedProfile(t *testing.T) {
	root := t.TempDir()
	fresh := filepath.Join(root, "Claude_New")
	p := profile("Claude_New", fresh)
	p.Managed = true

	got := BuildProfiles([]*platform.ProfileInfo{p}, nil, nil, "", noPlan)
	if len(got) != 1 {
		t.Fatalf("an MCS-created profile stays listed before sign-in, got %+v", got)
	}
	if got[0].SignedIn {
		t.Errorf("it has no account yet: %+v", got[0])
	}
}

// TestBuildProfilesShowsAPendingProfileWithoutCurating is the regression test for a
// created account vanishing the rest. On macOS a freshly created profile carries no
// Managed flag and has no account yet, so the first-run fallback would drop it — which
// is why an earlier version added it to the managed list, turning the list from nil
// into one element and hiding every account not in it. The pending registry shows it
// instead, so the existing signed-in account stays visible alongside it.
func TestBuildProfilesShowsAPendingProfileWithoutCurating(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "Claude")
	fresh := filepath.Join(root, "Claude_Personal")
	writeAccount(t, existing, "uuid-a")
	writeConfigWithoutAccount(t, fresh)
	freshP := profile("Claude_Personal", fresh) // no Managed flag, as on macOS

	got := BuildProfiles(
		[]*platform.ProfileInfo{profile("Claude", existing), freshP},
		nil,                        // first run: never curated
		[]string{"Claude_Personal"}, // but this one is pending sign-in
		"", noPlan)

	if len(got) != 2 {
		t.Fatalf("both the existing account and the pending profile must show, got %+v", got)
	}
	var sawExisting, sawFresh bool
	for _, p := range got {
		switch p.Folder {
		case "Claude":
			sawExisting = true
			if !p.SignedIn {
				t.Error("the existing account is signed in")
			}
		case "Claude_Personal":
			sawFresh = true
			if p.SignedIn {
				t.Error("the freshly created profile has no account yet")
			}
		}
	}
	if !sawExisting {
		t.Error("adding a profile must not hide the account the user already had")
	}
	if !sawFresh {
		t.Error("the freshly created profile must be visible in the panel it was made from")
	}
}

func TestBuildProfilesUsesTheDisplayNameAndPlan(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "Claude_Work")
	writeAccount(t, p, "uuid-a")

	got := BuildProfiles([]*platform.ProfileInfo{profile("Claude_Work", p)},
		[]string{"Claude_Work"}, nil, "", func(path string) string {
			if path != p {
				t.Errorf("plan lookup got %q, want the profile's path", path)
			}
			return "Max 20×"
		})

	if len(got) != 1 || got[0].Plan != "Max 20×" {
		t.Fatalf("plan not carried through: %+v", got)
	}
	// No display-name override is set, so the folder name stands in.
	if got[0].Name != "Claude_Work" {
		t.Fatalf("name = %q", got[0].Name)
	}
}

// writeAccount creates a profile folder that a real account is signed in to.
func writeAccount(t *testing.T, dir, uuid string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"lastKnownAccountUuid":"` + uuid + `"}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeConfigWithoutAccount creates a profile folder Claude Desktop has run in but
// nobody has signed in to: a config.json with no account in it.
func writeConfigWithoutAccount(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"locale":"en-US"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}
