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
	// Mirrors the on-device sample (spec §2): Claude(live 035899b2) with ghost
	// buckets ae543f88 + f047dab6; Claude_Profile2(live ae543f88) with ghost
	// f047dab6. Expect 2 complete + 1 ghost; the ae543f88 bucket in Claude is a
	// stale duplicate (ae543f88 is live in Profile2) and must be folded away.
	scans := []dirScan{
		{Folder: "Claude", LiveUUID: "035899b2",
			Identity: AccountIdentity{Email: "vincent@fontrip.com"}, Account: AccountTeam,
			Buckets: map[string]bucketStat{
				"035899b2": {Count: 395, LastUpdated: ts("2026-07-24")},
				"ae543f88": {Count: 82, LastUpdated: ts("2026-07-08")},
				"f047dab6": {Count: 19, LastUpdated: ts("2026-03-30")},
			}},
		{Folder: "Claude_Profile2", LiveUUID: "ae543f88",
			Identity: AccountIdentity{Email: "second@example.com"}, Account: AccountPersonal,
			Buckets: map[string]bucketStat{
				"ae543f88": {Count: 395, LastUpdated: ts("2026-07-23")},
				"f047dab6": {Count: 2, LastUpdated: ts("2026-04-02")},
			}},
	}
	got := assembleAccounts(scans)
	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %d: %+v", len(got), got)
	}
	// Sorted: complete first by HomeFolder, then ghosts by UUID.
	if got[0].HomeFolder != "Claude" || !got[0].Complete || got[0].Email != "vincent@fontrip.com" {
		t.Fatalf("row0: %+v", got[0])
	}
	if got[0].Convos != 395 || got[0].Note != "Team account — conversations can't be synced" {
		t.Fatalf("row0 team/convos: %+v", got[0])
	}
	if got[1].HomeFolder != "Claude_Profile2" || got[1].Note != "" {
		t.Fatalf("row1 (personal note must be blank): %+v", got[1])
	}
	ghost := got[2]
	if ghost.Complete || ghost.UUID != "f047dab6" || ghost.Convos != 21 {
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

func TestScanAccounts(t *testing.T) {
	root := t.TempDir()
	p1 := writeProfile(t, root, "Claude", "035899b2", map[string]int{"035899b2": 3, "f047dab6": 2})
	p2 := writeProfile(t, root, "Claude_Profile2", "ae543f88", map[string]int{"ae543f88": 4})
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
			if a.UUID != "f047dab6" || a.Convos != 2 {
				t.Fatalf("ghost row wrong: %+v", a)
			}
		}
	}
	if complete != 2 || ghost != 1 {
		t.Fatalf("counts: complete=%d ghost=%d", complete, ghost)
	}
}
