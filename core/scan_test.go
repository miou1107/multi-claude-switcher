package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miou1107/multi-claude-switcher/platform"
)

func ts(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func TestAssembleAccounts(t *testing.T) {
	// Mirrors the on-device sample (spec §2): Claude(live 11111111) with ghost
	// buckets 22222222 + 33333333; Claude_Profile2(live 22222222) with ghost
	// 33333333. Expect 2 complete + 1 ghost; the 22222222 bucket in Claude is a
	// stale duplicate (22222222 is live in Profile2) and must be folded away.
	scans := []dirScan{
		{Folder: "Claude", LiveUUID: "11111111",
			Identity: AccountIdentity{Email: "first@example.com"}, Account: AccountTeam,
			Buckets: map[string]bucketStat{
				"11111111": {Count: 395, LastUpdated: ts("2026-07-24")},
				"22222222": {Count: 82, LastUpdated: ts("2026-07-08")},
				"33333333": {Count: 19, LastUpdated: ts("2026-03-30")},
			}},
		{Folder: "Claude_Profile2", LiveUUID: "22222222",
			Identity: AccountIdentity{Email: "second@example.com"}, Account: AccountPersonal,
			Buckets: map[string]bucketStat{
				"22222222": {Count: 395, LastUpdated: ts("2026-07-23")},
				"33333333": {Count: 2, LastUpdated: ts("2026-04-02")},
			}},
	}
	got := assembleAccounts(scans)
	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %d: %+v", len(got), got)
	}
	// Sorted: complete first by HomeFolder, then ghosts by UUID.
	if got[0].HomeFolder != "Claude" || !got[0].Complete || got[0].Email != "first@example.com" {
		t.Fatalf("row0: %+v", got[0])
	}
	if got[0].Convos != 395 || got[0].Note != "Team account — conversations can't be synced" {
		t.Fatalf("row0 team/convos: %+v", got[0])
	}
	if got[1].HomeFolder != "Claude_Profile2" || got[1].Note != "" {
		t.Fatalf("row1 (personal note must be blank): %+v", got[1])
	}
	ghost := got[2]
	if ghost.Complete || ghost.UUID != "33333333" || ghost.Convos != 21 {
		t.Fatalf("ghost convos (19+2=21): %+v", ghost)
	}
	if !ghost.LastUpdated.Equal(ts("2026-04-02")) || ghost.Note != "Invalid account data" {
		t.Fatalf("ghost date/note: %+v", ghost)
	}
}

func TestAssembleMultiDirSameLive(t *testing.T) {
	// Same account is the live login of two dirs → two complete rows (two
	// switchable dirs), not collapsed.
	scans := []dirScan{
		{Folder: "Claude", LiveUUID: "aaa", Buckets: map[string]bucketStat{"aaa": {Count: 1}}},
		{Folder: "ClaudeWork", LiveUUID: "aaa", Buckets: map[string]bucketStat{"aaa": {Count: 2}}},
	}
	got := assembleAccounts(scans)
	if len(got) != 2 || !got[0].Complete || !got[1].Complete {
		t.Fatalf("want 2 complete rows, got %+v", got)
	}
}

func TestDeriveNote(t *testing.T) {
	if deriveNote(false, AccountTeam) != "Invalid account data" {
		t.Fatal("incomplete → invalid, regardless of type")
	}
	if deriveNote(true, AccountTeam) != "Team account — conversations can't be synced" {
		t.Fatal("complete team")
	}
	if deriveNote(true, AccountPersonal) != "" {
		t.Fatal("complete personal → blank")
	}
}

func writeProfile(t *testing.T, root, name, liveUUID string, buckets map[string]int) *platform.ProfileInfo {
	t.Helper()
	dir := filepath.Join(root, name)
	if liveUUID != "" {
		os.MkdirAll(dir, 0755)
		os.WriteFile(filepath.Join(dir, "config.json"),
			[]byte(`{"lastKnownAccountUuid":"`+liveUUID+`"}`), 0644)
	}
	for uuid, n := range buckets {
		bdir := filepath.Join(dir, "claude-code-sessions", uuid)
		os.MkdirAll(bdir, 0755)
		for i := 0; i < n; i++ {
			os.WriteFile(filepath.Join(bdir, "local_"+uuid+"_"+string(rune('a'+i))+".json"), []byte("{}"), 0644)
		}
	}
	return &platform.ProfileInfo{Name: name, Path: dir}
}

// writeSignedOutProfile creates a real profile folder nobody has signed in to:
// Claude Desktop ran with it and wrote a config.json, but there is no account
// in it. This is the shape that used to be dropped silently.
func writeSignedOutProfile(t *testing.T, root, name string) *platform.ProfileInfo {
	t.Helper()
	dir := filepath.Join(root, name)
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"locale":"en-US","userThemeMode":"dark","windowSizeWasSignedIn":false}`), 0644)
	return &platform.ProfileInfo{Name: name, Path: dir}
}

// writeNonProfileDir creates a directory that merely starts with "Claude", like
// the one the Claude Code CLI keeps beside the Desktop profiles. It has no
// config.json and must not be offered as a profile.
func writeNonProfileDir(t *testing.T, root, name string) *platform.ProfileInfo {
	t.Helper()
	dir := filepath.Join(root, name)
	os.MkdirAll(filepath.Join(dir, "ChromeNativeHost"), 0755)
	return &platform.ProfileInfo{Name: name, Path: dir}
}

func TestAssembleSignedOutProfile(t *testing.T) {
	scans := []dirScan{
		{Folder: "ClaudeWork", HasConfig: true, Buckets: map[string]bucketStat{}},
	}
	got := assembleAccounts(scans)
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d: %+v", len(got), got)
	}
	if !got[0].SignedOut || got[0].Complete || got[0].HomeFolder != "ClaudeWork" {
		t.Fatalf("row: %+v", got[0])
	}
	if got[0].Note != SignedOutNote {
		t.Fatalf("note should tell the user what to do, got %q", got[0].Note)
	}
}

func TestAssembleRowOrder(t *testing.T) {
	// Switchable accounts first, then folders awaiting sign-in, then ghosts.
	scans := []dirScan{
		{Folder: "ClaudeWork", HasConfig: true, Buckets: map[string]bucketStat{}},
		{Folder: "Claude", HasConfig: true, LiveUUID: "aaa", Buckets: map[string]bucketStat{
			"aaa": {Count: 1},
			"zzz": {Count: 5}, // orphan → ghost
		}},
	}
	got := assembleAccounts(scans)
	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %d: %+v", len(got), got)
	}
	if !got[0].Complete || got[0].HomeFolder != "Claude" {
		t.Fatalf("row0 should be the switchable account: %+v", got[0])
	}
	if !got[1].SignedOut || got[1].HomeFolder != "ClaudeWork" {
		t.Fatalf("row1 should be the folder awaiting sign-in: %+v", got[1])
	}
	if got[2].Complete || got[2].SignedOut || got[2].UUID != "zzz" {
		t.Fatalf("row2 should be the ghost: %+v", got[2])
	}
}

func TestScanAccountsListsProfileAwaitingSignIn(t *testing.T) {
	root := t.TempDir()
	live := writeProfile(t, root, "Claude", "11111111", map[string]int{"11111111": 3})
	waiting := writeSignedOutProfile(t, root, "ClaudeWork")
	cli := writeNonProfileDir(t, root, "Claude Code")

	got := ScanAccounts([]*platform.ProfileInfo{live, waiting, cli})
	if len(got) != 2 {
		t.Fatalf("want 2 rows (one signed in, one awaiting sign-in), got %d: %+v", len(got), got)
	}
	if !got[0].Complete || got[0].HomeFolder != "Claude" {
		t.Fatalf("row0: %+v", got[0])
	}
	if !got[1].SignedOut || got[1].HomeFolder != "ClaudeWork" || got[1].Note != SignedOutNote {
		t.Fatalf("row1: %+v", got[1])
	}
	for _, a := range got {
		if a.HomeFolder == "Claude Code" {
			t.Fatal("a directory with no config.json is not a profile and must not be listed")
		}
	}
}

func TestScanAccounts(t *testing.T) {
	root := t.TempDir()
	p1 := writeProfile(t, root, "Claude", "11111111", map[string]int{"11111111": 3, "33333333": 2})
	p2 := writeProfile(t, root, "Claude_Profile2", "22222222", map[string]int{"22222222": 4})
	junk := writeProfile(t, root, "Claude-3p", "", nil) // no login, no buckets → skipped

	got := ScanAccounts([]*platform.ProfileInfo{p1, p2, junk})
	if len(got) != 3 {
		t.Fatalf("want 3 (2 complete + 1 ghost), got %d: %+v", len(got), got)
	}
	var complete, ghost int
	for _, a := range got {
		if a.Complete {
			complete++
		} else {
			ghost++
			if a.UUID != "33333333" || a.Convos != 2 {
				t.Fatalf("ghost row wrong: %+v", a)
			}
		}
	}
	if complete != 2 || ghost != 1 {
		t.Fatalf("counts: complete=%d ghost=%d", complete, ghost)
	}
}
