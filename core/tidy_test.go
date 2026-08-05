package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
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

// tidiedProfileDir finds a profile's folder inside a tidied run. The name
// carries a digest of the profile path, so it cannot be reconstructed from the
// display name alone.
func tidiedProfileDir(t *testing.T, backups, profile string) string {
	t.Helper()
	run := tidiedDirIn(t, backups)
	entries, err := os.ReadDir(run)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), profile+"-") {
			return filepath.Join(run, e.Name())
		}
	}
	t.Fatalf("no folder for %s in %s", profile, run)
	return ""
}

// tidiedDirIn finds the one tidied-<date> folder a run created.
func tidiedDirIn(t *testing.T, backups string) string {
	t.Helper()
	entries, err := os.ReadDir(backups)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "tidied-") {
			return filepath.Join(backups, e.Name())
		}
	}
	t.Fatalf("no tidied-* folder in %s", backups)
	return ""
}

// scannedAs is what readBucketFiles would have recorded for a file.
func scannedAs(t *testing.T, p string) bucketFile {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return bucketFile{Rel: filepath.Base(p), MTime: fi.ModTime(), Size: fi.Size()}
}

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

// Equal times per FILE are the common case: 425 of the 564 measured were
// byte-identical with identical times, because copyFile preserves them. What
// must be older is the BUCKET, not each file against its own counterpart: the
// folder in use goes on being written, so its newest file outruns everything in
// a folder that stopped receiving writes when v0.11.2 shipped.
func TestTidyMovesAFileWhoseCounterpartHasTheSameTime(t *testing.T) {
	got := tidyCandidates([]profileSessions{{
		Profile: "Claude", ReadKnown: true, Read: bucket{"acctA", "orgA"},
		Buckets: map[bucket][]bucketFile{
			{"acctA", "orgA"}: {file("s1.json", tOlder), file("recent.json", tNow)},
			{"acctB", "orgB"}: {file("s1.json", tOlder)},
		},
	}})
	if len(got) != 1 {
		t.Errorf("candidates = %v, want the stray to move: its counterpart has the same time, and the folder in use is newer overall", moveRels(got))
	}
}

// Within the profile's OWN account, an organization it has been signed into is
// never touched: membership is a fact, while which organization is active is a
// guess, and the two cannot be told apart when the guess is wrong. This is the
// pure-function half of the live-organization protection.
func TestTidyLeavesAnOrganizationTheProfileHasBeenSignedInto(t *testing.T) {
	got := tidyCandidates([]profileSessions{{
		Profile: "Claude", ReadKnown: true, Read: bucket{"acctA", "orgB"},
		SignedInOrgs: map[string]bool{"orgA": true, "orgB": true},
		Buckets: map[bucket][]bucketFile{
			{"acctA", "orgB"}: {file("s1.json", tNow)},
			{"acctA", "orgA"}: {file("s1.json", tNow)}, // been here; may be where the user is
		},
	}})
	if len(got) != 0 {
		t.Errorf("candidates = %v, want none: this profile has been signed into orgA, so it may be the one it is reading", moveRels(got))
	}
}

// An organization with no stamp has never been opened by this profile, so it
// cannot be the one being read. That is what the defect produced: the old sync
// copied conversations in under the SOURCE profile's organization.
func TestTidyMovesAnOrganizationTheProfileHasNeverOpened(t *testing.T) {
	got := tidyCandidates([]profileSessions{{
		Profile: "Claude", ReadKnown: true, Read: bucket{"acctA", "orgB"},
		SignedInOrgs: map[string]bool{"orgB": true},
		Buckets: map[bucket][]bucketFile{
			{"acctA", "orgB"}:        {file("s1.json", tNow)},
			{"acctA", "orgSTRANGER"}: {file("s1.json", tNow)},
		},
	}})
	if len(got) != 1 {
		t.Errorf("candidates = %v, want the stray to move", moveRels(got))
	}
}

// Another ACCOUNT is safe whatever the organization: lastKnownAccountUuid is a
// recorded fact, not a guess, so a bucket under a different account cannot be
// the one being read.
func TestTidyMovesAnotherAccountsBucketEvenUnderAKnownOrganization(t *testing.T) {
	got := tidyCandidates([]profileSessions{{
		Profile: "Claude", ReadKnown: true, Read: bucket{"acctA", "orgA"},
		SignedInOrgs: map[string]bool{"orgA": true},
		Buckets: map[bucket][]bucketFile{
			{"acctA", "orgA"}: {file("s1.json", tNow)},
			{"acctB", "orgA"}: {file("s1.json", tNow)},
		},
	}})
	if len(got) != 1 {
		t.Errorf("candidates = %v, want the other account's bucket to move", moveRels(got))
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
	if err := moveFileInto(src, dst, scannedAs(t, src)); err != nil {
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
	if err := moveFileInto(src, dst, scannedAs(t, src)); err == nil {
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
	readBucket := bucket{Account: account, Org: org}
	for b, files := range buckets {
		// The folder in use has been written more recently than the strays,
		// which is both how a real machine looks and what the retention guard
		// in tidyCandidates requires.
		at := tNow.Add(-30 * 24 * time.Hour)
		if b == readBucket {
			at = tNow
		}
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
			if err := os.Chtimes(p, at, at); err != nil {
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
	// Found rather than reconstructed: building the name from a second
	// time.Now() fails on a run that straddles midnight.
	moved := filepath.Join(tidiedProfileDir(t, backups, "Claude"), "acctB", "orgB", "s1.json")
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

// StartTidyMisfiled is deliberately NOT exercised here.
//
// It resolves the real machine's profiles and the real backup root, so calling
// it from a test moves the conversations of whoever is running go test. An
// earlier version of this file did exactly that, and the only reason it caused
// no harm is that the machine it ran on had nothing left to tidy. It also
// asserted nothing: the entire body of StartTidyMisfiled is a goroutine launch,
// so it cannot block, and the goroutine outlived the test.
//
// What that test was reaching for is covered where it can be covered safely:
// TidyMisfiled is driven directly against a temporary directory throughout this
// file, and the one line each host calls is a goroutine launch with nothing in
// it worth a test.

// Reproduction for the worst case available here: the organization heuristic
// being WRONG, rather than unreadable.
//
// GetProfileActiveOrgUUID picks the newest dxt:allowlistLastUpdated stamp, and
// Claude Desktop refreshes that once per launch for the organization it
// launched into. Someone who launches into orgB and then switches to orgA
// in-app, without relaunching, leaves orgB holding the newest stamp while
// Claude actually reads orgA.
//
// The pre-0.11.2 defect is what makes this fatal rather than merely wrong: it
// is precisely what put the same conversation names under both organizations of
// one account, and copyFile preserves mtimes, so every file in the live orgA
// bucket has an equal-time counterpart in the believed-read orgB bucket. Without
// a guard, all of them qualify, all of them move, and the directory is removed.
// The user's current conversations disappear from the app.
//
// platform/activeorg.go's own comment says being wrong there "costs visibility,
// never data". This code is what would have made that false.
func TestTidyDoesNotEvacuateALiveOrgWhenTheHeuristicPicksTheWrongOne(t *testing.T) {
	root := t.TempDir()
	backups := t.TempDir()

	dir := filepath.Join(root, "Claude")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// orgB carries the newest stamp, so the heuristic answers orgB. Both orgs
	// carry one, because the user has been signed into both.
	cfg := map[string]any{
		"lastKnownAccountUuid":          "acctA",
		"dxt:allowlistLastUpdated:orgA": tNow.Add(-72 * time.Hour).Format(time.RFC3339),
		"dxt:allowlistLastUpdated:orgB": tNow.Format(time.RFC3339),
	}
	blob, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), blob, 0644); err != nil {
		t.Fatal(err)
	}
	write := func(org, name string, at time.Time) string {
		p := filepath.Join(dir, "claude-code-sessions", "acctA", org, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(org+"/"+name), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Equal times, which is the ordinary case: copyFile preserves them, so a
	// conversation the user has not reopened has the same time on both sides.
	stamp := tNow.Add(-10 * 24 * time.Hour)
	live := []string{
		write("orgA", "s1.json", stamp),
		write("orgA", "s2.json", stamp),
	}
	write("orgB", "s1.json", stamp)
	write("orgB", "s2.json", stamp)
	// orgB also holds one file newer than anything in orgA. This is not a
	// contrived detail: orgB is where the last launch did its writing, which is
	// the causal reason its stamp is the newest one. An earlier guard compared
	// exactly this and therefore agreed with the wrong stamp, and the data loss
	// came straight back.
	write("orgB", "from-the-last-launch.json", stamp.Add(time.Second))

	TidyMisfiled([]*platform.ProfileInfo{{Name: "Claude", Path: dir}}, backups)

	for _, p := range live {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("a live conversation was moved out of the organization the user is working in: %s: %v", filepath.Base(p), err)
		}
	}
}

// A profile whose believed-read folder holds nothing is not describing this
// profile's reality, so nothing in it may be called abandoned. Real cause: a
// stale account or organization record naming a folder that no longer exists.
func TestTidySkipsAProfileWhoseReadBucketIsEmpty(t *testing.T) {
	got := tidyCandidates([]profileSessions{{
		Profile: "Claude", ReadKnown: true, Read: bucket{"acctA", "orgGONE"},
		Buckets: map[bucket][]bucketFile{
			// The believed-read folder is not among them.
			{"acctA", "orgA"}: {file("s1.json", tOlder)},
			{"acctB", "orgB"}: {file("s1.json", tOlder)},
		},
	}})
	if len(got) != 0 {
		t.Errorf("candidates = %v, want none: a read folder holding nothing cannot be the one in use", moveRels(got))
	}
	// And scanForTidy is what sets ReadKnown from that check, so the same case
	// must survive the real scan.
	root := t.TempDir()
	p := writeTidyProfile(t, root, "Claude", "acctA", "orgGONE", map[bucket][]string{
		{"acctA", "orgA"}: {"s1.json"},
	})
	for _, ps := range scanForTidy([]*platform.ProfileInfo{p}) {
		if ps.ReadKnown {
			t.Error("scanForTidy trusted a read folder that holds nothing")
		}
	}
}

// The window between deciding and acting. The scan of every profile takes as
// long as it takes, and a sync or a switch writing into that folder meanwhile
// must not have its work moved away on the strength of a judgement about a
// different file.
func TestMoveFileIntoRefusesASourceThatChanged(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.json")
	if err := os.WriteFile(src, []byte("as scanned"), 0644); err != nil {
		t.Fatal(err)
	}
	scanned := scannedAs(t, src)

	// Rewritten since, as a sync would.
	later := scanned.MTime.Add(time.Minute)
	if err := os.WriteFile(src, []byte("written since"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(src, later, later); err != nil {
		t.Fatal(err)
	}

	if err := moveFileInto(src, filepath.Join(root, "dest", "a.json"), scanned); err == nil {
		t.Error("moveFileInto moved a file that had changed since it was examined")
	}
	if got, _ := os.ReadFile(src); string(got) != "written since" {
		t.Errorf("source = %q, want it left where it was", got)
	}
}

// A bucket whose every move failed has not been emptied by this run, and
// removing it because something else emptied it meanwhile is not this code's
// call to make.
func TestTidyDoesNotRemoveABucketWhoseMovesAllFailed(t *testing.T) {
	root := t.TempDir()
	backups := t.TempDir()

	p1 := writeTidyProfile(t, root, "Claude", "acctA", "orgA", map[bucket][]string{
		{"acctA", "orgA"}: {"s1.json"},
		{"acctB", "orgB"}: {"s1.json"},
	})
	p2 := writeTidyProfile(t, root, "Claude_Profile2", "acctB", "orgB", map[bucket][]string{
		{"acctB", "orgB"}: {"s1.json"},
	})

	// Block the only move by occupying its destination. The folder name carries
	// a digest of the profile path, so it is built the same way the code does.
	blocked := filepath.Join(backups, tidiedDirName(time.Now()),
		destDirFor(tidyMove{Profile: "Claude", ProfilePath: p1.Path}), "acctB", "orgB", "s1.json")
	if err := os.MkdirAll(filepath.Dir(blocked), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocked, []byte("from an earlier run"), 0644); err != nil {
		t.Fatal(err)
	}

	TidyMisfiled([]*platform.ProfileInfo{p1, p2}, backups)

	stray := filepath.Join(p1.Path, "claude-code-sessions", "acctB", "orgB")
	if _, err := os.Stat(filepath.Join(stray, "s1.json")); err != nil {
		t.Errorf("the file was removed even though its move failed: %v", err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Errorf("the bucket was removed even though nothing was moved out of it: %v", err)
	}
	if got, _ := os.ReadFile(blocked); string(got) != "from an earlier run" {
		t.Errorf("the existing destination was overwritten: %q", got)
	}
}

// Nested buckets are emptied all the way up. An earlier version stopped at the
// bucket and account levels, so a bucket holding only projects/x/s1.json kept
// projects/x and was never removed at all.
func TestTidyRemovesNestedEmptiedDirectories(t *testing.T) {
	root := t.TempDir()
	backups := t.TempDir()

	p1 := writeTidyProfile(t, root, "Claude", "acctA", "orgA", map[bucket][]string{
		{"acctA", "orgA"}: {"live.json"},
		{"acctB", "orgB"}: {filepath.Join("projects", "x", "s1.json")},
	})
	p2 := writeTidyProfile(t, root, "Claude_Profile2", "acctB", "orgB", map[bucket][]string{
		{"acctB", "orgB"}: {filepath.Join("projects", "x", "s1.json")},
	})

	TidyMisfiled([]*platform.ProfileInfo{p1, p2}, backups)

	if _, err := os.Stat(filepath.Join(p1.Path, "claude-code-sessions", "acctB")); err == nil {
		t.Error("the emptied account folder is still there, so the nested directories under it were not cleaned up")
	}
}

// Two profiles carrying the SAME display name is a real state on the Windows
// Store build: the live slot and a container directory both answer to it after
// a swap whose state write failed. Anything that resolved a name back to a path
// would plan against one and act on the other, and the repository already
// records that the inverse mapping does not exist. Every move therefore carries
// its own path.
func TestTidyKeepsTwoProfilesWithTheSameNameApart(t *testing.T) {
	root := t.TempDir()
	backups := t.TempDir()

	// Same Name, different Path, each with its own stray.
	a := writeTidyProfile(t, filepath.Join(root, "slot"), "Claude", "acctA", "orgA", map[bucket][]string{
		{"acctA", "orgA"}: {"live.json"},
		{"acctB", "orgB"}: {"stray-a.json"},
	})
	b := writeTidyProfile(t, filepath.Join(root, "container"), "Claude", "acctB", "orgB", map[bucket][]string{
		{"acctB", "orgB"}: {"stray-a.json", "stray-b.json"},
		{"acctA", "orgA"}: {"stray-b.json"},
	})

	TidyMisfiled([]*platform.ProfileInfo{a, b}, backups)

	// Each stray left its OWN profile.
	for _, gone := range []string{
		filepath.Join(a.Path, "claude-code-sessions", "acctB", "orgB", "stray-a.json"),
		filepath.Join(b.Path, "claude-code-sessions", "acctA", "orgA", "stray-b.json"),
	} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s was not moved: a move was executed against the wrong profile", gone)
		}
	}
	// And neither profile's read folder was touched.
	for _, kept := range []string{
		filepath.Join(a.Path, "claude-code-sessions", "acctA", "orgA", "live.json"),
		filepath.Join(b.Path, "claude-code-sessions", "acctB", "orgB", "stray-a.json"),
		filepath.Join(b.Path, "claude-code-sessions", "acctB", "orgB", "stray-b.json"),
	} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s was taken from a folder in use: %v", kept, err)
		}
	}
}

// A systemic failure must end the run rather than logging one line per file.
// The real cause is a redirected AppData making every rename a cross-device
// error, which would otherwise put 564 lines in the log at every launch forever.
func TestTidyStopsAfterRepeatedFailures(t *testing.T) {
	root := t.TempDir()
	backups := t.TempDir()

	var strays []string
	for i := 0; i < tidyGiveUpAfter+5; i++ {
		strays = append(strays, fmt.Sprintf("s%02d.json", i))
	}
	p1 := writeTidyProfile(t, root, "Claude", "acctA", "orgA", map[bucket][]string{
		{"acctA", "orgA"}: {"live.json"},
		{"acctB", "orgB"}: strays,
	})
	p2 := writeTidyProfile(t, root, "Claude_Profile2", "acctB", "orgB", map[bucket][]string{
		{"acctB", "orgB"}: strays,
	})

	// Every destination is already taken, so every move fails.
	dest := filepath.Join(backups, tidiedDirName(time.Now()),
		destDirFor(tidyMove{Profile: "Claude", ProfilePath: p1.Path}), "acctB", "orgB")
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	for _, s := range strays {
		if err := os.WriteFile(filepath.Join(dest, s), []byte("taken"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	TidyMisfiled([]*platform.ProfileInfo{p1, p2}, backups)

	// Nothing moved, and every stray is still in place: giving up must not be a
	// way of losing files.
	for _, s := range strays {
		if _, err := os.Stat(filepath.Join(p1.Path, "claude-code-sessions", "acctB", "orgB", s)); err != nil {
			t.Fatalf("%s was lost while the run was giving up: %v", s, err)
		}
	}
}
