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
	// A populated bucket is now recoverable, so this ghost reads as something to
	// act on rather than as bad data. "Invalid account data" is reserved for a
	// ghost with an empty bucket, which really is a dead end.
	if !ghost.LastUpdated.Equal(ts("2026-04-02")) || ghost.Note != RecoverableGhostNote {
		t.Fatalf("ghost date/note: %+v", ghost)
	}
	if !ghost.Recoverable || len(ghost.Sources) != 2 {
		t.Fatalf("its 21 conversations live in two dirs, both recoverable from: %+v", ghost)
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

	got := ScanAccounts([]*platform.ProfileInfo{live, waiting, cli}, nil)
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

	got := ScanAccounts([]*platform.ProfileInfo{p1, p2, junk}, nil)
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

func TestAssembleGhostRecoverable(t *testing.T) {
	// One dir, a live login, plus an orphan left behind by an in-app account
	// switch: the shape every reporter's machine had.
	scans := []dirScan{
		{Folder: "Claude", Path: "/data/Claude", LiveUUID: "cccccccc", Buckets: map[string]bucketStat{
			"cccccccc": {Count: 99, LastUpdated: ts("2026-07-30")},
			"bbbbbbbb": {Count: 94, LastUpdated: ts("2026-07-29")},
		}},
	}
	got := assembleAccounts(scans)
	if len(got) != 2 {
		t.Fatalf("want 1 complete + 1 ghost, got %d: %+v", len(got), got)
	}
	ghost := got[1]
	if ghost.Complete || ghost.UUID != "bbbbbbbb" {
		t.Fatalf("row1 must be the bbbbbbbb ghost: %+v", ghost)
	}
	if !ghost.Recoverable {
		t.Fatalf("a ghost with 94 conversations is recoverable: %+v", ghost)
	}
	if len(ghost.Sources) != 1 {
		t.Fatalf("want one source, got %+v", ghost.Sources)
	}
	// The path has to travel with the folder: recovery copies from it, and it
	// cannot be reconstructed from the name outside the platform package.
	if ghost.Sources[0].Folder != "Claude" || ghost.Sources[0].Path != "/data/Claude" {
		t.Fatalf("source must name the dir and its path: %+v", ghost.Sources[0])
	}
	if ghost.Sources[0].Convos != 94 {
		t.Fatalf("source must carry its own share: %+v", ghost.Sources[0])
	}
	if ghost.Note != RecoverableGhostNote {
		t.Fatalf("note = %q, want %q", ghost.Note, RecoverableGhostNote)
	}
}

func TestAssembleGhostEmptyBucketIsNotRecoverable(t *testing.T) {
	scans := []dirScan{
		{Folder: "Claude", Path: "/data/Claude", LiveUUID: "live", Buckets: map[string]bucketStat{
			"live":  {Count: 3, LastUpdated: ts("2026-07-30")},
			"empty": {Count: 0},
		}},
	}
	got := assembleAccounts(scans)
	ghost := got[len(got)-1]
	if ghost.UUID != "empty" {
		t.Fatalf("expected the empty ghost last: %+v", got)
	}
	if ghost.Recoverable {
		t.Fatalf("nothing to recover from an empty bucket: %+v", ghost)
	}
	if len(ghost.Sources) != 0 {
		t.Fatalf("a dead ghost has nothing to copy from: %+v", ghost.Sources)
	}
	if ghost.Note != "Invalid account data" {
		t.Fatalf("dead ghost keeps its existing note, got %q", ghost.Note)
	}
}

func TestAssembleGhostSplitAcrossTwoProfilesKeepsBothSources(t *testing.T) {
	// The same orphan has conversations in two dirs. Recovery must copy from both,
	// or the row's count promises more than it delivers.
	scans := []dirScan{
		{Folder: "Claude_Two", Path: "/data/Claude_Two", LiveUUID: "b", Buckets: map[string]bucketStat{
			"b": {Count: 1}, "orphan": {Count: 40, LastUpdated: ts("2026-07-02")},
		}},
		{Folder: "Claude", Path: "/data/Claude", LiveUUID: "a", Buckets: map[string]bucketStat{
			"a": {Count: 1}, "orphan": {Count: 5, LastUpdated: ts("2026-07-01")},
		}},
	}
	got := assembleAccounts(scans)
	var ghost ScannedAccount
	for _, r := range got {
		if r.UUID == "orphan" {
			ghost = r
		}
	}
	if ghost.Convos != 45 {
		t.Fatalf("counts sum across dirs: %+v", ghost)
	}
	if len(ghost.Sources) != 2 {
		t.Fatalf("want both sources, got %+v", ghost.Sources)
	}
	// Sorted by folder so the order is stable across scans — the scans slice
	// arrives in filesystem order, which is not guaranteed.
	if ghost.Sources[0].Folder != "Claude" || ghost.Sources[1].Folder != "Claude_Two" {
		t.Fatalf("sources must be sorted by folder: %+v", ghost.Sources)
	}
	if ghost.Sources[0].Convos != 5 || ghost.Sources[1].Convos != 40 {
		t.Fatalf("each source carries its own share: %+v", ghost.Sources)
	}
}

func TestRecoverableGhostSortsAboveDeadGhost(t *testing.T) {
	scans := []dirScan{
		{Folder: "Claude", Path: "/data/Claude", LiveUUID: "live", Buckets: map[string]bucketStat{
			"live": {Count: 1},
			"zzz":  {Count: 7, LastUpdated: ts("2026-07-30")}, // recoverable, sorts late by UUID
			"aaa":  {Count: 0},                                // dead, sorts early by UUID
		}},
	}
	got := assembleAccounts(scans)
	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %+v", got)
	}
	if got[1].UUID != "zzz" || !got[1].Recoverable {
		t.Fatalf("recoverable ghost must come first among ghosts: %+v", got)
	}
	if got[2].UUID != "aaa" || got[2].Recoverable {
		t.Fatalf("dead ghost must come last: %+v", got)
	}
}

func TestScanKeepsPendingProfileThatIsStillEmpty(t *testing.T) {
	// The add path, one moment after creating the folder: no config.json, no
	// buckets, no login. Without the pending exception ScanAccounts drops it,
	// and the profile the user was just told to sign in to vanishes.
	dir := t.TempDir()
	live := writeProfile(t, dir, "Claude", "live-uuid", map[string]int{"live-uuid": 3})
	fresh := filepath.Join(dir, "Claude_Personal")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	empty := &platform.ProfileInfo{Name: "Claude_Personal", Path: fresh, Exists: true, UUIDBuckets: map[string]int{}}

	got := ScanAccounts([]*platform.ProfileInfo{live, empty},
		[]PendingProfile{{Folder: "Claude_Personal"}})

	var row ScannedAccount
	found := false
	for _, r := range got {
		if r.HomeFolder == "Claude_Personal" {
			row, found = r, true
		}
	}
	if !found {
		t.Fatalf("pending profile was dropped: %+v", got)
	}
	if !row.Pending || row.Complete || row.Note != PendingSignInNote {
		t.Fatalf("row: %+v", row)
	}
	if row.PendingUUID != "" {
		t.Fatalf("add path expects any account, got %q", row.PendingUUID)
	}
}

func TestScanPendingRecoverySuppressesTheGhost(t *testing.T) {
	// Mid-recovery: the orphan's bucket has been copied into a new profile that
	// nobody has signed in to yet. The account must appear once, as the thing
	// being recovered, not also as the problem.
	dir := t.TempDir()
	source := writeProfile(t, dir, "Claude", "live-uuid",
		map[string]int{"live-uuid": 3, "orphan-uuid": 9})
	target := writeProfile(t, dir, "Claude_Recovered", "", map[string]int{"orphan-uuid": 9})

	got := ScanAccounts([]*platform.ProfileInfo{source, target},
		[]PendingProfile{{Folder: "Claude_Recovered", ExpectUUID: "orphan-uuid"}})

	for _, r := range got {
		if !r.Complete && r.UUID == "orphan-uuid" {
			t.Fatalf("orphan-uuid must not also appear as a ghost: %+v", got)
		}
	}
	var row ScannedAccount
	for _, r := range got {
		if r.HomeFolder == "Claude_Recovered" {
			row = r
		}
	}
	if !row.Pending || row.PendingUUID != "orphan-uuid" {
		t.Fatalf("pending recovery row: %+v", row)
	}
}

func TestScanKeepsPendingProfileWithNoDirectoryYet(t *testing.T) {
	// The Store build between creating a profile and the packaged app's first
	// launch: msixParkForNewIn renamed the slot away on purpose, so the profile
	// state.json names has no directory at all. msixFindProfiles reports it with
	// Exists false. It must still produce a pending row — this is the one platform
	// the whole feature exists for.
	dir := t.TempDir()
	live := writeProfile(t, dir, "Claude_Parked", "live-uuid", map[string]int{"live-uuid": 3})
	slot := &platform.ProfileInfo{
		Name: "Work", Path: filepath.Join(dir, "Claude"), Exists: false,
		UUIDBuckets: map[string]int{}, Managed: true,
	}

	got := ScanAccounts([]*platform.ProfileInfo{live, slot},
		[]PendingProfile{{Folder: "Work"}})

	for _, r := range got {
		if r.HomeFolder == "Work" {
			if !r.Pending || r.Note != PendingSignInNote {
				t.Fatalf("row: %+v", r)
			}
			return
		}
	}
	t.Fatalf("the just-created Store profile was dropped: %+v", got)
}

func TestScanPendingRowSortsWithFoldersAwaitingSignIn(t *testing.T) {
	dir := t.TempDir()
	live := writeProfile(t, dir, "Claude", "live-uuid", map[string]int{"live-uuid": 1, "orphan": 4})
	fresh := filepath.Join(dir, "Claude_New")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	pendingInfo := &platform.ProfileInfo{Name: "Claude_New", Path: fresh, Exists: true, UUIDBuckets: map[string]int{}}

	got := ScanAccounts([]*platform.ProfileInfo{live, pendingInfo},
		[]PendingProfile{{Folder: "Claude_New"}})

	if len(got) != 3 {
		t.Fatalf("want complete + pending + ghost, got %+v", got)
	}
	if !got[0].Complete {
		t.Fatalf("row0 must be the complete account: %+v", got[0])
	}
	if !got[1].Pending {
		t.Fatalf("row1 must be the pending profile: %+v", got[1])
	}
	if got[2].Complete || got[2].Pending || !got[2].Recoverable {
		t.Fatalf("row2 must be the recoverable ghost: %+v", got[2])
	}
}
