# Ghost Account Recovery Implementation Plan

> ## ⚠️ DO NOT EXECUTE AS WRITTEN — revision required
>
> Adversarial review on 2026-07-30 found defects that make this plan non-executable. It is kept because its structure and reasoning are sound; the tasks need correcting first. **Do not start Task 1 until this banner is removed.**
>
> **Hard blockers — a worker following the text cannot get past these:**
>
> 1. **Task 1 tells you to define a `writeProfile` test helper that already exists** at `core/scan_test.go:85`. Go rejects the redeclaration and the whole `core` test binary stops compiling, which also makes the "verify it fails" gate of Tasks 1, 3, 5, 8, 9 and 10 pass for the wrong reason. Reuse the existing helper. The import instruction in that step is wrong on both branches too.
> 2. **Five renderer tests can never pass.** Tasks 11, 12 and 14 assert on CSS class names (`dup-pill`, `note-bad`, `disabled`) with `strings.Contains` / `strings.Count`, but `shell()` emits every class name in its `<style>` block on every page, so those strings are always present. The paired positive assertions are vacuous for the same reason. Assert on the markup that carries the class, not the name.
> 3. **Tasks 11 and 12 violate this plan's own Global Constraint** by interpolating folder names into inline JS string arguments (`send('showMerge','%s')`), and their tests lock the violation in. `html.EscapeString` turns `'` into `&#39;`, which the HTML parser decodes back before the JS is parsed. Use `data-*` + `dataset`, as Tasks 13 and 14 correctly do.
> 4. **Task 13's test asserts `"different account"`** but the implementation in the same task renders `different</b> account`. It fails against its own code.
> 5. **The feature does not work on the Store build**, which is the platform it exists for. `filepath.Base(createdPath)` is always `"Claude"` there (the slot dir name), while `msixFindProfiles` names the slot `state.json`'s `Current` — the user's chosen name. So `pending.json` never matches, the entry is pruned as stale, and `managed.json` gains a phantom while the real profile stays invisible. Profile identity needs its own layer before any of this works on MSIX; `filepath.Base` is not it. `Create` also passes the name untrimmed while validation trims, reproducing the mismatch on Windows standalone.
> 6. **Line numbers cited by Task 5 have drifted** by roughly +25 lines, because Task 3 lengthens `core/scan.go` first. Locate by symbol, not by line.
>
> **Silent failures — tasks would report green while proving nothing:** Task 14's preselection test cannot detect the wrong card being preselected (the class attribute precedes `data-folder` in both cards, so the index comparison always holds); Task 14's busy test passes with `busy=false`; Task 16 never populates `ProfileVM.UUID` in `panelBuildProfiles`, so the duplicate warning and the merge entry point are dead on Windows; Task 16's `profilePathFor` snippet uses `plat`, which is nil in the panel process, instead of `panelPlat`; `platform/unsupported.go` is compiled by no verification step (add `GOOS=linux`).
>
> **Also:** Task 8's archive collision loop only exits on `os.IsNotExist`, so any other `Stat` error spins forever; and it retries `EXDEV` for 20 seconds before reporting a misleading "Claude may still be holding its files".
>
> **Merge (Task 9) is additionally blocked on a design question** — see the note at §5.2 of the spec. Where the keeper already holds a newer version of a record, sync leaves it alone and reports a conflict, and merge then moves the other side out of the scan path, so that version becomes reachable only by hand. Task 9 must also back up the keeper itself: `SyncSessions` does not snapshot anything, contrary to what an earlier draft of the spec asserted. Recovery does not have either problem and can ship first.
>
> The three-machine evidence, the mechanism table, the phasing and the task decomposition are all still good. Correct the above and delete this banner.

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

func TestStalePendingOnlyWhenSignedIn(t *testing.T) {
	// signedIn has a live login, so its pending entry has served its purpose.
	// waiting has no login yet. absent is not in the profile list at all, which is
	// the Store build between creating a profile and the app's first launch —
	// spec §3.3. Only the first is stale.
	dir := t.TempDir()
	signedIn := writeProfile(t, dir, "Claude_SignedIn", "uuid-a", nil)
	waiting := writeSignedOutProfile(t, dir, "Claude_Waiting")

	pending := []PendingProfile{
		{Folder: "Claude_SignedIn", ExpectUUID: "uuid-a"},
		{Folder: "Claude_Waiting", ExpectUUID: "uuid-b"},
		{Folder: "Claude_Absent", ExpectUUID: "uuid-c"},
	}
	got := StalePending(pending, []*platform.ProfileInfo{signedIn, waiting})

	if len(got) != 1 || got[0] != "Claude_SignedIn" {
		t.Fatalf("want only the signed-in folder stale, got %v", got)
	}
}
```

`writeProfile` and `writeSignedOutProfile` are **existing** helpers in the same package (`core/scan_test.go:85` and `:106`) — reuse them. Do not define your own: `core` is one test binary, so a second `func writeProfile` is a redeclaration, Go refuses to compile the package, and every "verify it fails" step in this plan then passes for the wrong reason.

`core/pending_test.go` imports exactly `"path/filepath"`, `"testing"`, and `"github.com/miou1107/multi-claude-switcher/platform"`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/ -run 'TestPending|TestStalePending|TestRemovePending' -v`
Expected: FAIL — `undefined: pendingPath`, `undefined: LoadPending`, and so on.

Read the failure. It must name the undefined symbols. If instead it reports a
redeclaration or any other compile error in a file you did not touch, the package is
broken and this gate is meaningless — fix that first.

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

// StalePending returns the folders whose pending entry no longer applies, which
// is exactly those that now have a live login: the sign-in happened, so the entry
// has served its purpose. Pure, so the rule is testable without a real profile
// tree; callers pass the result to RemovePending.
//
// A profile missing from profiles is deliberately NOT stale. On the Store build a
// just-created profile has no directory at all until the packaged app launches and
// makes one (msixParkForNewIn leaves the slot absent on purpose), so pruning on
// absence would discard the entry seconds after writing it, on the one platform
// this feature exists for. Sign-in is the only thing that means "finished". Spec
// §3.3.
func StalePending(pending []PendingProfile, profiles []*platform.ProfileInfo) []string {
	byName := map[string]*platform.ProfileInfo{}
	for _, p := range profiles {
		byName[p.Name] = p
	}
	var stale []string
	for _, e := range pending {
		p, ok := byName[e.Folder]
		if !ok {
			continue
		}
		if _, err := platform.GetProfileAccountUUID(p.Path); err == nil {
			stale = append(stale, e.Folder)
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
  - `func ValidateProfileName(name string) (clean string, err error)`
  - `func ProfileFolderName(clean string) string` — returns `"Claude_" + clean`
  - `const ProfileFolderPrefix = "Claude_"`

**It returns the cleaned name, not just an error** (spec §4.3). A validator that trims a
local copy and reports only `error` leaves the caller holding the raw string, which is
the shape of a live bug in shipped code: `msixValidateNameIn` trims for its checks and
`msixParkForNewIn` then writes the untrimmed argument into `state.json`
(`platform/windows_msix.go:151`, `:254`), so ` Work ` would become a profile identity
with spaces around it. Only the cleaned name travels onward from here.

- [ ] **Step 1: Write the failing tests**

```go
// core/profilename_test.go
package core

import "testing"

func TestValidateProfileName(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantClean string // ignored when wantErr
		wantErr   bool
	}{
		{"plain", "Personal", "Personal", false},
		{"with space", "Work Team", "Work Team", false},
		{"with dash and underscore", "work-2_b", "work-2_b", false},
		{"digits", "Acct2", "Acct2", false},
		{"trims to valid", "  Personal  ", "Personal", false},
		{"empty", "", "", true},
		{"whitespace only", "   ", "", true},
		{"forward slash", "a/b", "", true},
		{"backslash", `a\b`, "", true},
		{"dot dot", "..", "", true},
		{"leading dot", ".hidden", "", true},
		{"colon", "a:b", "", true},
		{"asterisk", "a*b", "", true},
		{"question mark", "a?b", "", true},
		{"quote", `a"b`, "", true},
		{"angle brackets", "a<b>c", "", true},
		{"pipe", "a|b", "", true},
		{"newline", "a\nb", "", true},
		{"reserved bare Claude", "Claude", "", true},
		{"reserved, untrimmed", "  claude ", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clean, err := ValidateProfileName(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ValidateProfileName(%q) = %q, nil; want an error", c.in, clean)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateProfileName(%q) = %v, want nil", c.in, err)
			}
			// The cleaned name is what becomes the identity and every registry
			// key, so it has to come back from here rather than be re-derived.
			if clean != c.wantClean {
				t.Fatalf("ValidateProfileName(%q) cleaned to %q, want %q", c.in, clean, c.wantClean)
			}
		})
	}
}

func TestProfileFolderName(t *testing.T) {
	clean, err := ValidateProfileName("  Work Team  ")
	if err != nil {
		t.Fatal(err)
	}
	if got := ProfileFolderName(clean); got != "Claude_Work Team" {
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

// ValidateProfileName reports whether name is usable for a new profile, and
// returns the cleaned form to use from then on. It runs before anything is
// created, so a rejected name never leaves a partial profile behind.
//
// The cleaned name is returned rather than just validated because it becomes the
// profile's display name and, on the standalone builds, part of its identity
// (spec §3.5). Callers must pass this value on, never the raw input: trimming in
// one place and creating from another is how ` Work ` ends up as an identity with
// spaces around it.
//
// Platform-specific limits — reserved names, collisions with an existing profile —
// are checked by the platform layer, which knows what a collision is on its own
// filesystem layout (platform/windows_msix.go's msixValidateNameIn).
func ValidateProfileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("enter a name for this account")
	}
	if strings.EqualFold(name, reservedProfileName) {
		return "", fmt.Errorf("%q is taken by the default profile, pick another name", reservedProfileName)
	}
	if strings.HasPrefix(name, ".") {
		return "", errors.New("a name can't start with a dot")
	}
	if strings.Contains(name, "..") {
		return "", errors.New("a name can't contain ..")
	}
	// Allow letters, digits, space, dash, underscore. Everything else is either a
	// path separator, a Windows-illegal filename character, or a control
	// character, and the point of an allowlist is that none of them need naming.
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == ' ', r == '-', r == '_':
		default:
			return "", errors.New("use only letters, numbers, spaces, dashes and underscores")
		}
	}
	return name, nil
}

// ProfileFolderName maps a cleaned name to the folder that holds it on the
// standalone builds. Call only with a value ValidateProfileName returned; it does
// no trimming of its own, on purpose, so a raw name cannot slip through.
func ProfileFolderName(clean string) string {
	return ProfileFolderPrefix + clean
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/ -run 'TestValidateProfileName|TestProfileFolderName' -v`
Expected: PASS, 19 subtests plus 1.

- [ ] **Step 5: Commit**

```bash
git add core/profilename.go core/profilename_test.go
git commit -m "core: validate profile names before anything is created"
```

---

### Task 3: Scanner — recoverable ghosts

A ghost with conversations in it can be brought back. One with an empty bucket cannot, and must keep reading as a dead end.

**Files:**
- Modify: `core/scan.go` — `ScannedAccount`, `dirScan`, `assembleAccounts`, `gatherDir`, `rowRank`
- Test: `core/scan_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `ScannedAccount.Recoverable bool`, `ScannedAccount.Sources []GhostSource`, `type GhostSource`, `const RecoverableGhostNote`, `dirScan.Path`.

**Locate every edit by symbol name, not by line number.** This task lengthens
`core/scan.go`, so any line number quoted for a later task is already wrong by the time
that task runs.

A ghost's sources are a **list**, not one folder (spec §3.1). `assembleAccounts` already
sums one orphan UUID across every dir holding a bucket for it, and that really happens:
sign in as an account in profile A, switch to B, sign in there too, sign out of both. A
single `SourceFolder` would recover one share and silently leave the rest behind, under a
row whose count promised both. Each source carries its `Path` too, because a folder name
cannot be turned back into a path outside `platform` (spec §3.5) — which means `dirScan`
has to start carrying the path it was handed.

- [ ] **Step 1: Write the failing tests**

```go
// append to core/scan_test.go
func TestAssembleGhostRecoverable(t *testing.T) {
	// Machine 2's layout from the spec: one dir, live login cccccccc, plus an orphan
	// bbbbbbbb left behind by an in-app account switch.
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
Expected: FAIL — `unknown field Path in struct literal of type dirScan`, `ghost.Recoverable undefined`, `undefined: RecoverableGhostNote`.

- [ ] **Step 3: Write the implementation**

In `core/scan.go`, add to `ScannedAccount`, after the `SignedOut` field:

```go
	// Recoverable marks a ghost whose conversations can be brought back: its
	// bucket is non-empty, so giving the account its own profile and signing in
	// once reunites account and history. The credentials are gone for good; the
	// conversations never were. False for a ghost with an empty bucket, which
	// really is a dead end.
	Recoverable bool

	// Sources lists every profile holding part of this ghost's conversations, in
	// folder order. Recovery copies from all of them: an orphan's conversations
	// really can be split across two profiles, and taking only the largest share
	// would quietly deliver less than this row's count promises. Ghost rows only,
	// and empty for a dead ghost.
	Sources []GhostSource
```

And, next to `ScannedAccount`:

```go
// GhostSource is one profile holding part of an orphaned account's conversations.
// It carries the path as well as the folder because a folder name cannot be turned
// back into a path outside the platform package — on the Store build the active
// profile's directory is named "Claude" whatever the profile is called. The path is
// already in hand: ScanAccounts is given []*platform.ProfileInfo.
type GhostSource struct {
	Folder string
	Path   string
	Convos int
}
```

Add `Path` to `dirScan`, and set it in `gatherDir`:

```go
type dirScan struct {
	Folder    string
	Path      string // the dir this was read from; recovery copies out of it
	LiveUUID  string
	// … existing fields unchanged …
}
```

In `gatherDir`, the first line becomes:

```go
	ds := dirScan{Folder: p.Name, Path: p.Path, Buckets: map[string]bucketStat{}}
```

Add the note constant next to `SignedOutNote`:

```go
// RecoverableGhostNote is the review note for an account that was signed out
// inside Claude Desktop. It is deliberately not phrased as a defect: the data is
// intact, and what is missing is a profile that claims the account.
const RecoverableGhostNote = "Its conversations are still here. Recover to sign back in."
```

In `assembleAccounts`, replace the ghost accumulation loop — the one that builds the
`ghost` map — with one that records every source:

```go
	ghost := map[string]*ScannedAccount{}
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
			if b.Count > 0 {
				// Only non-empty buckets are worth copying from. An empty one is
				// not a source, and listing it would make a dead ghost look
				// recoverable.
				g.Sources = append(g.Sources, GhostSource{Folder: s.Folder, Path: s.Path, Convos: b.Count})
			}
		}
	}
	for _, g := range ghost {
		// scans arrives in filesystem order, so sort for a stable UI and stable
		// tests.
		sort.Slice(g.Sources, func(i, j int) bool { return g.Sources[i].Folder < g.Sources[j].Folder })
		g.Recoverable = len(g.Sources) > 0
		if g.Recoverable {
			g.Note = RecoverableGhostNote
		} else {
			g.Note = deriveNote(false, AccountUnknown)
		}
	}
```

Two things to carry out of that snippet. The `Note` field is no longer set when the
`&ScannedAccount{...}` literal is built, so remove `Note: deriveNote(false, AccountUnknown)`
from it. And `Recoverable` is derived from `len(g.Sources)`, not from `g.Convos > 0` — they
agree today, but sources is what recovery actually needs, so a ghost with no source must
never be offered as recoverable regardless of what its count says.

`sort` is already imported by `core/scan.go`.

Replace `rowRank` with four bands:

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

Then run the whole package: `go test ./core/`. Expected: PASS. Anything else that asserted
on a ghost's note is now wrong for the same reason and must be updated rather than worked
around.

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
- Modify: `core/scan.go` — `dirScan`, `ScanAccounts`, `assembleAccounts`, the sort comparator, `rowRank`
- Modify: `cmd/mcs-menubar/main.go`, `cmd/mcs-picker/main.go`, `cmd/mcs-tray/panel_windows.go` — the `core.ScanAccounts(` call in each
- Test: `core/scan_test.go`

**Locate every edit by symbol, not by line number.** Task 3 lengthened `core/scan.go`
before this task runs, so every line number that was accurate when this plan was written
has drifted by roughly +25. Find the call sites with
`grep -rn "ScanAccounts(" --include="*.go" .`

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

func TestScanKeepsPendingProfileWithNoDirectoryYet(t *testing.T) {
	// The Store build between creating a profile and the packaged app's first
	// launch: msixParkForNewIn renamed the slot away on purpose, so the profile
	// state.json names has no directory at all. msixFindProfiles reports it with
	// Exists false (Task 7). It must still produce a pending row — this is the one
	// platform the whole feature exists for. Spec §3.3.
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
```

`core/scan_test.go` already imports `"os"` and `"path/filepath"` — no import changes.

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

Add to `dirScan`:

```go
	Pending     bool   // named in the pending-sign-in registry
	PendingUUID string // account this profile was created to receive ("" = any)
```

In `assembleAccounts`, emit pending rows. Insert after the `SignedOut` loop — and make the `SignedOut` loop skip pending dirs so one dir cannot produce two rows:

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

Change the existing `SignedOut` loop's guard from:

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

Update the sort comparator so pending rows sort by folder like the other folder-bearing rows:

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

Now update the three call sites — find them with `grep -rn "ScanAccounts(" --include="*.go" .`:

- `cmd/mcs-menubar/main.go` → `core.ScanAccounts(mustFindProfiles(), core.LoadPending())`
- `cmd/mcs-picker/main.go` → `core.ScanAccounts(profiles, core.LoadPending())`
- `cmd/mcs-tray/panel_windows.go` → `core.ScanAccounts(panelMustFindProfiles(), core.LoadPending())`

The existing test call sites in `core/scan_test.go` take a `nil` second argument. The same
grep finds them; do not go by line number.

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
- Modify: `platform/platform.go` — the `Platform` interface, and `ProfileInfo.Exists`'s doc
- Modify: `platform/darwin.go`, `platform/windows.go`, `platform/windows_msix.go`, `platform/unsupported.go`
- Create: `platform/newprofile_test.go`
- Modify: `core/switch_test.go` — `mockPlatform` must implement the three new methods

**Interfaces:**
- Consumes: `CopyDirMerge` (Task 6), `core.ProfileFolderPrefix` is **not** usable here (no core import); the prefix is duplicated as a package constant with a comment pointing at core.
- Produces, on `Platform`:
  - `CreateProfile(clean string) (identity string, dataDir string, err error)`
  - `PrepareRecovery(newProfilePath string, sources []RecoverySource) error`
  - `PrepareArchive(keepPath, archivePath string) (keep string, archive string, err error)`
  - `ArchiveDir() string`
- Produces, in `platform`: `type RecoverySource struct { Path string; UUID string }`

**`PrepareArchive` exists because of a hazard specific to the Store build.** There, the
active profile lives in the shared slot and `state.json` names it. Renaming the slot away
would leave `state.json` pointing at a directory that does not exist — a profile
permanently waiting to be signed in to, which the next switch would then try to park. So
only a *parked* profile may be archived, and when the user chooses to keep the parked one
instead of the active one, the two have to be swapped first. `msixSwapToIn` does exactly
that and is already shipped and tested.

The swap moves both directories, so **both paths change** and the method returns them. On
macOS and Windows standalone it is a no-op that hands back what it was given.

**`CreateProfile` returns the identity, and callers must not compute it.** This is the
defect that made the first draft of this plan inert on the Store build (spec §3.5). There,
the active profile's directory is always named `Claude` — it is the shared slot — while
`msixFindProfiles` names the profile from `state.json`'s `current`. So
`filepath.Base(dataDir)` yields `Claude` for every Store profile, and keying
`pending.json` on it writes an entry for a profile `FindProfiles` never reports: the entry
never matches, `managed.json` gains a phantom, and the real profile stays invisible.
Identity and directory are two different things and both have to come back.

**Two supporting fixes go in the same task**, because the feature does not work without
them:

- `msixFindProfiles` must emit the `state.json` profile with `Exists: false` when the slot
  directory is absent. Between `msixParkForNewIn` and the packaged app's next launch there
  *is* no slot — that is the whole point — so today the account list silently drops the
  current profile during that window, and a pending row could not be rendered at all.
- `msixParkForNewIn` must trim `newName` before writing it to `state.json`.
  `msixValidateNameIn` trims a local copy for its checks and the untrimmed argument is
  what gets stored, so ` Work ` becomes an identity with spaces around it. `core` now
  passes a cleaned name so this cannot be triggered from the new flow, but the bug is one
  line from here and should be closed rather than avoided.

**`PrepareRecovery` takes a list of sources**, because a ghost's conversations can live in
more than one profile (spec §3.1, Task 3). Each source carries the path it was scanned
from; nothing here reconstructs a path from a folder name.

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

	if err := prepareRecoveryByCopy(dst, []RecoverySource{{Path: src, UUID: "orphan-uuid"}}); err != nil {
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

func TestPrepareRecoveryMergesEverySource(t *testing.T) {
	// An orphan split across two profiles. Recovering one share and dropping the
	// other would deliver less than the row's count promised.
	root := t.TempDir()
	dst := filepath.Join(root, "Claude_Recovered")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []RecoverySource
	for i, name := range []string{"Claude", "Claude_Two"} {
		src := filepath.Join(root, name)
		bucket := filepath.Join(src, "claude-code-sessions", "orphan-uuid")
		if err := os.MkdirAll(bucket, 0o755); err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(bucket, "local_"+strconv.Itoa(i)+".json")
		if err := os.WriteFile(f, []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, RecoverySource{Path: src, UUID: "orphan-uuid"})
	}

	if err := prepareRecoveryByCopy(dst, sources); err != nil {
		t.Fatalf("prepareRecoveryByCopy: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dst, "claude-code-sessions", "orphan-uuid"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want both shares copied, got %d entries", len(entries))
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
	if err := prepareRecoveryByCopy(dst, []RecoverySource{{Path: src, UUID: "not-there"}}); err == nil {
		t.Fatal("want an error when there is nothing to recover")
	}
}

func TestPrepareRecoveryNoSourcesIsAnError(t *testing.T) {
	if err := prepareRecoveryByCopy(t.TempDir(), nil); err == nil {
		t.Fatal("a recovery with nothing to recover from must not report success")
	}
}
```

Imports: `"os"`, `"path/filepath"`, `"strconv"`, `"testing"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./platform/ -run TestPrepareRecovery -v`
Expected: FAIL — `undefined: prepareRecoveryByCopy`, `undefined: RecoverySource`.

- [ ] **Step 3: Write the shared helper and the interface**

Add to `platform/copydir.go`:

```go
// RecoverySource is one profile holding part of an orphaned account's
// conversations, as scanned. It carries the path because a profile's folder name
// cannot be turned back into a path (the Store build's active profile lives in a
// directory named "Claude" whatever the profile is called), so callers pass the
// path they were given rather than rebuilding one.
type RecoverySource struct {
	Path string
	UUID string
}

// prepareRecoveryByCopy makes an orphaned account's conversations available in a
// new profile by copying its session buckets across. Copy, not move: until the
// user has signed in to the new profile the sources are the only copies that
// matter, and a failure here must lose nothing. Once the account is live in the
// new profile the sources' now-stale buckets are folded away by the scanner as
// duplicates of an account live elsewhere, so the user never sees them twice.
//
// Every source is copied. An orphan's conversations can be split across two
// profiles, and recovering one share would silently deliver less than the row
// promised. Any single failure fails the whole call, so the caller's cleanup runs
// and no half-recovered profile is left looking complete.
//
// Used by the standalone builds. The Store build instead completes its copy after
// sign-in, from the one profile it has parked (see windows_msix.go).
func prepareRecoveryByCopy(newProfilePath string, sources []RecoverySource) error {
	if len(sources) == 0 {
		return fmt.Errorf("no saved conversations to recover")
	}
	for _, s := range sources {
		if s.UUID == "" {
			return fmt.Errorf("no account to recover")
		}
		srcBucket := filepath.Join(GetProfileSessionsDir(s.Path), s.UUID)
		fi, err := os.Stat(srcBucket)
		if err != nil || !fi.IsDir() {
			return fmt.Errorf("no saved conversations found for that account in %s", filepath.Base(s.Path))
		}
		dstBucket := filepath.Join(GetProfileSessionsDir(newProfilePath), s.UUID)
		if _, err := CopyDirMerge(srcBucket, dstBucket); err != nil {
			return fmt.Errorf("copy saved conversations from %s: %w", filepath.Base(s.Path), err)
		}
	}
	return nil
}
```

Add `"fmt"` to that file's imports.

Add to the `Platform` interface in `platform/platform.go`, with the doc comments:

```go
	// CreateProfile makes a new profile that Claude Desktop will populate on its
	// next launch. It returns the profile's IDENTITY — the name FindProfiles will
	// report for it, and the key every MCS registry uses — and the directory its
	// data will live in.
	//
	// The two are returned separately because they are not the same thing and
	// neither is derivable from the other. On the standalone builds the identity is
	// the directory name; on the Store build the identity is the name written to
	// state.json while the directory is the shared slot, always called "Claude".
	// Callers must use what is returned and must never take filepath.Base of the
	// directory.
	//
	// The directory is not guaranteed to exist yet: the Store build deliberately
	// leaves its slot absent so the packaged app creates a clean one.
	//
	// Caller must have terminated Claude first, and must pass a name
	// core.ValidateProfileName has accepted and cleaned.
	CreateProfile(clean string) (identity string, dataDir string, err error)

	// PrepareRecovery arranges for the saved conversations named by sources to end
	// up in newProfilePath once the user signs in. The standalone builds copy the
	// buckets across now; the Store build has already queued the copy as part of
	// CreateProfile and does nothing here.
	PrepareRecovery(newProfilePath string, sources []RecoverySource) error

	// PrepareArchive takes the two profiles by IDENTITY, puts them into a state
	// where the second can be archived by a plain rename, and returns where each
	// one's data sits afterwards.
	//
	// It takes identities rather than paths because resolving an identity to a path
	// is a platform concern, and because a Store-build swap moves both directories —
	// so any path the caller was holding is stale by the time this returns.
	//
	// It exists for the Store build, where the active profile occupies a shared slot
	// that state.json names. Renaming that slot away would leave state.json pointing
	// at nothing, so only a parked profile may be archived; when the caller wants to
	// keep the parked one, the two are swapped first. Elsewhere it only resolves the
	// paths.
	//
	// Caller must have terminated Claude first.
	PrepareArchive(keepIdentity, archiveIdentity string) (keepPath string, archivePath string, err error)

	// ArchiveDir returns the root that archived profiles are parked under. It is
	// chosen per platform so archiving is a same-volume rename and the result
	// sits outside FindProfiles' scan path, which is what stops an archived
	// profile reappearing on the next Rescan.
	ArchiveDir() string
```

Also correct `ProfileInfo.Exists`'s meaning, since this task gives the field its first
real use:

```go
	// Exists is false only for a profile MCS knows about that currently has no
	// directory. That is a real state on the Windows Store build: creating a
	// profile parks the live slot and leaves the slot absent so the packaged app
	// makes a clean one, so between those two moments state.json names a profile
	// with nothing on disk. It must still be listed — the user has just been told
	// to sign in to it.
	Exists bool
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
func (d *DarwinPlatform) CreateProfile(clean string) (string, string, error) {
	appSup := d.AppSupportDir()
	if appSup == "" {
		return "", "", fmt.Errorf("could not determine user home directory")
	}
	// Here identity and directory name coincide. They do not on the Store build,
	// which is why both are returned.
	identity := profileFolderPrefix + clean
	path := filepath.Join(appSup, identity)
	if _, err := os.Stat(path); err == nil {
		return "", "", fmt.Errorf("a profile folder named %q already exists", identity)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", "", fmt.Errorf("create profile folder: %w", err)
	}
	return identity, path, nil
}

func (d *DarwinPlatform) PrepareRecovery(newProfilePath string, sources []RecoverySource) error {
	return prepareRecoveryByCopy(newProfilePath, sources)
}

// PrepareArchive has nothing to prepare here: every profile is its own directory,
// so any of them can be renamed away without disturbing the others. It still
// resolves the two identities, so that resolution happens in exactly one place per
// platform.
func (d *DarwinPlatform) PrepareArchive(keepIdentity, archiveIdentity string) (string, string, error) {
	appSup := d.AppSupportDir()
	if appSup == "" {
		return "", "", fmt.Errorf("could not determine user home directory")
	}
	return filepath.Join(appSup, keepIdentity), filepath.Join(appSup, archiveIdentity), nil
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
func (w *WindowsPlatform) CreateProfile(clean string) (string, string, error) {
	if w.isMSIX() {
		roaming := msixRoamingDir()
		if roaming == "" {
			return "", "", fmt.Errorf("Store Claude Desktop data directory not found")
		}
		if err := msixParkForNewIn(roaming, clean); err != nil {
			return "", "", err
		}
		// The identity is the name state.json now holds, which is what
		// msixFindProfiles will report. It is NOT the slot's directory name: that
		// is always "Claude". The slot is deliberately absent right now — the
		// packaged app creates a clean one on next launch, which is what makes
		// this a signed-out profile.
		return clean, msixSlotDir(roaming), nil
	}
	root := w.AppSupportDir()
	if root == "" {
		return "", "", fmt.Errorf("could not determine %%APPDATA%% directory")
	}
	identity := profileFolderPrefix + clean
	path := filepath.Join(root, identity)
	if _, err := os.Stat(path); err == nil {
		return "", "", fmt.Errorf("a profile folder named %q already exists", identity)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", "", fmt.Errorf("create profile folder: %w", err)
	}
	return identity, path, nil
}

func (w *WindowsPlatform) PrepareRecovery(newProfilePath string, sources []RecoverySource) error {
	if w.isMSIX() {
		// msixParkForNewIn already set PendingMigrateFrom on the parked profile,
		// and msixAttemptMigrationIn copies the bucket matching whatever account
		// the user signs in as — which is exactly this recovery. Nothing to do,
		// and nothing may be written into a slot the app has not created yet.
		//
		// It copies from the one profile it parked, so a ghost split across
		// several profiles recovers only that profile's share here. The rest stays
		// visible as a ghost and can be recovered on a second pass (spec §9.7).
		return nil
	}
	return prepareRecoveryByCopy(newProfilePath, sources)
}

func (w *WindowsPlatform) PrepareArchive(keepIdentity, archiveIdentity string) (string, string, error) {
	if !w.isMSIX() {
		return w.standaloneProfilePath(keepIdentity), w.standaloneProfilePath(archiveIdentity), nil
	}
	roaming := msixRoamingDir()
	if roaming == "" {
		return "", "", fmt.Errorf("Store Claude Desktop data directory not found")
	}
	if strings.EqualFold(readMSIXStateIn(roaming).Current, archiveIdentity) {
		// The profile to archive is the slot occupant. Renaming the slot away would
		// leave state.json naming a directory that does not exist, so swap the
		// keeper in first: the keeper becomes the active profile — where the user
		// wants to end up anyway — and the other lands in .mcs-profiles, ready to
		// be renamed out. msixSwapToIn rolls its own parking back on failure.
		if err := msixSwapToIn(roaming, keepIdentity); err != nil {
			return "", "", err
		}
	}
	// Resolve after any swap: state.json has moved, and so have both directories.
	return msixProfilePath(roaming, keepIdentity), msixProfilePath(roaming, archiveIdentity), nil
}

func (w *WindowsPlatform) standaloneProfilePath(identity string) string {
	return filepath.Join(w.AppSupportDir(), identity)
}
```

And in `platform/windows_msix.go`, the identity-to-path resolver the Store build has been
missing. Every place that needs a Store profile's directory should go through it rather
than joining a path by hand:

```go
// msixProfilePath returns where the profile called identity keeps its data: the
// shared slot if it is the current profile, otherwise its parked directory.
//
// This is the only correct way to get a Store profile's path, and the inverse does
// not exist: filepath.Base of the slot is always "Claude", whatever the profile is
// called. See the identity rules in the ghost-account-recovery design spec.
func msixProfilePath(roaming, identity string) string {
	if strings.EqualFold(readMSIXStateIn(roaming).Current, identity) {
		return msixSlotDir(roaming)
	}
	return filepath.Join(msixContainerDir(roaming), identity)
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

`platform/windows_msix.go` — two corrections, both required for the Store build to work
at all:

```go
// in msixParkForNewIn, immediately after the msixValidateNameIn call:
	newName = strings.TrimSpace(newName)
```

Validation trims a local copy and this function stores its argument, so without this
` Work ` becomes a profile identity with spaces around it. `core` now passes a cleaned
name, so this closes the hole rather than papering over a live symptom. `strings` is
already imported.

```go
// platform/windows.go, in msixFindProfiles: replace the slot branch
	slot := msixSlotDir(roaming)
	if fi, err := os.Stat(slot); err == nil && fi.IsDir() {
		p := w.inspectProfile(st.Current, slot)
		p.Managed = true
		profiles = append(profiles, p)
	} else {
		// No slot directory. That is a real, expected state: creating a profile
		// parks the live slot and leaves the slot absent on purpose so the
		// packaged app makes a clean one. state.json still names the current
		// profile, and the user has just been told to sign in to it, so it has to
		// be listed. Reporting it with Exists false is how the scanner learns the
		// difference between "no data yet" and "not there".
		profiles = append(profiles, &ProfileInfo{
			Name: st.Current, Path: slot, Exists: false,
			UUIDBuckets: map[string]int{}, Managed: true,
		})
	}
```

`platform/unsupported.go`:

```go
func (p *unsupportedPlatform) CreateProfile(clean string) (string, string, error) {
	return "", "", notSupported()
}
func (p *unsupportedPlatform) PrepareRecovery(newProfilePath string, sources []RecoverySource) error {
	return notSupported()
}
func (p *unsupportedPlatform) PrepareArchive(keepIdentity, archiveIdentity string) (string, string, error) {
	return "", "", notSupported()
}
func (p *unsupportedPlatform) ArchiveDir() string { return "" }
```

`core/switch_test.go` — extend `mockPlatform` (find it with
`grep -n "type mockPlatform" core/switch_test.go`) with recording fields and the three
methods. Note that `createdIdentity` and `createdPath` are **separate** fields: the mock
must be able to represent the Store build's case, where they differ, or tests written
against it cannot catch the bug this task exists to fix.

```go
// add to the mockPlatform struct
	createdName     string // the cleaned name the caller passed in
	createdIdentity string // what CreateProfile hands back as the identity
	createdPath     string // and the directory, which need not share its name
	preparedSources []platform.RecoverySource
	archiveRoot     string
	// prepareArchive lets a test decide what PrepareArchive hands back, which is
	// how the Store build's swap (both paths moving) gets represented.
	prepareArchive func(keep, archive string) (string, string, error)

func (m *mockPlatform) CreateProfile(clean string) (string, string, error) {
	m.createdName = clean
	return m.createdIdentity, m.createdPath, nil
}
func (m *mockPlatform) PrepareRecovery(newProfilePath string, sources []platform.RecoverySource) error {
	m.preparedSources = sources
	return nil
}
func (m *mockPlatform) PrepareArchive(keepIdentity, archiveIdentity string) (string, string, error) {
	if m.prepareArchive != nil {
		return m.prepareArchive(keepIdentity, archiveIdentity)
	}
	return keepIdentity, archiveIdentity, nil
}
func (m *mockPlatform) ArchiveDir() string { return m.archiveRoot }
```

- [ ] **Step 4: Write the Store-build identity tests**

These go in `platform/windows_msix_test.go`, which carries `//go:build windows`. They
**cannot run on macOS** — the whole file is excluded from the build. That is already true
of the eight MSIX tests there. Write them anyway: they are what a Windows run and the QA
pass in Task 18 execute.

```go
func TestMSIXCreateProfileIdentityIsNotTheDirectoryName(t *testing.T) {
	roaming := t.TempDir()
	slot := filepath.Join(roaming, "Claude")
	if err := os.MkdirAll(slot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := msixParkForNewIn(roaming, "Work"); err != nil {
		t.Fatalf("park: %v", err)
	}
	if got := readMSIXStateIn(roaming).Current; got != "Work" {
		t.Fatalf("state.json current = %q, want %q", got, "Work")
	}
	// The identity is "Work"; the directory is "Claude". Anything deriving the
	// identity from the path gets "Claude" and silently addresses a profile that
	// does not exist.
	if filepath.Base(slot) == "Work" {
		t.Fatal("this test is meaningless if the slot is named after the profile")
	}
}

func TestMSIXParkTrimsTheName(t *testing.T) {
	roaming := t.TempDir()
	if err := os.MkdirAll(filepath.Join(roaming, "Claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := msixParkForNewIn(roaming, "  Work  "); err != nil {
		t.Fatalf("park: %v", err)
	}
	if got := readMSIXStateIn(roaming).Current; got != "Work" {
		t.Fatalf("state.json current = %q, want it trimmed", got)
	}
}

func TestMSIXFindProfilesListsTheSlotProfileWhenTheSlotIsAbsent(t *testing.T) {
	// Exactly the state msixParkForNewIn leaves behind: state.json names a
	// profile, and its directory does not exist yet.
	roaming := t.TempDir()
	if err := writeMSIXStateIn(roaming, msixState{Current: "Work"}); err != nil {
		t.Fatal(err)
	}
	w := &WindowsPlatform{}
	got, err := w.msixFindProfilesIn(roaming)
	if err != nil {
		t.Fatalf("msixFindProfilesIn: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want the current profile listed, got %+v", got)
	}
	if got[0].Name != "Work" {
		t.Fatalf("name = %q, want the state.json name", got[0].Name)
	}
	if got[0].Exists {
		t.Fatal("a profile with no directory must report Exists false")
	}
}
```

The third test needs `msixFindProfiles` split so the roaming dir can be injected, the same
way `msixParkForNewIn`/`msixSwapToIn` already are (`platform/windows_msix.go:176`, `:228`).
Extract the body into `func (w *WindowsPlatform) msixFindProfilesIn(roaming string) ([]*ProfileInfo, error)`
and have `msixFindProfiles` call it with `msixRoamingDir()`, returning the existing
"Store Claude Desktop data directory not found" error when that is empty. Without the
split there is no way to test any of this without a real Store install.

- [ ] **Step 5: Verify**

Run: `go test ./platform/ -run TestPrepareRecovery -v`
Expected: PASS, 4 tests.

Run: `go build ./... && go test ./... 2>&1 | tail -20`
Expected: builds exit 0; all existing tests still pass now that `mockPlatform` satisfies the widened interface.

Run: `GOOS=windows GOARCH=amd64 go build ./... && GOOS=windows GOARCH=amd64 go vet ./...`
Expected: exit 0 for both. `go vet` is what type-checks the windows-only test files —
`go build` skips `_test.go` entirely, so a broken MSIX test would go unnoticed until a
Windows run.

Run: `GOOS=linux GOARCH=amd64 go build ./platform/`
Expected: exit 0. `platform/unsupported.go` is `//go:build !darwin && !windows`, so it is
compiled by neither of the builds above; a missing method there is invisible until
somebody builds for a third OS.

- [ ] **Step 6: Commit**

```bash
git add platform/platform.go platform/darwin.go platform/windows.go platform/windows_msix.go platform/unsupported.go platform/copydir.go platform/newprofile_test.go platform/windows_msix_test.go core/switch_test.go
git commit -m "platform: profile creation returns an identity, not just a path"
```

---

### Task 8: Archive a profile

**Files:**
- Create: `core/archive.go`
- Test: `core/archive_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `func ArchiveProfile(profilePath, archiveRoot string) (string, error)`

Two hazards to get right, both of which a first draft of this task got wrong.

**Retry only what is worth retrying.** The rename is retried for 20 seconds because
Windows can still be releasing Claude's file handles after it exits. But a rename across
volumes will never succeed, and retrying it spends 20 seconds and then reports "Claude may
still be holding its files", which sends the user to Task Manager over a path problem. Fail
that case immediately, with a message that says what is actually wrong.

**A bounded loop, and no loop that only exits on one specific error.** The collision loop
must terminate on any `Stat` outcome it did not expect, not spin.

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

func TestArchiveProfileGivesUpAtOnceWhenRetryingCannotHelp(t *testing.T) {
	// A rename that fails for a reason no amount of waiting fixes must not spend
	// 20 seconds and then blame Claude for holding files.
	root := t.TempDir()
	profile := filepath.Join(root, "Claude_Work")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	archiveRoot := filepath.Join(root, "archive")

	orig := renameProfile
	renameProfile = func(from, to string) error {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EXDEV}
	}
	t.Cleanup(func() { renameProfile = orig })

	start := time.Now()
	_, err := ArchiveProfile(profile, archiveRoot)
	if err == nil {
		t.Fatal("want an error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("gave up after %v — an unretryable failure must not be retried", elapsed)
	}
	if strings.Contains(err.Error(), "holding its files") {
		t.Fatalf("message blames Claude for a path problem: %v", err)
	}
	if _, statErr := os.Stat(profile); statErr != nil {
		t.Fatalf("a failed archive must leave the profile in place: %v", statErr)
	}
}
```

Imports for this file: `"os"`, `"path/filepath"`, `"strings"`, `"syscall"`, `"testing"`,
`"time"`.

`renameProfile` is a package-level `var` holding `os.Rename` so this test can inject a
failure. Without a seam there is no way to exercise the retry policy at all, and the policy
is the part most likely to be wrong.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/ -run TestArchiveProfile -v`
Expected: FAIL — `undefined: ArchiveProfile`.

- [ ] **Step 3: Write the implementation**

```go
// core/archive.go
package core

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
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
	// archiveCollisionLimit bounds the search for an unused archive name. It is
	// far above any plausible number of archives of one profile in one second;
	// reaching it means something is wrong with the archive root, and giving up
	// with an error beats looping.
	archiveCollisionLimit = 100
)

// renameProfile is os.Rename behind a var so tests can inject a failure and
// exercise the retry policy, which is the part of this file most likely to be
// wrong and the part a real filesystem will not reproduce on demand.
var renameProfile = os.Rename

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
	dest, err := freeArchiveName(archiveRoot, base)
	if err != nil {
		return "", err
	}
	if err := renameProfileWithRetry(profilePath, dest); err != nil {
		if !archiveRetryWorthwhile(profilePath, dest, err) {
			return "", fmt.Errorf("couldn't archive %s — the archive folder %s is not on the same drive as your Claude data, and archiving moves the folder rather than copying it. (%w)",
				DisplayName(filepath.Base(profilePath)), archiveRoot, err)
		}
		return "", fmt.Errorf("couldn't archive %s — Claude may still be holding its files. Fully quit Claude and try again. (%w)",
			DisplayName(filepath.Base(profilePath)), err)
	}
	return dest, nil
}

// freeArchiveName finds an unused name under archiveRoot. Two archives of one
// profile within the same second would otherwise collide, and a collision must
// never overwrite an existing archive.
func freeArchiveName(archiveRoot, base string) (string, error) {
	for i := 1; i <= archiveCollisionLimit; i++ {
		dest := filepath.Join(archiveRoot, base)
		if i > 1 {
			dest = filepath.Join(archiveRoot, fmt.Sprintf("%s-%d", base, i))
		}
		_, err := os.Lstat(dest)
		if errors.Is(err, os.ErrNotExist) {
			return dest, nil
		}
		if err != nil {
			// Something other than "not there" — an unreadable archive root, say.
			// Reporting it beats looping on a condition that will not change.
			return "", fmt.Errorf("check archive destination %s: %w", dest, err)
		}
	}
	return "", fmt.Errorf("too many archives named %q already — clear out %s", base, archiveRoot)
}

// archiveRetryWorthwhile reports whether err is the kind of failure that waiting
// fixes. A locked directory is: Windows releases Claude's handles a moment after
// the processes exit. A cross-volume rename is not, and retrying it for 20 seconds
// before blaming Claude sends the user to Task Manager over a path problem.
//
// EXDEV covers the Unix case. Windows reports ERROR_NOT_SAME_DEVICE instead, which
// is not EXDEV, so the volume names are compared as well — on Unix VolumeName is
// always "", making that check a no-op there.
func archiveRetryWorthwhile(from, to string, err error) bool {
	if errors.Is(err, syscall.EXDEV) {
		return false
	}
	return filepath.VolumeName(from) == filepath.VolumeName(to)
}

func renameProfileWithRetry(from, to string) error {
	var err error
	for i := 0; i < archiveRenameAttempts; i++ {
		if err = renameProfile(from, to); err == nil {
			if i > 0 {
				log.Printf("archive rename %q -> %q succeeded after %d retries", filepath.Base(from), filepath.Base(to), i)
			}
			return nil
		}
		if !archiveRetryWorthwhile(from, to, err) {
			log.Printf("archive rename %q -> %q failed unretryably: %v", filepath.Base(from), filepath.Base(to), err)
			return err
		}
		time.Sleep(archiveRenameDelay)
	}
	log.Printf("archive rename %q -> %q FAILED after retries: %v", filepath.Base(from), filepath.Base(to), err)
	return err
}
```

`TestArchiveProfileMissingSourceIsAnError` returns before any retry, so it is fast. The collision test archives twice within the same second, so it exercises the counter without waiting. The unretryable test injects its failure, so it does not wait either — no test in this file sleeps.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/ -run TestArchiveProfile -v`
Expected: PASS, 4 tests, in well under a second. If the run takes 20 seconds, the retry
policy is still being applied to a failure it cannot fix.

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
- Consumes: `ArchiveProfile` (Task 8), `SyncSessions` + `SyncReport` (`core/sync.go`), `SetManaged`/`LoadManaged` (`core/managed.go`), `SetProfileName` (`core/names.go`), `NewBackupManager` (`core/backup.go`), `Platform.PrepareArchive`/`ArchiveDir` (Task 7).
- Produces:
  - `type MergeRequest struct { KeepIdentity, ArchiveIdentity, BackupRoot string }`
  - `type MergePlan struct { Combined, Conflicts int; ArchiveTo string }`
  - `func MergePreview(keepPath, archivePath, uuid string) (*MergePlan, error)`
  - `func MergeDuplicates(plat platform.Platform, req MergeRequest) (*SyncReport, error)`

`SyncSessions` re-buckets from the source account's UUID to the target's. Both ends of a merge are the same account, so the rename is a no-op and the copy lands exactly where Claude will read it.

Four things this task must get right, three of which an earlier draft did not.

1. **Take identities, not paths.** `PrepareArchive` can move both directories on the Store
   build, so any path the caller held is stale afterwards. Identities survive; paths do
   not (spec §3.5). The old draft also derived the managed-list key with
   `filepath.Base(archivePath)`, which is `Claude` for a Store build's active profile.
2. **Back up the keeper, explicitly.** `SyncSessions` snapshots nothing — the backup has
   always been the caller's job (`core/switch.go`, `core/align.go`, and the CLI each do
   their own). The earlier draft asserted the opposite, then called `bm.BackupIfHasData`
   where no `bm` existed.
3. **Compute the outcome before promising it.** The merge screen shows a combined total,
   and that total is the size of the **union** of the two buckets, not the sum. It also has
   to disclose conflicts: where the keeper already holds a newer version of a record,
   `SyncSessions` leaves it alone, and archiving then moves the other version out of the UI's
   reach. That is not rare — two profiles on one account diverge in both directions.
   `MergePreview` produces both numbers from one walk (spec §5.2).
4. **Clean up `names.json` too.** An archived profile's display name is dead weight, and if
   the user later creates a profile with the same identity it would inherit a stale name.

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

// mergePlatform resolves the two identities back to the fixture's paths, standing
// in for the platform without pretending to be a whole OS.
type mergePlatform struct {
	*mockPlatform
	root, archiveRoot string
}

func (m *mergePlatform) PrepareArchive(keepIdentity, archiveIdentity string) (string, string, error) {
	return filepath.Join(m.root, keepIdentity), filepath.Join(m.root, archiveIdentity), nil
}
func (m *mergePlatform) ArchiveDir() string { return m.archiveRoot }

func TestMergePreviewCountsTheUnionAndTheConflicts(t *testing.T) {
	keep, archive, _ := mergeFixture(t, "same-uuid", "same-uuid")
	// A third record both hold, with the keeper's copy newer and different. Sync
	// leaves the keeper's alone and reports a conflict, so the other copy ends up
	// only in the archive — the user has to be told before committing.
	shared := "both.json"
	kp := filepath.Join(keep, "claude-code-sessions", "same-uuid", shared)
	ap := filepath.Join(archive, "claude-code-sessions", "same-uuid", shared)
	if err := os.WriteFile(ap, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kp, []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(ap, old, old); err != nil {
		t.Fatal(err)
	}

	plan, err := MergePreview(keep, archive, "same-uuid")
	if err != nil {
		t.Fatalf("MergePreview: %v", err)
	}
	// only_in_keep, only_in_archive, both → 3, not the sum of 2 + 2.
	if plan.Combined != 3 {
		t.Fatalf("Combined = %d, want the union size 3", plan.Combined)
	}
	if plan.Conflicts != 1 {
		t.Fatalf("Conflicts = %d, want 1", plan.Conflicts)
	}
}

func TestMergePreviewIdenticalCopiesAreNotConflicts(t *testing.T) {
	keep, archive, _ := mergeFixture(t, "same-uuid", "same-uuid")
	for _, dir := range []string{keep, archive} {
		p := filepath.Join(dir, "claude-code-sessions", "same-uuid", "both.json")
		if err := os.WriteFile(p, []byte(`{"v":1}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := MergePreview(keep, archive, "same-uuid")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Conflicts != 0 {
		t.Fatalf("two identical copies are not a conflict, got %d", plan.Conflicts)
	}
	if plan.Combined != 3 {
		t.Fatalf("Combined = %d, want 3", plan.Combined)
	}
}

func TestMergeDuplicatesUnionsThenArchives(t *testing.T) {
	withStubbedManaged(t)
	withStubbedNames(t)
	if err := SetManaged([]string{"Claude_Keep", "Claude_Archive"}); err != nil {
		t.Fatal(err)
	}
	if err := SetProfileName("Claude_Archive", "Old Work"); err != nil {
		t.Fatal(err)
	}
	keep, archive, archiveRoot := mergeFixture(t, "same-uuid", "same-uuid")
	plat := &mergePlatform{mockPlatform: &mockPlatform{}, root: filepath.Dir(keep), archiveRoot: archiveRoot}

	backupRoot := filepath.Join(t.TempDir(), "backups")

	report, err := MergeDuplicates(plat, MergeRequest{
		KeepIdentity: "Claude_Keep", ArchiveIdentity: "Claude_Archive",
		BackupRoot: backupRoot,
	})
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
	// The archived profile's display name goes with it, or a later profile reusing
	// the identity would inherit a name the user never chose for it.
	if n := LoadProfileNames()["Claude_Archive"]; n != "" {
		t.Fatalf("display name for the archived profile survived: %q", n)
	}
	// A backup of the keeper was taken before anything was copied into it.
	backups, err := NewBackupManager(backupRoot).ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) == 0 {
		t.Fatal("the keeper must be snapshotted before it is written to")
	}
}

func TestMergeDuplicatesRefusesDifferentAccounts(t *testing.T) {
	withStubbedManaged(t)
	withStubbedNames(t)
	keep, archive, archiveRoot := mergeFixture(t, "uuid-a", "uuid-b")
	plat := &mergePlatform{mockPlatform: &mockPlatform{}, root: filepath.Dir(keep), archiveRoot: archiveRoot}

	if _, err := MergeDuplicates(plat, MergeRequest{
		KeepIdentity: "Claude_Keep", ArchiveIdentity: "Claude_Archive",
		BackupRoot: filepath.Join(t.TempDir(), "backups"),
	}); err == nil {
		t.Fatal("merging two different accounts must be refused")
	}
	// Nothing may have moved.
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("the other profile must be left alone: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("the keeper must be left alone: %v", err)
	}
}

func TestMergeDuplicatesLeavesManagedAloneWhenArchiveFails(t *testing.T) {
	withStubbedManaged(t)
	withStubbedNames(t)
	if err := SetManaged([]string{"Claude_Keep", "Claude_Archive"}); err != nil {
		t.Fatal(err)
	}
	keep, archive, _ := mergeFixture(t, "same-uuid", "same-uuid")
	// An archive root that cannot be created: a path under a regular file.
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	plat := &mergePlatform{
		mockPlatform: &mockPlatform{}, root: filepath.Dir(keep),
		archiveRoot: filepath.Join(blocker, "archive"),
	}

	if _, err := MergeDuplicates(plat, MergeRequest{
		KeepIdentity: "Claude_Keep", ArchiveIdentity: "Claude_Archive",
		BackupRoot: filepath.Join(t.TempDir(), "backups"),
	}); err == nil {
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

Imports for this file: `"os"`, `"path/filepath"`, `"testing"`, `"time"`.

Three test seams are needed, none of which exists yet.

`withStubbedManaged` — `core/managed_test.go` stubs `managedPath` inline in each test
(`grep -n "managedPath = " core/managed_test.go`). Extract it so both files can use it:

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

Read `core/managed_test.go` first and rework its existing tests to call the helper, keeping
their assertions unchanged.

`withStubbedNames` — `namesPath` in `core/names.go` is a plain `func`, not a `var`, so it
cannot be redirected. Change it to a `var` matching `managedPath`'s shape. `core/names.go`
has no test file today, so put the helper in `core/merge_test.go`, the only user:

```go
// core/merge_test.go
func withStubbedNames(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	orig := namesPath
	namesPath = func() string { return filepath.Join(dir, "names.json") }
	t.Cleanup(func() { namesPath = orig })
}
```

`MergeRequest.BackupRoot` — merge backs up the keeper, and a test must not write into the
real `~/.multi-claude-switcher/backups`. There is no `withStubbedBackupRoot` helper because
`core/backup_test.go` already solved this by passing an explicit root to
`NewBackupManager`, rather than redirecting `$HOME` — which would not work on Windows
anyway, where `os.UserHomeDir` reads `USERPROFILE`. `BackupRoot` is empty in production,
which `NewBackupManager` already reads as "use the default root", and the tests above set it
to a temp dir.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/ -run 'TestMerge' -v`
Expected: FAIL — `undefined: MergePreview`, `undefined: MergeDuplicates`.

- [ ] **Step 3: Write the implementation**

```go
// core/merge.go
package core

import (
	"fmt"
	"path/filepath"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// MergeRequest names the two profiles to merge, by identity. Paths are deliberately
// absent: PrepareArchive can move both directories, so a path chosen by the caller
// may be stale by the time the merge acts on it.
type MergeRequest struct {
	KeepIdentity    string
	ArchiveIdentity string
	// BackupRoot overrides where the keeper's pre-merge snapshot goes. Empty means
	// the default root, which is what production uses.
	BackupRoot string
}

// MergePlan is what a merge will actually do, computed before the user commits.
type MergePlan struct {
	// Combined is how many conversations the keeper will hold afterwards: the size
	// of the UNION of the two buckets, not the sum of their counts. A record both
	// profiles hold is one conversation, not two.
	Combined int
	// Conflicts counts records both profiles hold with different content where the
	// keeper's copy is the newer one. SyncSessions leaves those alone, and the merge
	// then moves the other copy out of the UI's reach, so the user has to be told
	// before committing.
	Conflicts int
	// ArchiveTo is where the profile being given up will be parked.
	ArchiveTo string
}

// MergePreview computes what a merge would do without doing any of it. The merge
// screen renders this, so the number it promises is the number the user gets.
func MergePreview(keepPath, archivePath, uuid string) (*MergePlan, error) {
	keepBucket := filepath.Join(platform.GetProfileSessionsDir(keepPath), uuid)
	archiveBucket := filepath.Join(platform.GetProfileSessionsDir(archivePath), uuid)

	keepFiles, err := sessionFilesByRelPath(keepBucket)
	if err != nil {
		return nil, err
	}
	archiveFiles, err := sessionFilesByRelPath(archiveBucket)
	if err != nil {
		return nil, err
	}

	plan := &MergePlan{Combined: len(keepFiles)}
	for rel, archivePathAbs := range archiveFiles {
		keepPathAbs, both := keepFiles[rel]
		if !both {
			plan.Combined++ // only the other side has it, so it will be copied in
			continue
		}
		same, err := filesEqual(archivePathAbs, keepPathAbs)
		if err != nil {
			return nil, fmt.Errorf("compare %s: %w", rel, err)
		}
		if same {
			continue
		}
		// Different content. SyncSessions keeps whichever is newer, so this is only
		// a conflict — a version the merge will strand in the archive — when the
		// keeper's copy is the one that wins.
		ai, err1 := os.Stat(archivePathAbs)
		ki, err2 := os.Stat(keepPathAbs)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("compare timestamps for %s", rel)
		}
		if !ai.ModTime().After(ki.ModTime()) {
			plan.Conflicts++
		}
	}
	return plan, nil
}

// sessionFilesByRelPath maps each .json session file under bucket to its full path,
// keyed by path relative to bucket. An absent bucket is empty, not an error: a
// profile that has never used Code has no bucket, and that is a valid side of a
// merge.
func sessionFilesByRelPath(bucket string) (map[string]string, error) {
	out := map[string]string{}
	if _, err := os.Stat(bucket); errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	err := filepath.Walk(bucket, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}
		rel, err := filepath.Rel(bucket, path)
		if err != nil {
			return err
		}
		out[rel] = path
		return nil
	})
	return out, err
}

// MergeDuplicates resolves two profiles signed in to the same account: the
// conversations from the profile being given up are copied into the keeper, then
// that profile is moved out of the scan path and dropped from MCS's registries.
//
// Only one direction is copied. The profile being archived is never written to,
// which makes the archive an untouched record of what was there — a better safety
// property than a two-way merge, and faster.
//
// Caller must have terminated Claude first. Order is verify, snapshot, copy, move,
// then update state, so a failure anywhere leaves MCS's view of the world no
// further ahead than the disk: both profiles stay listed and the duplicate warning
// stays up, which is exactly the state the user can retry from.
func MergeDuplicates(plat platform.Platform, req MergeRequest) (*SyncReport, error) {
	keepName := DisplayName(req.KeepIdentity)
	archiveName := DisplayName(req.ArchiveIdentity)

	// Resolve identities to paths, and on the Store build swap the keeper into the
	// slot if it is the other profile that currently holds it. Both paths can move
	// here, which is why nothing upstream is allowed to pass paths in.
	keepPath, archivePath, err := plat.PrepareArchive(req.KeepIdentity, req.ArchiveIdentity)
	if err != nil {
		return nil, err
	}

	keepUUID, err := platform.GetProfileAccountUUID(keepPath)
	if err != nil {
		return nil, fmt.Errorf("%s has no account signed in, so there is nothing to merge into", keepName)
	}
	archiveUUID, err := platform.GetProfileAccountUUID(archivePath)
	if err != nil {
		return nil, fmt.Errorf("%s has no account signed in, so there is nothing to merge", archiveName)
	}
	if keepUUID != archiveUUID {
		// A stale panel could ask to merge rows that have changed underneath it.
		// Merging two genuinely different accounts would mix their histories.
		return nil, fmt.Errorf("%s and %s are different accounts, so they can't be merged", keepName, archiveName)
	}

	// SyncSessions does NOT snapshot anything — the backup has always been the
	// caller's job (core/switch.go, core/align.go, and the CLI each take their
	// own). Take it here, and abort rather than copy unprotected.
	if _, err := NewBackupManager(req.BackupRoot).BackupIfHasData(keepPath); err != nil {
		return nil, fmt.Errorf("aborting merge: could not back up %s first: %w", keepName, err)
	}
	report, err := SyncSessions(archivePath, keepPath)
	if err != nil {
		return nil, fmt.Errorf("combine conversations: %w", err)
	}

	if _, err := ArchiveProfile(archivePath, plat.ArchiveDir()); err != nil {
		return report, err
	}

	// Registries last, and only now that the folder really is gone from the scan
	// path. Unmanaging a folder still in place would hide it while leaving it to
	// reappear on the next Rescan.
	var kept []string
	for _, m := range LoadManaged() {
		if m != req.ArchiveIdentity {
			kept = append(kept, m)
		}
	}
	if err := SetManaged(kept); err != nil {
		return report, fmt.Errorf("update the managed list: %w", err)
	}
	// The display name goes with the profile. Left behind, it would be inherited by
	// any later profile that happened to reuse the identity.
	if err := SetProfileName(req.ArchiveIdentity, ""); err != nil {
		log.Printf("merge: could not clear the display name for %q: %v", req.ArchiveIdentity, err)
	}
	return report, nil
}
```

Imports for `core/merge.go`: `"errors"`, `"fmt"`, `"log"`, `"os"`, `"path/filepath"`,
`"strings"`, and the `platform` package.

`filesEqual` is already in `core/sync.go` and unexported, so `MergePreview` reuses it
rather than defining a second notion of "the same record". That matters: a preview that
compared files differently from the sync would promise a number the sync does not deliver.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/ -run 'TestMerge|TestManaged' -v`
Expected: PASS, 5 merge tests plus the reworked managed tests.

Then confirm the preview and the sync agree, which is the property the merge screen's
promise rests on: in `TestMergePreviewCountsTheUnionAndTheConflicts`, run the merge after
the preview and check that `report.ConflictCount == plan.Conflicts` and that the keeper ends
up holding `plan.Combined` files. If they disagree, the preview is computing something the
sync does not do, and the number shown to the user is fiction.

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
- Consumes: `ValidateProfileName` (Task 2), `AddPending` (Task 1), `LoadManaged`/`SetManaged`, `SetProfileName`, `GhostSource` (Task 3), and the new `platform` methods (Task 7).
- Produces:
  - `type CreateProfileRequest struct { Name string; RecoverUUID string; Sources []GhostSource }`
  - `type CreatedProfile struct { Identity string; DataDir string }`
  - `type ProfileCreator struct { Plat platform.Platform }`
  - `func NewProfileCreator(p platform.Platform) *ProfileCreator`
  - `func (c *ProfileCreator) Create(req CreateProfileRequest) (*CreatedProfile, error)`

**This task is where the identity bug lived, and it had three faces.** All three must stay
fixed (spec §3.5):

1. It derived the registry key with `filepath.Base(createdPath)`. On the Store build that is
   `Claude` for every profile, so `pending.json` and `managed.json` would name a profile
   `FindProfiles` never reports — a phantom in the account list, an invisible real profile,
   and a pending entry that never matches. `CreateProfile` now returns the identity; use it.
2. It rebuilt the recovery source's path with
   `filepath.Join(c.Plat.AppSupportDir(), req.SourceFolder)`. On the Store build
   `AppSupportDir()` is `%APPDATA%` while the data is under
   `%LOCALAPPDATA%\Packages\…\LocalCache\Roaming`, and a parked profile is a level deeper
   again. The sources now arrive with their paths already in them, from the scan.
3. It passed `req.Name` to the platform untrimmed while validation trimmed, so ` Work `
   would have been validated as `Work` and created as ` Work `. Only the cleaned name
   travels on.

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
	withStubbedNames(t)
	root := t.TempDir()
	created := filepath.Join(root, "Claude_Personal")
	if err := os.MkdirAll(created, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &mockPlatform{running: true, createdIdentity: "Claude_Personal", createdPath: created}

	got, err := NewProfileCreator(m).Create(CreateProfileRequest{Name: "  Personal  "})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.DataDir != created || got.Identity != "Claude_Personal" {
		t.Fatalf("got %+v, want identity Claude_Personal at %q", got, created)
	}
	if !m.terminated {
		t.Fatal("Claude must be quit before its data dirs are touched")
	}
	// The platform receives the cleaned name, never the raw input.
	if m.createdName != "Personal" {
		t.Fatalf("platform got name %q, want it trimmed", m.createdName)
	}
	if len(m.preparedSources) != 0 {
		t.Fatalf("the add path must not prepare a recovery, got %+v", m.preparedSources)
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
	// The name the user typed becomes the display name, so both platforms show it
	// even though only one of them puts it in the folder name.
	if n := LoadProfileNames()["Claude_Personal"]; n != "Personal" {
		t.Fatalf("display name = %q, want %q", n, "Personal")
	}
}

func TestCreateProfileKeysRegistriesOnTheIdentityNotThePath(t *testing.T) {
	// The Store build: CreateProfile returns identity "Work" while the data lives
	// in a directory called "Claude" — the shared slot. Anything using
	// filepath.Base of the path writes "Claude" into the registries, names a
	// profile FindProfiles never reports, and leaves the real one invisible. Spec
	// §3.5.
	withStubbedManaged(t)
	withStubbedPending(t)
	withStubbedNames(t)
	root := t.TempDir()
	slot := filepath.Join(root, "Claude")
	if err := os.MkdirAll(slot, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &mockPlatform{createdIdentity: "Work", createdPath: slot}

	got, err := NewProfileCreator(m).Create(CreateProfileRequest{Name: "Work"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Identity != "Work" {
		t.Fatalf("identity = %q, want what the platform returned", got.Identity)
	}
	if p := LoadPending(); len(p) != 1 || p[0].Folder != "Work" {
		t.Fatalf("pending = %+v, want it keyed on the identity", p)
	}
	if mg := LoadManaged(); len(mg) != 1 || mg[0] != "Work" {
		t.Fatalf("managed = %v, want it keyed on the identity", mg)
	}
}

func TestCreateProfileRecoveryPath(t *testing.T) {
	withStubbedManaged(t)
	withStubbedPending(t)
	withStubbedNames(t)
	root := t.TempDir()
	created := filepath.Join(root, "Claude_Recovered")
	if err := os.MkdirAll(created, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &mockPlatform{createdIdentity: "Claude_Recovered", createdPath: created}

	_, err := NewProfileCreator(m).Create(CreateProfileRequest{
		Name: "Recovered", RecoverUUID: "orphan-uuid",
		Sources: []GhostSource{
			{Folder: "Claude", Path: "/data/Claude", Convos: 5},
			{Folder: "Claude_Two", Path: "/data/Claude_Two", Convos: 40},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Every source is passed through, with the path the scan gave it — nothing here
	// reconstructs a path from a folder name.
	if len(m.preparedSources) != 2 {
		t.Fatalf("all sources must reach the platform, got %+v", m.preparedSources)
	}
	for _, s := range m.preparedSources {
		if s.UUID != "orphan-uuid" {
			t.Fatalf("source has the wrong account: %+v", s)
		}
	}
	if m.preparedSources[0].Path != "/data/Claude" || m.preparedSources[1].Path != "/data/Claude_Two" {
		t.Fatalf("paths must come from the scan: %+v", m.preparedSources)
	}
	pending := LoadPending()
	if len(pending) != 1 || pending[0].ExpectUUID != "orphan-uuid" {
		t.Fatalf("pending must remember which account to wait for: %+v", pending)
	}
}

func TestCreateProfileRejectsBadNameBeforeTouchingAnything(t *testing.T) {
	withStubbedManaged(t)
	withStubbedPending(t)
	withStubbedNames(t)
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

func TestCreateProfileRecoveryWithNoSourcesIsRefused(t *testing.T) {
	withStubbedManaged(t)
	withStubbedPending(t)
	withStubbedNames(t)
	m := &mockPlatform{createdIdentity: "Claude_Recovered", createdPath: t.TempDir()}

	if _, err := NewProfileCreator(m).Create(CreateProfileRequest{
		Name: "Recovered", RecoverUUID: "orphan-uuid",
	}); err == nil {
		t.Fatal("a recovery with nowhere to copy from must be refused")
	}
	if m.terminated {
		t.Fatal("refuse before quitting Claude — nothing has changed yet")
	}
}

func TestCreateProfileRecoveryFailureLeavesNoState(t *testing.T) {
	withStubbedManaged(t)
	withStubbedPending(t)
	withStubbedNames(t)
	root := t.TempDir()
	created := filepath.Join(root, "Claude_Recovered")
	if err := os.MkdirAll(created, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &mockPlatform{createdIdentity: "Claude_Recovered", createdPath: created, prepareErr: os.ErrPermission}

	if _, err := NewProfileCreator(m).Create(CreateProfileRequest{
		Name: "Recovered", RecoverUUID: "orphan-uuid",
		Sources: []GhostSource{{Folder: "Claude", Path: "/data/Claude", Convos: 5}},
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

func (m *mockPlatform) PrepareRecovery(newProfilePath string, sources []platform.RecoverySource) error {
	m.preparedSources = sources
	return m.prepareErr
}
```

`withStubbedNames` comes from Task 9. If Task 10 is being done first, define it there
instead and have Task 9 reuse it — it must exist exactly once in the package.

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

// CreateProfileRequest describes a profile to create. RecoverUUID and Sources are
// set together, and only on the recovery path: they name the orphaned account whose
// conversations should end up in the new profile, and every profile currently
// holding some of them.
//
// Sources carry their own paths, straight from the scan. Nothing here rebuilds a
// path from a folder name: on the Store build the data root is not AppSupportDir()
// and a parked profile is a level deeper still (spec §3.5).
type CreateProfileRequest struct {
	Name        string
	RecoverUUID string
	Sources     []GhostSource
}

// CreatedProfile is what a create produced: the identity every MCS registry keys
// on, and the directory the data lives in. They are separate because they differ on
// the Store build, where the directory is the shared slot and always called
// "Claude". Never derive one from the other.
type CreatedProfile struct {
	Identity string
	DataDir  string
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
func (c *ProfileCreator) Create(req CreateProfileRequest) (*CreatedProfile, error) {
	clean, err := ValidateProfileName(req.Name)
	if err != nil {
		return nil, err
	}
	if req.RecoverUUID != "" && len(req.Sources) == 0 {
		return nil, fmt.Errorf("internal: a recovery needs the profiles holding the conversations")
	}

	// Claude holds its data dir open, and on the Store build the profile is
	// created by moving that very directory.
	if err := c.Plat.TerminateApp(); err != nil {
		return nil, err
	}

	// Both come back from the platform. The identity is what FindProfiles will
	// report and what every registry below keys on; the directory is where the data
	// goes. On the Store build they differ, and filepath.Base of the directory is
	// "Claude" for every profile — deriving the identity that way is the defect this
	// signature exists to prevent (spec §3.5).
	identity, dataDir, err := c.Plat.CreateProfile(clean)
	if err != nil {
		return nil, err
	}

	if req.RecoverUUID != "" {
		sources := make([]platform.RecoverySource, 0, len(req.Sources))
		for _, s := range req.Sources {
			sources = append(sources, platform.RecoverySource{Path: s.Path, UUID: req.RecoverUUID})
		}
		if err := c.Plat.PrepareRecovery(dataDir, sources); err != nil {
			// The sources were only ever read from, so removing what we just made
			// loses nothing and leaves the name free for a retry.
			if rmErr := os.RemoveAll(dataDir); rmErr != nil {
				log.Printf("could not clean up the half-made profile %q: %v", dataDir, rmErr)
			}
			return nil, err
		}
	}

	if err := AddPending(identity, req.RecoverUUID); err != nil {
		return nil, fmt.Errorf("record the new profile: %w", err)
	}
	// Managed at once, so the account list shows it while the user is being told
	// to go and sign in to it.
	managed := LoadManaged()
	if managed != nil {
		already := false
		for _, m := range managed {
			if m == identity {
				already = true
			}
		}
		if !already {
			if err := SetManaged(append(managed, identity)); err != nil {
				return nil, fmt.Errorf("update the managed list: %w", err)
			}
		}
	} else if err := SetManaged([]string{identity}); err != nil {
		return nil, fmt.Errorf("update the managed list: %w", err)
	}
	// Show the name the user typed, whatever the platform chose to call the folder.
	// Without this a profile created on macOS or Windows standalone reads as
	// "Claude_Work" while the same profile on the Store build reads as "Work".
	if err := SetProfileName(identity, clean); err != nil {
		log.Printf("could not record the display name for %q: %v", identity, err)
	}

	created := &CreatedProfile{Identity: identity, DataDir: dataDir}
	if err := c.Plat.LaunchProfile(dataDir); err != nil {
		return created, fmt.Errorf("the profile is ready but Claude didn't open: %w", err)
	}
	return created, nil
}
```

`os.RemoveAll(dataDir)` is the one removal in this codebase that touches a Claude data dir. It is confined to a directory MCS created seconds earlier, that no account has ever been signed in to, and whose only content is a copy whose sources are untouched. Do not widen it.

On the Store build that directory does not exist at this point — the slot is deliberately absent — so the `RemoveAll` is a no-op there and the parked original is untouched. That is correct: `PrepareRecovery` is also a no-op on that platform, so there is nothing to undo.

Note the `managed == nil` branch: `LoadManaged` returns nil on first run and callers must not treat that as "configured empty" (`core/managed.go`).

The display name is recorded with `SetProfileName` and a failure there is logged, not fatal: the profile exists, is registered, and works — a missing display name is cosmetic, and undoing a successful creation over it would be worse than the symptom.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/ -run TestCreateProfile -v`
Expected: PASS, 6 tests.

Run: `go test ./... 2>&1 | tail -10`
Expected: all packages pass.

`TestCreateProfileKeysRegistriesOnTheIdentityNotThePath` is the one that matters most here.
Confirm it is a real gate by temporarily replacing `identity` with
`filepath.Base(dataDir)` in the three registry calls: that test must fail and the others
must still pass. Put it back.

- [ ] **Step 5: Commit**

```bash
git add core/newprofile.go core/newprofile_test.go core/switch_test.go
git commit -m "core: one create-a-profile sequence shared by both hosts"
```

---

### Task 11: Account list — add card and duplicate warning

**Files:**
- Modify: `internal/panelui/render.go` — `ProfileVM`, `RenderList`, `shell`'s CSS
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
	// Folder names go through data-* and are read back with dataset, never
	// interpolated into an inline JS string. That is the v0.9.1 bug class: a folder
	// containing an apostrophe becomes &#39; via html.EscapeString, which the HTML
	// parser decodes back to ' before the JS is parsed.
	if !strings.Contains(html, `data-dup-a="Claude" data-dup-b="Claude_Work"`) {
		t.Fatalf("warning must offer the merge for that group:\n%s", html)
	}
	// Assert on the markup, not the class name: shell() emits every class name in
	// its <style> block on every page, so strings.Contains("dup-pill") is true even
	// on a page with no pills at all.
	if got := strings.Count(html, dupPillMarkup); got != 2 {
		t.Fatalf("both duplicate cards must be marked, got %d:\n%s", got, html)
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
	if strings.Contains(html, dupPillMarkup) {
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
	if !strings.Contains(html, `data-dup-a="Claude_A" data-dup-b="Claude_B"`) {
		t.Fatal("the first group by folder order goes first")
	}
	// All four cards are still flagged, so the user can see the second pair is
	// coming.
	if got := strings.Count(html, dupPillMarkup); got != 4 {
		t.Fatalf("every duplicate card is marked, got %d", got)
	}
}
```

And, at the top of the test file, the one place the pill's markup is written down:

```go
// dupPillMarkup is the rendered pill, not its class name. shell() puts every class
// name into the <style> block of every page, so asserting on a bare class name
// passes whether or not the element was rendered — and its negation can never fail.
const dupPillMarkup = `<span class="dup-pill">Duplicate</span>`
```

`internal/panelui/render_test.go` already imports `strings`.

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

Add CSS to `shell`, beside the existing `.note-bad` rule:

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
		// The two folder names travel as data-* and are read back with dataset.
		// Interpolating them into the onclick string would reintroduce the v0.9.1
		// bug: html.EscapeString turns an apostrophe into &#39;, the HTML parser
		// decodes it back to ' before the JS is parsed, and the handler breaks (or
		// worse) on any folder containing one.
		dupWarning = fmt.Sprintf(`<div class="dup">
  <div class="dt">%s and %s are the same account. Merge them to clean this up.</div>
  <button class="btn-sm" data-dup-a="%s" data-dup-b="%s" onclick="mergePair(this.dataset.dupA,this.dataset.dupB)">Merge</button>
</div>`, esc(nameOf(profiles, a)), esc(nameOf(profiles, b)), esc(a), esc(b))
	}
```

Add the JS helper beside `syncDir` in `shell`, which already does exactly this for its own
pair of folder names:

```js
  function mergePair(a,b){ send('showMerge', a+'|'+b); }
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
- Modify: `internal/panelui/render.go` — the `if !a.Complete` branch in `RenderRescan`
- Test: `internal/panelui/render_test.go`

**Interfaces:**
- Consumes: `core.ScannedAccount.Recoverable`, `.Sources`, `.Note` (Task 3).
- Produces: the panel action `showRecover` with arg `uuid`.

The action carries **only the account UUID**. The sources are looked up again by the host
when the recovery runs, from a fresh scan: a ghost can have several source profiles, and
their paths are only valid for the scan that produced them (spec §3.5). Packing them into
an onclick would be both wrong and unbounded.

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/panelui/render_test.go
func TestRenderRescanRecoverableGhostOffersRecovery(t *testing.T) {
	accounts := []core.ScannedAccount{{
		UUID: "bbbbbbbb-0000-4000-8000-000000000002", Complete: false,
		Recoverable: true, Convos: 94,
		Sources:     []core.GhostSource{{Folder: "Claude", Path: "/data/Claude", Convos: 94}},
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
	if !strings.Contains(html, `data-uuid="bbbbbbbb-0000-4000-8000-000000000002"`) {
		t.Fatalf("want a Recover action carrying the account:\n%s", html)
	}
	// Assert on the rendered note, not the class name: shell() emits .note-todo and
	// .note-bad in the <style> block of every page, so a bare class-name check is
	// true regardless of what was rendered, and its negation can never fail.
	if !strings.Contains(html, `<div class="note-todo">`+core.RecoverableGhostNote+`</div>`) {
		t.Fatalf("recoverable note uses the blue style, not the red one:\n%s", html)
	}
	if strings.Contains(html, `<div class="note-bad">`) {
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
	if !strings.Contains(html, `<div class="note-bad">Invalid account data</div>`) {
		t.Fatal("dead ghost keeps the red note")
	}
}

func TestRenderRescanRecoverableGhostIsNotSelectable(t *testing.T) {
	accounts := []core.ScannedAccount{{
		UUID: "u", Complete: false, Recoverable: true, Convos: 1,
		Sources: []core.GhostSource{{Folder: "Claude", Path: "/data/Claude", Convos: 1}},
		Note:    core.RecoverableGhostNote,
	}}
	html := RenderRescan(accounts, nil)
	// It has no folder to manage yet, so it must not join the checkbox set that
	// Confirm submits.
	if strings.Contains(html, `class="card selectable`) {
		t.Fatalf("a ghost cannot be managed, only recovered:\n%s", html)
	}
}
```

`render_test.go` already imports `core` — its existing tests build `core.ScannedAccount` values. Add `time` if it is not there.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/panelui/ -run TestRenderRescan -v`
Expected: FAIL — the existing ghost branch renders "Unrecognized account" for both.

- [ ] **Step 3: Write the implementation**

Replace the `if !a.Complete` branch in `RenderRescan` with a split on `Recoverable`:

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
        <button class="btn-sm" data-uuid="%s" onclick="send('showRecover',this.dataset.uuid)">Recover</button></div>`,
					esc(ShortID(a.UUID)), a.Convos, esc(date), esc(a.Note),
					esc(a.UUID)))
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

A UUID could not contain an apostrophe, but it still travels as `data-uuid` and is read back
with `dataset`. Every value that reaches JS goes the same way, so there is no judgement call
per call site about which strings are "safe enough" — that judgement is what produced the
v0.9.1 bug.

`.btn-sm` was added to `shell` in Task 11. If Task 11 has not landed, add it here instead and drop it from Task 11.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/panelui/ -v`
Expected: PASS. The pre-existing ghost test (`grep -n "Unrecognized account" internal/panelui/render_test.go`) may assert on the old markup — read it and, if its fixture has a populated bucket, retarget it at a dead ghost so it keeps testing what it was written to test.

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
- Consumes: `.btn-sm` and CSS from Task 11; `.rninput` already exists in `shell`.
- Produces:
  - `type NewProfileVM struct { RecoverUUID, SuggestedName string; Convos int; Err string }`
  - `func RenderNewProfile(vm NewProfileVM) string`
  - the panel action `createProfile` with a JSON arg `[name, recoverUUID]`

There is no source folder in the view or the action. A ghost can have several source
profiles and their paths are only valid for the scan that produced them, so the host looks
them up from a fresh scan when the recovery runs (spec §3.5, Task 12).

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
	// The copy is "a <b>different account</b>", so the phrase survives the markup.
	// Wrapping only the word — "a <b>different</b> account" — makes this assertion
	// fail against its own implementation, which is how the first draft of this task
	// shipped a test that could not pass.
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
	html := RenderNewProfile(NewProfileVM{RecoverUUID: "u-1"})
	// The v0.9.1 bug class: values must never be interpolated into inline JS string
	// arguments.
	if !strings.Contains(html, `data-uuid="u-1"`) {
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

	// "different account" stays one unbroken phrase inside the <b>, so a test can
	// assert on it as the user reads it.
	second := `<div class="hintw">Sign in as a <b>different account</b>. Signing in as one you already have creates a duplicate, and MCS will ask you to merge.</div>`
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
  <button class="btn btn-primary" data-uuid="` + esc(vm.RecoverUUID) + `" onclick="createProfileSave(this)">` + esc(confirm) + `</button>
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

Add the JS bridge function to `shell`'s script block, beside `renameSave`:

```js
  function createProfileSave(btn){
    var v=document.getElementById('np').value.trim();
    send('createProfile', JSON.stringify([v, btn.dataset.uuid||'']));
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
- Consumes: CSS from Tasks 11 and 13; `core.MergePlan` (Task 9).
- Produces:
  - `type MergeCandidateVM struct { Folder, Name, Plan string; Convos int; Current bool }`
  - `func RenderMerge(a, b MergeCandidateVM, plan core.MergePlan, status string, busy bool) string`
  - the panel action `mergeConfirm` with arg `keepIdentity|archiveIdentity`

**The screen renders a computed plan, not a sum.** `plan.Combined` is the union of the two
buckets — `a.Convos + b.Convos` double-counts every conversation both profiles hold, which
after a merge is one conversation, not two. And when `plan.Conflicts > 0` the screen has to
say so before the user commits: those records keep the keeper's version and the other
version survives only in the archive (spec §4.4, §5.2).

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/panelui/render_test.go
func TestRenderMergePreselectsTheProfileInUse(t *testing.T) {
	a := MergeCandidateVM{Folder: "Claude", Name: "Claude", Convos: 99}
	b := MergeCandidateVM{Folder: "Claude_Work", Name: "Work", Convos: 42, Current: true}
	html := RenderMerge(a, b, core.MergePlan{Combined: 141}, "", false)

	// Keeping the one already in use means no re-sign-in, so it is the default.
	//
	// Assert on the class and the folder together. Comparing their positions
	// separately cannot fail: the class attribute precedes data-folder inside every
	// card, so the index of "selected" is below the index of either folder whichever
	// card carries it.
	if !strings.Contains(html, `class="card selectable selected" data-folder="Claude_Work"`) {
		t.Fatalf("the in-use profile must be the preselected one:\n%s", html)
	}
	if !strings.Contains(html, `class="card selectable" data-folder="Claude"`) {
		t.Fatalf("the other profile must not be preselected:\n%s", html)
	}
	if !strings.Contains(html, "Will be archived") {
		t.Fatal("the other card must say what happens to it")
	}
}

func TestRenderMergePreselectsTheFirstWhenNeitherIsInUse(t *testing.T) {
	// Claude is quit by the time a merge runs, so "in use" can be unknown. The
	// screen must never render with nothing chosen.
	html := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Claude", Convos: 1},
		MergeCandidateVM{Folder: "Claude_Work", Name: "Work", Convos: 1},
		core.MergePlan{Combined: 2}, "", false)
	if !strings.Contains(html, `class="card selectable selected" data-folder="Claude"`) {
		t.Fatalf("fall back to the first card:\n%s", html)
	}
}

func TestRenderMergeShowsThePlansCombinedTotalNotTheSum(t *testing.T) {
	// Both profiles hold 99 and 42 conversations, 20 of them the same records, so
	// the keeper ends up with 121 — not 141. The screen must show what the merge
	// computed, or it promises conversations that do not exist.
	html := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Claude", Convos: 99, Current: true},
		MergeCandidateVM{Folder: "Claude_Work", Name: "Work", Convos: 42},
		core.MergePlan{Combined: 121}, "", false)
	if !strings.Contains(html, "121") {
		t.Fatalf("want the plan's union total:\n%s", html)
	}
	if strings.Contains(html, "141") {
		t.Fatalf("the sum of both sides double-counts shared records:\n%s", html)
	}
	if !strings.Contains(html, "archived, not deleted") {
		t.Fatal("must say nothing is deleted")
	}
}

func TestRenderMergeDisclosesConflicts(t *testing.T) {
	html := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Claude", Convos: 99, Current: true},
		MergeCandidateVM{Folder: "Claude_Work", Name: "Work", Convos: 42},
		core.MergePlan{Combined: 121, Conflicts: 3}, "", false)
	if !strings.Contains(html, "3 conversations exist in both") {
		t.Fatalf("a conflict strands a version in the archive; say so first:\n%s", html)
	}
}

func TestRenderMergeSaysNothingAboutConflictsWhenThereAreNone(t *testing.T) {
	html := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Claude", Convos: 99, Current: true},
		MergeCandidateVM{Folder: "Claude_Work", Name: "Work", Convos: 42},
		core.MergePlan{Combined: 141}, "", false)
	if strings.Contains(html, "exist in both") {
		t.Fatalf("no conflicts, no warning:\n%s", html)
	}
}

func TestRenderMergeUsesDataAttributesNotInlineArgs(t *testing.T) {
	html := RenderMerge(
		MergeCandidateVM{Folder: "Claude", Name: "Claude", Current: true},
		MergeCandidateVM{Folder: "Claude_Work", Name: "Work"},
		core.MergePlan{}, "", false)
	if strings.Contains(html, "mergeConfirm('") {
		t.Fatalf("no inline string args (v0.9.1 bug class):\n%s", html)
	}
	if !strings.Contains(html, "toggleMergePick(this)") {
		t.Fatalf("cards must switch the pick through a handler:\n%s", html)
	}
}

func TestRenderMergeBusyDisablesTheAction(t *testing.T) {
	a := MergeCandidateVM{Folder: "Claude", Name: "Claude", Current: true}
	b := MergeCandidateVM{Folder: "Claude_Work", Name: "Work"}

	busy := RenderMerge(a, b, core.MergePlan{Combined: 1}, "Merging…", true)
	if !strings.Contains(busy, "Merging…") {
		t.Fatal("status must be shown")
	}
	// Assert on the button, not the word: shell()'s CSS contains ".sbtn:disabled",
	// so strings.Contains(html, "disabled") is true on every page ever rendered and
	// this test would pass with busy=false.
	if !strings.Contains(busy, `<button class="btn btn-primary" disabled`) {
		t.Fatalf("a merge in flight must not be startable twice:\n%s", busy)
	}

	idle := RenderMerge(a, b, core.MergePlan{Combined: 1}, "", false)
	if strings.Contains(idle, `<button class="btn btn-primary" disabled`) {
		t.Fatalf("the button must be live when no merge is running:\n%s", idle)
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
//
// plan is what the merge will actually do, computed by core.MergePreview before
// this screen is rendered. The total shown comes from there and not from adding the
// two counts: a conversation both profiles hold is one conversation afterwards, not
// two.
func RenderMerge(a, b MergeCandidateVM, plan core.MergePlan, status string, busy bool) string {
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

	// Where both profiles hold a record and the keeper's copy is the newer one, the
	// sync leaves the keeper's alone and the archive keeps the other. Say so before
	// the user commits: after the merge that version is reachable only by opening the
	// archive folder.
	conflictNote := ""
	if plan.Conflicts > 0 {
		conflictNote = fmt.Sprintf(`<div class="hintw">%d conversations exist in both profiles and have changed since they were last in step. The newer version is kept. The other stays in the archived folder, which you can open from Settings.</div>`, plan.Conflicts)
	}

	body := `<div class="header">
  <button class="back" onclick="send('showList','')">‹</button>
  <div class="htext"><h1>Merge duplicates</h1><p>Both are the same account</p></div>
</div>` + st + `
<div class="cards">` + card(a) + card(b) + `</div>
<div class="hint">All ` + fmt.Sprint(plan.Combined) + ` conversations are combined into the account you keep. The other folder is archived, not deleted, so you can put it back yourself.</div>` + conflictNote + `
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

`internal/panelui` already imports `core` (`RenderRescan` takes `[]core.ScannedAccount`), so
taking a `core.MergePlan` adds no dependency.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/panelui/ -v`
Expected: PASS, 7 new tests plus all existing.

Then check the two negative tests are real gates, since both replace assertions that could
never fail: force `busy` to `false` inside `RenderMerge` and confirm
`TestRenderMergeBusyDisablesTheAction` fails; preselect `a.Folder` unconditionally and
confirm `TestRenderMergePreselectsTheProfileInUse` fails. Put both back.

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
