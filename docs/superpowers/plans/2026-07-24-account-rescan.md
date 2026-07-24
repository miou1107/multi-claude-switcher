# Account Rescan Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Rescan accounts…" tray action that scans the machine for Claude accounts, shows a 7-column review of each (UUID, completeness, email, Team, conversations, last-updated, note), and lets the user pick which complete accounts the switcher manages — persisted to a new `managed.json` registry that replaces the hardcoded menu filter.

**Architecture:** Four pure/testable core pieces (`core/identity.go`, `core/managed.go`, `core/scan.go`) plus a tray layer (`cmd/mcs-tray/rescan.go` + menu filter change). The scanner reads two UUID sources per `Claude*` dir — the live login (`config.json` `lastKnownAccountUuid`, cheap) and Code session buckets (`claude-code-sessions/<uuid>/`, persists across logout) — dedups accounts by UUID, and classifies each Complete (live login) or Incomplete/ghost (orphan buckets). The two-step UI is `osascript`: a review dialog then a multi-select pick.

**Tech Stack:** Go 1.22, `github.com/getlantern/systray`, `github.com/syndtr/goleveldb` (already a dependency), macOS `osascript`. No CGO. Module path `github.com/miou1107/multi-claude-switcher`.

## Global Constraints

- **No new dependencies.** goleveldb is already present; add nothing to `go.mod`.
- **No CGO; all platforms must still build.** macOS-only code goes in `//go:build darwin` files; provide no-op stubs for `!darwin` so `go build ./...` and CI (Linux) pass.
- **`core` may import `platform`** (it already does); `platform` must NOT import `core` (no cycle).
- **UI strings are English**, matching the existing tray voice (e.g. `autoSyncWarningMessage`). Exact note strings: Team → `Team account — conversations can't be synced`; Ghost/incomplete → `Invalid account data`; Personal complete → `` (empty).
- **Review-table column order (exact):** `UUID, Completeness, email, Team, Convos, Last updated, Note`.
- **Registry:** `~/.multi-claude-switcher/managed.json` = `{"managed":["Claude","Claude_Profile2"]}` (folder names). Mutex-guarded, atomic tmp+rename, mirroring `core/names.go`. `LoadManaged()` returns `nil` when the file is absent (first-run signal) and a non-nil (possibly empty) slice when present.
- **Completeness:** Complete = the dir's `config.json` has a `lastKnownAccountUuid` (the row's UUID is that value). Ghost = a session bucket whose UUID is no dir's live login anywhere. A session bucket whose UUID IS some dir's live login (in this or another dir) is a stale duplicate and is folded away (never its own row).
- **Manage unit = folder name.** The review lists accounts (deduped by UUID); a checked complete account maps to its home folder, which is what `managed.json` stores.
- **Docs:** update `FILELIST.md` (new source + test files), `CHANGELOG.md` (`[Unreleased]`), and both READMEs when the feature lands (Task 5).
- **Git:** commit author must display as `Vin`; never add a `Co-Authored-By` trailer. Commit with `git -c user.name="Vin" -c user.email="fontripdata@gmail.com"`.
- **macOS `display dialog` uses a proportional font**, so space-aligned columns are only approximately aligned — an accepted cosmetic limitation (spec §5). Render best-effort aligned monospace text anyway.

---

### Task 1: AccountIdentity reader (`core/identity.go`)

Extracts a human-readable identity for a profile's live-login account from its Local Storage LevelDB. Reuses `decodeLocalStorageValue` (already in `core/localstorage.go`) and the copy-then-open pattern from `readLocalStorageOrgs`.

**Files:**
- Create: `core/identity.go`
- Test: `core/identity_test.go`

**Interfaces:**
- Consumes: `decodeLocalStorageValue` (`core/localstorage.go`), `copyDir` (`core/backup.go`), goleveldb.
- Produces:
  - `type AccountIdentity struct { UUID, Email, DisplayName, FullName string }`
  - `func extractIdentity(decoded string) AccountIdentity` (pure)
  - `func readLocalStorageIdentity(profilePath string) (AccountIdentity, error)`

- [ ] **Step 1: Write the failing test for `extractIdentity`**

```go
package core

import "testing"

func TestExtractIdentity(t *testing.T) {
	// react-query-cache-ls shape
	rq := `{"x":{"uuid":"035899b2-b130-40b6-aa9e-93cf208df7b7","email_address":"vincent@fontrip.com","full_name":"Fontrip Vin","display_name":"Vin"}}`
	got := extractIdentity(rq)
	if got.Email != "vincent@fontrip.com" || got.UUID != "035899b2-b130-40b6-aa9e-93cf208df7b7" {
		t.Fatalf("react-query: got %+v", got)
	}
	if got.DisplayName != "Vin" || got.FullName != "Fontrip Vin" {
		t.Fatalf("react-query names: got %+v", got)
	}

	// ajs_user_traits shape (email + account_uuid)
	ajs := `{"traits":{"email":"someone@example.com","account_uuid":"ae543f88-0f24-4ae6-ae21-3033915bca76"}}`
	got = extractIdentity(ajs)
	if got.Email != "someone@example.com" || got.UUID != "ae543f88-0f24-4ae6-ae21-3033915bca76" {
		t.Fatalf("ajs: got %+v", got)
	}

	// non-JSON / no identity → zero value, no panic
	if id := extractIdentity("not json"); id != (AccountIdentity{}) {
		t.Fatalf("expected zero identity, got %+v", id)
	}
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./core/ -run TestExtractIdentity -v`
Expected: FAIL (undefined: extractIdentity).

- [ ] **Step 3: Implement `core/identity.go`**

```go
package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// AccountIdentity is the human-readable identity of a profile's live-login
// account, read from its cached Local Storage payloads. Fields are best-effort:
// any may be empty if the store lacks them.
type AccountIdentity struct {
	UUID        string
	Email       string
	DisplayName string
	FullName    string
}

// extractIdentity walks any nested JSON and fills an AccountIdentity from the two
// known account payloads: `ajs_user_traits` ({email, account_uuid}) and
// `react-query-cache-ls` ({uuid, email_address, full_name, display_name}). Later
// non-empty values win. Returns a zero value for non-JSON or identity-free input
// (never panics).
func extractIdentity(decoded string) AccountIdentity {
	var root interface{}
	if json.Unmarshal([]byte(decoded), &root) != nil {
		i := strings.IndexAny(decoded, "{[")
		if i < 0 {
			return AccountIdentity{}
		}
		if json.Unmarshal([]byte(decoded[i:]), &root) != nil {
			return AccountIdentity{}
		}
	}
	var id AccountIdentity
	set := func(dst *string, v interface{}) {
		if s, ok := v.(string); ok && s != "" {
			*dst = s
		}
	}
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			set(&id.Email, t["email"])
			set(&id.Email, t["email_address"])
			set(&id.UUID, t["account_uuid"])
			set(&id.UUID, t["uuid"])
			set(&id.DisplayName, t["display_name"])
			set(&id.FullName, t["full_name"])
			for _, vv := range t {
				walk(vv)
			}
		case []interface{}:
			for _, vv := range t {
				walk(vv)
			}
		}
	}
	walk(root)
	return id
}

// readLocalStorageIdentity copies the profile's Local Storage LevelDB to a temp
// dir (the live store is locked while Claude runs), opens it, and merges identity
// from every value that looks like an account payload. Best-effort: returns the
// merged identity and any fatal open/copy error.
func readLocalStorageIdentity(profilePath string) (AccountIdentity, error) {
	src := filepath.Join(profilePath, "Local Storage", "leveldb")
	if _, err := os.Stat(src); err != nil {
		return AccountIdentity{}, fmt.Errorf("local storage not found for %s: %w", profilePath, err)
	}
	tmp, err := os.MkdirTemp("", "mcs-id-*")
	if err != nil {
		return AccountIdentity{}, err
	}
	defer os.RemoveAll(tmp)

	dst := filepath.Join(tmp, "leveldb")
	if err := copyDir(src, dst); err != nil {
		return AccountIdentity{}, fmt.Errorf("copy local storage: %w", err)
	}
	db, err := leveldb.OpenFile(dst, &opt.Options{ReadOnly: true})
	if err != nil {
		if db, err = leveldb.OpenFile(dst, nil); err != nil {
			return AccountIdentity{}, fmt.Errorf("open leveldb: %w", err)
		}
	}
	defer db.Close()

	var id AccountIdentity
	it := db.NewIterator(nil, nil)
	defer it.Release()
	for it.Next() {
		s := decodeLocalStorageValue(it.Value())
		if !strings.Contains(s, "@") {
			continue // identity payloads always contain an email
		}
		got := extractIdentity(s)
		if got.Email != "" {
			id.Email = got.Email
		}
		if got.UUID != "" {
			id.UUID = got.UUID
		}
		if got.DisplayName != "" {
			id.DisplayName = got.DisplayName
		}
		if got.FullName != "" {
			id.FullName = got.FullName
		}
	}
	if err := it.Error(); err != nil {
		return id, err
	}
	return id, nil
}
```

- [ ] **Step 4: Run the pure test, verify it passes**

Run: `go test ./core/ -run TestExtractIdentity -v`
Expected: PASS.

- [ ] **Step 5: Write the reader smoke test**

Model it on the existing `core/accounttype_reader_test.go` (which builds a real fixture LevelDB with goleveldb). Build a temp `<profile>/Local Storage/leveldb`, `Put` one key whose value is a Latin-1-tagged (`0x01` prefix) JSON identity payload, then assert `readLocalStorageIdentity` returns the email/uuid.

```go
package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
)

func TestReadLocalStorageIdentity(t *testing.T) {
	profile := t.TempDir()
	ldb := filepath.Join(profile, "Local Storage", "leveldb")
	if err := os.MkdirAll(ldb, 0755); err != nil {
		t.Fatal(err)
	}
	db, err := leveldb.OpenFile(ldb, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"uuid":"035899b2","email_address":"vincent@fontrip.com","display_name":"Vin","full_name":"Fontrip Vin"}`
	// 0x01 = Latin-1/UTF-8 encoding tag (see decodeLocalStorageValue).
	if err := db.Put([]byte("_https://claude.ai\x00\x01react-query-cache-ls"), append([]byte{1}, []byte(payload)...), nil); err != nil {
		t.Fatal(err)
	}
	db.Close()

	id, err := readLocalStorageIdentity(profile)
	if err != nil {
		t.Fatalf("reader error: %v", err)
	}
	if id.Email != "vincent@fontrip.com" || id.DisplayName != "Vin" || id.UUID != "035899b2" {
		t.Fatalf("got %+v", id)
	}
}
```

- [ ] **Step 6: Run it, verify it passes**

Run: `go test ./core/ -run TestReadLocalStorageIdentity -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add core/identity.go core/identity_test.go
git -c user.name="Vin" -c user.email="fontripdata@gmail.com" commit -m "feat(core): read account identity (email/name/uuid) from Local Storage"
```

---

### Task 2: Managed-profile registry (`core/managed.go`)

Persists the user-chosen set of managed profile folders. Mirrors `core/names.go` exactly (mutex, atomic tmp+rename, `var`-redirectable path for tests).

**Files:**
- Create: `core/managed.go`
- Test: `core/managed_test.go`

**Interfaces:**
- Produces:
  - `func LoadManaged() []string` — `nil` when the file is absent; a non-nil slice when present.
  - `func SetManaged(folders []string) error`
  - `func IsManaged(folder string) bool`

- [ ] **Step 1: Write the failing test**

```go
package core

import (
	"path/filepath"
	"testing"
)

func TestManagedRegistry(t *testing.T) {
	dir := t.TempDir()
	orig := managedPath
	managedPath = func() string { return filepath.Join(dir, "managed.json") }
	defer func() { managedPath = orig }()

	// Absent file → nil (first-run signal).
	if got := LoadManaged(); got != nil {
		t.Fatalf("absent file: want nil, got %#v", got)
	}
	if IsManaged("Claude") {
		t.Fatal("absent file: IsManaged should be false")
	}

	if err := SetManaged([]string{"Claude", "Claude_Profile2"}); err != nil {
		t.Fatal(err)
	}
	got := LoadManaged()
	if len(got) != 2 || got[0] != "Claude" || got[1] != "Claude_Profile2" {
		t.Fatalf("round-trip: got %#v", got)
	}
	if !IsManaged("Claude") || IsManaged("Claude-3p") {
		t.Fatal("IsManaged wrong after save")
	}

	// Present-but-empty → non-nil empty slice (distinct from absent).
	if err := SetManaged([]string{}); err != nil {
		t.Fatal(err)
	}
	if got := LoadManaged(); got == nil {
		t.Fatal("present-empty: want non-nil empty slice, got nil")
	}
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./core/ -run TestManagedRegistry -v`
Expected: FAIL (undefined: managedPath / LoadManaged / …).

- [ ] **Step 3: Implement `core/managed.go`**

```go
package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var managedMu sync.Mutex

// managedPath is where the user-curated managed-profile list is stored. It is a
// var so tests can redirect it to a temp dir (same pattern as names.go).
var managedPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "multi-claude-switcher-managed.json")
	}
	return filepath.Join(home, ".multi-claude-switcher", "managed.json")
}

type managedFile struct {
	Managed []string `json:"managed"`
}

// LoadManaged returns the managed folder-name list. It returns nil when the file
// is absent (the first-run signal), and a non-nil (possibly empty) slice when the
// file exists — callers distinguish "never configured" from "configured empty".
func LoadManaged() []string {
	managedMu.Lock()
	defer managedMu.Unlock()
	data, err := os.ReadFile(managedPath())
	if err != nil {
		return nil // absent/unreadable → first-run
	}
	var mf managedFile
	if json.Unmarshal(data, &mf) != nil {
		return nil
	}
	if mf.Managed == nil {
		return []string{} // present but no key → configured empty, not first-run
	}
	return mf.Managed
}

// SetManaged persists the managed folder-name list (atomic tmp+rename).
func SetManaged(folders []string) error {
	managedMu.Lock()
	defer managedMu.Unlock()
	if folders == nil {
		folders = []string{}
	}
	data, err := json.MarshalIndent(managedFile{Managed: folders}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(managedPath()), 0755); err != nil {
		return err
	}
	tmp := managedPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, managedPath()); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// IsManaged reports whether the given folder is in the persisted managed list.
// Returns false when the registry is absent (first-run); callers apply their own
// first-run fallback.
func IsManaged(folder string) bool {
	for _, m := range LoadManaged() {
		if m == folder {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run it, verify it passes**

Run: `go test ./core/ -run TestManagedRegistry -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add core/managed.go core/managed_test.go
git -c user.name="Vin" -c user.email="fontripdata@gmail.com" commit -m "feat(core): managed-profile registry (managed.json)"
```

---

### Task 3: Account scanner (`core/scan.go`)

Assembles the deduped account list from all `Claude*` dirs. The pure `assembleAccounts` holds the dedup/completeness/fold/note logic (heavily tested); `gatherDir`/`ScanAccounts` do the IO.

**Files:**
- Create: `core/scan.go`
- Test: `core/scan_test.go`

**Interfaces:**
- Consumes: `platform.ProfileInfo`, `platform.GetProfileAccountUUID`, `platform.GetProfileSessionsDir`, `readLocalStorageIdentity` (Task 1), `DetectAccountType` (existing), `AccountType`/`AccountUnknown`/`AccountTeam` (existing).
- Produces:
  - `type ScannedAccount struct { UUID string; Complete bool; HomeFolder string; Email string; Account AccountType; Convos int; LastUpdated time.Time; Note string }`
  - `func ScanAccounts(profiles []*platform.ProfileInfo) []ScannedAccount`
  - internal: `bucketStat`, `dirScan`, `assembleAccounts`, `deriveNote`, `gatherDir`.

- [ ] **Step 1: Write the failing test for `assembleAccounts` + `deriveNote`**

```go
package core

import (
	"testing"
	"time"
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
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./core/ -run 'TestAssemble|TestDeriveNote' -v`
Expected: FAIL (undefined: dirScan / assembleAccounts / …).

- [ ] **Step 3: Implement the pure layer in `core/scan.go`**

```go
package core

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// ScannedAccount is one row of the rescan review: a deduped account with its
// completeness, identity, activity stats, and derived note.
type ScannedAccount struct {
	UUID        string
	Complete    bool      // true = live login somewhere (switchable); false = ghost
	HomeFolder  string    // folder where it is the live login ("" if ghost)
	Email       string
	Account     AccountType
	Convos      int
	LastUpdated time.Time
	Note        string
}

type bucketStat struct {
	Count       int
	LastUpdated time.Time
}

type dirScan struct {
	Folder   string
	LiveUUID string // config.json lastKnownAccountUuid ("" if logged out)
	Identity AccountIdentity
	Account  AccountType
	Buckets  map[string]bucketStat // accountUUID -> stats (from claude-code-sessions/)
}

// deriveNote returns the review note for a row: incomplete rows are invalid data;
// complete Team rows warn that conversations can't be synced; complete personal
// rows have no note.
func deriveNote(complete bool, acct AccountType) string {
	if !complete {
		return "Invalid account data"
	}
	if acct == AccountTeam {
		return "Team account — conversations can't be synced"
	}
	return ""
}

// assembleAccounts turns per-dir scans into deduped review rows. One complete row
// per live-login dir; one ghost row per orphan UUID (a bucket that is no dir's
// live login anywhere), summed across dirs. A bucket whose UUID is some dir's
// live login is a stale duplicate and is folded away. Output is sorted: complete
// first by HomeFolder, then ghosts by UUID.
func assembleAccounts(scans []dirScan) []ScannedAccount {
	live := map[string]bool{}
	for _, s := range scans {
		if s.LiveUUID != "" {
			live[s.LiveUUID] = true
		}
	}
	var out []ScannedAccount
	for _, s := range scans {
		if s.LiveUUID == "" {
			continue
		}
		b := s.Buckets[s.LiveUUID] // zero if this account has no Code sessions yet
		out = append(out, ScannedAccount{
			UUID:        s.LiveUUID,
			Complete:    true,
			HomeFolder:  s.Folder,
			Email:       s.Identity.Email,
			Account:     s.Account,
			Convos:      b.Count,
			LastUpdated: b.LastUpdated,
			Note:        deriveNote(true, s.Account),
		})
	}
	ghost := map[string]*ScannedAccount{}
	for _, s := range scans {
		for uuid, b := range s.Buckets {
			if uuid == s.LiveUUID || live[uuid] {
				continue // own live bucket, or stale dup of an account live elsewhere
			}
			g := ghost[uuid]
			if g == nil {
				g = &ScannedAccount{UUID: uuid, Complete: false, Account: AccountUnknown, Note: deriveNote(false, AccountUnknown)}
				ghost[uuid] = g
			}
			g.Convos += b.Count
			if b.LastUpdated.After(g.LastUpdated) {
				g.LastUpdated = b.LastUpdated
			}
		}
	}
	for _, g := range ghost {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Complete != out[j].Complete {
			return out[i].Complete
		}
		if out[i].Complete {
			return out[i].HomeFolder < out[j].HomeFolder
		}
		return out[i].UUID < out[j].UUID
	})
	return out
}
```

- [ ] **Step 4: Run the pure tests, verify they pass**

Run: `go test ./core/ -run 'TestAssemble|TestDeriveNote' -v`
Expected: PASS.

- [ ] **Step 5: Write the IO smoke test for `ScanAccounts`**

Build a temp App Support tree with two profiles and feed `[]*platform.ProfileInfo` (only `Name`+`Path` are needed by `gatherDir`). Assert the row count and completeness. Helper writes a `config.json` and session bucket json files.

```go
package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/miou1107/multi-claude-switcher/platform"
)

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
```

- [ ] **Step 6: Implement the IO layer (append to `core/scan.go`)**

```go
// gatherDir reads one profile dir into a dirScan: its live-login UUID (cheap,
// config.json), its session buckets (count + newest mtime), and — only for a
// live login — the account identity and type (best-effort Local Storage reads).
func gatherDir(p *platform.ProfileInfo) dirScan {
	ds := dirScan{Folder: p.Name, Buckets: map[string]bucketStat{}}
	if uuid, err := platform.GetProfileAccountUUID(p.Path); err == nil {
		ds.LiveUUID = uuid
	}
	sessDir := platform.GetProfileSessionsDir(p.Path)
	if entries, err := os.ReadDir(sessDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			count, newest := countAndNewest(filepath.Join(sessDir, e.Name()))
			ds.Buckets[e.Name()] = bucketStat{Count: count, LastUpdated: newest}
		}
	}
	if ds.LiveUUID != "" {
		if id, err := readLocalStorageIdentity(p.Path); err == nil {
			ds.Identity = id
		}
		if at, err := DetectAccountType(p.Path); err == nil {
			ds.Account = at
		}
	}
	return ds
}

// countAndNewest walks a session bucket, counting *.json files and tracking the
// newest modification time.
func countAndNewest(dir string) (int, time.Time) {
	var count int
	var newest time.Time
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}
		count++
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return count, newest
}

// ScanAccounts scans the given profile dirs and returns the deduped review rows.
// Dirs with neither a live login nor any session bucket (junk) are dropped.
func ScanAccounts(profiles []*platform.ProfileInfo) []ScannedAccount {
	var scans []dirScan
	for _, p := range profiles {
		ds := gatherDir(p)
		if ds.LiveUUID == "" && len(ds.Buckets) == 0 {
			continue
		}
		scans = append(scans, ds)
	}
	return assembleAccounts(scans)
}
```

- [ ] **Step 7: Run the scanner test, verify it passes**

Run: `go test ./core/ -run TestScanAccounts -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add core/scan.go core/scan_test.go
git -c user.name="Vin" -c user.email="fontripdata@gmail.com" commit -m "feat(core): account scanner (dedup by UUID, completeness/ghost model)"
```

---

### Task 4: Menu filter → managed registry (`cmd/mcs-tray`)

Replace the hardcoded `Claude`/`Claude_Profile2` filter with the managed registry, with a cheap first-run fallback (show any dir that has a live login).

**Files:**
- Create: `cmd/mcs-tray/managedfilter.go`
- Modify: `cmd/mcs-tray/main.go` (the filter at the profile loop, ~`main.go:75`)
- Test: `cmd/mcs-tray/managedfilter_test.go`

**Interfaces:**
- Produces: `func menuIncludes(managed []string, folder string, hasLiveLogin, managedFlag bool) bool`

- [ ] **Step 1: Write the failing test**

```go
package main

import "testing"

func TestMenuIncludes(t *testing.T) {
	// First run (managed == nil): show dirs with a live login or MSIX-managed.
	if !menuIncludes(nil, "Claude", true, false) {
		t.Fatal("first-run: live login should show")
	}
	if menuIncludes(nil, "Claude-3p", false, false) {
		t.Fatal("first-run: no login, not managed → hide")
	}
	if !menuIncludes(nil, "Parked", false, true) {
		t.Fatal("first-run: MSIX-managed should show")
	}
	// Registry present: authoritative, ignores live-login.
	m := []string{"Claude"}
	if !menuIncludes(m, "Claude", false, false) {
		t.Fatal("registry: listed → show")
	}
	if menuIncludes(m, "Claude_Profile2", true, false) {
		t.Fatal("registry: not listed → hide even with live login")
	}
	// Present-but-empty registry hides everything (user unchecked all).
	if menuIncludes([]string{}, "Claude", true, false) {
		t.Fatal("empty registry → hide all")
	}
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./cmd/mcs-tray/ -run TestMenuIncludes -v`
Expected: FAIL (undefined: menuIncludes).

- [ ] **Step 3: Implement `cmd/mcs-tray/managedfilter.go`**

```go
package main

// menuIncludes decides whether a profile folder appears in the tray menu.
//
// When the managed registry exists (managed != nil) it is authoritative: only
// listed folders show. When it is absent (managed == nil, first run) we fall back
// to a cheap heuristic — show any dir with a live login, plus MSIX-managed
// (parked) profiles — so behavior matches the pre-rescan build until the user
// makes an explicit choice via Rescan.
func menuIncludes(managed []string, folder string, hasLiveLogin, managedFlag bool) bool {
	if managed != nil {
		for _, m := range managed {
			if m == folder {
				return true
			}
		}
		return false
	}
	return hasLiveLogin || managedFlag
}
```

- [ ] **Step 4: Run it, verify it passes**

Run: `go test ./cmd/mcs-tray/ -run TestMenuIncludes -v`
Expected: PASS.

- [ ] **Step 5: Wire it into `main.go`**

Just before the profile loop (near `main.go:74`), load the registry once:

```go
	managed := core.LoadManaged()
```

Replace the filter body (currently):

```go
		if !p.HasSessionsDir && !p.Managed && p.Name != "Claude" && p.Name != "Claude_Profile2" {
			continue
		}
```

with:

```go
		_, uErr := platform.GetProfileAccountUUID(p.Path)
		if !menuIncludes(managed, p.Name, uErr == nil, p.Managed) {
			continue
		}
```

(`platform` and `core` are already imported in `main.go`.)

- [ ] **Step 6: Build + run the full tray test package**

Run: `go build ./... && go test ./cmd/mcs-tray/ -v`
Expected: build OK; tests PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/mcs-tray/managedfilter.go cmd/mcs-tray/managedfilter_test.go cmd/mcs-tray/main.go
git -c user.name="Vin" -c user.email="fontripdata@gmail.com" commit -m "feat(tray): gate menu on managed registry with first-run fallback"
```

---

### Task 5: Rescan action + two-step UI + docs (`cmd/mcs-tray`)

The "Rescan accounts…" menu item, the review render, the multi-select pick, and the doc updates that complete the feature.

**Files:**
- Create: `cmd/mcs-tray/rescan.go`
- Modify: `cmd/mcs-tray/dialog_darwin.go` (add two helpers), `cmd/mcs-tray/dialog_other.go` and the Windows dialog file (no-op stubs), `cmd/mcs-tray/main.go` (menu item + handler)
- Modify docs: `FILELIST.md`, `CHANGELOG.md`, `README.md`, `README.zh-TW.md`
- Test: `cmd/mcs-tray/rescan_test.go`

**Interfaces:**
- Consumes: `core.ScanAccounts`, `core.ScannedAccount`, `core.SetManaged`, `core.LoadManaged`, `core.DisplayName`, `core.AccountTeam`, `relaunchSelf`, `confirmDialogMultiline`, `chooseMultipleFromList`, `platform.New`.
- Produces:
  - `func renderReviewTable(accounts []core.ScannedAccount) string` (pure)
  - `func selectablePick(accounts []core.ScannedAccount, managed []string) (labels []string, labelToFolder map[string]string, preselected []string)` (pure)
  - `func runRescan()`
  - dialog helpers `confirmDialogMultiline(message, confirmLabel string) bool`, `chooseMultipleFromList(options, preselected []string, prompt string) ([]string, bool)`

- [ ] **Step 1: Write the failing test for the pure helpers**

```go
package main

import (
	"strings"
	"testing"
	"time"

	"github.com/miou1107/multi-claude-switcher/core"
)

func TestRenderReviewTable(t *testing.T) {
	accts := []core.ScannedAccount{
		{UUID: "035899b2", Complete: true, HomeFolder: "Claude", Email: "vincent@fontrip.com",
			Account: core.AccountTeam, Convos: 395, LastUpdated: time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC),
			Note: "Team account — conversations can't be synced"},
		{UUID: "f047dab6", Complete: false, Convos: 21,
			LastUpdated: time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC), Note: "Invalid account data"},
	}
	out := renderReviewTable(accts)
	for _, want := range []string{"035899b2", "vincent@fontrip.com", "Complete", "Incomplete",
		"395", "2026-07-24", "Invalid account data", "Yes"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q in:\n%s", want, out)
		}
	}
}

func TestSelectablePick(t *testing.T) {
	accts := []core.ScannedAccount{
		{UUID: "035899b2", Complete: true, HomeFolder: "Claude", Email: "vincent@fontrip.com", Account: core.AccountTeam},
		{UUID: "ae543f88", Complete: true, HomeFolder: "Claude_Profile2", Email: "b@x.com", Account: core.AccountPersonal},
		{UUID: "f047dab6", Complete: false}, // ghost — must NOT be selectable
	}
	labels, m, pre := selectablePick(accts, []string{"Claude"})
	if len(labels) != 2 {
		t.Fatalf("want 2 selectable, got %d", len(labels))
	}
	if len(pre) != 1 || m[pre[0]] != "Claude" {
		t.Fatalf("pre-select should map to managed folder Claude: pre=%v m=%v", pre, m)
	}
	for _, l := range labels {
		if strings.Contains(l, "f047dab6") {
			t.Fatal("ghost leaked into selectable labels")
		}
	}
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./cmd/mcs-tray/ -run 'TestRenderReviewTable|TestSelectablePick' -v`
Expected: FAIL (undefined: renderReviewTable / selectablePick).

- [ ] **Step 3: Implement `cmd/mcs-tray/rescan.go`**

```go
package main

import (
	"fmt"
	"strings"

	"github.com/getlantern/systray"
	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
)

// fmtDate renders a review date, or "—" when unset.
func fmtDate(a core.ScannedAccount) string {
	if a.LastUpdated.IsZero() {
		return "—"
	}
	return a.LastUpdated.Format("2006-01-02")
}

// teamCell renders the Team column: Yes/No for a complete account, "?" when
// unknown (ghosts and unclassifiable accounts).
func teamCell(a core.ScannedAccount) string {
	if !a.Complete || a.Account == core.AccountUnknown {
		return "?"
	}
	if a.Account == core.AccountTeam {
		return "Yes"
	}
	return "No"
}

// renderReviewTable builds the step-1 review text: a header line plus one aligned
// row per account, columns UUID / Completeness / email / Team / Convos / Last
// updated / Note. Best-effort monospace alignment (macOS dialogs use a
// proportional font, so alignment is approximate — spec §5).
func renderReviewTable(accounts []core.ScannedAccount) string {
	type row struct{ uuid, comp, email, team, convos, updated, note string }
	rows := []row{{"UUID", "Status", "Email", "Team", "Chats", "Last updated", "Note"}}
	for _, a := range accounts {
		comp := "Complete"
		if !a.Complete {
			comp = "Incomplete"
		}
		email := a.Email
		if email == "" {
			email = "—"
		}
		rows = append(rows, row{
			short(a.UUID), comp, email, teamCell(a),
			fmt.Sprintf("%d", a.Convos), fmtDate(a), a.Note,
		})
	}
	// Column widths.
	w := make([]int, 7)
	for _, r := range rows {
		for i, c := range []string{r.uuid, r.comp, r.email, r.team, r.convos, r.updated, r.note} {
			if len(c) > w[i] {
				w[i] = len(c)
			}
		}
	}
	var b strings.Builder
	for ri, r := range rows {
		cells := []string{r.uuid, r.comp, r.email, r.team, r.convos, r.updated, r.note}
		for i, c := range cells {
			b.WriteString(c)
			if i < len(cells)-1 {
				b.WriteString(strings.Repeat(" ", w[i]-len(c)+2))
			}
		}
		b.WriteString("\n")
		if ri == 0 {
			b.WriteString("\n") // blank line under the header
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// short truncates a UUID to its first 8 chars for display.
func short(uuid string) string {
	if len(uuid) > 8 {
		return uuid[:8]
	}
	return uuid
}

// pickLabel builds a one-line selectable label for a complete account, unique via
// its short UUID.
func pickLabel(a core.ScannedAccount) string {
	name := a.Email
	if name == "" {
		name = core.DisplayName(a.HomeFolder)
	}
	tag := ""
	if a.Account == core.AccountTeam {
		tag = "  🏢 Team"
	}
	return fmt.Sprintf("%s%s  [%s]", name, tag, short(a.UUID))
}

// selectablePick returns the multi-select labels for complete accounts only, a
// label→folder map, and the labels to pre-select (currently-managed folders).
func selectablePick(accounts []core.ScannedAccount, managed []string) (labels []string, labelToFolder map[string]string, preselected []string) {
	managedSet := map[string]bool{}
	for _, m := range managed {
		managedSet[m] = true
	}
	labelToFolder = map[string]string{}
	for _, a := range accounts {
		if !a.Complete {
			continue
		}
		l := pickLabel(a)
		labels = append(labels, l)
		labelToFolder[l] = a.HomeFolder
		if managedSet[a.HomeFolder] {
			preselected = append(preselected, l)
		}
	}
	return labels, labelToFolder, preselected
}

// runRescan is the "Rescan accounts…" handler: scan → review → pick → persist →
// relaunch (the menu is static, so a rebuild is needed to reflect changes).
func runRescan() {
	plat := platform.New()
	profiles, err := plat.FindProfiles()
	if err != nil {
		notify("Rescan failed", err.Error())
		return
	}
	accounts := core.ScanAccounts(profiles)
	if len(accounts) == 0 {
		infoDialog("Rescan accounts", "No Claude accounts found on this machine.")
		return
	}
	if !confirmDialogMultiline(renderReviewTable(accounts), "Continue") {
		return // cancelled at review
	}
	labels, labelToFolder, preselected := selectablePick(accounts, core.LoadManaged())
	if len(labels) == 0 {
		infoDialog("Rescan accounts", "No complete (switchable) accounts to manage.")
		return
	}
	selected, ok := chooseMultipleFromList(labels, preselected, "Select the accounts to manage:")
	if !ok {
		return // cancelled at pick
	}
	var folders []string
	for _, l := range selected {
		if f, ok := labelToFolder[l]; ok {
			folders = append(folders, f)
		}
	}
	if err := core.SetManaged(folders); err != nil {
		notify("Rescan failed", err.Error())
		return
	}
	relaunchSelf()
}

// wireRescan attaches the rescan handler to its menu item.
func wireRescan(mRescan *systray.MenuItem) {
	go func() {
		for range mRescan.ClickedCh {
			go runRescan()
		}
	}()
}
```

- [ ] **Step 4: Run the pure tests, verify they pass**

Run: `go test ./cmd/mcs-tray/ -run 'TestRenderReviewTable|TestSelectablePick' -v`
Expected: PASS.

- [ ] **Step 5: Add the darwin dialog helpers to `cmd/mcs-tray/dialog_darwin.go`**

```go
// confirmDialogMultiline shows a two-button (Cancel + confirmLabel) dialog whose
// body may contain newlines (each becomes a separate AppleScript line, unlike
// confirmDialog which collapses them). Returns true only if confirmLabel is
// picked.
func confirmDialogMultiline(message, confirmLabel string) bool {
	var quoted []string
	for _, l := range strings.Split(message, "\n") {
		quoted = append(quoted, osaQuote(l))
	}
	script := "display dialog " + strings.Join(quoted, " & return & ") +
		fmt.Sprintf(` buttons {"Cancel", %s} default button %s cancel button "Cancel" with title "Multi-Claude Switcher"`,
			osaQuote(confirmLabel), osaQuote(confirmLabel))
	return exec.Command("osascript", "-e", script).Run() == nil
}

// chooseMultipleFromList shows a multi-select "choose from list" with the given
// items pre-selected. Returns the selected items and ok=false if cancelled. Items
// are newline-joined on the way back (labels never contain newlines).
func chooseMultipleFromList(options, preselected []string, prompt string) ([]string, bool) {
	quote := func(ss []string) string {
		var q []string
		for _, s := range ss {
			q = append(q, osaQuote(s))
		}
		return strings.Join(q, ", ")
	}
	defItems := ""
	if len(preselected) > 0 {
		defItems = " default items {" + quote(preselected) + "}"
	}
	script := fmt.Sprintf(`set AppleScript's text item delimiters to "\n"
set sel to choose from list {%s} with prompt %s with multiple selections allowed%s
if sel is false then
	return "__CANCELLED__"
end if
return sel as text`, quote(options), osaQuote(prompt), defItems)
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return nil, false
	}
	s := strings.TrimRight(string(out), "\n")
	if s == "__CANCELLED__" {
		return nil, false
	}
	if s == "" {
		return []string{}, true // confirmed with nothing selected
	}
	return strings.Split(s, "\n"), true
}
```

- [ ] **Step 6: Add no-op stubs for non-darwin builds**

In `cmd/mcs-tray/dialog_other.go` (and the Windows dialog file that defines `confirmDialog`), add:

```go
func confirmDialogMultiline(message, confirmLabel string) bool { return false }

func chooseMultipleFromList(options, preselected []string, prompt string) ([]string, bool) {
	return nil, false
}
```

- [ ] **Step 7: Add the menu item + handler in `main.go`**

In the Maintenance submenu (after `mUpdate`, ~`main.go:143`):

```go
	mRescan := mMaint.AddSubMenuItem("Rescan accounts…", "Scan for Claude accounts and choose which to manage")
```

Near the other handler goroutines (e.g. after the `mUpdate` handler ~`main.go:272`):

```go
	wireRescan(mRescan)
```

- [ ] **Step 8: Build all platforms + full test**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: build OK; vet clean; all tests PASS.

- [ ] **Step 9: Update docs**

- `FILELIST.md`: add `core/identity.go`, `core/identity_test.go`, `core/managed.go`, `core/managed_test.go`, `core/scan.go`, `core/scan_test.go`, `cmd/mcs-tray/managedfilter.go`, `cmd/mcs-tray/managedfilter_test.go`, `cmd/mcs-tray/rescan.go`, `cmd/mcs-tray/rescan_test.go` under the appropriate sections.
- `CHANGELOG.md`: under `## [Unreleased]` → `### Added`, add: `Rescan accounts: scan the machine for Claude accounts, review each (UUID, completeness, email, Team, conversations, last-updated), and pick which to manage. Incomplete/ghost accounts (orphaned Code sessions with no login) are shown read-only as "Invalid account data".`
- `README.md` and `README.zh-TW.md`: add a short entry describing the "Rescan accounts…" Maintenance action and that only complete (switchable) accounts can be managed.

- [ ] **Step 10: Commit**

```bash
git add cmd/mcs-tray/rescan.go cmd/mcs-tray/rescan_test.go cmd/mcs-tray/dialog_darwin.go cmd/mcs-tray/dialog_other.go cmd/mcs-tray/main.go FILELIST.md CHANGELOG.md README.md README.zh-TW.md
# plus the Windows dialog file if it was edited
git -c user.name="Vin" -c user.email="fontripdata@gmail.com" commit -m "feat(tray): Rescan accounts review-to-manage picker"
```

---

## Post-plan verification

After all tasks: `go build ./... && go vet ./... && go test ./... && go test -race ./core/ ./cmd/mcs-tray/` all green, CGO-free. Then on-device smoke test (manual, requires the user): launch `./bin/mcs-tray`, open Maintenance → Rescan accounts…, confirm the review lists the two complete accounts + the ghost, pick a subset, and verify the menu rebuilds to match.
