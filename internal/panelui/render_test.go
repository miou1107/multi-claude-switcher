package panelui

import (
	"strings"
	"testing"

	"github.com/miou1107/multi-claude-switcher/core"
)

// The panel host cannot be tested without a display, so these cover the part
// that decides what the user is shown: both hosts consume this same output.

func TestComputePreselect(t *testing.T) {
	accounts := []core.ScannedAccount{
		{HomeFolder: "Claude", Complete: true, UUID: "aaa"},
		{HomeFolder: "ClaudeWork", SignedOut: true, Note: core.SignedOutNote},
		{UUID: "zzz"}, // ghost: no folder, never selectable
	}

	t.Run("first run preselects everything switchable or ready to sign in", func(t *testing.T) {
		pre := ComputePreselect(accounts, nil)
		if !pre["Claude"] {
			t.Error("the signed-in account should be preselected")
		}
		if !pre["ClaudeWork"] {
			t.Error("a folder awaiting sign-in should be preselected; leaving it out is what hid it")
		}
		if len(pre) != 2 {
			t.Errorf("ghosts are not selectable, got %v", pre)
		}
	})

	t.Run("an existing registry is authoritative", func(t *testing.T) {
		pre := ComputePreselect(accounts, []string{"ClaudeWork"})
		if pre["Claude"] {
			t.Error("not in the registry, so not preselected")
		}
		if !pre["ClaudeWork"] {
			t.Error("in the registry, so preselected")
		}
	})

	t.Run("configured empty preselects nothing", func(t *testing.T) {
		if pre := ComputePreselect(accounts, []string{}); len(pre) != 0 {
			t.Errorf("got %v", pre)
		}
	})
}

func TestRenderRescanShowsProfileAwaitingSignIn(t *testing.T) {
	accounts := []core.ScannedAccount{
		{HomeFolder: "Claude", Complete: true, UUID: "aaa-bbb", Email: "me@example.com"},
		{HomeFolder: "ClaudeWork", SignedOut: true, Note: core.SignedOutNote},
	}
	html := RenderRescan(accounts, ComputePreselect(accounts, nil))

	if !strings.Contains(html, "ClaudeWork") {
		t.Fatal("the folder awaiting sign-in must appear at all")
	}
	if !strings.Contains(html, core.SignedOutNote) {
		t.Fatal("it must say what to do about it, not just appear")
	}
	// It has to be tickable: managing it is what puts it in the account list,
	// which is the only way to switch to it and sign in.
	if !strings.Contains(html, `data-folder="ClaudeWork"`) {
		t.Fatal("it must be a selectable card")
	}
	if !strings.Contains(html, "me@example.com") {
		t.Fatal("the signed-in account should still render as before")
	}
}

func TestRenderSyncSkipsProfilesWithoutAnAccount(t *testing.T) {
	profiles := []ProfileVM{
		{Folder: "Claude", Name: "Claude", SignedIn: true},
		{Folder: "ClaudeWork", Name: "Work"}, // no account yet
		{Folder: "ClaudeThird", Name: "Third", SignedIn: true},
	}
	html := RenderSync(profiles, "", false)

	// Only the two signed-in accounts can be paired, in both directions.
	if strings.Contains(html, `data-to="ClaudeWork"`) || strings.Contains(html, `data-from="ClaudeWork"`) {
		t.Fatal("a profile with no account has no session bucket; syncing it can only fail")
	}
	if !strings.Contains(html, `data-from="Claude" data-to="ClaudeThird"`) {
		t.Fatal("the two signed-in accounts should still be offered")
	}
	if !strings.Contains(html, "not signed in yet") {
		t.Fatal("silently omitting it looks like a bug; say why it is missing")
	}
}

func TestRenderListFlagsProfileAwaitingSignIn(t *testing.T) {
	html := RenderList([]ProfileVM{
		{Folder: "Claude", Name: "Claude", Current: true, SignedIn: true},
		{Folder: "ClaudeWork", Name: "Work"},
	})
	if !strings.Contains(html, "Not signed in yet. Switch here, then sign in.") {
		t.Fatal("the card must say why it is different, or switching to it lands on an unexplained login screen")
	}
	if !strings.Contains(html, `data-folder="ClaudeWork"`) {
		t.Fatal("it must still be switchable; that is how the user signs in")
	}
}

func TestVersionShownFromVariableOnBothViews(t *testing.T) {
	want := "v" + core.Version // the "v" is a literal prefix; the number is never hardcoded
	list := RenderList([]ProfileVM{{Folder: "Claude", Name: "Claude", Current: true, SignedIn: true}})
	if !strings.Contains(list, want) {
		t.Fatalf("account list must show %q sourced from core.Version", want)
	}
	settings := RenderSettings(SettingsVM{Version: core.Version})
	if !strings.Contains(settings, want) {
		t.Fatalf("settings must show %q in the same format as the list", want)
	}
}

func TestRenderRescanGhostStaysReadOnly(t *testing.T) {
	accounts := []core.ScannedAccount{{UUID: "zzz-yyy", Convos: 4, Note: "Invalid account data"}}
	html := RenderRescan(accounts, nil)

	if !strings.Contains(html, "Unrecognized account") {
		t.Fatal("ghost row missing")
	}
	if strings.Contains(html, `data-folder="`) {
		t.Fatal("a ghost has no folder to manage and must not be selectable")
	}
}
