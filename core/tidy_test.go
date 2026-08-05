package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/miou1107/multi-claude-switcher/platform"
)

var (
	tNow   = time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	tOlder = tNow.Add(-48 * time.Hour)
	tNewer = tNow.Add(48 * time.Hour)
)

func file(rel string, at time.Time) bucketFile { return bucketFile{Rel: rel, MTime: at} }

func moveRels(m []tidyMove) []string {
	var out []string
	for _, x := range m {
		out = append(out, x.Profile+"/"+x.Bucket.Account+"/"+x.Bucket.Org+"/"+x.Rel)
	}
	sort.Strings(out)
	return out
}

// The ordinary case: sync refiled a conversation under a different account, so
// the readable copy is in ANOTHER profile entirely. Looking only inside the
// file's own profile would find nothing and move nothing.
func TestTidyFindsCounterpartsAcrossProfiles(t *testing.T) {
	got := tidyCandidates([]profileSessions{
		{
			Profile: "Claude", ReadKnown: true, Read: bucket{"acctA", "orgA"},
			Buckets: map[bucket][]bucketFile{
				{"acctA", "orgA"}: {file("s1.json", tNow)},
				{"acctB", "orgB"}: {file("s1.json", tOlder)}, // stray
			},
		},
		{
			Profile: "Claude_Profile2", ReadKnown: true, Read: bucket{"acctB", "orgB"},
			Buckets: map[bucket][]bucketFile{
				{"acctB", "orgB"}: {file("s1.json", tNow)},
			},
		},
	})
	want := []string{"Claude/acctB/orgB/s1.json"}
	if !reflect.DeepEqual(moveRels(got), want) {
		t.Errorf("candidates = %v, want %v", moveRels(got), want)
	}
}

func TestTidyLeavesAFileWithNoCounterpart(t *testing.T) {
	got := tidyCandidates([]profileSessions{{
		Profile: "Claude", ReadKnown: true, Read: bucket{"acctA", "orgA"},
		Buckets: map[bucket][]bucketFile{
			{"acctA", "orgA"}: {file("s1.json", tNow)},
			{"acctB", "orgB"}: {file("only-here.json", tOlder)},
		},
	}})
	if len(got) != 0 {
		t.Errorf("candidates = %v, want none: a conversation that exists nowhere else must stay", moveRels(got))
	}
}

// The safety margin. Every diverging pair measured had the readable copy newer,
// but that is an observation, and a file whose only readable counterpart is
// older is the one case where moving loses something.
func TestTidyLeavesAFileNewerThanItsCounterpart(t *testing.T) {
	got := tidyCandidates([]profileSessions{{
		Profile: "Claude", ReadKnown: true, Read: bucket{"acctA", "orgA"},
		Buckets: map[bucket][]bucketFile{
			{"acctA", "orgA"}: {file("s1.json", tOlder)},
			{"acctB", "orgB"}: {file("s1.json", tNewer)},
		},
	}})
	if len(got) != 0 {
		t.Errorf("candidates = %v, want none: the readable copy is older, so the stray is the better one", moveRels(got))
	}
}

// Equal times are the common case: the measurement found 425 of 564 pairs
// byte-identical with identical mtimes, because copyFile preserves them.
func TestTidyMovesAFileWhoseCounterpartHasTheSameTime(t *testing.T) {
	got := tidyCandidates([]profileSessions{{
		Profile: "Claude", ReadKnown: true, Read: bucket{"acctA", "orgA"},
		Buckets: map[bucket][]bucketFile{
			{"acctA", "orgA"}: {file("s1.json", tNow)},
			{"acctB", "orgB"}: {file("s1.json", tNow)},
		},
	}})
	if len(got) != 1 {
		t.Errorf("candidates = %v, want the stray to move", moveRels(got))
	}
}

func TestTidyNeverTouchesTheBucketAProfileReads(t *testing.T) {
	got := tidyCandidates([]profileSessions{
		{
			Profile: "Claude", ReadKnown: true, Read: bucket{"acctA", "orgA"},
			Buckets: map[bucket][]bucketFile{{"acctA", "orgA"}: {file("s1.json", tNow)}},
		},
		{
			// Reads the same bucket coordinates, so every file has a
			// counterpart. The read bucket must still be excluded.
			Profile: "Claude_Profile2", ReadKnown: true, Read: bucket{"acctA", "orgA"},
			Buckets: map[bucket][]bucketFile{{"acctA", "orgA"}: {file("s1.json", tNow)}},
		},
	})
	if len(got) != 0 {
		t.Errorf("candidates = %v, want none: these are the folders Claude actually opens", moveRels(got))
	}
}

// The dangerous one. A profile whose read bucket cannot be determined must
// contribute no candidates: treating "unknown" as "reads nothing" makes every
// bucket it owns a candidate, and that is the mistake that would move somebody's
// live conversations.
func TestTidySkipsAProfileWhoseReadBucketIsUnknown(t *testing.T) {
	got := tidyCandidates([]profileSessions{
		{
			Profile: "Claude", ReadKnown: true, Read: bucket{"acctA", "orgA"},
			Buckets: map[bucket][]bucketFile{{"acctA", "orgA"}: {file("s1.json", tNow)}},
		},
		{
			Profile: "Claude_NotSignedIn", ReadKnown: false,
			Buckets: map[bucket][]bucketFile{
				// Every file here has a counterpart in the profile above, so
				// each one would qualify if the profile were not skipped.
				{"acctA", "orgA"}: {file("s1.json", tOlder)},
				{"acctZ", "orgZ"}: {file("s1.json", tOlder)},
			},
		},
	})
	if len(got) != 0 {
		t.Errorf("candidates = %v, want none: a profile whose read folder is unknown must be left entirely alone", moveRels(got))
	}
}

// The other half of failing closed: an unknown profile's buckets are not
// evidence that anything is readable, so they must not vouch for another
// profile's strays.
func TestTidyDoesNotTakeCounterpartsFromAnUnknownProfile(t *testing.T) {
	got := tidyCandidates([]profileSessions{
		{
			Profile: "Claude", ReadKnown: true, Read: bucket{"acctA", "orgA"},
			Buckets: map[bucket][]bucketFile{
				{"acctA", "orgA"}: {file("other.json", tNow)},
				{"acctB", "orgB"}: {file("s1.json", tOlder)}, // stray, needs a counterpart
			},
		},
		{
			// Holds s1.json in what LOOKS like its read bucket, but ReadKnown is
			// false, so nobody knows whether anything reads it and it proves
			// nothing. Read is filled in deliberately: with it left at the zero
			// value the guard under test would be unreachable, and a future
			// scanForTidy that sets Read while forgetting ReadKnown is exactly
			// the bug this has to catch.
			Profile: "Claude_NotSignedIn", ReadKnown: false, Read: bucket{"acctB", "orgB"},
			Buckets: map[bucket][]bucketFile{{"acctB", "orgB"}: {file("s1.json", tNow)}},
		},
	})
	if len(got) != 0 {
		t.Errorf("candidates = %v, want none: a file in a folder that may or may not be read is not proof of a readable copy", moveRels(got))
	}
}

// The same conversation misfiled in two profiles is two files, and both go.
func TestTidyHandlesTheSameConversationMisfiledTwice(t *testing.T) {
	got := tidyCandidates([]profileSessions{
		{
			Profile: "Claude", ReadKnown: true, Read: bucket{"acctA", "orgA"},
			Buckets: map[bucket][]bucketFile{
				{"acctA", "orgA"}: {file("s1.json", tNow)},
				{"acctB", "orgB"}: {file("s1.json", tOlder)},
			},
		},
		{
			Profile: "Claude_Profile2", ReadKnown: true, Read: bucket{"acctB", "orgB"},
			Buckets: map[bucket][]bucketFile{
				{"acctB", "orgB"}: {file("s1.json", tNow)},
				{"acctA", "orgA"}: {file("s1.json", tOlder)},
			},
		},
	})
	want := []string{"Claude/acctB/orgB/s1.json", "Claude_Profile2/acctA/orgA/s1.json"}
	if !reflect.DeepEqual(moveRels(got), want) {
		t.Errorf("candidates = %v, want %v", moveRels(got), want)
	}
}

// Nested paths address a file the same way in every bucket, so a conversation
// in a subfolder is matched like any other.
func TestTidyMatchesNestedPaths(t *testing.T) {
	got := tidyCandidates([]profileSessions{{
		Profile: "Claude", ReadKnown: true, Read: bucket{"acctA", "orgA"},
		Buckets: map[bucket][]bucketFile{
			{"acctA", "orgA"}: {file("projects/x/s1.json", tNow)},
			{"acctB", "orgB"}: {file("projects/x/s1.json", tOlder), file("projects/y/s2.json", tOlder)},
		},
	}})
	want := []string{"Claude/acctB/orgB/projects/x/s1.json"}
	if !reflect.DeepEqual(moveRels(got), want) {
		t.Errorf("candidates = %v, want %v: only the one with a counterpart", moveRels(got), want)
	}
}

// The name a run writes into must be invisible to the backup pruner, or the
// tidied files would be staged for deletion 30 days later.
func TestTidiedFolderIsNotASnapshotName(t *testing.T) {
	name := tidiedDirName(tNow)
	if name != "tidied-20260805" {
		t.Errorf("tidiedDirName = %q", name)
	}
	if _, _, _, ok := parseBackupName(name); ok {
		t.Error("the tidied folder parses as a snapshot, so the pruner would eventually delete it")
	}
	if got := snapshotsToPrune([]string{name, "Claude_20260801_120000"}, 0); len(got) != 0 {
		t.Errorf("the pruner selected %v, which must never include the tidied folder", got)
	}
}

func TestMoveFileIntoCreatesParentsAndMoves(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.json")
	if err := os.WriteFile(src, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "tidied-20260805", "Claude", "acct", "org", "a.json")
	if err := moveFileInto(src, dst); err != nil {
		t.Fatalf("moveFileInto: %v", err)
	}
	if _, err := os.Stat(src); err == nil {
		t.Error("the source is still there")
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "hello" {
		t.Errorf("destination = %q, %v", got, err)
	}
}

// A second run on the same day lands in the same folder. Whatever is already
// there came from the earlier run, so it is the copy worth keeping.
func TestMoveFileIntoRefusesToOverwrite(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.json")
	dst := filepath.Join(root, "dest", "a.json")
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{src, dst} {
		if err := os.WriteFile(p, []byte(filepath.Base(filepath.Dir(p))), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := moveFileInto(src, dst); err == nil {
		t.Error("moveFileInto overwrote an existing destination")
	}
	if got, _ := os.ReadFile(dst); string(got) != "dest" {
		t.Errorf("destination = %q, want it untouched", got)
	}
	if _, err := os.Stat(src); err != nil {
		t.Error("the source was removed even though nothing was moved")
	}
}

func TestRemoveIfEmptyLeavesAFolderThatStillHoldsSomething(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "keep")
	if err := os.MkdirAll(keep, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keep, "still-here.json"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	removeIfEmpty(keep)
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("a folder that still holds a conversation was removed: %v", err)
	}
}

// An emptied folder goes, and so does a .DS_Store that came with it: that is
// the operating system's leftover, not the user's file.
func TestRemoveIfEmptyRemovesAnEmptiedFolder(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name  string
		extra []string
	}{
		{"plain", nil},
		{"with os metadata", []string{".DS_Store"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(root, tc.name)
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			for _, e := range tc.extra {
				if err := os.WriteFile(filepath.Join(dir, e), []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			removeIfEmpty(dir)
			if _, err := os.Stat(dir); err == nil {
				t.Error("the emptied folder is still there")
			}
		})
	}
}

// writeTidyProfile builds a profile on disk: a config.json naming the account and
// the organization it reads, plus whatever buckets are asked for.
func writeTidyProfile(t *testing.T, root, name, account, org string, buckets map[bucket][]string) *platform.ProfileInfo {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{}
	if account != "" {
		cfg["lastKnownAccountUuid"] = account
	}
	if org != "" {
		// The stamp GetProfileActiveOrgUUID reads: an RFC3339 string, not a
		// number. Newest wins, and there is only one here.
		cfg["dxt:allowlistLastUpdated:"+org] = tNow.Format(time.RFC3339)
	}
	blob, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), blob, 0644); err != nil {
		t.Fatal(err)
	}
	for b, files := range buckets {
		bd := filepath.Join(dir, "claude-code-sessions", b.Account, b.Org)
		if err := os.MkdirAll(bd, 0755); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			p := filepath.Join(bd, f)
			if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(name+"/"+b.Account+"/"+b.Org+"/"+f), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return &platform.ProfileInfo{Name: name, Path: dir}
}

func TestTidyMisfiledEndToEnd(t *testing.T) {
	root := t.TempDir()
	backups := t.TempDir()

	p1 := writeTidyProfile(t, root, "Claude", "acctA", "orgA", map[bucket][]string{
		{"acctA", "orgA"}: {"s1.json", "s2.json"}, // read
		{"acctB", "orgB"}: {"s1.json"},            // stray, counterpart in p2's read bucket
		{"acctC", "orgC"}: {"orphan.json"},        // nothing anywhere else
	})
	p2 := writeTidyProfile(t, root, "Claude_Profile2", "acctB", "orgB", map[bucket][]string{
		{"acctB", "orgB"}: {"s1.json"}, // read
	})

	TidyMisfiled([]*platform.ProfileInfo{p1, p2}, backups)

	// The stray moved, structure preserved.
	moved := filepath.Join(backups, tidiedDirName(time.Now()), "Claude", "acctB", "orgB", "s1.json")
	if _, err := os.Stat(moved); err != nil {
		t.Errorf("the stray did not arrive at %s: %v", moved, err)
	}
	if _, err := os.Stat(filepath.Join(p1.Path, "claude-code-sessions", "acctB", "orgB", "s1.json")); err == nil {
		t.Error("the stray is still in the profile: it was copied, not moved")
	}
	// The emptied bucket and its account folder are gone.
	if _, err := os.Stat(filepath.Join(p1.Path, "claude-code-sessions", "acctB")); err == nil {
		t.Error("the emptied account folder is still there")
	}
	// Everything else untouched.
	for _, keep := range []string{
		filepath.Join(p1.Path, "claude-code-sessions", "acctA", "orgA", "s1.json"),
		filepath.Join(p1.Path, "claude-code-sessions", "acctA", "orgA", "s2.json"),
		filepath.Join(p1.Path, "claude-code-sessions", "acctC", "orgC", "orphan.json"),
		filepath.Join(p2.Path, "claude-code-sessions", "acctB", "orgB", "s1.json"),
	} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("%s was taken and should not have been: %v", keep, err)
		}
	}
}

// A profile with no account signed in must come through completely untouched,
// even though every file it holds has a counterpart elsewhere.
func TestTidyMisfiledLeavesAProfileWithNoAccountAlone(t *testing.T) {
	root := t.TempDir()
	backups := t.TempDir()

	p1 := writeTidyProfile(t, root, "Claude", "acctA", "orgA", map[bucket][]string{
		{"acctA", "orgA"}: {"s1.json"},
	})
	// No account, no organization stamp: nothing can be said about what it reads.
	p2 := writeTidyProfile(t, root, "Claude_NotSignedIn", "", "", map[bucket][]string{
		{"acctA", "orgA"}: {"s1.json"},
		{"acctZ", "orgZ"}: {"s1.json"},
	})

	TidyMisfiled([]*platform.ProfileInfo{p1, p2}, backups)

	for _, keep := range []string{
		filepath.Join(p2.Path, "claude-code-sessions", "acctA", "orgA", "s1.json"),
		filepath.Join(p2.Path, "claude-code-sessions", "acctZ", "orgZ", "s1.json"),
	} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("a profile whose read folder is unknown lost %s: %v", keep, err)
		}
	}
}

// A run against a machine with nothing to tidy must move nothing and must not
// create the destination folder. That is the state of every machine after the
// first run, so it is the case that runs forever.
func TestTidyMisfiledDoesNothingWhenThereIsNothingToDo(t *testing.T) {
	root := t.TempDir()
	backups := t.TempDir()
	p := writeTidyProfile(t, root, "Claude", "acctA", "orgA", map[bucket][]string{
		{"acctA", "orgA"}: {"s1.json"},
	})

	TidyMisfiled([]*platform.ProfileInfo{p}, backups)

	entries, err := os.ReadDir(backups)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("created %v in the backup folder with nothing to tidy", names)
	}
	if _, err := os.Stat(filepath.Join(p.Path, "claude-code-sessions", "acctA", "orgA", "s1.json")); err != nil {
		t.Errorf("the read bucket lost a file: %v", err)
	}
}

// Both hosts call StartTidyMisfiled and nothing else. It must return at once
// and must survive a platform that cannot list profiles.
func TestStartTidyMisfiledReturnsImmediately(t *testing.T) {
	done := make(chan struct{})
	go func() {
		StartTidyMisfiled(platform.New())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("StartTidyMisfiled blocked: both hosts call it on the startup path")
	}
}
