# Team-account Detection & Import-locked Warnings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect whether a Claude Desktop profile's logged-in account is a Team account, tag it in the tray, and warn the user whenever an action would try to import Code sessions *into* a Team account.

**Architecture:** A pure classifier (`core`) decides Team/Personal/Unknown from a profile's cached organization list. A reader (`core`, goleveldb) copies the profile's `Local Storage/leveldb`, opens it read-only, and extracts the org list. The tray caches the per-profile result, appends a `🏢 Team` tag to the profile title, and gates two warnings (Auto Sync enable, and manual sync directions targeting a Team account).

**Tech Stack:** Go 1.22, `github.com/syndtr/goleveldb` (pure Go, no CGO), `getlantern/systray`, macOS `osascript` dialogs.

## Global Constraints

- Go module: `github.com/miou1107/multi-claude-switcher`, `go 1.22`.
- Tray must stay **CGO-free** (pure Go dependencies only) so Windows builds.
- New dependency allowed: `github.com/syndtr/goleveldb` only.
- Detection is **best-effort**: on any read/parse failure or unrecognized tier → `Unknown`; never hard-block an action, never mislabel on uncertainty.
- Repo output language: **English** (code, comments, docs, commit messages).
- Commits: author is Vin; **no `Co-Authored-By` trailer**.
- Team tier allow-list (extensible): `{"default_raven"}`. Personal tier allow-list: `{"default_claude_ai","default_claude_pro","default_claude_max","default_claude_max_5x","default_claude_max_20x","auto_api_evaluation"}`.
- Reuse the existing `copyDir` helper in `core/backup.go` for copying the LevelDB dir (do not reimplement).
- Update `FILELIST.md` when adding files; update `CHANGELOG.md` under `[Unreleased]`; update both `README.md` and `README.zh-TW.md` for the user-facing behavior.

---

### Task 1: AccountType enum + pure classifier

**Files:**
- Create: `core/accounttype.go`
- Test: `core/accounttype_test.go`
- Modify: `FILELIST.md`

**Interfaces:**
- Produces: `type AccountType int` with `AccountUnknown`, `AccountPersonal`, `AccountTeam`; `func (AccountType) String() string`; `type orgInfo struct { Name, Tier, Billing string }`; `func classify(orgs []orgInfo) AccountType`; package vars `teamTiers`, `personalTiers` (`map[string]bool`).

- [ ] **Step 1: Write the failing test**

```go
// core/accounttype_test.go
package core

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		orgs []orgInfo
		want AccountType
	}{
		{"team only", []orgInfo{{Name: "Fontrip", Tier: "default_raven"}}, AccountTeam},
		{"team plus personal", []orgInfo{
			{Name: "x's Organization", Tier: "default_claude_ai"},
			{Name: "Fontrip", Tier: "default_raven"},
		}, AccountTeam},
		{"personal max", []orgInfo{{Name: "x's Organization", Tier: "default_claude_max_20x"}}, AccountPersonal},
		{"personal mix", []orgInfo{
			{Tier: "default_claude_ai"}, {Tier: "auto_api_evaluation"},
		}, AccountPersonal},
		{"empty", nil, AccountUnknown},
		{"unknown tier", []orgInfo{{Tier: "default_claude_ai"}, {Tier: "default_mystery"}}, AccountUnknown},
	}
	for _, c := range cases {
		if got := classify(c.orgs); got != c.want {
			t.Errorf("%s: classify=%v want %v", c.name, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/ -run TestClassify -v`
Expected: FAIL (build error: `AccountType`, `classify`, `orgInfo` undefined).

- [ ] **Step 3: Write minimal implementation**

```go
// core/accounttype.go
package core

// AccountType classifies a profile's logged-in Claude account for sync purposes.
// Team accounts serve their Code sidebar from an Anthropic server API, so sessions
// copied into a Team profile locally never appear (import is a no-op).
type AccountType int

const (
	AccountUnknown AccountType = iota
	AccountPersonal
	AccountTeam
)

func (t AccountType) String() string {
	switch t {
	case AccountPersonal:
		return "Personal"
	case AccountTeam:
		return "Team"
	default:
		return "Unknown"
	}
}

// orgInfo is one organization the account belongs to, as read from the account
// bootstrap payload cached in Local Storage.
type orgInfo struct {
	Name    string
	Tier    string
	Billing string
}

// teamTiers/personalTiers are explicit allow-lists of Anthropic rate-limit tier
// codenames. "raven" is the internal codename for the Team/Enterprise product.
// Unrecognized tiers deliberately fall through to Unknown (graceful under-warn).
var teamTiers = map[string]bool{
	"default_raven": true,
}

var personalTiers = map[string]bool{
	"default_claude_ai":      true,
	"default_claude_pro":     true,
	"default_claude_max":     true,
	"default_claude_max_5x":  true,
	"default_claude_max_20x": true,
	"auto_api_evaluation":    true,
}

// classify returns Team if any org is on a team tier; Personal if the (non-empty)
// list is entirely personal tiers; Unknown otherwise (empty list or any tier in
// neither allow-list).
func classify(orgs []orgInfo) AccountType {
	if len(orgs) == 0 {
		return AccountUnknown
	}
	allPersonal := true
	for _, o := range orgs {
		if teamTiers[o.Tier] {
			return AccountTeam
		}
		if !personalTiers[o.Tier] {
			allPersonal = false
		}
	}
	if allPersonal {
		return AccountPersonal
	}
	return AccountUnknown
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./core/ -run TestClassify -v`
Expected: PASS.

- [ ] **Step 5: Update FILELIST.md**

Add under the `core/` section of `FILELIST.md` (near the other `core/` entries):

```markdown
- `core/accounttype.go` — Account-type classifier: maps a profile's cached org tiers to Team / Personal / Unknown.
```

- [ ] **Step 6: Commit**

```bash
git add core/accounttype.go core/accounttype_test.go FILELIST.md
git commit -m "feat(core): add account-type classifier (Team/Personal/Unknown)"
```

---

### Task 2: Chromium value decoder + org extraction (pure)

**Files:**
- Create: `core/localstorage.go`
- Test: `core/localstorage_test.go`
- Modify: `FILELIST.md`

**Interfaces:**
- Consumes: `orgInfo` (Task 1).
- Produces: `func decodeLocalStorageValue(v []byte) string`; `func extractOrgs(decoded string) []orgInfo`.

- [ ] **Step 1: Write the failing test**

```go
// core/localstorage_test.go
package core

import "testing"

func TestDecodeLocalStorageValue(t *testing.T) {
	// Latin-1/UTF-8 form: leading byte 1, then raw bytes.
	if got := decodeLocalStorageValue([]byte{1, 'h', 'i'}); got != "hi" {
		t.Errorf("utf8 decode = %q want %q", got, "hi")
	}
	// UTF-16LE form: leading byte 0, then 16-bit little-endian code units.
	utf16le := []byte{0, 'h', 0, 'i', 0}
	if got := decodeLocalStorageValue(utf16le); got != "hi" {
		t.Errorf("utf16 decode = %q want %q", got, "hi")
	}
	if got := decodeLocalStorageValue(nil); got != "" {
		t.Errorf("empty decode = %q want \"\"", got)
	}
}

func TestExtractOrgs(t *testing.T) {
	blob := `{"account":{"organizations":[` +
		`{"name":"Fontrip","rate_limit_tier":"default_raven","billing_type":"stripe_subscription"},` +
		`{"name":"x's Organization","rate_limit_tier":"default_claude_ai","billing_type":"none"}` +
		`]}}`
	orgs := extractOrgs(blob)
	if len(orgs) != 2 {
		t.Fatalf("got %d orgs want 2: %+v", len(orgs), orgs)
	}
	if orgs[0].Name != "Fontrip" || orgs[0].Tier != "default_raven" {
		t.Errorf("org0 = %+v", orgs[0])
	}
	// Non-JSON and org-free JSON return nothing, never panic.
	if got := extractOrgs("buttery"); got != nil {
		t.Errorf("non-json = %+v want nil", got)
	}
	if got := extractOrgs(`{"foo":1}`); got != nil {
		t.Errorf("no-org json = %+v want nil", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/ -run 'TestDecodeLocalStorageValue|TestExtractOrgs' -v`
Expected: FAIL (build error: `decodeLocalStorageValue`, `extractOrgs` undefined).

- [ ] **Step 3: Write minimal implementation**

```go
// core/localstorage.go
package core

import (
	"encoding/json"
	"strings"
	"unicode/utf16"
)

// decodeLocalStorageValue decodes a Chromium Local Storage LevelDB value. The
// first byte is an encoding tag: 0 = UTF-16LE, 1 = Latin-1/UTF-8. Any other
// leading byte is returned as-is.
func decodeLocalStorageValue(v []byte) string {
	if len(v) == 0 {
		return ""
	}
	switch v[0] {
	case 0:
		u := make([]uint16, 0, len(v)/2)
		for i := 1; i+1 < len(v); i += 2 {
			u = append(u, uint16(v[i])|uint16(v[i+1])<<8)
		}
		return string(utf16.Decode(u))
	case 1:
		return string(v[1:])
	default:
		return string(v)
	}
}

// extractOrgs pulls every organization ({name, rate_limit_tier, billing_type})
// out of a decoded Local Storage value. It walks any nested JSON, collecting each
// object that has a "rate_limit_tier" field. Returns nil for non-JSON or org-free
// input (never panics).
func extractOrgs(decoded string) []orgInfo {
	var root interface{}
	if json.Unmarshal([]byte(decoded), &root) != nil {
		i := strings.IndexAny(decoded, "{[")
		if i < 0 {
			return nil
		}
		if json.Unmarshal([]byte(decoded[i:]), &root) != nil {
			return nil
		}
	}
	var out []orgInfo
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			if tier, ok := t["rate_limit_tier"].(string); ok {
				name, _ := t["name"].(string)
				billing, _ := t["billing_type"].(string)
				out = append(out, orgInfo{Name: name, Tier: tier, Billing: billing})
			}
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
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./core/ -run 'TestDecodeLocalStorageValue|TestExtractOrgs' -v`
Expected: PASS.

- [ ] **Step 5: Update FILELIST.md**

Add under the `core/` section of `FILELIST.md`:

```markdown
- `core/localstorage.go` — Chromium Local Storage value decoding + organization extraction (feeds the account-type classifier).
```

- [ ] **Step 6: Commit**

```bash
git add core/localstorage.go core/localstorage_test.go FILELIST.md
git commit -m "feat(core): decode Chromium Local Storage values and extract org list"
```

---

### Task 3: LevelDB reader + DetectAccountType (adds goleveldb)

**Files:**
- Modify: `core/accounttype.go` (append reader + public entry point)
- Modify: `go.mod`, `go.sum`
- Test: `core/accounttype_reader_test.go`

**Interfaces:**
- Consumes: `copyDir` (from `core/backup.go`), `decodeLocalStorageValue`/`extractOrgs` (Task 2), `classify`/`orgInfo`/`AccountType` (Task 1).
- Produces: `func DetectAccountType(profilePath string) (AccountType, error)`.

- [ ] **Step 1: Add the goleveldb dependency**

Run:
```bash
go get github.com/syndtr/goleveldb@v1.0.0
go mod tidy
```
Expected: `go.mod` now requires `github.com/syndtr/goleveldb v1.0.0`; `go.sum` updated. Confirm no CGO was pulled in:
```bash
go list -deps ./core/ | grep -i 'cgo\|sqlite' || echo "no cgo deps"
```
Expected: `no cgo deps`.

- [ ] **Step 2: Write the failing test**

```go
// core/accounttype_reader_test.go
package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
)

// writeFixtureLS builds a minimal Chromium-style Local Storage LevelDB under
// <profile>/Local Storage/leveldb containing one bootstrap value with the given
// org JSON, and returns the profile path.
func writeFixtureLS(t *testing.T, orgJSON string) string {
	t.Helper()
	profile := t.TempDir()
	ldbDir := filepath.Join(profile, "Local Storage", "leveldb")
	if err := os.MkdirAll(ldbDir, 0755); err != nil {
		t.Fatal(err)
	}
	db, err := leveldb.OpenFile(ldbDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	val := append([]byte{1}, []byte(orgJSON)...) // 1 = UTF-8 encoding tag
	if err := db.Put([]byte("_https://claude.ai\x00\x01bootstrap"), val, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return profile
}

func TestDetectAccountType(t *testing.T) {
	team := writeFixtureLS(t, `{"organizations":[{"name":"Fontrip","rate_limit_tier":"default_raven","billing_type":"stripe_subscription"}]}`)
	if got, err := DetectAccountType(team); err != nil || got != AccountTeam {
		t.Errorf("team: got %v err %v want Team", got, err)
	}

	personal := writeFixtureLS(t, `{"organizations":[{"name":"x's Organization","rate_limit_tier":"default_claude_max_20x","billing_type":"stripe_subscription"}]}`)
	if got, err := DetectAccountType(personal); err != nil || got != AccountPersonal {
		t.Errorf("personal: got %v err %v want Personal", got, err)
	}

	// Missing Local Storage → Unknown + error, never a guess.
	if got, err := DetectAccountType(t.TempDir()); err == nil || got != AccountUnknown {
		t.Errorf("missing LS: got %v err %v want Unknown+error", got, err)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./core/ -run TestDetectAccountType -v`
Expected: FAIL (build error: `DetectAccountType` undefined).

- [ ] **Step 4: Write minimal implementation (append to `core/accounttype.go`)**

Add these imports to the top of `core/accounttype.go` (turn the current `package core` line into a package + import block):

```go
package core

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)
```

Append the reader and public entry point:

```go
// readLocalStorageOrgs copies the profile's Local Storage LevelDB to a temp dir
// (the live store is locked while Claude runs), opens it, and extracts every
// organization from cached account payloads.
func readLocalStorageOrgs(profilePath string) ([]orgInfo, error) {
	src := filepath.Join(profilePath, "Local Storage", "leveldb")
	if _, err := os.Stat(src); err != nil {
		return nil, fmt.Errorf("local storage not found for %s: %w", profilePath, err)
	}
	tmp, err := os.MkdirTemp("", "mcs-ls-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	dst := filepath.Join(tmp, "leveldb")
	if err := copyDir(src, dst); err != nil {
		return nil, fmt.Errorf("copy local storage: %w", err)
	}

	db, err := leveldb.OpenFile(dst, &opt.Options{ReadOnly: true})
	if err != nil {
		// Some stores need write-ahead-log recovery; retry writable on the
		// throwaway copy (never touches the real profile).
		if db, err = leveldb.OpenFile(dst, nil); err != nil {
			return nil, fmt.Errorf("open leveldb: %w", err)
		}
	}
	defer db.Close()

	var orgs []orgInfo
	it := db.NewIterator(nil, nil)
	defer it.Release()
	for it.Next() {
		s := decodeLocalStorageValue(it.Value())
		if !strings.Contains(s, "rate_limit_tier") {
			continue
		}
		orgs = append(orgs, extractOrgs(s)...)
	}
	if err := it.Error(); err != nil {
		return nil, err
	}
	return orgs, nil
}

// DetectAccountType classifies the account logged into the given profile by
// reading its cached organization list. Returns AccountUnknown + error if the
// store can't be read. Unrecognized tiers are logged so the allow-lists can grow.
func DetectAccountType(profilePath string) (AccountType, error) {
	orgs, err := readLocalStorageOrgs(profilePath)
	if err != nil {
		return AccountUnknown, err
	}
	for _, o := range orgs {
		if !teamTiers[o.Tier] && !personalTiers[o.Tier] {
			log.Printf("account-type: unrecognized rate_limit_tier %q (org %q) in %s", o.Tier, o.Name, profilePath)
		}
	}
	return classify(orgs), nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./core/ -run TestDetectAccountType -v`
Expected: PASS.

- [ ] **Step 6: Run the full core suite**

Run: `go test ./core/`
Expected: PASS (no regressions).

- [ ] **Step 7: Commit**

```bash
git add core/accounttype.go core/accounttype_reader_test.go go.mod go.sum
git commit -m "feat(core): read Local Storage and detect account type via goleveldb"
```

---

### Task 4: Tray cache + Team title tag

**Files:**
- Create: `cmd/mcs-tray/accounttype.go`
- Test: `cmd/mcs-tray/accounttype_test.go`
- Modify: `cmd/mcs-tray/main.go` (menu build ~line 80, `markActive` ~line 305, startup detection)

**Interfaces:**
- Consumes: `core.AccountType`, `core.DetectAccountType`, `platform.ProfileInfo`.
- Produces: `func setAcctType(path string, t core.AccountType)`; `func getAcctType(path string) core.AccountType`; `func profileTitle(name string, t core.AccountType, current bool) string`; `func detectAccountTypes(profiles []*platform.ProfileInfo)`.

- [ ] **Step 1: Write the failing test**

```go
// cmd/mcs-tray/accounttype_test.go
package main

import (
	"testing"

	"github.com/miou1107/multi-claude-switcher/core"
)

func TestProfileTitle(t *testing.T) {
	cases := []struct {
		name    string
		t       core.AccountType
		current bool
		want    string
	}{
		{"Company", core.AccountTeam, false, "Company  🏢 Team"},
		{"Company", core.AccountTeam, true, "Company  🏢 Team  (current)"},
		{"Personal", core.AccountPersonal, false, "Personal"},
		{"Personal", core.AccountPersonal, true, "Personal  (current)"},
		{"Mystery", core.AccountUnknown, false, "Mystery"},
	}
	for _, c := range cases {
		if got := profileTitle(c.name, c.t, c.current); got != c.want {
			t.Errorf("profileTitle(%q,%v,%v)=%q want %q", c.name, c.t, c.current, got, c.want)
		}
	}
}

func TestAcctTypeCache(t *testing.T) {
	setAcctType("/p/x", core.AccountTeam)
	if got := getAcctType("/p/x"); got != core.AccountTeam {
		t.Errorf("cache get = %v want Team", got)
	}
	if got := getAcctType("/p/unknown"); got != core.AccountUnknown {
		t.Errorf("cache miss = %v want Unknown", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/mcs-tray/ -run 'TestProfileTitle|TestAcctTypeCache' -v`
Expected: FAIL (build error: `profileTitle`, `setAcctType`, `getAcctType` undefined).

- [ ] **Step 3: Write minimal implementation**

```go
// cmd/mcs-tray/accounttype.go
package main

import (
	"log"
	"sync"

	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
)

// acctTypes caches each profile's detected account type, keyed by profile path.
// Populated at startup and after every switch/sync; read by the menu build,
// markActive, and the warning gates. Never opens LevelDB inline.
var acctTypes = struct {
	sync.Mutex
	m map[string]core.AccountType
}{m: map[string]core.AccountType{}}

func setAcctType(path string, t core.AccountType) {
	acctTypes.Lock()
	acctTypes.m[path] = t
	acctTypes.Unlock()
}

func getAcctType(path string) core.AccountType {
	acctTypes.Lock()
	defer acctTypes.Unlock()
	return acctTypes.m[path] // zero value is AccountUnknown
}

// profileTitle composes a profile menu title: name, an optional "🏢 Team" tag
// for Team accounts, and an optional "(current)" marker.
func profileTitle(name string, t core.AccountType, current bool) string {
	title := name
	if t == core.AccountTeam {
		title += "  🏢 Team"
	}
	if current {
		title += "  (current)"
	}
	return title
}

// detectAccountTypes classifies every profile (best-effort) and updates the cache.
// Errors are logged and leave the profile as Unknown.
func detectAccountTypes(profiles []*platform.ProfileInfo) {
	for _, p := range profiles {
		t, err := core.DetectAccountType(p.Path)
		if err != nil {
			log.Printf("detect account type for %s: %v", p.Name, err)
		}
		setAcctType(p.Path, t)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/mcs-tray/ -run 'TestProfileTitle|TestAcctTypeCache' -v`
Expected: PASS.

- [ ] **Step 5: Use `profileTitle` in the menu build**

In `cmd/mcs-tray/main.go`, replace the parent-item creation line (currently
`parent := systray.AddMenuItem(core.DisplayName(p.Name), "")`, ~line 80) with:

```go
		parent := systray.AddMenuItem(profileTitle(core.DisplayName(p.Name), getAcctType(p.Path), false), "")
```

- [ ] **Step 6: Use `profileTitle` in `markActive`**

In `cmd/mcs-tray/main.go`, replace the body of the `for item, p := range items` loop inside `markActive` (~line 310) with:

```go
	for item, p := range items {
		current := samePath(p.Path, activePath)
		item.SetTitle(profileTitle(core.DisplayName(p.Name), getAcctType(p.Path), current))
		if current {
			item.Check()
		} else {
			item.Uncheck()
		}
	}
```

- [ ] **Step 7: Detect at startup and refresh titles**

In `cmd/mcs-tray/main.go`, immediately after the profile-marking background poller goroutine is started (after the block ending `time.Sleep(4 * time.Second)`, ~line 226), add a one-shot detection goroutine:

```go
	// Detect account types in the background (copies + reads each profile's Local
	// Storage), then refresh titles so the "🏢 Team" tag appears.
	go func() {
		detectAccountTypes(profiles)
		active, _ := plat.DetectRunningProfile()
		markActive(profileItems, active)
	}()
```

- [ ] **Step 8: Re-detect after a switch and after a manual align**

In the switch handler (`cmd/mcs-tray/main.go`, inside the `mSwitch.ClickedCh` loop, right after the successful `markActive(profileItems, target.Path)` call, ~line 172) add:

```go
					go func() { detectAccountTypes(profiles); markActive(profileItems, target.Path) }()
```

In the manual-align handler (inside the `alignItems` `m.ClickedCh` loop, right after the success `notify("Align complete", ...)` call, ~line 210) add:

```go
				go func() { detectAccountTypes(profiles); markActive(profileItems, "") }()
```

Note: `markActive` with `""` leaves the current marker off for all; the background poller re-marks the active profile within 4s. This only refreshes the Team tags after a sync.

- [ ] **Step 9: Build and run the suite**

Run: `go build ./... && go test ./cmd/mcs-tray/`
Expected: build succeeds; tests PASS.

- [ ] **Step 10: Update FILELIST.md and commit**

Add under the `cmd/mcs-tray/` section of `FILELIST.md`:

```markdown
- `cmd/mcs-tray/accounttype.go` — Tray-side account-type cache, "🏢 Team" title tag, and background detection.
```

```bash
git add cmd/mcs-tray/accounttype.go cmd/mcs-tray/accounttype_test.go cmd/mcs-tray/main.go FILELIST.md
git commit -m "feat(tray): cache account type and tag Team profiles in the menu"
```

---

### Task 5: Auto Sync enable warning mentions Team import limit

**Files:**
- Modify: `cmd/mcs-tray/autosync.go` (`askEnableAutoSync`, `toggleAutoSync`)
- Modify: `cmd/mcs-tray/main.go` (the `mAutoSync.ClickedCh` handler, ~line 269)
- Test: `cmd/mcs-tray/autosync_test.go` (add a message test)

**Interfaces:**
- Consumes: `getAcctType` (Task 4), `core.AccountType`.
- Produces: `func autoSyncWarningMessage(teamNames []string) string`; `func teamProfileNames(profiles []*platform.ProfileInfo) []string`; `toggleAutoSync` gains a `teamNames []string` parameter.

- [ ] **Step 1: Write the failing test (append to `cmd/mcs-tray/autosync_test.go`)**

```go
func TestAutoSyncWarningMessage(t *testing.T) {
	base := autoSyncWarningMessage(nil)
	if base == "" {
		t.Fatal("base message empty")
	}
	withTeam := autoSyncWarningMessage([]string{"Company"})
	if withTeam == base {
		t.Error("expected an extra Team note when a Team profile is present")
	}
	if !strings.Contains(withTeam, "Company") || !strings.Contains(withTeam, "cannot be imported") {
		t.Errorf("Team note missing details: %q", withTeam)
	}
}
```

Add `"strings"` to the test file's imports (it currently imports only `"testing"`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/mcs-tray/ -run TestAutoSyncWarningMessage -v`
Expected: FAIL (build error: `autoSyncWarningMessage` undefined).

- [ ] **Step 3: Implement the message builder and thread team names through**

In `cmd/mcs-tray/autosync.go`, add imports `"fmt"`, `"strings"`, and `"github.com/miou1107/multi-claude-switcher/platform"`, then add:

```go
// autoSyncWarningMessage builds the enable-time warning. When any profile is a
// Team account, it appends a note that Code conversations cannot be imported into
// those accounts (Auto Sync only exports out of them).
func autoSyncWarningMessage(teamNames []string) string {
	base := "With this on, every account switch bidirectionally syncs — both accounts' conversations will merge. Enable?"
	if len(teamNames) == 0 {
		return base
	}
	return base + "\n\n⚠️ " + strings.Join(teamNames, ", ") +
		" is a Team account — Code conversations cannot be imported into it. Auto Sync will only export out of it, never merge others' conversations in."
}

// teamProfileNames returns the display names of profiles detected as Team.
func teamProfileNames(profiles []*platform.ProfileInfo) []string {
	var names []string
	for _, p := range profiles {
		if getAcctType(p.Path) == core.AccountTeam {
			names = append(names, core.DisplayName(p.Name))
		}
	}
	return names
}
```

Replace `askEnableAutoSync` to take the message:

```go
// askEnableAutoSync shows the enable-time warning and returns the user's choice.
func askEnableAutoSync(teamNames []string) autoSyncChoice {
	return askEnableAutoSyncChoice(autoSyncWarningMessage(teamNames))
}
```

Change `toggleAutoSync`'s signature and its `askEnableAutoSync` call:

```go
func toggleAutoSync(m *systray.MenuItem, teamNames []string) {
```
and inside the warning branch:
```go
		switch askEnableAutoSync(teamNames) {
```

Remove the now-unused `fmt` import if it was not otherwise needed. (Verify with `go vet`.)

- [ ] **Step 4: Update the caller in `main.go`**

In `cmd/mcs-tray/main.go`, the `mAutoSync.ClickedCh` handler (~line 269) currently calls `toggleAutoSync(mAutoSync)`. Replace with:

```go
		for range mAutoSync.ClickedCh {
			toggleAutoSync(mAutoSync, teamProfileNames(shown))
			setManualDirectionsEnabled(!core.AutoSyncOnSwitch())
		}
```

(`shown` is the profile slice already computed for the Sync submenu.)

- [ ] **Step 5: Run tests + vet**

Run: `go vet ./cmd/mcs-tray/ && go test ./cmd/mcs-tray/`
Expected: vet clean; tests PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/mcs-tray/autosync.go cmd/mcs-tray/autosync_test.go cmd/mcs-tray/main.go
git commit -m "feat(tray): warn that Team accounts can't be imported when enabling Auto Sync"
```

---

### Task 6: Warn on manual sync directions targeting a Team account

**Files:**
- Modify: `cmd/mcs-tray/main.go` (align click handler ~line 195; add `confirmImportIntoTeam` helper near `confirmAlign` ~line 409)
- Test: none new (behavior is a dialog branch; covered by the `getAcctType` gate already unit-tested). Add an inline gate helper test to keep the decision testable.
- Create: `cmd/mcs-tray/importwarn_test.go`

**Interfaces:**
- Consumes: `getAcctType` (Task 4), `confirmDialog` (existing), `confirmAlign` (existing).
- Produces: `func importTargetIsTeam(dstPath string) bool`; `func confirmImportIntoTeam(dstName string) bool`.

- [ ] **Step 1: Write the failing test**

```go
// cmd/mcs-tray/importwarn_test.go
package main

import (
	"testing"

	"github.com/miou1107/multi-claude-switcher/core"
)

func TestImportTargetIsTeam(t *testing.T) {
	setAcctType("/p/team", core.AccountTeam)
	setAcctType("/p/personal", core.AccountPersonal)
	if !importTargetIsTeam("/p/team") {
		t.Error("team target should be flagged")
	}
	if importTargetIsTeam("/p/personal") {
		t.Error("personal target should not be flagged")
	}
	if importTargetIsTeam("/p/unknown") {
		t.Error("unknown target should not be flagged (no false warning)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/mcs-tray/ -run TestImportTargetIsTeam -v`
Expected: FAIL (build error: `importTargetIsTeam` undefined).

- [ ] **Step 3: Add the helpers in `main.go`**

Add near `confirmAlign` (~line 409) in `cmd/mcs-tray/main.go`:

```go
// importTargetIsTeam reports whether the sync destination is a Team account,
// whose Code sidebar is server-authoritative so a local import is a no-op.
func importTargetIsTeam(dstPath string) bool {
	return getAcctType(dstPath) == core.AccountTeam
}

// confirmImportIntoTeam warns that copying sessions into a Team account does
// nothing (the import half is a no-op), and asks whether to continue.
func confirmImportIntoTeam(dst string) bool {
	msg := fmt.Sprintf("%q is a Team account — Code conversations cannot be imported into it, so this sync's import half will do nothing. Continue anyway?", dst)
	return confirmDialog(msg, "Continue")
}
```

- [ ] **Step 4: Gate the align handler**

In `cmd/mcs-tray/main.go`, the align handler currently starts each click with:
```go
				if !confirmAlign(core.DisplayName(pr.src.Name), core.DisplayName(pr.dst.Name)) {
					log.Printf("Align %s -> %s cancelled by user.", pr.src.Name, pr.dst.Name)
					continue
				}
```
Replace that block with:
```go
				dstName := core.DisplayName(pr.dst.Name)
				confirmed := false
				if importTargetIsTeam(pr.dst.Path) {
					confirmed = confirmImportIntoTeam(dstName)
				} else {
					confirmed = confirmAlign(core.DisplayName(pr.src.Name), dstName)
				}
				if !confirmed {
					log.Printf("Align %s -> %s cancelled by user.", pr.src.Name, pr.dst.Name)
					continue
				}
```

- [ ] **Step 5: Run tests + build**

Run: `go build ./... && go test ./cmd/mcs-tray/`
Expected: build succeeds; tests PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/mcs-tray/main.go cmd/mcs-tray/importwarn_test.go
git commit -m "feat(tray): warn before a manual sync direction that imports into a Team account"
```

---

### Task 7: Docs — CHANGELOG + README (both languages)

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md`, `README.zh-TW.md`

**Interfaces:** none (documentation).

- [ ] **Step 1: Add a CHANGELOG entry**

Under `## [Unreleased]` in `CHANGELOG.md`, add an `### Added` subsection (create it if absent):

```markdown
### Added
- Tray now detects **Team** accounts (from the cached organization list) and tags them `🏢 Team` in the profile menu. Actions that would import Code sessions *into* a Team account — enabling Auto Sync, or a manual sync direction targeting a Team account — now warn that import is a no-op for Team accounts (they are export-only). Detection is best-effort; unrecognized accounts are left untagged rather than mislabeled.
```

- [ ] **Step 2: Note the behavior in `README.md`**

In the "🔄 Syncing sessions between accounts" section of `README.md`, in the existing "⚠️ Claude Team accounts are export-only" blockquote, append one line:

```markdown
>
> The tray tags a detected Team account with `🏢 Team` and warns you when an action
> would try to import into it (enabling Auto Sync, or a manual sync direction that
> targets it). Detection is best-effort — an account it can't classify is left
> untagged rather than mislabeled.
```

- [ ] **Step 3: Mirror the note in `README.zh-TW.md`**

In the corresponding "⚠️ Claude Team 帳號只能「匯出」" blockquote of `README.zh-TW.md`, append:

```markdown
>
> tray 會把偵測到的 Team 帳號標上 `🏢 Team`,並在你做「會匯入它」的動作時提醒(開 Auto Sync、或 Sync 方向指向它)。偵測是盡力而為 ── 判不出來的帳號會維持不標,不會亂標。
```

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md README.md README.zh-TW.md
git commit -m "docs: document Team-account tag and import warnings in tray"
```

---

## Self-Review

**Spec coverage:**
- §2 signal source (Local Storage LevelDB, tiers) → Tasks 1–3. ✅
- §3.1 reader (copy-then-read, value decode, extract) → Tasks 2, 3. ✅
- §3.2 classifier (team/personal allow-lists, Unknown fallback, log unknown tier) → Tasks 1, 3. ✅
- §3.3 caching/timing (startup + after switch/sync, in-memory cache) → Task 4. ✅
- §4.1 passive label coexisting with `(current)` → Task 4 (`profileTitle`, `markActive`). ✅
- §4.2.1 Auto Sync enable warning → Task 5. ✅
- §4.2.2 manual sync direction into Team → Task 6. ✅
- §5 goleveldb, no CGO, Windows path via platform layer → Task 3 (dep + CGO check); reader uses profile path so Windows works. ✅
- §6 limitations — enforced by design (Unknown fallback, allow-lists). ✅
- §7 testing (classifier table, decoder, reader smoke, warning gate) → Tasks 1, 2, 3, 5, 6. ✅

**Placeholder scan:** No TBD/TODO; every code step shows complete code. ✅

**Type consistency:** `AccountType`/`AccountTeam`/`AccountPersonal`/`AccountUnknown`, `orgInfo{Name,Tier,Billing}`, `classify`, `DetectAccountType`, `getAcctType`/`setAcctType`, `profileTitle`, `detectAccountTypes`, `autoSyncWarningMessage`, `teamProfileNames`, `toggleAutoSync(m, teamNames)`, `importTargetIsTeam`, `confirmImportIntoTeam` — names consistent across tasks. ✅
