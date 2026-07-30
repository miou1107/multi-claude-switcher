# Ghost Account Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every platform a reachable way to create a new account profile and to recover an account that was signed out inside Claude Desktop, and make two profiles holding one account impossible to leave in place.

**Architecture:** `core` gains four pure-ish units (pending registry, name validation, archive, merge) plus one orchestrator (`ProfileCreator`) that drives the `platform` interface. `platform` gains three methods so each OS supplies its own profile-creation, recovery-preparation, and archive-root strategy while callers stay platform-blind. `internal/panelui` gains two screens and two decorations; both webview hosts wire the same actions.

**Tech Stack:** Go 1.x, standard library only. `fyne.io/systray`, CGO Objective-C (macOS menu bar), `jchv/go-webview2` (Windows panel). Tests are stdlib `testing`, table-driven, temp dirs via `t.TempDir()`.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-30-ghost-account-recovery-design.md`. Every task's requirements implicitly include it.
- **`platform` must never import `core`.** `core` imports `platform` (`core/scan.go:10`). Name validation therefore lives in `core` and runs before any `platform` call.
- **MCS never deletes user data.** The strongest permitted action is a rename into an archive root. No `os.RemoveAll` on a Claude profile, ever.
- **Order of operations everywhere: verify, then copy, then move, then update state.** `managed.json` and `pending.json` are written last so MCS's view is never ahead of the disk.
- **Escaping:** folder names reach JS via `data-*` attributes read through `dataset`, never as inline JS string arguments. This is the v0.9.1 bug class (`4b2fb61`).
- All repo output is English: code, comments, docs, commit messages.
- Commit messages: no `Co-Authored-By` trailer.
- Copy style, verbatim from spec §4.2: recoverable ghosts read `Signed out in Claude Desktop` with note `Its conversations are still here. Recover to sign back in.` in the blue `note-todo` style. Red (`note-bad`) is reserved for dead ghosts and the duplicate warning.
- Panel width is 400px. Nothing may widen it.
- Branch: `ghost-account-recovery`, already created, spec committed at `348ccda`.

**Clarification this plan locks in (spec §4.1 is singular-framed):** the duplicate warning addresses **one group at a time** — the group whose first member sorts first by folder name. After that group is merged, if another remains the warning reappears for it. There is no multi-group merge UI.

---

### Task 1: Pending-sign-in registry

A profile MCS just created has no `config.json`, so the scanner would drop it or misfile it. This registry is how the scanner learns such folders exist. It lives in MCS's own directory, not inside a Claude profile.

**Files:**
- Create: `core/pending.go`
- Test: `core/pending_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type PendingProfile struct { Folder string; ExpectUUID string; CreatedAt string }`
  - `func LoadPending() []PendingProfile`
  - `func AddPending(folder, expectUUID string) error`
  - `func RemovePending(folder string) error`
  - `func StalePending(pending []PendingProfile, profiles []*platform.ProfileInfo) []string`
  - `var pendingPath func() string` (test seam, mirrors `managedPath` at `core/managed.go:14`)

- [ ] **Step 1: Write the failing tests**

```go
// core/pending_test.go
package core

import (
	"path/filepath"
	"testing"

	"github.com/miou1107/multi-claude-switcher/platform"
)

func withStubbedPending(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	orig := pendingPath
	pendingPath = func() string { return filepath.Join(dir, "pending.json") }
	t.Cleanup(func() { pendingPath = orig })
}

func TestPendingAbsentIsEmpty(t *testing.T) {
	withStubbedPending(t)
	if got := LoadPending(); len(got) != 0 {
		t.Fatalf("want no entries, got %+v", got)
	}
}

func TestPendingAddLoadRemove(t *testing.T) {
	withStubbedPending(t)
	if err := AddPending("Claude_Work", "uuid-a"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := AddPending("Claude_Personal", ""); err != nil {
		t.Fatalf("add empty uuid: %v", err)
	}
	got := LoadPending()
	if len(got) != 2 {
		t.Fatalf("want 2, got %+v", got)
	}
	if got[0].Folder != "Claude_Work" || got[0].ExpectUUID != "uuid-a" || got[0].CreatedAt == "" {
		t.Fatalf("entry0: %+v", got[0])
	}
	if got[1].ExpectUUID != "" {
		t.Fatalf("add path must allow an empty expectUUID: %+v", got[1])
	}
	if err := RemovePending("Claude_Work"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got = LoadPending()
	if len(got) != 1 || got[0].Folder != "Claude_Personal" {
		t.Fatalf("after remove: %+v", got)
	}
}

func TestPendingAddIsIdempotentPerFolder(t *testing.T) {
	withStubbedPending(t)
	_ = AddPending("Claude_Work", "uuid-a")
	_ = AddPending("Claude_Work", "uuid-b")
	got := LoadPending()
	if len(got) != 1 || got[0].ExpectUUID != "uuid-b" {
		t.Fatalf("re-adding a folder must replace it, got %+v", got)
	}
}

func TestRemovePendingMissingIsNotAnError(t *testing.T) {
	withStubbedPending(t)
	if err := RemovePending("nope"); err != nil {
		t.Fatalf("removing an absent folder must be a no-op, got %v", err)
	}
}

func TestStalePending(t *testing.T) {
	// signedIn has a live login, so its pending entry has served its purpose.
	// waiting has no login yet and must be kept. gone no longer exists on disk.
	dir := t.TempDir()
	signedIn := writeProfile(t, dir, "Claude_SignedIn", "uuid-a", nil)
	waiting := &platform.ProfileInfo{Name: "Claude_Waiting", Path: filepath.Join(dir, "Claude_Waiting"), Exists: true}

	pending := []PendingProfile{
		{Folder: "Claude_SignedIn", ExpectUUID: "uuid-a"},
		{Folder: "Claude_Waiting", ExpectUUID: "uuid-b"},
		{Folder: "Claude_Gone", ExpectUUID: "uuid-c"},
	}
	got := StalePending(pending, []*platform.ProfileInfo{signedIn, waiting})

	want := map[string]bool{"Claude_SignedIn": true, "Claude_Gone": true}
	if len(got) != 2 {
		t.Fatalf("want 2 stale, got %v", got)
	}
	for _, f := range got {
		if !want[f] {
			t.Fatalf("unexpected stale folder %q (got %v)", f, got)
		}
	}
}
```

`writeProfile` is a shared test helper created in Task 3. For this task, add it to `core/pending_test.go` temporarily only if Task 3 has not landed yet; otherwise reuse it. To avoid a duplicate-symbol conflict, **create the helper in Task 3 and run this task after it**, or define it here and have Task 3 reuse it. This plan orders Task 1 first, so **define the helper here**:

```go
// writeProfile creates a profile dir under root with an optional live login and
// session buckets, and returns the ProfileInfo the scanner would see. buckets
// maps account UUID to the number of .json session files to create.
func writeProfile(t *testing.T, root, name, liveUUID string, buckets map[string]int) *platform.ProfileInfo {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "{}"
	if liveUUID != "" {
		cfg = `{"lastKnownAccountUuid":"` + liveUUID + `"}`
	}
	if err := os.WriteFile(filepath.Join(path, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	info := &platform.ProfileInfo{Name: name, Path: path, Exists: true, UUIDBuckets: map[string]int{}}
	for uuid, n := range buckets {
		bucket := filepath.Join(path, "claude-code-sessions", uuid)
		if err := os.MkdirAll(bucket, 0o755); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < n; i++ {
			f := filepath.Join(bucket, "local_"+uuid+"_"+strconv.Itoa(i)+".json")
			if err := os.WriteFile(f, []byte(`{}`), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		info.UUIDBuckets[uuid] = n
		info.HasSessionsDir = true
	}
	return info
}
```

Add `"os"` and `"strconv"` to the test file's imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/ -run 'TestPending|TestStalePending|TestRemovePending' -v`
Expected: FAIL — `undefined: pendingPath`, `undefined: LoadPending`, and so on.

- [ ] **Step 3: Write the implementation**

```go
// core/pending.go
package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/miou1107/multi-claude-switcher/platform"
)

var pendingMu sync.Mutex

// pendingPath is where the pending-sign-in registry is stored. It is a var so
// tests can redirect it to a temp dir (same pattern as managed.go's managedPath).
var pendingPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "multi-claude-switcher-pending.json")
	}
	return filepath.Join(home, ".multi-claude-switcher", "pending.json")
}

// PendingProfile is a profile MCS created that is waiting for its one-time
// sign-in. It exists because a brand-new profile dir has no config.json, so
// nothing on disk distinguishes it from a stray directory until Claude has run
// in it — and the user has just been told to go and sign in to it, so it must
// stay visible in the panel until they do.
type PendingProfile struct {
	Folder string `json:"folder"`
	// ExpectUUID names the account this profile was created to receive, set on
	// the recovery path. Empty on the plain add path, which accepts any account.
	ExpectUUID string `json:"expectUUID,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

type pendingFile struct {
	Pending []PendingProfile `json:"pending"`
}

// LoadPending returns the pending-sign-in entries, or nil when the file is
// absent or unreadable. Unlike LoadManaged there is no first-run distinction to
// preserve: no entries and no file mean the same thing.
func LoadPending() []PendingProfile {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	return loadPendingLocked()
}

func loadPendingLocked() []PendingProfile {
	data, err := os.ReadFile(pendingPath())
	if err != nil {
		return nil
	}
	var pf pendingFile
	if json.Unmarshal(data, &pf) != nil {
		return nil
	}
	return pf.Pending
}

func savePendingLocked(entries []PendingProfile) error {
	if entries == nil {
		entries = []PendingProfile{}
	}
	data, err := json.MarshalIndent(pendingFile{Pending: entries}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(pendingPath()), 0o755); err != nil {
		return err
	}
	// Atomic write: a crash mid-write must not corrupt the registry.
	tmp := pendingPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, pendingPath()); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// AddPending records a folder as awaiting sign-in, replacing any existing entry
// for the same folder so a retried create cannot leave two.
func AddPending(folder, expectUUID string) error {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	entries := loadPendingLocked()
	out := make([]PendingProfile, 0, len(entries)+1)
	for _, e := range entries {
		if e.Folder != folder {
			out = append(out, e)
		}
	}
	out = append(out, PendingProfile{
		Folder:     folder,
		ExpectUUID: expectUUID,
		CreatedAt:  time.Now().Format(time.RFC3339),
	})
	return savePendingLocked(out)
}

// RemovePending drops a folder's entry. Removing an absent folder is a no-op,
// so callers can prune unconditionally.
func RemovePending(folder string) error {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	entries := loadPendingLocked()
	out := make([]PendingProfile, 0, len(entries))
	changed := false
	for _, e := range entries {
		if e.Folder == folder {
			changed = true
			continue
		}
		out = append(out, e)
	}
	if !changed {
		return nil
	}
	return savePendingLocked(out)
}

// StalePending returns the folders whose pending entry no longer applies: the
// folder now has a live login (the sign-in happened) or is gone from disk. Pure
// so the rule is testable without a real profile tree; callers pass the result
// to RemovePending.
func StalePending(pending []PendingProfile, profiles []*platform.ProfileInfo) []string {
	byName := map[string]*platform.ProfileInfo{}
	for _, p := range profiles {
		byName[p.Name] = p
	}
	var stale []string
	for _, e := range pending {
		p, ok := byName[e.Folder]
		if !ok {
			stale = append(stale, e.Folder) // folder no longer exists
			continue
		}
		if _, err := platform.GetProfileAccountUUID(p.Path); err == nil {
			stale = append(stale, e.Folder) // signed in; entry has served its purpose
		}
	}
	return stale
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/ -run 'TestPending|TestStalePending|TestRemovePending' -v`
Expected: PASS, 5 tests.

- [ ] **Step 5: Commit**

```bash
git add core/pending.go core/pending_test.go
git commit -m "core: pending-sign-in registry for freshly created profiles"
```

---

### Task 2: Profile name validation

Runs before anything touches disk, so a bad name never leaves a half-made profile. Generic rules only; MSIX keeps its own extra checks (`platform/windows_msix.go:151`).

**Files:**
- Create: `core/profilename.go`
- Test: `core/profilename_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func ValidateProfileName(name string) error`
  - `func ProfileFolderName(name string) string` — returns `"Claude_" + trimmed name`
  - `const ProfileFolderPrefix = "Claude_"`

- [ ] **Step 1: Write the failing tests**

```go
// core/profilename_test.go
package core

import "testing"

func TestValidateProfileName(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"plain", "Personal", false},
		{"with space", "Work Team", false},
		{"with dash and underscore", "work-2_b", false},
		{"digits", "Acct2", false},
		{"trims to valid", "  Personal  ", false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"forward slash", "a/b", true},
		{"backslash", `a\b`, true},
		{"dot dot", "..", true},
		{"leading dot", ".hidden", true},
		{"colon", "a:b", true},
		{"asterisk", "a*b", true},
		{"question mark", "a?b", true},
		{"quote", `a"b`, true},
		{"angle brackets", "a<b>c", true},
		{"pipe", "a|b", true},
		{"newline", "a\nb", true},
		{"reserved bare Claude", "Claude", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateProfileName(c.in)
			if c.wantErr && err == nil {
				t.Fatalf("ValidateProfileName(%q) = nil, want an error", c.in)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("ValidateProfileName(%q) = %v, want nil", c.in, err)
			}
		})
	}
}

func TestProfileFolderName(t *testing.T) {
	if got := ProfileFolderName("  Work Team  "); got != "Claude_Work Team" {
		t.Fatalf("got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/ -run 'TestValidateProfileName|TestProfileFolderName' -v`
Expected: FAIL — `undefined: ValidateProfileName`.

- [ ] **Step 3: Write the implementation**

```go
// core/profilename.go
package core

import (
	"errors"
	"fmt"
	"strings"
)

// ProfileFolderPrefix is what a profile folder MCS creates is named with, so
// that platform.FindProfiles (which matches a "Claude" prefix) picks it up.
const ProfileFolderPrefix = "Claude_"

// reservedProfileName is the default profile's folder name. A user-supplied name
// producing it would collide with the profile Claude Desktop already owns.
const reservedProfileName = "Claude"

// ValidateProfileName reports whether name is usable for a new profile. It runs
// before anything is created, so a rejected name never leaves a partial profile
// behind. Platform-specific limits are checked separately by the platform layer
// (see platform/windows_msix.go's msixValidateNameIn).
func ValidateProfileName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("enter a name for this account")
	}
	if strings.EqualFold(name, reservedProfileName) {
		return fmt.Errorf("%q is taken by the default profile, pick another name", reservedProfileName)
	}
	if strings.HasPrefix(name, ".") {
		return errors.New("a name can't start with a dot")
	}
	if strings.Contains(name, "..") {
		return errors.New("a name can't contain ..")
	}
	// Allow letters, digits, space, dash, underscore. Everything else is either a
	// path separator, a Windows-illegal filename character, or a control
	// character, and the point of an allowlist is that none of them need naming.
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == ' ', r == '-', r == '_':
		default:
			return errors.New("use only letters, numbers, spaces, dashes and underscores")
		}
	}
	return nil
}

// ProfileFolderName maps a display name to the folder that holds it. Call only
// with a name ValidateProfileName has accepted.
func ProfileFolderName(name string) string {
	return ProfileFolderPrefix + strings.TrimSpace(name)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/ -run 'TestValidateProfileName|TestProfileFolderName' -v`
Expected: PASS, 20 subtests plus 1.

- [ ] **Step 5: Commit**

```bash
git add core/profilename.go core/profilename_test.go
git commit -m "core: validate profile names before anything is created"
```

---

### Task 3: Scanner — recoverable ghosts

A ghost with conversations in it can be brought back. One with an empty bucket cannot, and must keep reading as a dead end.

**Files:**
- Modify: `core/scan.go` — `ScannedAccount` (line 21), `assembleAccounts` (line 77), `rowRank` (line 148)
- Test: `core/scan_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `ScannedAccount.Recoverable bool`, `ScannedAccount.SourceFolder string`, `const RecoverableGhostNote`.

- [ ] **Step 1: Write the failing tests**

```go
// append to core/scan_test.go
func TestAssembleGhostRecoverable(t *testing.T) {
	// Machine 2's layout from the spec: one dir, live login cccccccc, plus an orphan
	// bbbbbbbb left behind by an in-app account switch.
	scans := []dirScan{
		{Folder: "Claude", LiveUUID: "cccccccc", Buckets: map[string]bucketStat{
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
	if ghost.SourceFolder != "Claude" {
		t.Fatalf("SourceFolder must name the dir holding the bucket: %+v", ghost)
	}
	if ghost.Note != RecoverableGhostNote {
		t.Fatalf("note = %q, want %q", ghost.Note, RecoverableGhostNote)
	}
}

func TestAssembleGhostEmptyBucketIsNotRecoverable(t *testing.T) {
	scans := []dirScan{
		{Folder: "Claude", LiveUUID: "live", Buckets: map[string]bucketStat{
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
	if ghost.Note != "Invalid account data" {
		t.Fatalf("dead ghost keeps its existing note, got %q", ghost.Note)
	}
}

func TestAssembleGhostSourceFolderPrefersFullestBucket(t *testing.T) {
	// The same orphan appears in two dirs. Recovery should copy from whichever
	// holds more of its conversations.
	scans := []dirScan{
		{Folder: "Claude", LiveUUID: "a", Buckets: map[string]bucketStat{
			"a": {Count: 1}, "orphan": {Count: 5, LastUpdated: ts("2026-07-01")},
		}},
		{Folder: "Claude_Two", LiveUUID: "b", Buckets: map[string]bucketStat{
			"b": {Count: 1}, "orphan": {Count: 40, LastUpdated: ts("2026-07-02")},
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
		t.Fatalf("counts still sum across dirs: %+v", ghost)
	}
	if ghost.SourceFolder != "Claude_Two" {
		t.Fatalf("SourceFolder = %q, want the dir with 40 conversations", ghost.SourceFolder)
	}
}

func TestRecoverableGhostSortsAboveDeadGhost(t *testing.T) {
	scans := []dirScan{
		{Folder: "Claude", LiveUUID: "live", Buckets: map[string]bucketStat{
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/ -run 'TestAssembleGhost|TestRecoverableGhost' -v`
Expected: FAIL — `ghost.Recoverable undefined`, `undefined: RecoverableGhostNote`.

- [ ] **Step 3: Write the implementation**

In `core/scan.go`, add to `ScannedAccount` (after the `SignedOut` field, line 37):

```go
	// Recoverable marks a ghost whose conversations can be brought back: its
	// bucket is non-empty, so giving the account its own profile and signing in
	// once reunites account and history. The credentials are gone for good; the
	// conversations never were. False for a ghost with an empty bucket, which
	// really is a dead end.
	Recoverable bool

	// SourceFolder names the profile folder whose bucket holds this ghost's
	// conversations, i.e. where a recovery copies from. When several dirs hold a
	// bucket for the same orphan, it is the one with the most conversations.
	// Ghost rows only.
	SourceFolder string
```

Add the note constant next to `SignedOutNote` (line 43):

```go
// RecoverableGhostNote is the review note for an account that was signed out
// inside Claude Desktop. It is deliberately not phrased as a defect: the data is
// intact, and what is missing is a profile that claims the account.
const RecoverableGhostNote = "Its conversations are still here. Recover to sign back in."
```

In `assembleAccounts`, replace the ghost accumulation loop (lines 101-117) with one that also tracks the fullest source:

```go
	ghost := map[string]*ScannedAccount{}
	ghostBest := map[string]int{} // uuid -> conversation count of its best source so far
	for _, s := range scans {
		for uuid, b := range s.Buckets {
			if uuid == s.LiveUUID || live[uuid] {
				continue // own live bucket, or stale dup of an account live elsewhere
			}
			g := ghost[uuid]
			if g == nil {
				g = &ScannedAccount{UUID: uuid, Complete: false, Account: AccountUnknown}
				ghost[uuid] = g
			}
			g.Convos += b.Count
			if b.LastUpdated.After(g.LastUpdated) {
				g.LastUpdated = b.LastUpdated
			}
			// Recovery copies from one dir, so pick the one holding the most of
			// this account's conversations. Ties break on folder name to keep the
			// choice deterministic across scans.
			if b.Count > ghostBest[uuid] || (b.Count == ghostBest[uuid] && s.Folder < g.SourceFolder) {
				ghostBest[uuid] = b.Count
				g.SourceFolder = s.Folder
			}
		}
	}
	for _, g := range ghost {
		g.Recoverable = g.Convos > 0
		if g.Recoverable {
			g.Note = RecoverableGhostNote
		} else {
			g.Note = deriveNote(false, AccountUnknown)
			g.SourceFolder = "" // nothing to copy from
		}
	}
```

Note the `Note` field is no longer set at construction time, so remove `Note: deriveNote(false, AccountUnknown)` from the `&ScannedAccount{...}` literal as shown above.

Replace `rowRank` (lines 148-157) with four bands:

```go
// rowRank orders the review: accounts you can switch to now, then folders
// waiting to be signed in to, then orphans you can recover, then orphans you
// cannot. Recoverable orphans sort above dead ones because they are the only
// ghost rows the user can act on.
func rowRank(a ScannedAccount) int {
	switch {
	case a.Complete:
		return 0
	case a.SignedOut:
		return 1
	case a.Recoverable:
		return 2
	default:
		return 3
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/ -run 'TestAssemble|TestRecoverableGhost|TestScanAccounts' -v`
Expected: PASS. The pre-existing `TestAssembleAccounts` still passes: its `33333333` ghost has 21 conversations, so it becomes `Recoverable` with the new note. **That assertion changes** — update the existing test's final check from `ghost.Note != "Invalid account data"` to `ghost.Note != RecoverableGhostNote`, and note in a comment that a populated bucket is now recoverable.

- [ ] **Step 5: Commit**

```bash
git add core/scan.go core/scan_test.go
git commit -m "core: mark ghosts with surviving conversations as recoverable"
```

---

### Task 4: CUT during self-review — do not implement

Spec §3.4 puts duplicate detection in the scanner, as `ScannedAccount.Duplicate` plus a `DuplicateGroups` helper. Writing the plan showed that layer has no consumer.

The duplicate warning lives on the account list, which `RenderList` draws from `[]ProfileVM` built by each host's `buildProfiles` from `FindProfiles` — not from `ScanAccounts`. `buildProfiles` already reads each profile's account UUID (`cmd/mcs-menubar/main.go:330`, currently discarding it), so the account list has everything duplicate detection needs and can group in the renderer for free. Routing it through the scanner instead would mean calling `ScanAccounts` on every panel open purely to learn something the host already knows, and `ScanAccounts` reads Local Storage per signed-in profile, which is the expensive part the panel deliberately renders around (`cmd/mcs-menubar/main.go:83`).

Nothing else consumes scanner-side duplicate state: the Rescan view has no duplicate treatment in the spec, and `mergeCandidate` resolves counts per folder rather than per group.

**Duplicate detection is therefore implemented in Task 11 (renderer), and spec §3.4 was corrected to match.** `ScannedAccount` gains no `Duplicate` field and no `DuplicateGroups` exists.

Task numbering is left as written so that every cross-reference in the tasks below stays valid. Skip straight from Task 3 to Task 5.

<details>
<summary>Original Task 4 content, kept for the record</summary>

**Files:**
- Modify: `core/scan.go` — `ScannedAccount`, `assembleAccounts`
- Test: `core/scan_test.go`

**Interfaces:**
- Consumes: Task 3's `ScannedAccount` shape.
- Produces: `ScannedAccount.Duplicate bool`, `func DuplicateGroups(rows []ScannedAccount) [][]ScannedAccount`.

- [ ] **Step 1: Write the failing tests**

```go
// append to core/scan_test.go
func TestAssembleMarksDuplicates(t *testing.T) {
	scans := []dirScan{
		{Folder: "Claude", LiveUUID: "same", Buckets: map[string]bucketStat{"same": {Count: 99}}},
		{Folder: "Claude_Work", LiveUUID: "same", Buckets: map[string]bucketStat{"same": {Count: 42}}},
		{Folder: "Claude_Other", LiveUUID: "other", Buckets: map[string]bucketStat{"other": {Count: 1}}},
	}
	got := assembleAccounts(scans)
	if len(got) != 3 {
		t.Fatalf("want 3 complete rows, got %+v", got)
	}
	for _, r := range got {
		wantDup := r.UUID == "same"
		if r.Duplicate != wantDup {
			t.Fatalf("%s: Duplicate = %v, want %v", r.HomeFolder, r.Duplicate, wantDup)
		}
	}
}

func TestDuplicateGroups(t *testing.T) {
	rows := []ScannedAccount{
		{Complete: true, HomeFolder: "Claude", UUID: "same", Duplicate: true},
		{Complete: true, HomeFolder: "Claude_Work", UUID: "same", Duplicate: true},
		{Complete: true, HomeFolder: "Claude_Solo", UUID: "solo"},
		{Complete: false, UUID: "ghost", Recoverable: true},
	}
	groups := DuplicateGroups(rows)
	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d: %+v", len(groups), groups)
	}
	if len(groups[0]) != 2 {
		t.Fatalf("group must hold both members: %+v", groups[0])
	}
	if groups[0][0].HomeFolder != "Claude" || groups[0][1].HomeFolder != "Claude_Work" {
		t.Fatalf("group members must stay in folder order: %+v", groups[0])
	}
}

func TestDuplicateGroupsOrderedByFirstFolder(t *testing.T) {
	rows := []ScannedAccount{
		{Complete: true, HomeFolder: "Claude_A", UUID: "z", Duplicate: true},
		{Complete: true, HomeFolder: "Claude_B", UUID: "z", Duplicate: true},
		{Complete: true, HomeFolder: "Claude_C", UUID: "a", Duplicate: true},
		{Complete: true, HomeFolder: "Claude_D", UUID: "a", Duplicate: true},
	}
	groups := DuplicateGroups(rows)
	if len(groups) != 2 {
		t.Fatalf("want 2 groups, got %+v", groups)
	}
	// The panel shows one group at a time, so the order must be stable and
	// independent of UUID, which is meaningless to the user.
	if groups[0][0].HomeFolder != "Claude_A" || groups[1][0].HomeFolder != "Claude_C" {
		t.Fatalf("groups must be ordered by their first folder: %+v", groups)
	}
}

func TestDuplicateGroupsNoneWhenAllUnique(t *testing.T) {
	rows := []ScannedAccount{
		{Complete: true, HomeFolder: "Claude", UUID: "a"},
		{Complete: true, HomeFolder: "Claude_Two", UUID: "b"},
	}
	if groups := DuplicateGroups(rows); len(groups) != 0 {
		t.Fatalf("want no groups, got %+v", groups)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/ -run 'TestAssembleMarksDuplicates|TestDuplicateGroups' -v`
Expected: FAIL — `r.Duplicate undefined`, `undefined: DuplicateGroups`.

- [ ] **Step 3: Write the implementation**

Add to `ScannedAccount`:

```go
	// Duplicate marks a complete row whose account is also the live login of
	// another profile. Every member of the group is marked. It can only arise
	// from signing in to an account that already has a profile, which the
	// create-profile flow warns against and this flag lets the panel insist on
	// cleaning up. In-app account switching produces ghosts, not duplicates.
	Duplicate bool
```

In `assembleAccounts`, after the complete-row loop (after line 100) and before the ghost loop, add:

```go
	// Two profiles signed in to one account is a state the user has to resolve,
	// so mark every member of each group rather than picking a winner here: the
	// panel asks which one to keep.
	seen := map[string]int{}
	for _, r := range out {
		seen[r.UUID]++
	}
	for i := range out {
		if seen[out[i].UUID] > 1 {
			out[i].Duplicate = true
		}
	}
```

At the end of `core/scan.go`, add:

```go
// DuplicateGroups returns the sets of complete rows that share an account,
// largest-first order not applied: groups keep the row order they arrived in
// (folder order, from assembleAccounts' sort) and the groups themselves are
// ordered by their first member's folder. The panel resolves one group at a
// time, so a stable, human-meaningful order matters more than any ranking.
func DuplicateGroups(rows []ScannedAccount) [][]ScannedAccount {
	byUUID := map[string][]ScannedAccount{}
	var order []string
	for _, r := range rows {
		if !r.Complete || !r.Duplicate {
			continue
		}
		if _, ok := byUUID[r.UUID]; !ok {
			order = append(order, r.UUID)
		}
		byUUID[r.UUID] = append(byUUID[r.UUID], r)
	}
	var out [][]ScannedAccount
	for _, uuid := range order {
		if g := byUUID[uuid]; len(g) > 1 {
			out = append(out, g)
		}
	}
	return out
}
```

`order` is built in row order, and rows arrive sorted by folder, so groups come out ordered by their first folder without an extra sort.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/ -run 'TestAssemble|TestDuplicateGroups' -v`
Expected: PASS. `TestAssembleMultiDirSameLive` (existing, `core/scan_test.go:60`) covers two dirs with the same live UUID — check whether it now needs a `Duplicate` assertion added rather than changed; it should still pass untouched.

- [ ] **Step 5: Commit**

```bash
git add core/scan.go core/scan_test.go
git commit -m "core: detect two profiles signed in to one account"
```

</details>

---

### Task 5: Scanner — pending rows, and the drop-filter exception

The riskiest scanner change: `ScanAccounts` gains a parameter, so three call sites move with it.

**Files:**
- Modify: `core/scan.go` — `dirScan` (line 50), `ScanAccounts` (line 215), `assembleAccounts`, sort comparator (line 134)
- Modify: `cmd/mcs-menubar/main.go:268`, `cmd/mcs-picker/main.go:36`, `cmd/mcs-tray/panel_windows.go:395`
- Test: `core/scan_test.go`

**Interfaces:**
- Consumes: `core.PendingProfile` (Task 1).
- Produces:
  - `func ScanAccounts(profiles []*platform.ProfileInfo, pending []PendingProfile) []ScannedAccount`
  - `ScannedAccount.Pending bool`, `ScannedAccount.PendingUUID string`
  - `const PendingSignInNote`

- [ ] **Step 1: Write the failing tests**

```go
// append to core/scan_test.go
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
```

Add `"os"` and `"path/filepath"` to `core/scan_test.go` imports if not already present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/ -run 'TestScanKeepsPending|TestScanPending' -v`
Expected: FAIL — `not enough arguments in call to ScanAccounts`, `undefined: PendingSignInNote`.

- [ ] **Step 3: Write the implementation**

Add to `ScannedAccount`:

```go
	// Pending marks a profile MCS created that is waiting to be signed in to.
	// It is a separate flag from PendingUUID because an empty PendingUUID is
	// meaningful on its own — "waiting for any account", the add path — as
	// opposed to "not pending at all".
	Pending bool

	// PendingUUID names the account this profile was created to receive, on the
	// recovery path. Empty on the add path. Only meaningful when Pending is true.
	PendingUUID string
```

Add the note constant beside the others:

```go
// PendingSignInNote is the note on a profile MCS has just created. The user has
// been sent to sign in to it, so it has to stay listed until they do.
const PendingSignInNote = "Sign in to finish setting this up."
```

Add to `dirScan` (line 50):

```go
	Pending     bool   // named in the pending-sign-in registry
	PendingUUID string // account this profile was created to receive ("" = any)
```

In `assembleAccounts`, emit pending rows. Insert after the `SignedOut` loop (after line 130) — and make the `SignedOut` loop skip pending dirs so one dir cannot produce two rows:

```go
	// Profiles MCS just created, still waiting for their one-time sign-in. These
	// come before the ghost loop because a recovery's expected account must not
	// also be reported as an orphan.
	for _, s := range scans {
		if !s.Pending || s.LiveUUID != "" {
			continue
		}
		out = append(out, ScannedAccount{
			HomeFolder:  s.Folder,
			Pending:     true,
			PendingUUID: s.PendingUUID,
			Account:     AccountUnknown,
			Note:        PendingSignInNote,
		})
	}
```

Change the existing `SignedOut` loop's guard (line 121) from:

```go
		if s.LiveUUID != "" || len(s.Buckets) > 0 || !s.HasConfig {
```

to:

```go
		if s.LiveUUID != "" || len(s.Buckets) > 0 || !s.HasConfig || s.Pending {
```

In the ghost loop, suppress buckets a pending recovery has claimed. Build the set before the loop:

```go
	// A recovery in flight has already copied its orphan's bucket into the new
	// profile. Reporting it as a ghost as well would show the same conversations
	// twice: once as the thing being recovered, once as the problem.
	pendingExpect := map[string]bool{}
	for _, s := range scans {
		if s.Pending && s.PendingUUID != "" {
			pendingExpect[s.PendingUUID] = true
		}
	}
```

and add to the loop's skip condition:

```go
			if uuid == s.LiveUUID || live[uuid] || pendingExpect[uuid] {
				continue
			}
```

Update the sort comparator (line 138) so pending rows sort by folder like the other folder-bearing rows:

```go
		if out[i].Complete || out[i].SignedOut || out[i].Pending {
			return out[i].HomeFolder < out[j].HomeFolder
		}
```

and `rowRank` gains the pending band alongside `SignedOut`:

```go
	case a.SignedOut, a.Pending:
		return 1
```

Finally, `ScanAccounts` takes the registry and applies the drop exception:

```go
// ScanAccounts scans the given profile dirs and returns the deduped review rows.
// Dirs that are not Claude Desktop profiles at all (no config.json, no login, no
// session bucket) are dropped — except dirs named in pending, which MCS created
// itself and which look exactly like that until Claude has run in them. Dropping
// those would make the profile the user was just told to sign in to disappear
// from the panel before they could.
func ScanAccounts(profiles []*platform.ProfileInfo, pending []PendingProfile) []ScannedAccount {
	byFolder := map[string]PendingProfile{}
	for _, p := range pending {
		byFolder[p.Folder] = p
	}
	var scans []dirScan
	for _, p := range profiles {
		ds := gatherDir(p)
		if pp, ok := byFolder[p.Name]; ok {
			ds.Pending = true
			ds.PendingUUID = pp.ExpectUUID
		}
		if ds.LiveUUID == "" && len(ds.Buckets) == 0 && !ds.HasConfig && !ds.Pending {
			continue
		}
		scans = append(scans, ds)
	}
	return assembleAccounts(scans)
}
```

Now update the three call sites:

- `cmd/mcs-menubar/main.go:268` → `core.ScanAccounts(mustFindProfiles(), core.LoadPending())`
- `cmd/mcs-picker/main.go:36` → `core.ScanAccounts(profiles, core.LoadPending())`
- `cmd/mcs-tray/panel_windows.go:395` → `core.ScanAccounts(panelMustFindProfiles(), core.LoadPending())`

And the two existing test call sites in `core/scan_test.go` (lines 171 and 194) take a `nil` second argument.

- [ ] **Step 4: Run tests and build to verify**

Run: `go test ./core/... -v`
Expected: PASS, all scanner tests including the pre-existing ones.

Run: `go build ./...`
Expected: exit 0.

Run: `GOOS=windows GOARCH=amd64 go build ./...`
Expected: exit 0, proving the Windows call site was updated too.

- [ ] **Step 5: Commit**

```bash
git add core/scan.go core/scan_test.go cmd/mcs-menubar/main.go cmd/mcs-picker/main.go cmd/mcs-tray/panel_windows.go
git commit -m "core: keep freshly created profiles visible until they are signed in to"
```

---

### Task 6: Share the directory-copy helper

`copyDirMerge` and `copyFile` live in `platform/windows_msix.go`, which is `//go:build windows`. macOS recovery needs them. This is a pure move, no behaviour change, done as its own task so a reviewer can confirm nothing else shifted.

**Files:**
- Create: `platform/copydir.go`
- Modify: `platform/windows_msix.go` — delete `copyDirMerge` (lines 298-325) and `copyFile` (lines 327-345)

**Interfaces:**
- Consumes: nothing.
- Produces: `func CopyDirMerge(src, dst string) (int, error)` (exported so `core` can use it), and unexported `copyFile`.

- [ ] **Step 1: Create the new file with the moved code**

```go
// platform/copydir.go
package platform

import (
	"io"
	"os"
	"path/filepath"
)

// CopyDirMerge recursively copies files from src into dst (creating dst),
// skipping any file that already exists in dst, and returns the number of files
// copied. Never clobbering means a merge can be retried safely and can never
// destroy a newer copy at the destination.
//
// It lives here rather than in windows_msix.go, where it started, because macOS
// recovery needs it too and that file is windows-only.
func CopyDirMerge(src, dst string) (int, error) {
	copied := 0
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if _, e := os.Stat(target); e == nil {
			return nil // don't clobber anything already there
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := copyFile(path, target); err != nil {
			return err
		}
		copied++
		return nil
	})
	return copied, err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, cerr := io.Copy(out, in)
	if closeErr := out.Close(); cerr == nil {
		cerr = closeErr
	}
	return cerr
}
```

Read `platform/windows_msix.go:327-345` before writing `copyFile` here and reproduce its body exactly, including its error handling, rather than trusting the excerpt above.

- [ ] **Step 2: Delete the originals and update the caller**

Remove `copyDirMerge` and `copyFile` from `platform/windows_msix.go`. Update its one call site at line 289 from `copyDirMerge(fromBucket, dstBucket)` to `CopyDirMerge(fromBucket, dstBucket)`. Remove `"io"` from that file's imports if nothing else there uses it.

- [ ] **Step 3: Verify both platforms still build and MSIX tests still pass**

Run: `go build ./... && GOOS=windows GOARCH=amd64 go build ./...`
Expected: exit 0 for both.

Run: `GOOS=windows GOARCH=amd64 go vet ./platform/`
Expected: exit 0, no unused-import complaints.

The MSIX tests are windows-only, so they cannot run on macOS. Confirm they at least compile:

Run: `GOOS=windows GOARCH=amd64 go vet ./...`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add platform/copydir.go platform/windows_msix.go
git commit -m "platform: hoist the directory-copy helper out of the windows-only file"
```

---

### Task 7: Platform methods for profile creation, recovery prep, and archive root

**Files:**
- Modify: `platform/platform.go` — the `Platform` interface
- Modify: `platform/darwin.go`, `platform/windows.go`, `platform/unsupported.go`
- Create: `platform/newprofile_test.go`
- Modify: `core/switch_test.go` — `mockPlatform` must implement the three new methods

**Interfaces:**
- Consumes: `CopyDirMerge` (Task 6), `core.ProfileFolderPrefix` is **not** usable here (no core import); the prefix is duplicated as a package constant with a comment pointing at core.
- Produces, on `Platform`:
  - `CreateProfile(name string) (string, error)`
  - `PrepareRecovery(newProfilePath, sourceProfilePath, expectUUID string) error`
  - `ArchiveDir() string`

- [ ] **Step 1: Write the failing test**

```go
// platform/newprofile_test.go
package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareRecoveryCopiesTheBucket(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "Claude")
	dst := filepath.Join(root, "Claude_Recovered")
	bucket := filepath.Join(src, "claude-code-sessions", "orphan-uuid")
	if err := os.MkdirAll(bucket, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucket, "local_1.json"), []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := prepareRecoveryByCopy(dst, src, "orphan-uuid"); err != nil {
		t.Fatalf("prepareRecoveryByCopy: %v", err)
	}

	got := filepath.Join(dst, "claude-code-sessions", "orphan-uuid", "local_1.json")
	b, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("session not copied into the new profile: %v", err)
	}
	if string(b) != `{"a":1}` {
		t.Fatalf("contents = %q", b)
	}
	// The source is the only copy that matters until sign-in succeeds, so it
	// must be left exactly as it was.
	if _, err := os.Stat(filepath.Join(bucket, "local_1.json")); err != nil {
		t.Fatalf("source bucket was disturbed: %v", err)
	}
}

func TestPrepareRecoveryMissingSourceBucketIsAnError(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "Claude")
	dst := filepath.Join(root, "Claude_Recovered")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := prepareRecoveryByCopy(dst, src, "not-there"); err == nil {
		t.Fatal("want an error when there is nothing to recover")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./platform/ -run TestPrepareRecovery -v`
Expected: FAIL — `undefined: prepareRecoveryByCopy`.

- [ ] **Step 3: Write the shared helper and the interface**

Add to `platform/copydir.go`:

```go
// prepareRecoveryByCopy makes an orphaned account's conversations available in a
// new profile by copying its session bucket across. Copy, not move: until the
// user has signed in to the new profile the source is the only copy that
// matters, and a failure here must lose nothing. Once the account is live in the
// new profile the source's now-stale bucket is folded away by the scanner as a
// duplicate of an account live elsewhere, so the user never sees it twice.
//
// Used by the standalone builds. The Store build instead completes its copy
// after sign-in, from a profile it has parked (see windows_msix.go).
func prepareRecoveryByCopy(newProfilePath, sourceProfilePath, expectUUID string) error {
	if expectUUID == "" {
		return fmt.Errorf("no account to recover")
	}
	srcBucket := filepath.Join(GetProfileSessionsDir(sourceProfilePath), expectUUID)
	fi, err := os.Stat(srcBucket)
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("no saved conversations found for that account in %s", filepath.Base(sourceProfilePath))
	}
	dstBucket := filepath.Join(GetProfileSessionsDir(newProfilePath), expectUUID)
	if _, err := CopyDirMerge(srcBucket, dstBucket); err != nil {
		return fmt.Errorf("copy saved conversations: %w", err)
	}
	return nil
}
```

Add `"fmt"` to that file's imports.

Add to the `Platform` interface in `platform/platform.go`, with the doc comments:

```go
	// CreateProfile makes a new profile that Claude Desktop will populate on its
	// next launch, and returns its path. The path is not guaranteed to exist yet:
	// the Store build deliberately leaves its slot absent so the packaged app
	// creates a clean one. Caller must have terminated Claude first, and must
	// have validated the name (see core.ValidateProfileName).
	CreateProfile(name string) (string, error)

	// PrepareRecovery arranges for the account expectUUID's saved conversations
	// to end up in newProfilePath once the user signs in as that account. The
	// standalone builds copy the bucket across now; the Store build has already
	// queued the copy as part of CreateProfile and does nothing here.
	PrepareRecovery(newProfilePath, sourceProfilePath, expectUUID string) error

	// ArchiveDir returns the root that archived profiles are parked under. It is
	// chosen per platform so archiving is a same-volume rename and the result
	// sits outside FindProfiles' scan path, which is what stops an archived
	// profile reappearing on the next Rescan.
	ArchiveDir() string
```

Add a package constant near the top of `platform/platform.go`:

```go
// profileFolderPrefix is what MCS names a profile folder it creates, chosen so
// FindProfiles' "Claude" prefix match picks it up. It duplicates
// core.ProfileFolderPrefix on purpose: platform must not import core.
const profileFolderPrefix = "Claude_"
```

`platform/darwin.go`:

```go
func (d *DarwinPlatform) CreateProfile(name string) (string, error) {
	appSup := d.AppSupportDir()
	if appSup == "" {
		return "", fmt.Errorf("could not determine user home directory")
	}
	path := filepath.Join(appSup, profileFolderPrefix+name)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("a profile folder named %q already exists", filepath.Base(path))
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", fmt.Errorf("create profile folder: %w", err)
	}
	return path, nil
}

func (d *DarwinPlatform) PrepareRecovery(newProfilePath, sourceProfilePath, expectUUID string) error {
	return prepareRecoveryByCopy(newProfilePath, sourceProfilePath, expectUUID)
}

func (d *DarwinPlatform) ArchiveDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "multi-claude-switcher-archive")
	}
	return filepath.Join(home, ".multi-claude-switcher", "archive")
}
```

`platform/windows.go`:

```go
func (w *WindowsPlatform) CreateProfile(name string) (string, error) {
	if w.isMSIX() {
		roaming := msixRoamingDir()
		if roaming == "" {
			return "", fmt.Errorf("Store Claude Desktop data directory not found")
		}
		if err := msixParkForNewIn(roaming, name); err != nil {
			return "", err
		}
		// The slot is deliberately absent now: the packaged app creates a clean
		// one on next launch, which is what makes it a signed-out profile.
		return msixSlotDir(roaming), nil
	}
	root := w.AppSupportDir()
	if root == "" {
		return "", fmt.Errorf("could not determine %%APPDATA%% directory")
	}
	path := filepath.Join(root, profileFolderPrefix+name)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("a profile folder named %q already exists", filepath.Base(path))
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", fmt.Errorf("create profile folder: %w", err)
	}
	return path, nil
}

func (w *WindowsPlatform) PrepareRecovery(newProfilePath, sourceProfilePath, expectUUID string) error {
	if w.isMSIX() {
		// msixParkForNewIn already set PendingMigrateFrom on the parked profile,
		// and msixAttemptMigrationIn copies the bucket matching whatever account
		// the user signs in as — which is exactly this recovery. Nothing to do,
		// and nothing may be written into a slot the app has not created yet.
		return nil
	}
	return prepareRecoveryByCopy(newProfilePath, sourceProfilePath, expectUUID)
}

func (w *WindowsPlatform) ArchiveDir() string {
	if w.isMSIX() {
		// Stay inside the package container. Renames within it are what the
		// shipped code already does successfully; moving out of an MSIX
		// virtualized container to %USERPROFILE% is unverified on a real Store
		// install. .mcs-archive sits beside .mcs-profiles, and msixFindProfiles
		// enumerates only the slot and .mcs-profiles, so it stays invisible to
		// scanning.
		if roaming := msixRoamingDir(); roaming != "" {
			return filepath.Join(roaming, ".mcs-archive")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "multi-claude-switcher-archive")
	}
	return filepath.Join(home, ".multi-claude-switcher", "archive")
}
```

`platform/unsupported.go`:

```go
func (p *unsupportedPlatform) CreateProfile(name string) (string, error) { return "", notSupported() }
func (p *unsupportedPlatform) PrepareRecovery(newProfilePath, sourceProfilePath, expectUUID string) error {
	return notSupported()
}
func (p *unsupportedPlatform) ArchiveDir() string { return "" }
```

`core/switch_test.go` — extend `mockPlatform` (the struct begins at line 15) with recording fields and the three methods:

```go
// add to the mockPlatform struct
	createdName   string
	createdPath   string
	preparedFrom  string
	preparedUUID  string
	archiveRoot   string

func (m *mockPlatform) CreateProfile(name string) (string, error) {
	m.createdName = name
	return m.createdPath, nil
}
func (m *mockPlatform) PrepareRecovery(newProfilePath, sourceProfilePath, expectUUID string) error {
	m.preparedFrom = sourceProfilePath
	m.preparedUUID = expectUUID
	return nil
}
func (m *mockPlatform) ArchiveDir() string { return m.archiveRoot }
```

- [ ] **Step 4: Verify**

Run: `go test ./platform/ -run TestPrepareRecovery -v`
Expected: PASS, 2 tests.

Run: `go build ./... && GOOS=windows GOARCH=amd64 go build ./... && go test ./... 2>&1 | tail -20`
Expected: builds exit 0; all existing tests still pass now that `mockPlatform` satisfies the widened interface.

- [ ] **Step 5: Commit**

```bash
git add platform/platform.go platform/darwin.go platform/windows.go platform/unsupported.go platform/copydir.go platform/newprofile_test.go core/switch_test.go
git commit -m "platform: per-OS profile creation, recovery prep, and archive root"
```

---

### Task 8: Archive a profile

**Files:**
- Create: `core/archive.go`
- Test: `core/archive_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `func ArchiveProfile(profilePath, archiveRoot string) (string, error)`

- [ ] **Step 1: Write the failing tests**

```go
// core/archive_test.go
package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveProfileMovesItOutOfTheScanPath(t *testing.T) {
	root := t.TempDir()
	scanPath := filepath.Join(root, "appsupport")
	archiveRoot := filepath.Join(root, "archive")
	profile := filepath.Join(scanPath, "Claude_Work")
	if err := os.MkdirAll(filepath.Join(profile, "claude-code-sessions", "uuid"), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(profile, "claude-code-sessions", "uuid", "local_1.json")
	if err := os.WriteFile(marker, []byte(`{"keep":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	dest, err := ArchiveProfile(profile, archiveRoot)
	if err != nil {
		t.Fatalf("ArchiveProfile: %v", err)
	}
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Fatalf("profile must be gone from the scan path, stat err = %v", err)
	}
	if !strings.HasPrefix(dest, archiveRoot) {
		t.Fatalf("dest %q must sit under the archive root %q", dest, archiveRoot)
	}
	if !strings.Contains(filepath.Base(dest), "Claude_Work") {
		t.Fatalf("archive name must keep the profile name, got %q", filepath.Base(dest))
	}
	// Nothing is deleted, so the contents must survive byte-for-byte.
	b, err := os.ReadFile(filepath.Join(dest, "claude-code-sessions", "uuid", "local_1.json"))
	if err != nil {
		t.Fatalf("archived contents missing: %v", err)
	}
	if string(b) != `{"keep":true}` {
		t.Fatalf("contents changed: %q", b)
	}
}

func TestArchiveProfileCollisionGetsACounter(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	first := filepath.Join(root, "Claude_Work")
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}
	destA, err := ArchiveProfile(first, archiveRoot)
	if err != nil {
		t.Fatal(err)
	}
	// Re-create the same folder name and archive it again in the same second, so
	// the timestamped name collides.
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}
	destB, err := ArchiveProfile(first, archiveRoot)
	if err != nil {
		t.Fatalf("second archive must not fail on a name collision: %v", err)
	}
	if destA == destB {
		t.Fatalf("both archives landed on %q", destA)
	}
	if _, err := os.Stat(destA); err != nil {
		t.Fatalf("the first archive must be untouched: %v", err)
	}
}

func TestArchiveProfileMissingSourceIsAnError(t *testing.T) {
	root := t.TempDir()
	if _, err := ArchiveProfile(filepath.Join(root, "nope"), filepath.Join(root, "archive")); err == nil {
		t.Fatal("want an error for a profile that is not there")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/ -run TestArchiveProfile -v`
Expected: FAIL — `undefined: ArchiveProfile`.

- [ ] **Step 3: Write the implementation**

```go
// core/archive.go
package core

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// archiveRenameAttempts and archiveRenameDelay bound the retry on a locked
// directory. Claude Desktop can still be releasing file handles for a moment
// after it exits, and a rename that fails for that reason succeeds shortly
// after. Mirrors platform/windows_msix.go's renameWithRetry, which cannot be
// reused here because that file is windows-only.
const (
	archiveRenameAttempts = 40
	archiveRenameDelay    = 500 * time.Millisecond
)

// ArchiveProfile moves a profile out of the directory the scanner looks in, into
// archiveRoot, and returns where it landed.
//
// This is the strongest action MCS takes on user data, and it is deliberately a
// rename rather than a delete: everything stays on disk, in one piece, and the
// user can move it back by hand. The point of moving it rather than merely
// dropping it from managed.json is that a folder left in place reappears on the
// next Rescan, so "one profile per account" would not hold.
func ArchiveProfile(profilePath, archiveRoot string) (string, error) {
	if fi, err := os.Stat(profilePath); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("nothing to archive at %s", profilePath)
	}
	if err := os.MkdirAll(archiveRoot, 0o755); err != nil {
		return "", fmt.Errorf("create archive folder: %w", err)
	}
	base := filepath.Base(profilePath) + "-" + time.Now().Format("20060102-150405")
	dest := filepath.Join(archiveRoot, base)
	// Two archives of the same profile name within one second would collide, and
	// a collision must never overwrite an existing archive.
	for i := 2; ; i++ {
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			break
		}
		dest = filepath.Join(archiveRoot, fmt.Sprintf("%s-%d", base, i))
	}
	if err := renameProfileWithRetry(profilePath, dest); err != nil {
		return "", fmt.Errorf("couldn't archive %s — Claude may still be holding its files. Fully quit Claude and try again. (%w)",
			DisplayName(filepath.Base(profilePath)), err)
	}
	return dest, nil
}

func renameProfileWithRetry(from, to string) error {
	var err error
	for i := 0; i < archiveRenameAttempts; i++ {
		if err = os.Rename(from, to); err == nil {
			if i > 0 {
				log.Printf("archive rename %q -> %q succeeded after %d retries", filepath.Base(from), filepath.Base(to), i)
			}
			return nil
		}
		time.Sleep(archiveRenameDelay)
	}
	log.Printf("archive rename %q -> %q FAILED after retries: %v", filepath.Base(from), filepath.Base(to), err)
	return err
}
```

`TestArchiveProfileMissingSourceIsAnError` returns before any retry, so it is fast. The collision test archives twice within the same second, so it exercises the counter without waiting.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/ -run TestArchiveProfile -v`
Expected: PASS, 3 tests.

- [ ] **Step 5: Commit**

```bash
git add core/archive.go core/archive_test.go
git commit -m "core: archive a profile by moving it out of the scan path"
```

---

### Task 9: Merge two profiles holding one account

**Files:**
- Create: `core/merge.go`
- Test: `core/merge_test.go`

**Interfaces:**
- Consumes: `ArchiveProfile` (Task 8), `SyncSessions` + `SyncReport` (`core/sync.go:44`), `SetManaged`/`LoadManaged` (`core/managed.go`).
- Produces: `func MergeDuplicates(keepPath, archivePath, archiveRoot string) (*SyncReport, error)`

`SyncSessions` re-buckets from the source account's UUID to the target's. Both ends of a merge are the same account, so the rename is a no-op and the copy lands exactly where Claude will read it.

- [ ] **Step 1: Write the failing tests**

```go
// core/merge_test.go
package core

import (
	"os"
	"path/filepath"
	"testing"
)

// mergeFixture builds two profiles signed in to the same account, each holding
// one session the other does not, and returns their paths plus the archive root.
func mergeFixture(t *testing.T, keepUUID, archiveUUID string) (keep, archive, archiveRoot string) {
	t.Helper()
	root := t.TempDir()
	keep = filepath.Join(root, "Claude_Keep")
	archive = filepath.Join(root, "Claude_Archive")
	archiveRoot = filepath.Join(root, "archive")
	for path, uuid := range map[string]string{keep: keepUUID, archive: archiveUUID} {
		bucket := filepath.Join(path, "claude-code-sessions", uuid)
		if err := os.MkdirAll(bucket, 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := `{"lastKnownAccountUuid":"` + uuid + `"}`
		if err := os.WriteFile(filepath.Join(path, "config.json"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		name := "only_in_keep.json"
		if path == archive {
			name = "only_in_archive.json"
		}
		if err := os.WriteFile(filepath.Join(bucket, name), []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return keep, archive, archiveRoot
}

func TestMergeDuplicatesUnionsThenArchives(t *testing.T) {
	withStubbedManaged(t)
	if err := SetManaged([]string{"Claude_Keep", "Claude_Archive"}); err != nil {
		t.Fatal(err)
	}
	keep, archive, archiveRoot := mergeFixture(t, "same-uuid", "same-uuid")

	report, err := MergeDuplicates(keep, archive, archiveRoot)
	if err != nil {
		t.Fatalf("MergeDuplicates: %v", err)
	}
	if report == nil || report.CopiedCount != 1 {
		t.Fatalf("want 1 session copied, got %+v", report)
	}
	// The keeper now holds both sessions.
	for _, name := range []string{"only_in_keep.json", "only_in_archive.json"} {
		p := filepath.Join(keep, "claude-code-sessions", "same-uuid", name)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("keeper is missing %s: %v", name, err)
		}
	}
	// The archived profile is gone from the scan path.
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatalf("archived profile still in place, stat err = %v", err)
	}
	// And no longer managed.
	for _, m := range LoadManaged() {
		if m == "Claude_Archive" {
			t.Fatalf("archived folder must be unmanaged, got %v", LoadManaged())
		}
	}
	if len(LoadManaged()) != 1 || LoadManaged()[0] != "Claude_Keep" {
		t.Fatalf("managed = %v, want just the keeper", LoadManaged())
	}
}

func TestMergeDuplicatesRefusesDifferentAccounts(t *testing.T) {
	withStubbedManaged(t)
	keep, archive, archiveRoot := mergeFixture(t, "uuid-a", "uuid-b")

	if _, err := MergeDuplicates(keep, archive, archiveRoot); err == nil {
		t.Fatal("merging two different accounts must be refused")
	}
	// Nothing may have moved.
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("the other profile must be left alone: %v", err)
	}
}

func TestMergeDuplicatesLeavesManagedAloneWhenArchiveFails(t *testing.T) {
	withStubbedManaged(t)
	if err := SetManaged([]string{"Claude_Keep", "Claude_Archive"}); err != nil {
		t.Fatal(err)
	}
	keep, archive, _ := mergeFixture(t, "same-uuid", "same-uuid")
	// An archive root that cannot be created: a path under a regular file.
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	badRoot := filepath.Join(blocker, "archive")

	if _, err := MergeDuplicates(keep, archive, badRoot); err == nil {
		t.Fatal("want an error when the archive root cannot be created")
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("profile must still be in place after a failed archive: %v", err)
	}
	// Never unmanage a folder that is still on disk: the user has to keep seeing
	// the warning and be able to retry.
	if len(LoadManaged()) != 2 {
		t.Fatalf("managed = %v, want both still listed", LoadManaged())
	}
}
```

`withStubbedManaged` does not exist yet. `core/managed_test.go:11` stubs `managedPath` inline. Extract it into a helper in `core/managed_test.go` so both files can use it:

```go
// core/managed_test.go — replace the inline stubbing in TestManaged* with this
func withStubbedManaged(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	orig := managedPath
	managedPath = func() string { return filepath.Join(dir, "managed.json") }
	t.Cleanup(func() { managedPath = orig })
}
```

Read `core/managed_test.go` first and rework its existing tests to call the helper, keeping their assertions unchanged.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/ -run TestMergeDuplicates -v`
Expected: FAIL — `undefined: MergeDuplicates`.

- [ ] **Step 3: Write the implementation**

```go
// core/merge.go
package core

import (
	"fmt"
	"path/filepath"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// MergeDuplicates resolves two profiles signed in to the same account: the
// conversations from archivePath are copied into keepPath, then archivePath is
// moved out of the scan path and dropped from the managed list.
//
// Only one direction is copied. The profile being archived is never written to,
// which makes the archive an untouched record of what was there — a better
// safety property than a two-way merge, and faster.
//
// Caller must have terminated Claude first. Order is verify, copy, move, then
// update state, so a failure anywhere leaves MCS's view of the world no further
// ahead than the disk: both profiles stay listed and the duplicate warning stays
// up, which is exactly the state the user can retry from.
func MergeDuplicates(keepPath, archivePath, archiveRoot string) (*SyncReport, error) {
	keepUUID, err := platform.GetProfileAccountUUID(keepPath)
	if err != nil {
		return nil, fmt.Errorf("%s has no account signed in, so there is nothing to merge into",
			DisplayName(filepath.Base(keepPath)))
	}
	archiveUUID, err := platform.GetProfileAccountUUID(archivePath)
	if err != nil {
		return nil, fmt.Errorf("%s has no account signed in, so there is nothing to merge",
			DisplayName(filepath.Base(archivePath)))
	}
	if keepUUID != archiveUUID {
		// A stale panel could ask to merge rows that have changed underneath it.
		// Merging two genuinely different accounts would mix their histories.
		return nil, fmt.Errorf("%s and %s are different accounts, so they can't be merged",
			DisplayName(filepath.Base(keepPath)), DisplayName(filepath.Base(archivePath)))
	}

	// SyncSessions snapshots the destination before writing, keeps the newer copy
	// on a clash, and reports clashes rather than resolving them silently.
	report, err := SyncSessions(archivePath, keepPath)
	if err != nil {
		return nil, fmt.Errorf("combine conversations: %w", err)
	}

	if _, err := ArchiveProfile(archivePath, archiveRoot); err != nil {
		return report, err
	}

	folder := filepath.Base(archivePath)
	var kept []string
	for _, m := range LoadManaged() {
		if m != folder {
			kept = append(kept, m)
		}
	}
	if err := SetManaged(kept); err != nil {
		return report, fmt.Errorf("update the managed list: %w", err)
	}
	return report, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/ -run 'TestMergeDuplicates|TestManaged' -v`
Expected: PASS, 3 merge tests plus the reworked managed tests.

Run: `go test ./core/... -v 2>&1 | tail -5`
Expected: all core tests pass.

- [ ] **Step 5: Commit**

```bash
git add core/merge.go core/merge_test.go core/managed_test.go
git commit -m "core: merge duplicate profiles, keeping one and archiving the other"
```

---

### Task 10: Profile creation orchestrator

The one place both hosts call, so the sequence cannot drift between macOS and Windows.

**Files:**
- Create: `core/newprofile.go`
- Test: `core/newprofile_test.go`

**Interfaces:**
- Consumes: `ValidateProfileName`/`ProfileFolderName` (Task 2), `AddPending`/`RemovePending` (Task 1), `LoadManaged`/`SetManaged`, and the three new `platform` methods (Task 7).
- Produces:
  - `type CreateProfileRequest struct { Name string; RecoverUUID string; SourceFolder string }`
  - `type ProfileCreator struct { Plat platform.Platform }`
  - `func NewProfileCreator(p platform.Platform) *ProfileCreator`
  - `func (c *ProfileCreator) Create(req CreateProfileRequest) (createdPath string, err error)`

- [ ] **Step 1: Write the failing tests**

```go
// core/newprofile_test.go
package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateProfileAddPath(t *testing.T) {
	withStubbedManaged(t)
	withStubbedPending(t)
	root := t.TempDir()
	created := filepath.Join(root, "Claude_Personal")
	if err := os.MkdirAll(created, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &mockPlatform{running: true, createdPath: created}

	got, err := NewProfileCreator(m).Create(CreateProfileRequest{Name: "Personal"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got != created {
		t.Fatalf("path = %q, want %q", got, created)
	}
	if !m.terminated {
		t.Fatal("Claude must be quit before its data dirs are touched")
	}
	if m.createdName != "Personal" {
		t.Fatalf("platform got name %q", m.createdName)
	}
	if m.preparedUUID != "" {
		t.Fatalf("the add path must not prepare a recovery, got %q", m.preparedUUID)
	}
	if !m.launched || m.launchedPath != created {
		t.Fatalf("must launch the new profile, launched=%v path=%q", m.launched, m.launchedPath)
	}
	pending := LoadPending()
	if len(pending) != 1 || pending[0].Folder != "Claude_Personal" || pending[0].ExpectUUID != "" {
		t.Fatalf("pending = %+v", pending)
	}
	managed := LoadManaged()
	if len(managed) != 1 || managed[0] != "Claude_Personal" {
		t.Fatalf("managed = %v, want the new folder listed so it shows up at once", managed)
	}
}

func TestCreateProfileRecoveryPath(t *testing.T) {
	withStubbedManaged(t)
	withStubbedPending(t)
	root := t.TempDir()
	created := filepath.Join(root, "Claude_Recovered")
	if err := os.MkdirAll(created, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &mockPlatform{createdPath: created}

	_, err := NewProfileCreator(m).Create(CreateProfileRequest{
		Name: "Recovered", RecoverUUID: "orphan-uuid", SourceFolder: "Claude",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if m.preparedUUID != "orphan-uuid" {
		t.Fatalf("recovery must be prepared for the orphan, got %q", m.preparedUUID)
	}
	if filepath.Base(m.preparedFrom) != "Claude" {
		t.Fatalf("recovery source = %q, want the folder holding the bucket", m.preparedFrom)
	}
	pending := LoadPending()
	if len(pending) != 1 || pending[0].ExpectUUID != "orphan-uuid" {
		t.Fatalf("pending must remember which account to wait for: %+v", pending)
	}
}

func TestCreateProfileRejectsBadNameBeforeTouchingAnything(t *testing.T) {
	withStubbedManaged(t)
	withStubbedPending(t)
	m := &mockPlatform{}

	if _, err := NewProfileCreator(m).Create(CreateProfileRequest{Name: "  "}); err == nil {
		t.Fatal("want an error for an empty name")
	}
	if m.terminated {
		t.Fatal("a rejected name must not quit Claude")
	}
	if m.createdName != "" {
		t.Fatal("a rejected name must not reach the platform")
	}
	if len(LoadPending()) != 0 || len(LoadManaged()) != 0 {
		t.Fatal("a rejected name must not write any state")
	}
}

func TestCreateProfileRecoveryFailureLeavesNoState(t *testing.T) {
	withStubbedManaged(t)
	withStubbedPending(t)
	root := t.TempDir()
	created := filepath.Join(root, "Claude_Recovered")
	if err := os.MkdirAll(created, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &mockPlatform{createdPath: created, prepareErr: os.ErrPermission}

	if _, err := NewProfileCreator(m).Create(CreateProfileRequest{
		Name: "Recovered", RecoverUUID: "orphan-uuid", SourceFolder: "Claude",
	}); err == nil {
		t.Fatal("want the copy failure surfaced")
	}
	if len(LoadPending()) != 0 {
		t.Fatalf("pending must not be written when the copy failed: %+v", LoadPending())
	}
	if len(LoadManaged()) != 0 {
		t.Fatalf("managed must not be written when the copy failed: %v", LoadManaged())
	}
	if m.launched {
		t.Fatal("must not launch a profile whose recovery failed")
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("the half-made profile must be cleaned up, stat err = %v", err)
	}
}
```

`mockPlatform` needs a `prepareErr error` field, and `PrepareRecovery` must return it. Extend the mock added in Task 7:

```go
// core/switch_test.go — add the field and honour it
	prepareErr error

func (m *mockPlatform) PrepareRecovery(newProfilePath, sourceProfilePath, expectUUID string) error {
	m.preparedFrom = sourceProfilePath
	m.preparedUUID = expectUUID
	return m.prepareErr
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/ -run TestCreateProfile -v`
Expected: FAIL — `undefined: NewProfileCreator`.

- [ ] **Step 3: Write the implementation**

```go
// core/newprofile.go
package core

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// CreateProfileRequest describes a profile to create. RecoverUUID and
// SourceFolder are set together, and only on the recovery path: they name the
// orphaned account whose conversations should end up in the new profile, and the
// folder currently holding them.
type CreateProfileRequest struct {
	Name         string
	RecoverUUID  string
	SourceFolder string
}

// ProfileCreator runs the create-a-profile sequence. It exists so the macOS and
// Windows hosts share one ordering rather than each growing their own.
type ProfileCreator struct {
	Plat platform.Platform
}

func NewProfileCreator(p platform.Platform) *ProfileCreator {
	return &ProfileCreator{Plat: p}
}

// Create validates the name, quits Claude, creates the profile, arranges for a
// recovered account's conversations to follow, registers the profile as awaiting
// sign-in, and opens Claude on it.
//
// The order matters: nothing is written to MCS's own state until the disk work
// has succeeded, and nothing on disk is touched until the name is known to be
// good. A recovery that cannot copy its conversations removes the profile it
// just made, so a retry starts from a clean slate rather than colliding with a
// half-made folder.
func (c *ProfileCreator) Create(req CreateProfileRequest) (string, error) {
	if err := ValidateProfileName(req.Name); err != nil {
		return "", err
	}
	if req.RecoverUUID != "" && req.SourceFolder == "" {
		return "", fmt.Errorf("internal: a recovery needs the folder holding the conversations")
	}

	// Claude holds its data dir open, and on the Store build the profile is
	// created by moving that very directory.
	if err := c.Plat.TerminateApp(); err != nil {
		return "", err
	}

	createdPath, err := c.Plat.CreateProfile(req.Name)
	if err != nil {
		return "", err
	}

	if req.RecoverUUID != "" {
		sourcePath := filepath.Join(c.Plat.AppSupportDir(), req.SourceFolder)
		if err := c.Plat.PrepareRecovery(createdPath, sourcePath, req.RecoverUUID); err != nil {
			// The source was only ever read from, so removing what we just made
			// loses nothing and leaves the name free for a retry.
			if rmErr := os.RemoveAll(createdPath); rmErr != nil {
				log.Printf("could not clean up the half-made profile %q: %v", createdPath, rmErr)
			}
			return "", err
		}
	}

	folder := filepath.Base(createdPath)
	if err := AddPending(folder, req.RecoverUUID); err != nil {
		return "", fmt.Errorf("record the new profile: %w", err)
	}
	// Managed at once, so the account list shows it while the user is being told
	// to go and sign in to it.
	managed := LoadManaged()
	if managed != nil {
		already := false
		for _, m := range managed {
			if m == folder {
				already = true
			}
		}
		if !already {
			if err := SetManaged(append(managed, folder)); err != nil {
				return "", fmt.Errorf("update the managed list: %w", err)
			}
		}
	} else if err := SetManaged([]string{folder}); err != nil {
		return "", fmt.Errorf("update the managed list: %w", err)
	}

	if err := c.Plat.LaunchProfile(createdPath); err != nil {
		return createdPath, fmt.Errorf("the profile is ready but Claude didn't open: %w", err)
	}
	return createdPath, nil
}
```

`os.RemoveAll(createdPath)` is the one removal in this codebase that touches a Claude data dir. It is confined to a directory MCS created seconds earlier, that no account has ever been signed in to, and whose only content is a copy whose source is untouched. Do not widen it.

Note the `managed == nil` branch: `LoadManaged` returns nil on first run and callers must not treat that as "configured empty" (`core/managed.go:26`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/ -run TestCreateProfile -v`
Expected: PASS, 4 tests.

Run: `go test ./... 2>&1 | tail -10`
Expected: all packages pass.

- [ ] **Step 5: Commit**

```bash
git add core/newprofile.go core/newprofile_test.go core/switch_test.go
git commit -m "core: one create-a-profile sequence shared by both hosts"
```

---

### Task 11: Account list — add card and duplicate warning

**Files:**
- Modify: `internal/panelui/render.go` — `ProfileVM` (line 21), `RenderList` (line 188), `shell` CSS (line 53)
- Test: `internal/panelui/render_test.go`

**Interfaces:**
- Consumes: nothing from core directly; the host supplies `ProfileVM.UUID`.
- Produces: `ProfileVM.UUID string`, and the panel actions `showNewProfile` (no arg) and `showMerge` (arg `folderA|folderB`).

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/panelui/render_test.go
func TestRenderListHasAnAddCard(t *testing.T) {
	html := RenderList([]ProfileVM{{Folder: "Claude", Name: "Claude", UUID: "a", SignedIn: true}})
	if !strings.Contains(html, "Add another account") {
		t.Fatal("the account list must offer a way to add one")
	}
	if !strings.Contains(html, "send('showNewProfile'") {
		t.Fatalf("add card must trigger showNewProfile:\n%s", html)
	}
}

func TestRenderListWarnsAboutDuplicates(t *testing.T) {
	html := RenderList([]ProfileVM{
		{Folder: "Claude", Name: "Claude", UUID: "same", SignedIn: true},
		{Folder: "Claude_Work", Name: "Work", UUID: "same", SignedIn: true},
		{Folder: "Claude_Solo", Name: "Solo", UUID: "solo", SignedIn: true},
	})
	if !strings.Contains(html, "the same account") {
		t.Fatalf("want a duplicate warning:\n%s", html)
	}
	if !strings.Contains(html, `send('showMerge','Claude|Claude_Work')`) {
		t.Fatalf("warning must offer the merge for that group:\n%s", html)
	}
	if strings.Count(html, "dup-pill") != 2 {
		t.Fatalf("both duplicate cards must be marked, got %d:\n%s", strings.Count(html, "dup-pill"), html)
	}
}

func TestRenderListNoWarningWhenAccountsAreUnique(t *testing.T) {
	html := RenderList([]ProfileVM{
		{Folder: "Claude", Name: "Claude", UUID: "a", SignedIn: true},
		{Folder: "Claude_Two", Name: "Two", UUID: "b", SignedIn: true},
	})
	if strings.Contains(html, "the same account") {
		t.Fatal("no duplicates, no warning")
	}
	if strings.Contains(html, "dup-pill") {
		t.Fatal("no duplicates, no pills")
	}
}

func TestRenderListDuplicateWarningIgnoresProfilesWithNoAccount(t *testing.T) {
	// Two profiles awaiting sign-in both have an empty UUID. That is not two
	// profiles sharing an account.
	html := RenderList([]ProfileVM{
		{Folder: "Claude_A", Name: "A", UUID: "", SignedIn: false},
		{Folder: "Claude_B", Name: "B", UUID: "", SignedIn: false},
	})
	if strings.Contains(html, "the same account") {
		t.Fatalf("empty UUIDs must not group:\n%s", html)
	}
}

func TestRenderListOneWarningForTheFirstGroupOnly(t *testing.T) {
	html := RenderList([]ProfileVM{
		{Folder: "Claude_A", Name: "A", UUID: "x", SignedIn: true},
		{Folder: "Claude_B", Name: "B", UUID: "x", SignedIn: true},
		{Folder: "Claude_C", Name: "C", UUID: "y", SignedIn: true},
		{Folder: "Claude_D", Name: "D", UUID: "y", SignedIn: true},
	})
	if strings.Count(html, "the same account") != 1 {
		t.Fatalf("one group at a time, got %d warnings:\n%s", strings.Count(html, "the same account"), html)
	}
	if !strings.Contains(html, `send('showMerge','Claude_A|Claude_B')`) {
		t.Fatal("the first group by folder order goes first")
	}
	// All four cards are still flagged, so the user can see the second pair is
	// coming.
	if strings.Count(html, "dup-pill") != 4 {
		t.Fatalf("every duplicate card is marked, got %d", strings.Count(html, "dup-pill"))
	}
}
```

Check `internal/panelui/render_test.go`'s existing imports include `strings` and add it if not.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/panelui/ -run TestRenderList -v`
Expected: FAIL — `unknown field UUID in struct literal`.

- [ ] **Step 3: Write the implementation**

Add to `ProfileVM`:

```go
	// UUID is the account signed in to this profile, empty when none is. The
	// account list groups by it to spot two profiles holding one account, which
	// is a state the user has to resolve.
	UUID string
```

Add CSS to `shell` (beside `.note-bad`, line 91):

```css
.dup{background:#fde4e4;border-radius:12px;padding:11px 13px;margin-bottom:11px;display:flex;align-items:center;gap:10px}
.dup .dt{flex:1;font-size:12px;color:#a32d2d;line-height:1.45}
.dup-pill{font-size:10.5px;font-weight:700;padding:2px 8px;border-radius:999px;background:#fde4e4;color:#a32d2d;white-space:nowrap}
.btn-sm{font:inherit;font-size:12px;font-weight:700;border:none;cursor:pointer;border-radius:9px;padding:7px 12px;background:linear-gradient(135deg,#7c6cf0,#9b6bff);color:#fff;flex:none}
.btn-sm:hover{filter:brightness(1.05)}
.addcard{display:flex;align-items:center;justify-content:center;gap:7px;background:transparent;border:2px dashed #cdc8e0;border-radius:14px;padding:12px 14px;cursor:pointer;font:inherit;font-size:13px;font-weight:700;color:#6b6580;width:100%}
.addcard:hover{border-color:#7c6cf0;color:#7c6cf0;background:#faf9ff}
```

In `RenderList`, before the card loop, compute the duplicate state:

```go
	// Two profiles signed in to one account is a state to resolve, not a
	// preference. Group by account so every affected card can be marked, and
	// offer the merge for one group at a time: each merge needs Claude quit, so
	// batching them would only mean a longer sequence of the same interruption.
	byUUID := map[string][]string{}
	var uuidOrder []string
	for _, p := range profiles {
		if p.UUID == "" {
			continue // no account signed in yet; nothing to be a duplicate of
		}
		if _, ok := byUUID[p.UUID]; !ok {
			uuidOrder = append(uuidOrder, p.UUID)
		}
		byUUID[p.UUID] = append(byUUID[p.UUID], p.Folder)
	}
	dupFolder := map[string]bool{}
	var firstGroup []string
	for _, u := range uuidOrder {
		g := byUUID[u]
		if len(g) < 2 {
			continue
		}
		for _, f := range g {
			dupFolder[f] = true
		}
		if firstGroup == nil {
			firstGroup = g
		}
	}
	dupWarning := ""
	if firstGroup != nil {
		a, b := firstGroup[0], firstGroup[1]
		dupWarning = fmt.Sprintf(`<div class="dup">
  <div class="dt">%s and %s are the same account. Merge them to clean this up.</div>
  <button class="btn-sm" onclick="send('showMerge','%s')">Merge</button>
</div>`, esc(nameOf(profiles, a)), esc(nameOf(profiles, b)), esc(a+"|"+b))
	}
```

`profiles` arrives in folder order from both hosts (`buildProfiles` iterates `FindProfiles`, which reads a sorted directory), so `uuidOrder` yields groups ordered by their first folder without a sort.

Add the small lookup helper at file scope:

```go
// nameOf returns a profile's display name by folder, falling back to the folder
// itself. Used by the duplicate warning, which names two cards the user is
// looking at.
func nameOf(profiles []ProfileVM, folder string) string {
	for _, p := range profiles {
		if p.Folder == folder {
			return p.Name
		}
	}
	return folder
}
```

Inside the card loop, add the pill for a duplicate card. Locate where the plan pill is appended in `RenderList` and add alongside it:

```go
		dupPill := ""
		if dupFolder[p.Folder] {
			dupPill = `<span class="dup-pill">Duplicate</span>`
		}
```

and include `dupPill` in the card's `row1`, immediately after the plan pill.

After the card loop, append the add card to the cards builder:

```go
	// In the list rather than the footer: the footer already holds Rescan,
	// Settings and Quit, and a fourth labelled button does not fit in 400px
	// without demoting Rescan to a bare icon.
	cards.WriteString(`
      <button class="addcard" onclick="send('showNewProfile','')">＋&nbsp; Add another account</button>`)
```

Finally, put `dupWarning` into the body immediately before `<div class="cards">`.

Read `RenderList` in full before editing: the `esc` local and the exact `row1` markup must be matched rather than guessed, and the existing empty-state branch (line 217) must still render when there are no profiles — with the add card, that branch's "Run Rescan to add some" text now has a button beside it, so keep both.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/panelui/ -v`
Expected: PASS, including the pre-existing render tests.

- [ ] **Step 5: Commit**

```bash
git add internal/panelui/render.go internal/panelui/render_test.go
git commit -m "panel: offer to add an account, and insist on merging duplicates"
```

---

### Task 12: Rescan — Recover on recoverable ghosts

**Files:**
- Modify: `internal/panelui/render.go` — `RenderRescan` ghost branch (line 309)
- Test: `internal/panelui/render_test.go`

**Interfaces:**
- Consumes: `core.ScannedAccount.Recoverable`, `.SourceFolder`, `.Note` (Task 3).
- Produces: the panel action `showRecover` with arg `uuid|sourceFolder`.

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/panelui/render_test.go
func TestRenderRescanRecoverableGhostOffersRecovery(t *testing.T) {
	accounts := []core.ScannedAccount{{
		UUID: "bbbbbbbb-0000-4000-8000-000000000002", Complete: false,
		Recoverable: true, SourceFolder: "Claude", Convos: 94,
		LastUpdated: time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC),
		Note:        core.RecoverableGhostNote,
	}}
	html := RenderRescan(accounts, nil)
	if !strings.Contains(html, "Signed out in Claude Desktop") {
		t.Fatalf("recoverable ghost needs its own heading:\n%s", html)
	}
	if strings.Contains(html, "Unrecognized account") {
		t.Fatal("a recoverable account is not unrecognised")
	}
	if !strings.Contains(html, `send('showRecover','bbbbbbbb-0000-4000-8000-000000000002|Claude')`) {
		t.Fatalf("want a Recover action carrying uuid and source folder:\n%s", html)
	}
	if !strings.Contains(html, "note-todo") {
		t.Fatal("recoverable note uses the blue style, not the red one")
	}
	if strings.Contains(html, "note-bad") {
		t.Fatal("red is reserved for dead ghosts")
	}
	if !strings.Contains(html, "94 chats") {
		t.Fatal("the conversation count is how the user recognises the account")
	}
}

func TestRenderRescanDeadGhostStaysReadOnly(t *testing.T) {
	accounts := []core.ScannedAccount{{
		UUID: "dead", Complete: false, Recoverable: false, Note: "Invalid account data",
	}}
	html := RenderRescan(accounts, nil)
	if !strings.Contains(html, "Unrecognized account") {
		t.Fatal("a dead ghost keeps its existing heading")
	}
	if strings.Contains(html, "showRecover") {
		t.Fatal("nothing to recover, so no Recover button")
	}
	if !strings.Contains(html, "note-bad") {
		t.Fatal("dead ghost keeps the red note")
	}
}

func TestRenderRescanRecoverableGhostIsNotSelectable(t *testing.T) {
	accounts := []core.ScannedAccount{{
		UUID: "u", Complete: false, Recoverable: true, SourceFolder: "Claude", Convos: 1,
		Note: core.RecoverableGhostNote,
	}}
	html := RenderRescan(accounts, nil)
	// It has no folder to manage yet, so it must not join the checkbox set that
	// Confirm submits.
	if strings.Contains(html, `class="card selectable`) {
		t.Fatalf("a ghost cannot be managed, only recovered:\n%s", html)
	}
}
```

Check that `render_test.go` imports `time` and `core`; the existing tests at line 50 use `core.ScannedAccount`, so `core` is present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/panelui/ -run TestRenderRescan -v`
Expected: FAIL — the existing ghost branch renders "Unrecognized account" for both.

- [ ] **Step 3: Write the implementation**

Replace the `if !a.Complete` branch in `RenderRescan` (lines 309-317) with a split on `Recoverable`:

```go
		if !a.Complete {
			if a.Recoverable {
				// Not selectable: there is no folder to manage yet. The action is
				// to give this account one, which is what Recover does. The note
				// is deliberately blue — nothing here is broken, the
				// conversations are intact and only the profile is missing.
				cards.WriteString(fmt.Sprintf(`
      <div class="card"><div style="width:21px;flex:none"></div>
        <div class="body"><div class="row1"><span class="name">Signed out in Claude Desktop</span></div>
          <div class="meta"><span class="chip">%s</span><span class="dot">·</span>%d chats<span class="dot">·</span>%s</div>
          <div class="note-todo">%s</div></div>
        <button class="btn-sm" onclick="send('showRecover','%s')">Recover</button></div>`,
					esc(ShortID(a.UUID)), a.Convos, esc(date), esc(a.Note),
					esc(a.UUID+"|"+a.SourceFolder)))
				continue
			}
			cards.WriteString(fmt.Sprintf(`
      <div class="card ghost"><div style="width:21px;flex:none"></div>
        <div class="body"><div class="row1"><span class="name">Unrecognized account</span></div>
          <div class="meta"><span class="chip">%s</span><span class="dot">·</span>%d chats<span class="dot">·</span>%s</div>
          <div class="note-bad">%s</div></div></div>`,
				esc(ShortID(a.UUID)), a.Convos, esc(date), esc(a.Note)))
			continue
		}
```

The recoverable card drops the `ghost` class so it is not dimmed to 55% opacity: it is the one ghost row the user can act on.

`.btn-sm` was added to `shell` in Task 11. If Task 11 has not landed, add it here instead and drop it from Task 11.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/panelui/ -v`
Expected: PASS. `TestRenderRescanGhostStaysReadOnly` (existing, line 118) may assert on the old markup — read it and, if it uses a populated bucket, retarget it at a dead ghost so it keeps testing what it was written to test.

- [ ] **Step 5: Commit**

```bash
git add internal/panelui/render.go internal/panelui/render_test.go
git commit -m "panel: let a signed-out account be recovered from Rescan"
```

---

### Task 13: The name-the-profile screen

**Files:**
- Modify: `internal/panelui/render.go`
- Test: `internal/panelui/render_test.go`

**Interfaces:**
- Consumes: `.btn-sm` and CSS from Task 11; `.rninput` already exists (line 122).
- Produces:
  - `type NewProfileVM struct { RecoverUUID, SourceFolder, SuggestedName string; Convos int; Err string }`
  - `func RenderNewProfile(vm NewProfileVM) string`
  - the panel action `createProfile` with a JSON arg `[name, recoverUUID, sourceFolder]`

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/panelui/render_test.go
func TestRenderNewProfileAddVariant(t *testing.T) {
	html := RenderNewProfile(NewProfileVM{})
	if !strings.Contains(html, "Add another account") {
		t.Fatalf("title:\n%s", html)
	}
	if !strings.Contains(html, `value=""`) {
		t.Fatal("the add path starts with an empty name")
	}
	if !strings.Contains(html, "different account") {
		t.Fatal("the add path must warn against signing in as an existing account")
	}
	if strings.Contains(html, "Its conversations come back") {
		t.Fatal("no recovery copy on the add path")
	}
}

func TestRenderNewProfileRecoverVariant(t *testing.T) {
	html := RenderNewProfile(NewProfileVM{
		RecoverUUID:   "bbbbbbbb-0000-4000-8000-000000000002",
		SourceFolder:  "Claude",
		SuggestedName: "Recovered 2026-07-29",
		Convos:        94,
	})
	if !strings.Contains(html, "Recover this account") {
		t.Fatalf("title:\n%s", html)
	}
	if !strings.Contains(html, `value="Recovered 2026-07-29"`) {
		t.Fatal("the recovery path pre-fills the name")
	}
	if !strings.Contains(html, "bbbbbbbb") {
		t.Fatal("must say which account to sign in as")
	}
	if !strings.Contains(html, "94") {
		t.Fatal("the conversation count helps the user recognise the account")
	}
	if strings.Contains(html, "different account") {
		t.Fatal("the different-account warning belongs to the add path only")
	}
}

func TestRenderNewProfileShowsAnError(t *testing.T) {
	html := RenderNewProfile(NewProfileVM{Err: "use only letters, numbers, spaces, dashes and underscores"})
	if !strings.Contains(html, "use only letters") {
		t.Fatalf("a rejected name must say why:\n%s", html)
	}
}

func TestRenderNewProfilePassesContextThroughDataAttributes(t *testing.T) {
	html := RenderNewProfile(NewProfileVM{RecoverUUID: "u-1", SourceFolder: "Claude"})
	// The v0.9.1 bug class: folder names must never be interpolated into inline
	// JS string arguments.
	if !strings.Contains(html, `data-uuid="u-1"`) || !strings.Contains(html, `data-source="Claude"`) {
		t.Fatalf("context must travel as data attributes:\n%s", html)
	}
	if strings.Contains(html, "createProfileSave('") {
		t.Fatalf("no inline string args:\n%s", html)
	}
}

func TestRenderNewProfileEscapesTheSuggestedName(t *testing.T) {
	html := RenderNewProfile(NewProfileVM{SuggestedName: `a"><script>x</script>`})
	if strings.Contains(html, "<script>x</script>") {
		t.Fatalf("suggested name must be escaped:\n%s", html)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/panelui/ -run TestRenderNewProfile -v`
Expected: FAIL — `undefined: NewProfileVM`.

- [ ] **Step 3: Write the implementation**

Add to `internal/panelui/render.go`:

```go
// NewProfileVM drives the name-the-profile screen. One screen serves both
// entry points: RecoverUUID empty is the plain add path, set is a recovery of
// that account. They run the same underlying operation and differ only in copy
// and in whether a session bucket comes along, so sharing the view is what keeps
// the two from drifting apart.
type NewProfileVM struct {
	RecoverUUID   string
	SourceFolder  string
	SuggestedName string
	Convos        int
	Err           string
}

// RenderNewProfile is the in-panel screen for naming a new account profile.
func RenderNewProfile(vm NewProfileVM) string {
	esc := html.EscapeString
	recovering := vm.RecoverUUID != ""

	title, sub, confirm := "Add another account", "It gets its own profile", "Add"
	if recovering {
		title, sub, confirm = "Recover this account", "It gets its own profile", "Recover"
	}

	second := `<div class="hintw">Sign in as a <b>different</b> account. Signing in as one you already have creates a duplicate, and MCS will ask you to merge.</div>`
	if recovering {
		second = fmt.Sprintf(`<div class="hintw">Sign in as the account ending <b>%s</b> (%d chats). Its conversations come back on their own.</div>`,
			esc(ShortID(vm.RecoverUUID)), vm.Convos)
	}

	errBlock := ""
	if vm.Err != "" {
		errBlock = `<div class="errbox">` + esc(vm.Err) + `</div>`
	}

	body := `<div class="header">
  <button class="back" onclick="send('showList','')">‹</button>
  <div class="htext"><h1>` + esc(title) + `</h1><p>` + esc(sub) + `</p></div>
</div>` + errBlock + `
<input id="np" class="rninput" type="text" value="` + esc(vm.SuggestedName) + `" placeholder="Personal">
<div class="hint">Claude closes, your current account is saved, and a clean Claude opens.</div>` + second + `
<div class="footer">
  <button class="btn btn-light" onclick="send('showList','')">Cancel</button>
  <button class="btn btn-primary" data-uuid="` + esc(vm.RecoverUUID) + `" data-source="` + esc(vm.SourceFolder) + `" onclick="createProfileSave(this)">` + esc(confirm) + `</button>
</div>
<script>var e=document.getElementById('np'); e.focus(); e.select();</script>`
	return shell(body)
}
```

Add the two CSS rules to `shell`, beside `.rninput`:

```css
.hint{font-size:12px;color:#6b6580;line-height:1.5;margin-top:11px}
.hintw{background:#fff6e0;color:#854f0b;font-size:12px;line-height:1.5;padding:9px 12px;border-radius:11px;margin-top:10px}
.errbox{background:#fde4e4;color:#a32d2d;font-size:12px;font-weight:700;padding:9px 12px;border-radius:11px;margin-bottom:11px}
```

Add the JS bridge function to `shell`'s script block, beside `renameSave` (line 156):

```js
  function createProfileSave(btn){
    var v=document.getElementById('np').value.trim();
    send('createProfile', JSON.stringify([v, btn.dataset.uuid||'', btn.dataset.source||'']));
  }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/panelui/ -v`
Expected: PASS, 5 new tests plus the existing ones.

- [ ] **Step 5: Commit**

```bash
git add internal/panelui/render.go internal/panelui/render_test.go
git commit -m "panel: one screen for naming a new or recovered account"
```

---

### Task 14: The merge screen

**Files:**
- Modify: `internal/panelui/render.go`
- Test: `internal/panelui/render_test.go`

**Interfaces:**
- Consumes: CSS from Tasks 11 and 13.
- Produces:
  - `type MergeCandidateVM struct { Folder, Name, Plan string; Convos int; Current bool }`
  - `func RenderMerge(a, b MergeCandidateVM, status string, busy bool) string`
  - the panel action `mergeConfirm` with arg `keepFolder|archiveFolder`

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/panelui/render_test.go
func TestRenderMergePreselectsTheProfileInUse(t *testing.T) {
	a := MergeCandidateVM{Folder: "Claude", Name: "Claude", Convos: 99}
	b := MergeCandidateVM{Folder: "Claude_Work", Name: "Work", Convos: 42, Current: true}
	html := RenderMerge(a, b, "", false)

	// Keeping the one already in use means no re-sign-in, so it is the default.
	iA := strings.Index(html, `data-folder="Claude"`)
	iB := strings.Index(html, `data-folder="Claude_Work"`)
	if iA < 0 || iB < 0 {
		t.Fatalf("both candidates must be rendered:\n%s", html)
	}
	selIdx := strings.Index(html, "card selectable selected")
	if selIdx < 0 {
		t.Fatalf("one card must be preselected:\n%s", html)
	}
	if selIdx > iB {
		t.Fatalf("the in-use profile (Claude_Work) must be the preselected one:\n%s", html)
	}
	if !strings.Contains(html, "Will be archived") {
		t.Fatal("the other card must say what happens to it")
	}
}

func TestRenderMergeShowsTheCombinedTotal(t *testing.T) {
	html := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Claude", Convos: 99, Current: true},
		MergeCandidateVM{Folder: "Claude_Work", Name: "Work", Convos: 42},
		"", false)
	if !strings.Contains(html, "141") {
		t.Fatalf("want the union total 99+42:\n%s", html)
	}
	if !strings.Contains(html, "archived, not deleted") {
		t.Fatal("must say nothing is deleted")
	}
}

func TestRenderMergeUsesDataAttributesNotInlineArgs(t *testing.T) {
	html := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Claude", Current: true},
		MergeCandidateVM{Folder: "Claude_Work", Name: "Work"},
		"", false)
	if strings.Contains(html, "mergeConfirm('") {
		t.Fatalf("no inline string args (v0.9.1 bug class):\n%s", html)
	}
	if !strings.Contains(html, "toggleMergePick(this)") {
		t.Fatalf("cards must switch the pick through a handler:\n%s", html)
	}
}

func TestRenderMergeBusyDisablesTheAction(t *testing.T) {
	html := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Claude", Current: true},
		MergeCandidateVM{Folder: "Claude_Work", Name: "Work"},
		"Merging…", true)
	if !strings.Contains(html, "Merging…") {
		t.Fatal("status must be shown")
	}
	if !strings.Contains(html, "disabled") {
		t.Fatalf("a merge in flight must not be startable twice:\n%s", html)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/panelui/ -run TestRenderMerge -v`
Expected: FAIL — `undefined: MergeCandidateVM`.

- [ ] **Step 3: Write the implementation**

```go
// MergeCandidateVM is one side of a duplicate pair on the merge screen.
type MergeCandidateVM struct {
	Folder  string
	Name    string
	Plan    string
	Convos  int
	Current bool // the profile Claude is running on right now
}

// RenderMerge is the in-panel screen for resolving two profiles signed in to one
// account. The profile in use is preselected to keep, because keeping it means
// the user does not have to sign in again; they can pick the other one when they
// prefer its name. Conversations are combined either way, so the choice only
// decides which name survives.
func RenderMerge(a, b MergeCandidateVM, status string, busy bool) string {
	esc := html.EscapeString
	st := ""
	if status != "" {
		st = `<div class="status">` + esc(status) + `</div>`
	}
	// Keep the in-use profile by default. If neither is running, fall back to the
	// first, so the screen is never rendered with nothing chosen.
	keep := a.Folder
	if b.Current && !a.Current {
		keep = b.Folder
	}

	card := func(c MergeCandidateVM) string {
		cls := "card selectable"
		if c.Folder == keep {
			cls += " selected"
		}
		sub := "Will be archived"
		if c.Folder == keep {
			sub = "Keep this one"
			if c.Current {
				sub = "In use now · keep this one"
			}
		}
		return fmt.Sprintf(`
      <div class="%s" data-folder="%s" onclick="toggleMergePick(this)">
        <input type="checkbox" class="chk"%s>
        <div class="body"><div class="row1"><span class="name">%s</span>%s</div>
          <div class="meta">%d chats<span class="dot">·</span>%s</div></div></div>`,
			cls, esc(c.Folder), map[bool]string{true: " checked", false: ""}[c.Folder == keep],
			esc(c.Name), planPill(c.Plan), c.Convos, esc(sub))
	}

	dis, oc := "", `onclick="mergeConfirm()"`
	if busy {
		dis, oc = " disabled", ""
	}

	body := `<div class="header">
  <button class="back" onclick="send('showList','')">‹</button>
  <div class="htext"><h1>Merge duplicates</h1><p>Both are the same account</p></div>
</div>` + st + `
<div class="cards">` + card(a) + card(b) + `</div>
<div class="hint">All ` + fmt.Sprint(a.Convos+b.Convos) + ` conversations are combined into the account you keep. The other folder is archived, not deleted, so you can put it back yourself.</div>
<div class="footer">
  <button class="btn btn-light" onclick="send('showList','')">Cancel</button>
  <button class="btn btn-primary"` + dis + ` ` + oc + `>Merge</button>
</div>`
	return shell(body)
}
```

Add to `shell`'s script block:

```js
  function toggleMergePick(el){
    var cards=el.parentNode.querySelectorAll('.card.selectable');
    for(var i=0;i<cards.length;i++){
      var on=cards[i]===el;
      cards[i].classList.toggle('selected',on);
      var c=cards[i].querySelector('.chk'); if(c) c.checked=on;
    }
  }
  function mergeConfirm(){
    var sel=document.querySelector('.card.selectable.selected');
    var all=document.querySelectorAll('.card.selectable');
    if(!sel||all.length!==2) return;
    var other=all[0]===sel?all[1]:all[0];
    send('mergeConfirm', sel.dataset.folder+'|'+other.dataset.folder);
  }
```

`toggleMergePick` picks exactly one, unlike `toggleCard`, which toggles freely for the Rescan multi-select. A merge with both or neither chosen has no meaning.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/panelui/ -v`
Expected: PASS, 4 new tests plus all existing.

- [ ] **Step 5: Commit**

```bash
git add internal/panelui/render.go internal/panelui/render_test.go
git commit -m "panel: merge screen, keeping the profile already in use by default"
```

---

### Task 15: Wire the macOS host

**Files:**
- Modify: `cmd/mcs-menubar/main.go` — `goPanelAction` (line 106), `reloadPanel` (line 260), `buildProfiles` (line 324)

**Interfaces:**
- Consumes: everything from Tasks 1-14.
- Produces: nothing for later tasks except the pattern Task 16 mirrors.

This host is CGO/darwin-only and has no unit tests; verification is a build plus a manual pass.

- [ ] **Step 1: Add the view state and the UUID to buildProfiles**

Extend the view comment and the package vars (line 40):

```go
	currentView  = "list" // "list" | "rescan" | "settings" | "sync" | "rename" | "newprofile" | "merge"
	renameFolder string   // the folder being renamed in the "rename" view

	// newProfileVM carries the pending name screen's context between the action
	// that opened it and the render that draws it, including the validation
	// error from a rejected attempt.
	newProfileVM panelui.NewProfileVM
	// mergeFolders is the pair being resolved in the "merge" view.
	mergeFolders [2]string
```

In `buildProfiles`, keep the UUID instead of discarding it (line 330):

```go
	for _, p := range profiles {
		uuid, uErr := platform.GetProfileAccountUUID(p.Path)
		if !panelIncludes(managed, p.Name, uErr == nil, p.Managed) {
			continue
		}
		vm := panelui.ProfileVM{Folder: p.Name, Name: core.DisplayName(p.Name), Current: p.Path == running, Plan: cachedPlan(p.Path), UUID: uuid, SignedIn: uErr == nil}
		out = append(out, vm)
	}
```

Check whether `SignedIn` was already being set here; if it was, keep the existing assignment rather than duplicating it.

- [ ] **Step 2: Add the render cases**

In `reloadPanel`'s switch:

```go
	case "newprofile":
		mu.Lock()
		vm := newProfileVM
		mu.Unlock()
		htmlStr = panelui.RenderNewProfile(vm)
	case "merge":
		mu.Lock()
		pair := mergeFolders
		mu.Unlock()
		htmlStr = panelui.RenderMerge(mergeCandidate(pair[0]), mergeCandidate(pair[1]), getStatus(), getBusy())
```

Add both helpers at file scope:

```go
// profilePathFor resolves a folder name to its real path by looking it up among
// the discovered profiles. Not filepath.Join onto the data root: on the Windows
// Store build a profile lives in the slot or under .mcs-profiles, so joining
// would produce a path that does not exist. macOS only has sibling dirs today,
// but a lookup is correct on both hosts and keeps them from diverging.
func profilePathFor(folder string) string {
	for _, p := range mustFindProfiles() {
		if p.Name == folder {
			return p.Path
		}
	}
	return filepath.Join(plat.AppSupportDir(), folder)
}

// mergeCandidate builds one side of the merge screen: the display name, plan,
// how many conversations it holds for its own account, and whether Claude is
// running on it.
func mergeCandidate(folder string) panelui.MergeCandidateVM {
	path := profilePathFor(folder)
	running, _ := plat.DetectRunningProfile()
	vm := panelui.MergeCandidateVM{
		Folder:  folder,
		Name:    core.DisplayName(folder),
		Plan:    cachedPlan(path),
		Current: path == running,
	}
	if uuid, err := platform.GetProfileAccountUUID(path); err == nil {
		for _, p := range mustFindProfiles() {
			if p.Name == folder {
				vm.Convos = p.UUIDBuckets[uuid]
			}
		}
	}
	return vm
}
```

- [ ] **Step 3: Add the action handlers**

In `goPanelAction`'s switch:

```go
	case "showNewProfile":
		mu.Lock()
		newProfileVM = panelui.NewProfileVM{}
		mu.Unlock()
		setView("newprofile")
		go reloadPanel()
	case "showRecover":
		// arg is "<uuid>|<sourceFolder>"
		parts := strings.SplitN(arg, "|", 2)
		if len(parts) != 2 {
			return
		}
		vm := panelui.NewProfileVM{
			RecoverUUID:   parts[0],
			SourceFolder:  parts[1],
			SuggestedName: recoverySuggestedName(parts[0], parts[1]),
			Convos:        recoveryConvos(parts[0], parts[1]),
		}
		mu.Lock()
		newProfileVM = vm
		mu.Unlock()
		setView("newprofile")
		go reloadPanel()
	case "createProfile":
		var a []string
		if json.Unmarshal([]byte(arg), &a) != nil || len(a) != 3 {
			return
		}
		if getBusy() {
			return
		}
		setBusyStatus(true, "Setting up…")
		reloadPanel()
		go func() {
			_, err := core.NewProfileCreator(plat).Create(core.CreateProfileRequest{
				Name: a[0], RecoverUUID: a[1], SourceFolder: a[2],
			})
			setBusyStatus(false, "")
			if err != nil {
				// Back to the same screen with the reason, so the typed name and
				// the recovery context are not lost.
				mu.Lock()
				newProfileVM.Err = err.Error()
				mu.Unlock()
				setView("newprofile")
			} else {
				setView("list")
			}
			reloadPanel()
		}()
	case "showMerge":
		parts := strings.SplitN(arg, "|", 2)
		if len(parts) != 2 {
			return
		}
		mu.Lock()
		mergeFolders = [2]string{parts[0], parts[1]}
		mu.Unlock()
		setStatus("")
		setView("merge")
		go reloadPanel()
	case "mergeConfirm":
		// arg is "<keepFolder>|<archiveFolder>"
		parts := strings.SplitN(arg, "|", 2)
		if len(parts) != 2 || getBusy() {
			return
		}
		keep, archive := parts[0], parts[1]
		setBusyStatus(true, "Merging…")
		reloadPanel()
		go func() {
			// Resolve paths before quitting Claude: on the Store build quitting is
			// what frees the directories, but the lookup itself only reads.
			keepPath, archivePath := profilePathFor(keep), profilePathFor(archive)
			if err := plat.TerminateApp(); err != nil {
				setBusyStatus(false, err.Error())
				reloadPanel()
				return
			}
			_, err := core.MergeDuplicates(keepPath, archivePath, plat.ArchiveDir())
			if err != nil {
				setBusyStatus(false, err.Error())
				reloadPanel()
				return
			}
			setBusyStatus(false, "Merged.")
			setView("list")
			reloadPanel()
		}()
```

Add the two small helpers at file scope:

```go
// recoverySuggestedName proposes a name for a recovered account, dated by when
// it was last used so the user can tell two recoveries apart.
func recoverySuggestedName(uuid, sourceFolder string) string {
	for _, a := range core.ScanAccounts(mustFindProfiles(), core.LoadPending()) {
		if a.UUID == uuid && !a.LastUpdated.IsZero() {
			return "Recovered " + a.LastUpdated.Format("2006-01-02")
		}
	}
	return "Recovered"
}

// recoveryConvos reports how many conversations the orphan has, which is how the
// user recognises which account they are about to sign in as.
func recoveryConvos(uuid, sourceFolder string) int {
	for _, a := range core.ScanAccounts(mustFindProfiles(), core.LoadPending()) {
		if a.UUID == uuid {
			return a.Convos
		}
	}
	return 0
}
```

Prune stale pending entries where the panel opens, so a signed-in profile stops being labelled as awaiting sign-in. In `goPanelWillOpen` (line 80), inside the existing goroutine:

```go
//export goPanelWillOpen
func goPanelWillOpen() {
	setView("list") // always open to the account list
	setStatus("")   // clear any stale feedback
	go func() {
		// A profile that has since been signed in to is no longer pending, and
		// the panel is the only place that notices.
		profiles := mustFindProfiles()
		for _, f := range core.StalePending(core.LoadPending(), profiles) {
			_ = core.RemovePending(f)
		}
		reloadPanel()
	}()
}
```

Add `"encoding/json"` to the imports if absent — line 21 shows it is already there.

- [ ] **Step 4: Verify**

Run: `go build ./...`
Expected: exit 0.

Run: `go vet ./cmd/mcs-menubar/`
Expected: exit 0.

Build the app bundle and drive it by hand:

```bash
./scripts/package-app.sh
```

Then, with the app running: open the panel, confirm the dashed add card appears; open it, confirm Cancel returns to the list; run Rescan on a machine with an orphan and confirm the Recover row appears with a working button. Full recovery needs a real second account, so if one is not to hand, stop at the name screen.

- [ ] **Step 5: Commit**

```bash
git add cmd/mcs-menubar/main.go
git commit -m "menubar: wire add, recover and merge into the panel"
```

---

### Task 16: Wire the Windows host

Mirrors Task 15 against the WebView2 panel. Read Task 15's diff first and keep the two hosts' behaviour identical; they share the renderer and must not drift.

**Files:**
- Modify: `cmd/mcs-tray/panel_windows.go` — the action switch (line 272) and the render switch (line 394)

**Interfaces:**
- Consumes: Tasks 1-14, and Task 15 as the reference implementation.
- Produces: nothing.

- [ ] **Step 1: Read the macOS wiring and the Windows panel's existing structure**

Read `cmd/mcs-menubar/main.go`'s `goPanelAction` and `reloadPanel` as changed in Task 15, and `cmd/mcs-tray/panel_windows.go` in full. The Windows file uses `panelSetView`, `panelMustFindProfiles`, and its own status helpers; map each macOS call onto its Windows equivalent rather than copying names.

- [ ] **Step 2: Port the state, render cases, and handlers**

Add the same package-level state (`newProfileVM`, `mergeFolders`), the same two render cases calling `panelui.RenderNewProfile` and `panelui.RenderMerge`, the same six action cases (`showNewProfile`, `showRecover`, `createProfile`, `showMerge`, `mergeConfirm`), and the same `mergeCandidate` / `recoverySuggestedName` / `recoveryConvos` helpers, using the Windows file's existing helpers for profile discovery and status.

Two Windows-specific points:

- `profilePathFor` (added to the macOS host in Task 15) is what makes the Store build work: a profile there lives in the slot or under `.mcs-profiles`, so joining a folder name onto the data root produces a path that does not exist. Port it using this host's discovery helper:

```go
// profilePathFor resolves a folder name to its real path. On the Store build a
// profile lives in the slot or under .mcs-profiles, so joining onto the data
// root would produce a path that does not exist.
func profilePathFor(folder string) string {
	for _, p := range panelMustFindProfiles() {
		if p.Name == folder {
			return p.Path
		}
	}
	return filepath.Join(plat.AppSupportDir(), folder)
}
```

Use it in both `mergeCandidate` and the `mergeConfirm` handler, exactly as Task 15 does.

- Where the Store build's create flow needs its post-sign-in migration watcher, it is already started by the tray at boot (`onready_windows.go:97`). A profile created from the panel queues its migration in `msixParkForNewIn`, but the watcher in the *tray* process started before that. Restart it after a successful create so the queued migration is actually polled:

```go
		// The Store build's migration watcher runs in the tray process and only
		// starts if a migration was already queued at boot. A create from the
		// panel queues one afterwards, so ask the tray to pick it up.
		notifyTrayMigrationQueued()
```

Implement `notifyTrayMigrationQueued` by writing a line to the panel's stdout that `readPanelMessages` (`cmd/mcs-tray/onready_windows.go:264`) understands, following the existing `MCS_QUIT` pattern: add a `MCS_MIGRATION_QUEUED` case that calls `startMigrationWatcher()`. `startMigrationWatcher` already returns immediately when nothing is queued, so a spurious message is harmless.

- [ ] **Step 3: Verify**

Run: `GOOS=windows GOARCH=amd64 go build ./...`
Expected: exit 0.

Run: `GOOS=windows GOARCH=amd64 go vet ./cmd/mcs-tray/`
Expected: exit 0.

Run: `go build ./... && go test ./... 2>&1 | tail -5`
Expected: builds clean, all tests pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/mcs-tray/panel_windows.go cmd/mcs-tray/onready_windows.go cmd/mcs-menubar/main.go
git commit -m "windows: wire add, recover and merge into the panel"
```

---

### Task 17: Settings — open the archive folder

**Files:**
- Modify: `internal/panelui/render.go` — `RenderSettings`
- Modify: `cmd/mcs-menubar/main.go`, `cmd/mcs-tray/panel_windows.go` — the `openBackups` handler's neighbourhood
- Test: `internal/panelui/render_test.go`

**Interfaces:**
- Consumes: `platform.Platform.ArchiveDir()` (Task 7).
- Produces: the panel action `openArchive`.

- [ ] **Step 1: Write the failing test**

```go
// append to internal/panelui/render_test.go
func TestRenderSettingsOffersTheArchiveFolder(t *testing.T) {
	html := RenderSettings(SettingsVM{Version: "0.11.0"})
	if !strings.Contains(html, "Open archive folder") {
		t.Fatalf("merged-away profiles have to be findable:\n%s", html)
	}
	if !strings.Contains(html, "send('openArchive'") {
		t.Fatalf("want the openArchive action:\n%s", html)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/panelui/ -run TestRenderSettingsOffers -v`
Expected: FAIL — no such text.

- [ ] **Step 3: Implement**

Read `RenderSettings` and find the existing "Open backup folder" row. Add an identical row directly after it:

```go
  <button class="sbtn" onclick="send('openArchive','')">Open archive folder</button>
```

Match the surrounding markup exactly rather than assuming the snippet above fits.

In both hosts, add the handler beside the existing backup-folder one. `openBackups` on macOS is `cmd/mcs-menubar/main.go:196`:

```go
	case "openBackups":
		home, _ := os.UserHomeDir()
		_ = exec.Command("open", filepath.Join(home, ".multi-claude-switcher", "backups")).Start()
```

so the macOS case is:

```go
	case "openArchive":
		dir := plat.ArchiveDir()
		// Create it first: until something has been archived the folder does not
		// exist, and `open` on a missing path fails with a dialog.
		_ = os.MkdirAll(dir, 0o755)
		_ = exec.Command("open", dir).Start()
```

Read the Windows host's own `openBackups` case and mirror it the same way, keeping whatever launcher it uses (`explorer` rather than `open`) and adding the same `os.MkdirAll` first.

- [ ] **Step 4: Verify**

Run: `go test ./internal/panelui/ -v && go build ./... && GOOS=windows GOARCH=amd64 go build ./...`
Expected: PASS and both builds exit 0.

- [ ] **Step 5: Commit**

```bash
git add internal/panelui/render.go internal/panelui/render_test.go cmd/mcs-menubar/main.go cmd/mcs-tray/panel_windows.go
git commit -m "panel: settings shortcut to the archive folder"
```

---

### Task 18: Docs, and the full verification pass

**Files:**
- Modify: `README.md`, `README.zh-TW.md`, `CHANGELOG.md`, `FILELIST.md`
- Modify: `docs/superpowers/specs/2026-07-24-account-rescan-design.md` — annotate limitation 6.1

**Interfaces:** none.

- [ ] **Step 1: Rewrite the README FAQ entry**

`README.md:144` currently answers "Why do I only see one account in the list?" by pointing at Rescan. It now needs to cover the case this release fixes. Replace that entry with two:

```markdown
**Why do I only see one account in the list?**
Open the panel and run **Rescan**, then tick the accounts you want to manage.
Accounts you have never signed into are listed too: tick one, switch to it, and
sign in from there.

**Rescan shows "Signed out in Claude Desktop" and I can't tick it.**
That account was signed out from inside Claude Desktop, which overwrites the one
login slot that folder has. Its conversations are still on disk. Click
**Recover**, give it a name, and sign in to it once in the Claude window that
opens — the conversations come back on their own.

To avoid this, add accounts with **＋ Add another account** in the panel rather
than signing out inside Claude Desktop. Each account then gets its own profile
from the start.
```

Add the equivalent entries to `README.zh-TW.md`, matching its existing tone and structure. Read both files' FAQ sections in full first; the two READMEs are kept in step.

- [ ] **Step 2: Add the CHANGELOG entry**

Read the top of `CHANGELOG.md` and follow its existing format exactly. The entry covers: recover an account signed out inside Claude Desktop, add an account from the panel on every platform, and detect and merge two profiles holding one account, archiving rather than deleting the one you do not keep.

- [ ] **Step 3: Update FILELIST**

Add one line per new file, in the style of the surrounding entries: `core/pending.go`, `core/profilename.go`, `core/archive.go`, `core/merge.go`, `core/newprofile.go`, `platform/copydir.go`, `platform/newprofile_test.go`. The spec and this plan were already listed when they were committed.

- [ ] **Step 4: Annotate the superseded limitation**

In `docs/superpowers/specs/2026-07-24-account-rescan-design.md`, mark limitation 6.1 as superseded so it is not read as current:

```markdown
1. ~~**Logged-out accounts in a shared dir are unrecoverable.**~~ **Superseded**
   by `2026-07-30-ghost-account-recovery-design.md`: the credentials genuinely
   cannot be restored, but the conversations survive in the session bucket, so
   the account can be given its own profile and signed back in to.
```

- [ ] **Step 5: Full verification**

Run: `go build ./...`
Expected: exit 0.

Run: `GOOS=windows GOARCH=amd64 go build ./...`
Expected: exit 0.

Run: `go vet ./... && GOOS=windows GOARCH=amd64 go vet ./...`
Expected: exit 0 for both.

Run: `go test ./... -v 2>&1 | tail -30`
Expected: PASS, no failures, and read the count to confirm the new tests actually ran.

- [ ] **Step 6: Commit**

```bash
git add README.md README.zh-TW.md CHANGELOG.md FILELIST.md docs/superpowers/specs/2026-07-24-account-rescan-design.md
git commit -m "docs: how to recover an account signed out inside Claude Desktop"
```

---

## After the last task

1. **Code review.** Use `superpowers:requesting-code-review` with `BASE_SHA=348ccda` (the spec commit) and `HEAD_SHA=$(git rev-parse HEAD)`.
2. **Verification.** Use `superpowers:verification-before-completion` before claiming anything works.
3. **Manual QA that cannot be automated here:**
   - macOS: full add and recover, including the sign-in, on a machine with a second real account.
   - **Windows MSIX (Store):** the whole point of this change. Recover an orphan and confirm the conversations arrive after sign-in, then deliberately sign in as an existing account to produce a duplicate and confirm the warning and the merge. Only reproducible on a real Store install.
   - Windows standalone: the same pass on a non-Store install.
4. **Version.** `0.11.0` by convention for a feature, but the bump needs the maintainer's sign-off before release, and `core/version.go` plus the git tag move together.
