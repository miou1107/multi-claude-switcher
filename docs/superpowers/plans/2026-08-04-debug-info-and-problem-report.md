# Debug info and problem report — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Debug info panel screen that shows exactly what MCS knows about this machine, masked, and turns it into a GitHub issue the user files themselves.

**Architecture:** A new pure package `core/diagnostics` owns masking and formatting and does no platform I/O; each host gathers its own `diagnostics.Input` the way it already gathers `SettingsVM` and hands it over. The shared renderer gains one view; both hosts gain one action each. Nothing is uploaded — the report goes to the clipboard and a prefilled GitHub issue page opens.

**Tech Stack:** Go 1.22, no new dependencies. `internal/panelui` string-concatenation HTML, WKWebView (macOS) and WebView2 (Windows) hosts.

Spec: [`docs/superpowers/specs/2026-08-04-debug-info-and-problem-report-design.md`](../specs/2026-08-04-debug-info-and-problem-report-design.md)

## Global Constraints

- Go 1.22. **No new third-party dependencies.**
- All user-facing text is English. Sentence case. No em dash (`—`) in UI strings.
- `internal/panelui` builds HTML by string concatenation with `html.EscapeString`; there is no templating engine. Both hosts must consume identical HTML.
- The JS→Go bridge passes exactly `(action string, arg string)`. Anything richer is JSON-encoded into `arg`, as `createProfileSave` already does.
- Every new file needs a `FILELIST.md` entry; user-visible changes need a `CHANGELOG.md` entry under `[Unreleased]`.
- Platform-specific code goes behind `//go:build darwin` / `//go:build windows` with a stub in the `unsupported`/`other` file, matching the existing `dialog_*.go` split.
- Verification for every task: `gofmt -l .` clean, `go build ./...`, `go vet ./...`, `go test ./... -count=1`, plus `GOOS=windows go build ./...` and `GOOS=windows go vet ./...` when Windows files change.
- **Tests must never write to the real home directory.** Redirect package-level path funcs the way `withStubbedActiveProfile` does in `core/activeprofile_test.go`.
- Nothing in this feature may perform a network request.

## File structure

| File | Responsibility |
| --- | --- |
| `core/diagnostics/mask.go` (new) | `Masker`: registration, stable pseudonyms, boundary-aware replacement, path rewriting |
| `core/diagnostics/sweep.go` (new) | Shape-based backstop for identifiers that escaped registration |
| `core/diagnostics/report.go` (new) | `Input`, `Profile`, `Build` — formatting only |
| `core/diagnostics/issue.go` (new) | `IssueURL` — title masking, capping, escaping |
| `platform/claudeversion.go` (new) | Claude Desktop version + Claude Code CLI version from a profile |
| `platform/platform.go` | `InstallKind()` added to the `Platform` interface |
| `platform/darwin.go`, `windows.go`, `unsupported.go` | `InstallKind()` per platform |
| `internal/panelui/render.go` | `.dbgbox`/`.dbgarea` CSS, `DebugVM`, `RenderDebug`, Debug info row on Settings |
| `internal/clip/clip_darwin.go`, `clip_windows.go`, `clip_other.go` (new) | `clip.Set(string) error`, shared by both hosts |
| `cmd/mcs-menubar/main.go`, `cmd/mcs-tray/panel_windows.go` | gather `Input`, `showDebug` + `reportProblem` actions, `debug` view |

---

### Task 1: macOS gets a log file at all

**Files:**
- Modify: `cmd/mcs-menubar/main.go:565-570` (`main`)
- Modify: `cmd/mcs-menubar/main.go:383-385` (`openLog` case)
- Modify: `cmd/mcs-tray/panel_windows.go:381-383` (`openLog` case)

**Interfaces:**
- Consumes: `core.SetupLogging(component string) (io.Closer, string, error)`, `core.LogDir() string` — both already exist in `core/logging.go`.
- Produces: a `~/.multi-claude-switcher/logs/mcs-menubar.log` file on macOS. Task 6 reads it.

The menu-bar process has never called `SetupLogging`, so on macOS every `log.Printf` goes to stderr and is lost. With the log a mandatory part of every report, a macOS report would carry an empty log section. Both hosts also re-derive the log path inline in `openLog` instead of calling `core.LogDir()`, so `LogDir`'s fallback is unreachable from the UI.

- [ ] **Step 1: Add logging to the menu-bar startup**

In `cmd/mcs-menubar/main.go`, replace the body of `main`:

```go
func main() {
	// Without this the menu-bar process logs to stderr only, which a bundled .app
	// discards: there was no log file on macOS at all. Diagnostics reports include
	// the log, so an unlogged host produces an empty section.
	if c, _, err := core.SetupLogging("mcs-menubar"); err == nil {
		defer func() { _ = c.Close() }()
	}
	plat = platform.New()
	switcher = core.NewSwitcher(plat, core.NewBackupManager(""))
	startUpdateChecker() // periodic background self-update
	C.RunMenuBar()
}
```

- [ ] **Step 2: Route both `openLog` cases through `core.LogDir()`**

`cmd/mcs-menubar/main.go`:

```go
	case "openLog":
		_ = exec.Command("open", core.LogDir()).Start()
```

`cmd/mcs-tray/panel_windows.go`:

```go
	case "openLog":
		_ = exec.Command("explorer.exe", core.LogDir()).Start()
```

Remove the now-unused `home, _ := os.UserHomeDir()` line from each case only. The `openBackups` and `openArchive` cases keep theirs.

- [ ] **Step 3: Build both hosts**

Run: `go build ./... && GOOS=windows go build ./... && go vet ./...`
Expected: no output. If `os` or `filepath` became unused in a file, remove the import.

- [ ] **Step 4: Verify a log file appears**

Run:
```bash
go run ./cmd/mcs-menubar &
sleep 3; kill %1
head -1 ~/.multi-claude-switcher/logs/mcs-menubar.log
```
Expected: `=== mcs-menubar v0.11.2 started (log: /Users/…/mcs-menubar.log) ===`

- [ ] **Step 5: Commit**

```bash
git add cmd/mcs-menubar/main.go cmd/mcs-tray/panel_windows.go
git commit -m "fix: give the macOS menu bar a log file

It never called SetupLogging, so every log line went to stderr, which a
bundled .app discards. There was no log on macOS to open, read or attach.

Both hosts also rebuilt the log path by hand instead of calling LogDir,
which made LogDir's fallback unreachable from the UI."
```

---

### Task 2: Masker — registration and stable pseudonyms

**Files:**
- Create: `core/diagnostics/mask.go`
- Test: `core/diagnostics/mask_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  ```go
  func NewMasker() *Masker
  func (m *Masker) RegisterAccount(uuid, email string)
  func (m *Masker) RegisterOrg(uuid string)
  func (m *Masker) RegisterWord(value, replacement string)
  func (m *Masker) Apply(s string) string
  ```
  Tasks 3, 4, 5 and 9 all use these.

One table keyed by value, never by role: a UUID registered as both an account and an organization keeps whichever pseudonym it got first, so the report never contradicts itself depending on registration order.

- [ ] **Step 1: Write the failing test**

Create `core/diagnostics/mask_test.go`:

```go
package diagnostics

import "testing"

// TestMaskerCollapsesOneAccountToOnePseudonym pins the property the whole
// report is read through: the same account is the same name wherever it turns
// up, whether it appears as an address in the summary or as a bare UUID in a
// log line thirty lines further down.
func TestMaskerCollapsesOneAccountToOnePseudonym(t *testing.T) {
	m := NewMasker()
	m.RegisterAccount("035899b2-b130-40b6-aa9e-93cf208df7b7", "vincent@fontrip.com")
	m.RegisterAccount("ae543f88-0f24-4ae6-ae21-3033915bca76", "other@example.com")

	got := m.Apply("vincent@fontrip.com switched; bucket 035899b2-b130-40b6-aa9e-93cf208df7b7 has 91 files")
	want := "account-1 switched; bucket account-1 has 91 files"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if got := m.Apply("other@example.com"); got != "account-2" {
		t.Errorf("second account = %q, want account-2", got)
	}
}

// TestMaskerNumbersInFirstSeenOrder keeps two reports from the same machine
// comparable: registration order is the report's own order, not map order.
func TestMaskerNumbersInFirstSeenOrder(t *testing.T) {
	m := NewMasker()
	m.RegisterOrg("d129c8c1-7834-4e6c-84a4-dc19dfeedc8f")
	m.RegisterOrg("245fb00c-4b74-4d8d-9ba8-3580e216ff85")
	if got := m.Apply("d129c8c1-7834-4e6c-84a4-dc19dfeedc8f"); got != "org-A" {
		t.Errorf("first org = %q, want org-A", got)
	}
	if got := m.Apply("245fb00c-4b74-4d8d-9ba8-3580e216ff85"); got != "org-B" {
		t.Errorf("second org = %q, want org-B", got)
	}
}

// TestMaskerKeepsOnePseudonymPerValue guards the ordering trap: a UUID that
// arrives in two roles must not get two names depending on which call came
// first, or the report says two different things about one thing.
func TestMaskerKeepsOnePseudonymPerValue(t *testing.T) {
	shared := "d129c8c1-7834-4e6c-84a4-dc19dfeedc8f"

	m1 := NewMasker()
	m1.RegisterAccount(shared, "")
	m1.RegisterOrg(shared)

	m2 := NewMasker()
	m2.RegisterOrg(shared)
	m2.RegisterAccount(shared, "")

	if got := m1.Apply(shared); got != "account-1" {
		t.Errorf("account-first = %q, want account-1", got)
	}
	if got := m2.Apply(shared); got != "org-A" {
		t.Errorf("org-first = %q, want org-A", got)
	}
	if m1.Apply(shared+" "+shared) != "account-1 account-1" {
		t.Error("a value must mask to one name within one report")
	}
}

// TestMaskerIgnoresEmptyRegistrations stops an unsigned-in profile, whose email
// and uuid are both "", from turning every empty string in the report into
// account-1.
func TestMaskerIgnoresEmptyRegistrations(t *testing.T) {
	m := NewMasker()
	m.RegisterAccount("", "")
	m.RegisterOrg("")
	if got := m.Apply("nothing to mask here"); got != "nothing to mask here" {
		t.Errorf("got %q, want the input unchanged", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./core/diagnostics/ -run TestMasker -v`
Expected: FAIL — the package does not exist yet (`no Go files in …/core/diagnostics`).

- [ ] **Step 3: Write the minimal implementation**

Create `core/diagnostics/mask.go`:

```go
// Package diagnostics turns what MCS knows about a machine into a report a user
// can publish. Its job is not gathering — each host does that — but masking and
// formatting, which is the part worth testing and the part that must not depend
// on which platform it runs on.
package diagnostics

import (
	"fmt"
	"sort"
	"strings"
)

// Masker replaces identifying values with stable pseudonyms.
//
// Pseudonyms rather than asterisks, because "vin***@fontrip.com" still gives up
// a name and an employer, and two occurrences of it cannot be told apart. A
// report is made of relationships — this account's conversations turned up in
// that account's folder — and a pseudonym keeps every relationship while giving
// up none of the values.
type Masker struct {
	// byValue maps a raw value to its pseudonym. One table, keyed by value and
	// never by role: a UUID that arrives as both an account and an organization
	// must not answer to two names depending on which was registered first.
	byValue  map[string]string
	accounts int
	orgs     int
}

func NewMasker() *Masker {
	return &Masker{byValue: map[string]string{}}
}

// RegisterAccount ties a UUID and an email to one pseudonym, so a log line that
// only ever mentions the UUID still reads as the same account as the summary
// line that mentions the address. Either may be empty; a profile that is not
// signed in has both.
func (m *Masker) RegisterAccount(uuid, email string) {
	name := ""
	for _, v := range []string{uuid, email} {
		if v == "" {
			continue
		}
		if existing, ok := m.byValue[v]; ok {
			name = existing
			break
		}
	}
	if name == "" {
		if uuid == "" && email == "" {
			return
		}
		m.accounts++
		name = fmt.Sprintf("account-%d", m.accounts)
	}
	m.put(uuid, name)
	m.put(email, name)
}

// RegisterOrg gives an organization UUID a letter. Letters rather than numbers
// so an org is never mistaken for an account at a glance.
func (m *Masker) RegisterOrg(uuid string) {
	if uuid == "" {
		return
	}
	if _, ok := m.byValue[uuid]; ok {
		return
	}
	m.orgs++
	m.put(uuid, "org-"+orgLetter(m.orgs))
}

// RegisterWord registers a value with a caller-chosen replacement, for the
// values that are not identifiers in their own right but give a person away all
// the same: the OS user name, the host name.
func (m *Masker) RegisterWord(value, replacement string) {
	if value == "" {
		return
	}
	m.put(value, replacement)
}

func (m *Masker) put(value, name string) {
	if value == "" {
		return
	}
	if _, ok := m.byValue[value]; ok {
		return
	}
	m.byValue[value] = name
}

// orgLetter numbers organizations A, B, … Z, AA, AB. Sequential rather than
// hashed so two reports from one machine line up.
func orgLetter(n int) string {
	out := ""
	for n > 0 {
		n--
		out = string(rune('A'+n%26)) + out
		n /= 26
	}
	return out
}

// Apply replaces every registered value in s.
//
// Longest first: an email and a UUID can share a prefix with something else
// registered, and replacing the shorter one first would leave a fragment of the
// longer one behind.
func (m *Masker) Apply(s string) string {
	if s == "" || len(m.byValue) == 0 {
		return s
	}
	for _, v := range m.sortedValues() {
		s = strings.ReplaceAll(s, v, m.byValue[v])
	}
	return s
}

func (m *Masker) sortedValues() []string {
	out := make([]string, 0, len(m.byValue))
	for v := range m.byValue {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./core/diagnostics/ -run TestMasker -v`
Expected: all four PASS.

- [ ] **Step 5: Commit**

```bash
git add core/diagnostics/mask.go core/diagnostics/mask_test.go
git commit -m "feat: mask identifiers with stable pseudonyms

One table keyed by value rather than by role, so a uuid that arrives as
both an account and an organization keeps one name instead of two that
depend on registration order."
```

---

### Task 3: Masker — word boundaries and paths

**Files:**
- Modify: `core/diagnostics/mask.go`
- Modify: `core/diagnostics/mask_test.go`

**Interfaces:**
- Consumes: Task 2's `Masker`.
- Produces:
  ```go
  func (m *Masker) RegisterBoundedWord(value, replacement string)
  func (m *Masker) RegisterHome(home, replacement string)
  ```
  Task 9 registers the OS user name and host name through these.

The home-prefix rule cannot reach `/Volumes/VincentData/Claude` or `D:\WorkData\vincentkao\`, so the user name is registered as a value in its own right. Because user names are short ordinary words, replacing them everywhere turns `administrator` into `useristrator` for a user called `admin` — so these are replaced only at a boundary.

- [ ] **Step 1: Write the failing tests**

Append to `core/diagnostics/mask_test.go`:

```go
// TestMaskerBoundedWordDoesNotEatLongerWords is the admin/administrator trap.
// A user name is a short ordinary word, so replacing it everywhere corrupts
// unrelated text, and corrupted text in a bug report is worse than absent text.
func TestMaskerBoundedWordDoesNotEatLongerWords(t *testing.T) {
	m := NewMasker()
	m.RegisterBoundedWord("admin", "user")

	cases := []struct{ in, want string }{
		{"administrator rights", "administrator rights"},
		{"the admin account", "the user account"},
		{"/Volumes/Data/admin/Claude", "/Volumes/Data/user/Claude"},
		{`D:\WorkData\admin\Claude`, `D:\WorkData\user\Claude`},
		{"admin", "user"},
		{"badmin", "badmin"},
		{"admin@example.com", "user@example.com"},
	}
	for _, c := range cases {
		if got := m.Apply(c.in); got != c.want {
			t.Errorf("Apply(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMaskerRewritesTheHomePrefix keeps the tail of a path, because which
// folder inside the profile a file landed in is usually the whole bug.
func TestMaskerRewritesTheHomePrefix(t *testing.T) {
	m := NewMasker()
	m.RegisterHome("/Users/vincentkao", "~")

	in := "backup /Users/vincentkao/Library/Application Support/Claude/config.json"
	want := "backup ~/Library/Application Support/Claude/config.json"
	if got := m.Apply(in); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestMaskerRewritesTheHomePrefixWithMixedSeparators covers what Windows
// actually emits: one string carrying both spellings, because a Go-built path
// and a command line reported by the OS meet in the same log line.
func TestMaskerRewritesTheHomePrefixWithMixedSeparators(t *testing.T) {
	m := NewMasker()
	m.RegisterHome(`C:\Users\Adam`, "%USERPROFILE%")

	cases := []struct{ in, want string }{
		{`C:\Users\Adam\AppData\Roaming\Claude`, `%USERPROFILE%\AppData\Roaming\Claude`},
		{`C:\Users\Adam/AppData/Roaming/Claude`, `%USERPROFILE%/AppData/Roaming/Claude`},
		{`c:\users\adam\AppData`, `%USERPROFILE%\AppData`},
	}
	for _, c := range cases {
		if got := m.Apply(c.in); got != c.want {
			t.Errorf("Apply(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./core/diagnostics/ -run TestMasker -v`
Expected: FAIL — `m.RegisterBoundedWord undefined`, `m.RegisterHome undefined`.

- [ ] **Step 3: Implement**

In `core/diagnostics/mask.go`, add `regexp` to the imports, add two fields to `Masker`, and add the code below.

```go
type Masker struct {
	byValue  map[string]string
	bounded  []boundedRule
	homes    []homeRule
	accounts int
	orgs     int
}

type boundedRule struct {
	re   *regexp.Regexp
	with string
}

type homeRule struct {
	re   *regexp.Regexp
	with string
}

// RegisterBoundedWord replaces value only where it stands on its own — bounded
// by a separator, not by a letter or digit.
//
// The OS user name has to be masked as a value, because the home-prefix rule
// cannot reach /Volumes/…/<user>/… or D:\WorkData\<user>\…. But user names are
// short ordinary words, and replacing "admin" everywhere turns "administrator"
// into "useristrator". A boundary is the difference between a masked report and
// a corrupted one.
func (m *Masker) RegisterBoundedWord(value, replacement string) {
	if value == "" {
		return
	}
	m.bounded = append(m.bounded, boundedRule{
		re:   regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}])` + regexp.QuoteMeta(value) + `($|[^\p{L}\p{N}])`),
		with: "${1}" + replacement + "${2}",
	})
}

// RegisterHome rewrites a home directory prefix, keeping everything after it.
// Case-insensitive and separator-blind, because Windows reports both spellings
// and mixes them inside one string.
func (m *Masker) RegisterHome(home, replacement string) {
	if home == "" {
		return
	}
	pat := regexp.QuoteMeta(home)
	// Match either separator wherever the registered prefix has one.
	pat = strings.ReplaceAll(pat, `\\`, `[\\/]`)
	pat = strings.ReplaceAll(pat, `/`, `[\\/]`)
	m.homes = append(m.homes, homeRule{
		re:   regexp.MustCompile(`(?i)` + pat),
		with: replacement,
	})
}
```

Then rewrite `Apply` so the ordering is explicit — exact values first (they are the most specific), then home prefixes, then bounded words (the least specific, and the ones most likely to fire inside a path the earlier rules already handled):

```go
func (m *Masker) Apply(s string) string {
	if s == "" {
		return s
	}
	for _, v := range m.sortedValues() {
		s = strings.ReplaceAll(s, v, m.byValue[v])
	}
	for _, h := range m.homes {
		s = h.re.ReplaceAllString(s, h.with)
	}
	for _, b := range m.bounded {
		// Twice: adjacent matches share the separator the pattern consumes, so a
		// single pass leaves the second of "…/admin/admin/…" behind.
		s = b.re.ReplaceAllString(s, b.with)
		s = b.re.ReplaceAllString(s, b.with)
	}
	return s
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./core/diagnostics/ -run TestMasker -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add core/diagnostics/mask.go core/diagnostics/mask_test.go
git commit -m "feat: mask user names at word boundaries, and homes on both separators

The home-prefix rule cannot reach a path outside the home directory, so
the user name is masked as a value too. Because it is a short ordinary
word, replacing it everywhere would turn administrator into
useristrator, so it is replaced only at a boundary."
```

---

### Task 4: The sweep

**Files:**
- Create: `core/diagnostics/sweep.go`
- Test: `core/diagnostics/sweep_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func Sweep(s string) string` and `const UnregisteredMarker = "[redacted: unregistered]"`. Task 6 runs `Sweep` last inside `Build`; Task 6's tests assert its absence.

Registration masks what someone thought to register. Every leak found in review had that shape — not a rule written wrongly, but a place nobody had thought of. The sweep matches by shape instead of by list, so an unregistered identifier is caught by what it looks like.

- [ ] **Step 1: Write the failing test**

Create `core/diagnostics/sweep_test.go`:

```go
package diagnostics

import (
	"strings"
	"testing"
)

// TestSweepCatchesWhatRegistrationMissed is the backstop's whole point: a value
// nobody registered still must not reach a public issue.
func TestSweepCatchesWhatRegistrationMissed(t *testing.T) {
	cases := []struct{ name, in string }{
		{"an address in a log line", "2026/08/04 10:50 signed in as stranger@example.com"},
		{"a bare uuid", "bucket 6c7b2c78-0d0a-4ab6-bffa-e9e6fe671d61 has 12 files"},
		{"a uuid inside a path", "open ~/sessions/6c7b2c78-0d0a-4ab6-bffa-e9e6fe671d61/x.json"},
		{"an uppercase uuid", "ORG 6C7B2C78-0D0A-4AB6-BFFA-E9E6FE671D61"},
	}
	for _, c := range cases {
		got := Sweep(c.in)
		if !strings.Contains(got, UnregisteredMarker) {
			t.Errorf("%s: Sweep(%q) = %q, want it redacted", c.name, c.in, got)
		}
		if strings.Contains(got, "stranger@example.com") || strings.Contains(strings.ToLower(got), "6c7b2c78-0d0a-4ab6-bffa-e9e6fe671d61") {
			t.Errorf("%s: the value survived: %q", c.name, got)
		}
	}
}

// TestSweepLeavesTheReportAlone guards against the backstop eating the report.
// Pseudonyms, versions, paths and counts must all survive it.
func TestSweepLeavesTheReportAlone(t *testing.T) {
	in := `MCS 0.11.2 · macOS 15.5 · arm64
Claude Desktop 1.24012.11 · standalone
claude-code 2.1.219
  Claude_Profile2 — account-2
    org-B · 95 convos
2026/08/04 10:50:12 [Safe Switch] ~/…/Claude to ~/…/Claude_test`
	if got := Sweep(in); got != in {
		t.Errorf("sweep changed a clean report:\n%q", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./core/diagnostics/ -run TestSweep -v`
Expected: FAIL — `undefined: Sweep`, `undefined: UnregisteredMarker`.

- [ ] **Step 3: Implement**

Create `core/diagnostics/sweep.go`:

```go
package diagnostics

import "regexp"

// UnregisteredMarker replaces an identifier the masker never knew about.
//
// It names the failure rather than hiding it. A silent "[redacted]" would keep
// the user safe and let the missing rule live forever; this one is asserted
// against in the tests, so forgetting to register a new field turns the suite
// red instead of turning up in a public issue.
const UnregisteredMarker = "[redacted: unregistered]"

var (
	// Deliberately loose. A false positive costs one line of a bug report; a
	// false negative costs someone their address in a search index.
	emailShape = regexp.MustCompile(`[\p{L}\p{N}._%+\-]+@[\p{L}\p{N}.\-]+\.[\p{L}]{2,}`)
	uuidShape  = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
)

// Sweep is the backstop, not the mechanism.
//
// Registration only masks what someone thought to register, so a field added
// later leaks by default with nothing to say so. Sweep matches by shape instead:
// anything still looking like an address or a uuid after masking is a value that
// escaped registration.
//
// A swept value loses its identity — two occurrences can no longer be recognised
// as the same thing — which is exactly why it is a last resort and not a
// substitute for registering properly.
func Sweep(s string) string {
	s = emailShape.ReplaceAllString(s, UnregisteredMarker)
	return uuidShape.ReplaceAllString(s, UnregisteredMarker)
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./core/diagnostics/ -count=1 -v`
Expected: all PASS, including Task 2 and 3's.

- [ ] **Step 5: Commit**

```bash
git add core/diagnostics/sweep.go core/diagnostics/sweep_test.go
git commit -m "feat: sweep the report for identifiers registration missed

Registration masks what someone thought of, so a field added later leaks
by default and nothing says so. The sweep matches by shape and marks a
hit as unregistered rather than quietly redacting it, so a missing rule
fails a test instead of reaching a public issue."
```

---

### Task 5: Claude Desktop and Claude Code versions, and the install kind

**Files:**
- Create: `platform/claudeversion.go`
- Test: `platform/claudeversion_test.go`
- Modify: `platform/platform.go` (add `InstallKind()` to the `Platform` interface)
- Modify: `platform/darwin.go`, `platform/windows.go`, `platform/unsupported.go`

**Interfaces:**
- Consumes: `GetProfileConfigPath(profilePath string) string` (`platform/platform.go:141`).
- Produces:
  ```go
  func GetProfileClaudeVersion(profilePath string) (string, error)
  func GetProfileClaudeCodeVersion(profilePath string) (string, error)
  // on the Platform interface:
  InstallKind() string // "standalone" | "store" | "macos" | "unsupported"
  ```
  Task 9 and Task 10 call all three.

`updaterLastSeenVersion` is chosen over reading the installed app because `config.json` is already being read for two other keys, and it is the same value on every platform. Measured on a real machine: `1.24012.11` in both profiles, matching `/Applications/Claude.app`'s `CFBundleShortVersionString`.

- [ ] **Step 1: Write the failing test**

Create `platform/claudeversion_test.go`:

```go
package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetProfileClaudeVersion(t *testing.T) {
	t.Run("reads the updater's last seen version", func(t *testing.T) {
		p := t.TempDir()
		if err := os.WriteFile(GetProfileConfigPath(p),
			[]byte(`{"lastKnownAccountUuid":"x","updaterLastSeenVersion":"1.24012.11"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := GetProfileClaudeVersion(p)
		if err != nil {
			t.Fatalf("GetProfileClaudeVersion: %v", err)
		}
		if got != "1.24012.11" {
			t.Errorf("got %q, want 1.24012.11", got)
		}
	})

	t.Run("a config without the key is reported, not guessed", func(t *testing.T) {
		p := t.TempDir()
		if err := os.WriteFile(GetProfileConfigPath(p), []byte(`{"lastKnownAccountUuid":"x"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, err := GetProfileClaudeVersion(p); err == nil {
			t.Errorf("got %q with no error; an absent version must be reported", got)
		}
	})

	t.Run("no config.json", func(t *testing.T) {
		if _, err := GetProfileClaudeVersion(t.TempDir()); err == nil {
			t.Error("a profile with no config.json must report an error")
		}
	})
}

func TestGetProfileClaudeCodeVersion(t *testing.T) {
	t.Run("the newest version directory wins", func(t *testing.T) {
		p := t.TempDir()
		for _, v := range []string{"2.1.9", "2.1.219", "2.0.1"} {
			if err := os.MkdirAll(filepath.Join(p, "claude-code", v), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		got, err := GetProfileClaudeCodeVersion(p)
		if err != nil {
			t.Fatalf("GetProfileClaudeCodeVersion: %v", err)
		}
		// Numeric, not lexical: "2.1.9" sorts after "2.1.219" as text.
		if got != "2.1.219" {
			t.Errorf("got %q, want 2.1.219", got)
		}
	})

	t.Run("no claude-code directory", func(t *testing.T) {
		if _, err := GetProfileClaudeCodeVersion(t.TempDir()); err == nil {
			t.Error("a profile with no CLI must report an error, not an empty version")
		}
	})
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./platform/ -run "TestGetProfileClaude" -v`
Expected: FAIL — `undefined: GetProfileClaudeVersion`.

- [ ] **Step 3: Implement**

Create `platform/claudeversion.go`:

```go
package platform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// GetProfileClaudeVersion reads which Claude Desktop version a profile last saw.
//
// Read from config.json rather than from the installed app, because config.json
// is already being read for the account and the organization, and the key is the
// same on every platform — where the app itself is an Info.plist on macOS, a
// versioned directory name on the Windows standalone build, and a package
// identity on the Store build.
func GetProfileClaudeVersion(profilePath string) (string, error) {
	data, err := os.ReadFile(GetProfileConfigPath(profilePath))
	if err != nil {
		return "", fmt.Errorf("read config.json: %w", err)
	}
	var cfg struct {
		UpdaterLastSeenVersion string `json:"updaterLastSeenVersion"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parse config.json: %w", err)
	}
	if cfg.UpdaterLastSeenVersion == "" {
		return "", fmt.Errorf("no updaterLastSeenVersion in %s", GetProfileConfigPath(profilePath))
	}
	return cfg.UpdaterLastSeenVersion, nil
}

// GetProfileClaudeCodeVersion reads the bundled CLI's version, which Claude
// Desktop records only as a directory name: <profile>/claude-code/<version>/.
// More than one can be present after an update, so the newest wins.
func GetProfileClaudeCodeVersion(profilePath string) (string, error) {
	dir := filepath.Join(profilePath, "claude-code")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dir, err)
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "" && e.Name()[0] != '.' {
			versions = append(versions, e.Name())
		}
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("no version directory under %s", dir)
	}
	sort.Slice(versions, func(i, j int) bool { return lessVersion(versions[j], versions[i]) })
	return versions[0], nil
}

// lessVersion compares dotted versions component by component as numbers, so
// 2.1.9 sorts below 2.1.219 instead of above it the way text would.
func lessVersion(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		ai, bi := versionPart(as, i), versionPart(bs, i)
		if ai != bi {
			return ai < bi
		}
	}
	return a < b
}

func versionPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, err := strconv.Atoi(parts[i])
	if err != nil {
		return 0
	}
	return n
}
```

- [ ] **Step 4: Add `InstallKind` to the interface and all three platforms**

In `platform/platform.go`, add to the `Platform` interface:

```go
	// InstallKind names which Claude Desktop install this machine has, for bug
	// reports: "standalone", "store", "macos". The two Windows builds behave
	// differently enough that a report which does not say which one it is cannot
	// be acted on.
	InstallKind() string
```

`platform/darwin.go`:

```go
func (d *DarwinPlatform) InstallKind() string { return "macos" }
```

`platform/windows.go`:

```go
func (w *WindowsPlatform) InstallKind() string {
	if w.isMSIX() {
		return "store"
	}
	return "standalone"
}
```

`platform/unsupported.go`:

```go
func (u *UnsupportedPlatform) InstallKind() string { return "unsupported" }
```

Match the receiver names already used in each file. If `core` has a mock platform used by tests (`core/switch_test.go`'s `mockPlatform`), add the same method there returning `"macos"`.

- [ ] **Step 5: Run the tests**

Run: `go test ./... -count=1 && GOOS=windows go build ./... && GOOS=windows go vet ./...`
Expected: all PASS, both builds clean. A missing `InstallKind` on a mock shows up here as a compile error.

- [ ] **Step 6: Commit**

```bash
git add platform/claudeversion.go platform/claudeversion_test.go platform/platform.go platform/darwin.go platform/windows.go platform/unsupported.go core/switch_test.go
git commit -m "feat: read the Claude Desktop and CLI versions, and name the install

Both come from the profile directory, which is already being read, rather
than from the installed app, whose shape differs on all three targets.
Version directories are compared numerically so 2.1.9 sorts below 2.1.219."
```

---

### Task 6: Build the report

**Files:**
- Create: `core/diagnostics/report.go`
- Test: `core/diagnostics/report_test.go`

**Interfaces:**
- Consumes: `NewMasker`, `RegisterAccount`, `RegisterOrg`, `RegisterBoundedWord`, `RegisterHome`, `Apply` (Tasks 2–3); `Sweep`, `UnregisteredMarker` (Task 4).
- Produces:
  ```go
  type Profile struct {
      Folder, AccountUUID, Email, OrgUUID, Path string
      SignedIn, Running bool
      Convos int
  }
  type Input struct {
      Version, OS, Arch, OSVersion, Install string
      ClaudeVer, ClaudeCodeVer string
      ClaudeVerErr, ClaudeCodeVerErr string
      AutoSync, LoginItem bool
      Profiles []Profile
      ActiveRecord string
      Home, UserName, HostName string
      HomeReplacement string
      LogDir string
  }
  func Build(in Input) string
  ```
  Tasks 9 and 10 populate `Input`; Task 8 consumes `Build`'s output.

- [ ] **Step 1: Write the failing test**

Create `core/diagnostics/report_test.go`:

```go
package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fullInput(t *testing.T) Input {
	t.Helper()
	logDir := t.TempDir()
	body := "2026/08/04 10:50:12 [Safe Switch] from /Users/vincentkao/Library/Application Support/Claude\n" +
		"2026/08/04 10:50:13 account 035899b2-b130-40b6-aa9e-93cf208df7b7 (vincent@fontrip.com)\n"
	if err := os.WriteFile(filepath.Join(logDir, "mcs-tray.log"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Input{
		Version: "0.11.2", OS: "darwin", Arch: "arm64", OSVersion: "15.5", Install: "macos",
		ClaudeVer: "1.24012.11", ClaudeCodeVer: "2.1.219",
		AutoSync: true, LoginItem: true,
		Profiles: []Profile{
			{Folder: "Claude", AccountUUID: "035899b2-b130-40b6-aa9e-93cf208df7b7",
				Email: "vincent@fontrip.com", OrgUUID: "d129c8c1-7834-4e6c-84a4-dc19dfeedc8f",
				Path: "/Users/vincentkao/Library/Application Support/Claude",
				SignedIn: true, Running: true, Convos: 252},
			{Folder: "Claude_Profile2", AccountUUID: "ae543f88-0f24-4ae6-ae21-3033915bca76",
				Email: "ft@example.com", OrgUUID: "245fb00c-4b74-4d8d-9ba8-3580e216ff85",
				Path: "/Users/vincentkao/Library/Application Support/Claude_Profile2",
				SignedIn: true, Convos: 95},
		},
		ActiveRecord:    "Claude",
		Home:            "/Users/vincentkao",
		HomeReplacement: "~",
		UserName:        "vincentkao",
		HostName:        "Vins-MacBook-Pro.local",
		LogDir:          logDir,
	}
}

// TestBuildLeavesNothingUnregistered is the regression test the sweep exists
// for: add a field to the report, forget to register its identifiers, and this
// goes red rather than a user's address turning up in a public issue.
func TestBuildLeavesNothingUnregistered(t *testing.T) {
	got := Build(fullInput(t))
	if strings.Contains(got, UnregisteredMarker) {
		t.Errorf("report carries an unregistered identifier:\n%s", got)
	}
}

// TestBuildMasksEverySurface walks the leaks found in review, one assertion
// each, because each was a place nobody had thought of rather than a rule
// written wrongly.
func TestBuildMasksEverySurface(t *testing.T) {
	in := fullInput(t)
	in.Profiles[1].Folder = "vincent@fontrip.com"          // a folder named after an address
	in.ClaudeVerErr = "open /Users/vincentkao/Library/Application Support/Claude/config.json: permission denied"
	got := Build(in)

	for _, leak := range []string{
		"vincent@fontrip.com",
		"035899b2-b130-40b6-aa9e-93cf208df7b7",
		"d129c8c1-7834-4e6c-84a4-dc19dfeedc8f",
		"/Users/vincentkao",
		"vincentkao",
		"Vins-MacBook-Pro",
	} {
		if strings.Contains(got, leak) {
			t.Errorf("%q survived into the report:\n%s", leak, got)
		}
	}
	for _, keep := range []string{"account-1", "org-A", "0.11.2", "1.24012.11", "252"} {
		if !strings.Contains(got, keep) {
			t.Errorf("%q missing from the report:\n%s", keep, got)
		}
	}
	// The error still has to be readable as an error.
	if !strings.Contains(got, "permission denied") {
		t.Errorf("the reason for an unknown field was dropped:\n%s", got)
	}
}

// TestBuildReportsPathShapeWithoutValues is what replaces an unmask switch: a
// bug caused by the shape of a path stays diagnosable without the path.
func TestBuildReportsPathShapeWithoutValues(t *testing.T) {
	in := fullInput(t)
	in.Home = "/Users/張小明"
	in.UserName = "張小明"
	got := Build(in)
	if !strings.Contains(got, "non-ASCII: yes") {
		t.Errorf("a non-ASCII home must be reported as a property:\n%s", got)
	}
	if strings.Contains(got, "張小明") {
		t.Errorf("the value leaked while reporting its shape:\n%s", got)
	}
}

// TestBuildAdmitsWhatItCouldNotRead keeps a gap visible instead of letting an
// absent field read as an absent problem.
func TestBuildAdmitsWhatItCouldNotRead(t *testing.T) {
	in := fullInput(t)
	in.ClaudeVer, in.ClaudeVerErr = "", "no updaterLastSeenVersion in config.json"
	in.ClaudeCodeVer, in.ClaudeCodeVerErr = "", "no version directory"
	got := Build(in)
	if !strings.Contains(got, "unknown (no updaterLastSeenVersion in config.json)") {
		t.Errorf("an unreadable field must say why:\n%s", got)
	}
}

// TestBuildTruncatesTheLogAndSaysSo stops a 40 MB log from becoming the report.
func TestBuildTruncatesTheLogAndSaysSo(t *testing.T) {
	in := fullInput(t)
	var b strings.Builder
	for i := 0; i < 500; i++ {
		b.WriteString("2026/08/04 10:50:12 line\n")
	}
	if err := os.WriteFile(filepath.Join(in.LogDir, "mcs-tray.log"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Build(in)
	if !strings.Contains(got, "mcs-tray.log (last 200 lines)") {
		t.Errorf("the log section must head each file and say it is truncated:\n%s", got)
	}
	if n := strings.Count(got, "10:50:12 line"); n != 200 {
		t.Errorf("kept %d lines, want 200", n)
	}
}

// TestBuildNamesAMissingLogRatherThanOmittingIt stops a report with no log from
// looking like a run with no activity.
func TestBuildNamesAMissingLogRatherThanOmittingIt(t *testing.T) {
	in := fullInput(t)
	in.LogDir = filepath.Join(t.TempDir(), "gone")
	got := Build(in)
	if !strings.Contains(got, "no log files") {
		t.Errorf("an absent log directory must be stated:\n%s", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./core/diagnostics/ -run TestBuild -v`
Expected: FAIL — `undefined: Build`, `undefined: Input`.

- [ ] **Step 3: Implement**

Create `core/diagnostics/report.go`:

```go
package diagnostics

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// logTailLines is how much of each log file the report carries. Enough to hold
// a switch, its sync and whatever went wrong after; short enough that the report
// still fits in a clipboard and a reader's patience.
const logTailLines = 200

// Profile is one account's worth of what a report needs. Raw values: masking
// happens here, once, rather than at every call site that fills this in.
type Profile struct {
	Folder      string
	AccountUUID string
	Email       string
	OrgUUID     string
	Path        string
	SignedIn    bool
	Running     bool
	Convos      int
}

// Input is everything a host gathers. Deliberately plain data: the hosts differ
// in how they find it, and none of that difference belongs in here.
type Input struct {
	Version   string
	OS        string
	Arch      string
	OSVersion string
	Install   string

	ClaudeVer        string
	ClaudeVerErr     string
	ClaudeCodeVer    string
	ClaudeCodeVerErr string

	AutoSync  bool
	LoginItem bool

	Profiles     []Profile
	ActiveRecord string

	Home            string
	HomeReplacement string
	UserName        string
	HostName        string

	LogDir string
}

// NewMaskerFor builds the masker Build uses.
//
// Exported because the user's own comment and the issue title have to be masked
// with the same registrations: a user pastes the error they saw, and the error
// they saw names their account. A fresh masker there would know nothing and mask
// nothing.
func NewMaskerFor(in Input) *Masker {
	m := NewMasker()
	m.RegisterHome(in.Home, in.HomeReplacement)
	for _, p := range in.Profiles {
		m.RegisterAccount(p.AccountUUID, p.Email)
		m.RegisterOrg(p.OrgUUID)
	}
	// After the accounts, so an address that is also a user name reads as the
	// account it belongs to rather than as "user".
	m.RegisterBoundedWord(in.UserName, "user")
	m.RegisterBoundedWord(in.HostName, "host")
	return m
}

// Build renders the report. Every string that reaches the output goes through
// the masker, and the whole thing goes through the sweep last.
func Build(in Input) string {
	m := NewMaskerFor(in)

	var b strings.Builder
	w := func(format string, args ...any) {
		fmt.Fprintf(&b, format+"\n", args...)
	}

	w("MCS %s · %s %s · %s", in.Version, in.OS, in.OSVersion, in.Arch)
	w("Claude Desktop %s · %s", orUnknown(in.ClaudeVer, in.ClaudeVerErr, m), in.Install)
	w("claude-code %s", orUnknown(in.ClaudeCodeVer, in.ClaudeCodeVerErr, m))
	w("Auto sync on switch: %s · Login item: %s", onOff(in.AutoSync), onOff(in.LoginItem))
	w("%s", pathShape(in.Home))
	w("")

	w("Profiles (%d)", len(in.Profiles))
	for _, p := range in.Profiles {
		state := ""
		switch {
		case !p.SignedIn:
			state = " — not signed in"
		case p.Running:
			state = " — " + m.Apply(p.Email) + " — running"
		default:
			state = " — " + m.Apply(p.Email)
		}
		w("  %s%s", m.Apply(p.Folder), state)
		if p.SignedIn {
			w("    %s · %d convos", m.Apply(p.OrgUUID), p.Convos)
		}
	}
	w("Active record: %s", orNone(m.Apply(in.ActiveRecord)))
	w("")

	b.WriteString(logSections(in.LogDir, m))

	return Sweep(b.String())
}

func orUnknown(value, reason string, m *Masker) string {
	if value != "" {
		return value
	}
	if reason == "" {
		return "unknown"
	}
	// The reason is masked like everything else: *os.PathError prints the
	// absolute path it failed on, so an unmasked reason reintroduces exactly what
	// the field beside it removed.
	return "unknown (" + m.Apply(reason) + ")"
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// pathShape states what a path is like without saying what it is.
//
// This is what stands in for an unmask switch. A bug caused by the shape of a
// path — a non-ASCII user name, a space, an unusual length — is invisible once
// the path is a pseudonym, and those bugs are common. Nearly all of them are
// breaking on a property that can simply be stated.
func pathShape(home string) string {
	nonASCII := false
	for _, r := range home {
		if r > unicode.MaxASCII {
			nonASCII = true
			break
		}
	}
	return fmt.Sprintf("Home path: %d chars, non-ASCII: %s, spaces: %s",
		len([]rune(home)), yesNo(nonASCII), yesNo(strings.ContainsRune(home, ' ')))
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// logSections renders the tail of every log file, one headed section each, so
// two components' lines are never read as one stream.
func logSections(dir string, m *Masker) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "Logs: no log files (" + m.Apply(err.Error()) + ")\n"
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return "Logs: no log files in " + m.Apply(dir) + "\n"
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "%s (last %d lines)\n", name, logTailLines)
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			// Named rather than omitted, so a report never silently looks like a
			// run with no activity.
			fmt.Fprintf(&b, "  unreadable (%s)\n\n", m.Apply(err.Error()))
			continue
		}
		for _, line := range tail(string(data), logTailLines) {
			b.WriteString(m.Apply(line) + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func tail(s string, n int) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./core/diagnostics/ -count=1 -v`
Expected: all PASS. If `TestBuildLeavesNothingUnregistered` fails, a value is reaching the output without being registered — register it rather than loosening the sweep.

- [ ] **Step 5: Commit**

```bash
git add core/diagnostics/report.go core/diagnostics/report_test.go
git commit -m "feat: build the diagnostics report

Everything that reaches the output is masked, including the reasons
attached to fields that could not be read: os.PathError quotes the
absolute path it failed on, so an unmasked reason undoes the field beside
it. Path shape is reported without the path, which is what a bug caused
by a non-ASCII or spaced path needs and what an unmask switch would
otherwise have been for."
```

---

### Task 7: The issue URL

**Files:**
- Create: `core/diagnostics/issue.go`
- Test: `core/diagnostics/issue_test.go`

**Interfaces:**
- Consumes: `Masker` (Task 2–3).
- Produces: `func IssueURL(comment string, m *Masker) string`. Tasks 9 and 10 call it.

The report itself travels by clipboard: a prefilled issue URL is limited to roughly 8 KB and 200 log lines do not fit. The URL carries a title and a paste instruction only.

- [ ] **Step 1: Write the failing test**

Create `core/diagnostics/issue_test.go`:

```go
package diagnostics

import (
	"net/url"
	"strings"
	"testing"
)

func TestIssueURL(t *testing.T) {
	m := NewMasker()
	m.RegisterAccount("", "vincent@fontrip.com")

	t.Run("the first line of the comment becomes the title", func(t *testing.T) {
		u := IssueURL("Switching closed my other account\nIt happened twice", m)
		if got := titleOf(t, u); got != "Switching closed my other account" {
			t.Errorf("title = %q", got)
		}
	})

	t.Run("an empty comment still has a title", func(t *testing.T) {
		if got := titleOf(t, IssueURL("   \n  ", m)); got != "Problem report" {
			t.Errorf("title = %q, want Problem report", got)
		}
	})

	t.Run("the title is masked", func(t *testing.T) {
		u := IssueURL("fails for vincent@fontrip.com", m)
		if strings.Contains(u, "fontrip") {
			t.Errorf("an address reached the url: %s", u)
		}
		if got := titleOf(t, u); got != "fails for account-1" {
			t.Errorf("title = %q", got)
		}
	})

	t.Run("a long comment cannot run away with the url", func(t *testing.T) {
		u := IssueURL(strings.Repeat("very long ", 500), m)
		if len(u) > 8000 {
			t.Errorf("url is %d bytes, want under 8000", len(u))
		}
		if n := len([]rune(titleOf(t, u))); n > 80 {
			t.Errorf("title is %d runes, want at most 80", n)
		}
	})

	t.Run("punctuation cannot break the url", func(t *testing.T) {
		u := IssueURL(`sync & switch: "why?" #3`, m)
		parsed, err := url.Parse(u)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if got := parsed.Query().Get("title"); got != `sync & switch: "why?" #3` {
			t.Errorf("title round-tripped as %q", got)
		}
	})
}

func titleOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u.Query().Get("title")
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./core/diagnostics/ -run TestIssueURL -v`
Expected: FAIL — `undefined: IssueURL`.

- [ ] **Step 3: Implement**

Create `core/diagnostics/issue.go`:

```go
package diagnostics

import (
	"net/url"
	"strings"
)

// issueBase is where a problem report goes. A public repository, which is why
// the confirm step says so in those words.
const issueBase = "https://github.com/miou1107/multi-claude-switcher/issues/new"

// maxTitleRunes keeps the title readable in a list and the URL well short of the
// roughly 8 KB a prefilled issue link tolerates.
const maxTitleRunes = 80

// issueBody is all the URL carries. The report itself goes by clipboard: 200
// log lines do not fit in a link, and truncating them to fit would ship a report
// that is missing exactly the part someone asked for.
const issueBody = "Paste the report here (Cmd+V / Ctrl+V).\n"

// IssueURL builds the prefilled new-issue link.
//
// The title is masked before anything else: a user describing their problem
// tends to paste the error they saw, and the error they saw has their path in
// it. It is then flattened to one line, capped, and escaped — a comment
// containing & or # must not be able to truncate the URL or reach a shell.
func IssueURL(comment string, m *Masker) string {
	title := strings.TrimSpace(m.Apply(comment))
	if i := strings.IndexAny(title, "\r\n"); i >= 0 {
		title = strings.TrimSpace(title[:i])
	}
	if title == "" {
		title = "Problem report"
	}
	if r := []rune(title); len(r) > maxTitleRunes {
		title = strings.TrimSpace(string(r[:maxTitleRunes]))
	}
	q := url.Values{}
	q.Set("title", title)
	q.Set("body", issueBody)
	return issueBase + "?" + q.Encode()
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./core/diagnostics/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add core/diagnostics/issue.go core/diagnostics/issue_test.go
git commit -m "feat: build the prefilled issue url

The title is masked first, because a user describing a problem pastes the
error they saw and the error they saw has their path in it. It is then
flattened, capped and escaped so punctuation cannot truncate the link."
```

---

### Task 8: The Debug info screen

**Files:**
- Modify: `internal/panelui/render.go` (CSS near line 143, `RenderSettings` near line 397, new `RenderDebug` after it)
- Modify: `internal/panelui/render_test.go`

**Interfaces:**
- Consumes: nothing from `core` — the renderer takes strings only, as every other view does.
- Produces:
  ```go
  type DebugVM struct {
      Report  string
      Comment string
      Status  string
  }
  func RenderDebug(vm DebugVM) string
  ```
  Both hosts render this for the `debug` view; both handle the `showDebug` and `reportProblem` actions it emits.

- [ ] **Step 1: Write the failing test**

Append to `internal/panelui/render_test.go`:

```go
// TestRenderDebugShowsWhatWillBePublished pins the three things the screen
// exists for: the report itself, a way to say what went wrong, and a statement
// of what has been removed. The notice is not decoration — it is the only place
// the user is told the report was masked at all.
func TestRenderDebugShowsWhatWillBePublished(t *testing.T) {
	h := RenderDebug(DebugVM{Report: "MCS 0.11.2\naccount-1", Comment: ""})

	for _, want := range []string{
		"MCS 0.11.2",
		"account-1",
		"removed",
		`send('showSettings','')`,
		`id="dbgc"`,
		"Report a problem",
		"Copy",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q from the debug view", want)
		}
	}
}

// TestRenderDebugEscapesTheReportAndTheComment stops a log line containing
// markup from rewriting the panel it is displayed in.
func TestRenderDebugEscapesTheReportAndTheComment(t *testing.T) {
	h := RenderDebug(DebugVM{
		Report:  `<script>alert(1)</script>`,
		Comment: `</textarea><img src=x onerror=alert(1)>`,
	})
	if strings.Contains(h, "<script>alert(1)</script>") {
		t.Error("the report was not escaped")
	}
	if strings.Contains(h, "</textarea><img") {
		t.Error("the comment was not escaped")
	}
	if !strings.Contains(h, "&lt;script&gt;") {
		t.Error("the report should still be readable once escaped")
	}
}

// TestRenderSettingsOffersDebugInfo keeps the screen reachable. A view nothing
// links to is a view nobody uses.
func TestRenderSettingsOffersDebugInfo(t *testing.T) {
	h := RenderSettings(SettingsVM{Version: "0.11.2"})
	if !strings.Contains(h, `send('showDebug','')`) {
		t.Error("Settings must offer Debug info")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/panelui/ -run "TestRenderDebug|TestRenderSettingsOffers" -v`
Expected: FAIL — `undefined: RenderDebug`, `undefined: DebugVM`.

- [ ] **Step 3: Add the CSS**

In `render.go`'s `<style>` block, after the `.hintw` rule (line 142):

```css
.dbgnote{background:#e9f5ee;color:#1a7a3d;font-size:11.5px;line-height:1.5;padding:9px 12px;border-radius:11px;margin-bottom:9px}
.dbgbox{background:#fff;border-radius:12px;padding:11px 12px;font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:10.5px;line-height:1.65;color:#514b66;max-height:210px;overflow:auto;white-space:pre-wrap;word-break:break-word}
.dbgarea{width:100%;height:60px;font:inherit;font-size:12px;padding:10px 12px;border:2px solid #e0dcf3;border-radius:12px;background:#fff;color:#241f38;outline:none;resize:none}
.dbgarea:focus{border-color:#7c6cf0}
```

- [ ] **Step 4: Add the Debug info row to Settings**

In `RenderSettings`, between the `openArchive` button and the quit button:

```go
  <button class="sbtn" onclick="send('showDebug','')">Debug info…</button>
```

- [ ] **Step 5: Add `RenderDebug`**

After `RenderSettings` in `render.go`:

```go
// DebugVM is the Debug info view: what MCS knows about this machine, already
// masked, and a box to say what went wrong.
type DebugVM struct {
	Report  string
	Comment string
	Status  string // transient feedback, e.g. after Copy
}

// RenderDebug shows the report before it goes anywhere.
//
// There is no unmask switch and no "include the log" checkbox, so what is on
// screen is exactly what is copied — one version of the truth, and no way to
// publish something the user was not shown.
func RenderDebug(vm DebugVM) string {
	esc := html.EscapeString
	status := ""
	if vm.Status != "" {
		status = `<div class="status">` + esc(vm.Status) + `</div>`
	}
	body := `<div class="header">
  <button class="back" onclick="send('showSettings','')">‹</button>
  <div class="htext"><h1>Debug info</h1><p>Exactly what a report contains</p></div>
</div>` + status + `
<div class="dbgnote">Email addresses, account IDs and your user name are removed before anything leaves this screen.</div>
<div class="dbgbox">` + esc(vm.Report) + `</div>
<div class="hint">What went wrong? (optional)</div>
<textarea class="dbgarea" id="dbgc" placeholder="Switching to my work account left the personal one closed…">` + esc(vm.Comment) + `</textarea>
<div class="footer">
  <button class="btn btn-light" style="flex:none;padding:10px 14px" onclick="send('copyDebug', document.getElementById('dbgc').value)">Copy</button>
  <button class="btn btn-primary" onclick="askReport()">Report a problem</button>
</div>`
	return shell(body)
}
```

- [ ] **Step 6: Add `askReport` to the shell script**

In the `<script>` block, beside `askSwitch`/`askSync` (around line 214), add:

```js
  function askReport(){
    askConfirm('reportProblem', document.getElementById('dbgc').value,
      'Open a GitHub issue?',
      'The report above and your comment are copied to your clipboard, and your browser opens a new issue on the MCS repository. Paste it there and you can still edit it before submitting.',
      'Copy and open');
  }
```

The modal's fixed warning line ("Anything unsaved in Claude is interrupted.") is wrong for this dialog. Give `askConfirm` a sixth parameter for it, defaulting to the existing text so no other caller changes:

```js
  function askConfirm(action, arg, title, body, okLabel, warn){
    _pending = {a:action, arg:arg};
    document.getElementById('mcsModalTitle').textContent = title;
    document.getElementById('mcsModalBody').textContent = body;
    document.querySelector('#mcsModal .warn').textContent =
      warn || 'Anything unsaved in Claude is interrupted.';
    document.getElementById('mcsModalOk').textContent = okLabel;
    document.getElementById('mcsModal').classList.add('on');
    document.getElementById('mcsModalCancel').focus();
  }
```

and pass the report's own warning from `askReport`:

```js
      'Copy and open',
      'GitHub issues are public. What you saw on the previous screen is all that is included, with email addresses, account IDs and your user name already removed.');
```

Match the existing `askConfirm` body exactly when editing — only the `warn` parameter and the `.warn` line are new.

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/panelui/ -count=1 -v`
Expected: all PASS, including the pre-existing render tests. If a golden-HTML test fails because of the new Settings row, update its expectation.

- [ ] **Step 8: Commit**

```bash
git add internal/panelui/render.go internal/panelui/render_test.go
git commit -m "feat: add the Debug info panel view

No unmask switch and no include-the-log checkbox, so what is on screen is
what gets copied. The confirm dialog takes its own warning line: the
shared one is about closing Claude, and this dialog is about publishing
to a public issue tracker."
```

---

### Task 9: Clipboard

**Files:**
- Create: `internal/clip/clip_darwin.go`, `internal/clip/clip_windows.go`, `internal/clip/clip_other.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func clip.Set(text string) error`. Task 10 calls it from both hosts.

Its own package, next to `internal/panelui`, because both hosts need it and the two host packages cannot import each other. Copying six lines into each would be the third place in this repo where the hosts drift apart by being written twice — which is exactly what `panelui.BuildProfiles` exists to have stopped.

The write is awaited. A browser that wins the race against PowerShell puts the user in front of an issue form where paste yields whatever they copied last — content MCS never saw and cannot mask, which is worse than anything masking guards against.

- [ ] **Step 1: Write the macOS implementation**

`internal/clip/clip_darwin.go`:

```go
//go:build darwin

// Package clip puts text on the system clipboard, and waits for it to land.
//
// Waiting is the point, and the reason this is not a one-liner at each call
// site. Its caller opens a browser next; a browser that arrives first leaves the
// user pasting whatever they copied last into a public issue — content the
// program never saw and could not have masked.
package clip

import (
	"os/exec"
	"strings"
)

// Set writes text to the clipboard, returning only once it is there.
func Set(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
```

- [ ] **Step 2: Write the Windows implementation**

`internal/clip/clip_windows.go`:

```go
//go:build windows

package clip

import (
	"encoding/base64"
	"os/exec"
	"syscall"
	"unicode/utf16"
)

// Set writes text to the clipboard, returning only once it is there.
//
// The text is passed base64-encoded and decoded inside PowerShell rather than
// quoted into the script: a report contains quotes, backticks, dollar signs and
// newlines, all of which are PowerShell syntax, and single-quote escaping only
// handles the first of them.
//
// cmd.Run, not Start. Launching PowerShell costs several hundred milliseconds,
// and the caller opens a browser next; losing that race means the user pastes
// their previous clipboard into a public issue.
func Set(text string) error {
	script := `Set-Clipboard -Value ([System.Text.Encoding]::Unicode.GetString(` +
		`[System.Convert]::FromBase64String('` +
		base64.StdEncoding.EncodeToString(utf16le(text)) + `')))`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(utf16le(script)))
	// CREATE_NO_WINDOW: the hosts are background processes, and a console
	// flashing up while copying a bug report is the kind of thing users report
	// as a bug of its own.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	return cmd.Run()
}

// utf16le encodes a string the way PowerShell's -EncodedCommand and
// System.Text.Encoding.Unicode both expect. Used for the script and the payload
// alike, which is why it lives here rather than being borrowed from the tray's
// dialog helpers.
func utf16le(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, 0, len(u)*2)
	for _, c := range u {
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}
```

- [ ] **Step 3: Write the stub**

`internal/clip/clip_other.go`:

```go
//go:build !darwin && !windows

package clip

import "errors"

// Set reports that there is no clipboard here rather than pretending to write
// to one: its caller decides not to open a browser on this error.
func Set(string) error { return errors.New("clipboard not supported on this platform") }
```

- [ ] **Step 4: Verify all three targets build**

Run:
```bash
go build ./... && GOOS=windows go build ./... && GOOS=linux go build ./internal/clip && go vet ./...
```
Expected: no output.

- [ ] **Step 5: Verify the clipboard actually works on this machine**

Run:
```bash
cat > /tmp/cliptest.go <<'EOF'
package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func main() {
	c := exec.Command("pbcopy")
	c.Stdin = strings.NewReader("MCS clipboard check ✅\nline two")
	if err := c.Run(); err != nil {
		fmt.Println("write failed:", err)
		return
	}
	out, _ := exec.Command("pbpaste").Output()
	fmt.Printf("read back: %q\n", string(out))
}
EOF
go run /tmp/cliptest.go && rm /tmp/cliptest.go
```
Expected: `read back: "MCS clipboard check ✅\nline two"`

- [ ] **Step 6: Commit**

```bash
git add internal/clip/
git commit -m "feat: write to the clipboard, and wait for it

The caller opens a browser next. A browser that wins the race leaves the
user pasting their previous clipboard into a public issue, which is
content MCS never saw and could not mask.

Windows takes the payload base64-encoded rather than quoted: a report is
full of quotes, backticks and dollar signs, all of which are PowerShell
syntax."
```

---

### Task 10: Wire up both hosts

**Files:**
- Modify: `cmd/mcs-menubar/main.go` (`goPanelAction`, `reloadPanel`, new `buildDiagnostics`)
- Modify: `cmd/mcs-tray/panel_windows.go` (`dispatchAction`, `reloadPanel`, new `panelBuildDiagnostics`)
- Create: `cmd/mcs-menubar/diagnostics.go`, `cmd/mcs-tray/paneldiagnostics_windows.go`

**Interfaces:**
- Consumes: `diagnostics.Build`, `diagnostics.Input`, `diagnostics.Profile`, `diagnostics.NewMasker`, `diagnostics.IssueURL` (Tasks 2–7); `panelui.RenderDebug`, `panelui.DebugVM` (Task 8); `clip.Set` (Task 9); `platform.GetProfileClaudeVersion`, `GetProfileClaudeCodeVersion`, `InstallKind` (Task 5).
- Produces: nothing further consumes these.

Both hosts gather the same `Input` in the same order. The gathering is duplicated rather than shared because each host already duplicates `buildProfiles`, `SettingsVM` and every other view's assembly; a single shared gatherer would need the platform, the profile list and the running path threaded through it, which is what `Input` exists to avoid.

- [ ] **Step 1: Write the macOS gatherer**

Create `cmd/mcs-menubar/diagnostics.go`:

```go
package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/core/diagnostics"
	"github.com/miou1107/multi-claude-switcher/platform"
)

// The clipboard lives in internal/clip so both hosts share one implementation:
// see that package's doc comment for why the write is awaited.

// buildDiagnostics gathers what the report needs. Raw values throughout: masking
// happens once, inside diagnostics.Build, so no caller can forget.
func buildDiagnostics() diagnostics.Input {
	profiles := mustFindProfiles()
	running, _ := plat.DetectRunningProfile()

	in := diagnostics.Input{
		Version:         core.Version,
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		OSVersion:       osVersion(),
		Install:         plat.InstallKind(),
		AutoSync:        core.AutoSyncOnSwitch(),
		LoginItem:       core.LoginItemEnabled(),
		ActiveRecord:    core.LoadActiveProfile(),
		HomeReplacement: "~",
		LogDir:          core.LogDir(),
	}
	in.Home, _ = os.UserHomeDir()
	in.HostName, _ = os.Hostname()
	in.UserName = os.Getenv("USER")

	// One scan for every address. ScanAccounts is the only exported route to an
	// account's email — core.readLocalStorageIdentity is unexported, and copying
	// a locked LevelDB is not something to reimplement for a report.
	emails := map[string]string{}
	for _, a := range core.ScanAccounts(profiles, core.LoadPending()) {
		if a.Email != "" {
			emails[a.UUID] = a.Email
		}
	}

	for _, p := range profiles {
		uuid, uuidErr := platform.GetProfileAccountUUID(p.Path)
		org, _ := platform.GetProfileActiveOrgUUID(p.Path)
		in.Profiles = append(in.Profiles, diagnostics.Profile{
			Folder:      p.Name,
			AccountUUID: uuid,
			Email:       emails[uuid],
			OrgUUID:     org,
			Path:        p.Path,
			SignedIn:    uuidErr == nil,
			Running:     running != "" && platform.SamePath(p.Path, running),
			Convos:      p.UUIDBuckets[uuid],
		})
	}

	// Versions come from whichever profile can answer; they describe the install,
	// not the account, so the first readable one is the answer for all of them.
	for _, p := range profiles {
		if in.ClaudeVer == "" {
			v, err := platform.GetProfileClaudeVersion(p.Path)
			if err == nil {
				in.ClaudeVer = v
			} else if in.ClaudeVerErr == "" {
				in.ClaudeVerErr = err.Error()
			}
		}
		if in.ClaudeCodeVer == "" {
			v, err := platform.GetProfileClaudeCodeVersion(p.Path)
			if err == nil {
				in.ClaudeCodeVer = v
			} else if in.ClaudeCodeVerErr == "" {
				in.ClaudeCodeVerErr = err.Error()
			}
		}
	}
	return in
}

// osVersion is best effort: an unknown OS version costs one line of a report,
// so it is never worth failing the screen over.
func osVersion() string {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
```

- [ ] **Step 2: Add the two macOS actions**

In `goPanelAction`, beside `openLog`:

```go
	case "showDebug":
		setDebugComment("")
		setView("debug")
		go reloadPanel()
	case "reportProblem":
		setDebugComment(arg)
		go func() {
			report, m := debugReport()
			if arg != "" {
				report += "\n---\n" + m.Apply(arg) + "\n"
			}
			if err := clip.Set(report); err != nil {
				// The browser is not opened: an issue form with nothing to paste is
				// worse than no browser at all.
				setStatus("Couldn't copy the report: " + err.Error())
				reloadPanel()
				return
			}
			_ = exec.Command("open", diagnostics.IssueURL(arg, m)).Start()
			setStatus("Report copied. Paste it into the issue.")
			reloadPanel()
		}()
	case "copyDebug":
		setDebugComment(arg)
		go func() {
			report, _ := debugReport()
			if err := clip.Set(report); err != nil {
				setStatus("Couldn't copy: " + err.Error())
			} else {
				setStatus("Copied.")
			}
			reloadPanel()
		}()
```

Both actions store the comment first. Every panel action re-renders the whole page, so a comment that is not carried back into the view model is erased the moment a status banner appears — which is exactly when the user most wants to keep what they typed. Add beside the other view globals in `main.go`:

```go
// debugComment survives the reload that every action triggers. Without it,
// pressing Copy wipes what the user just typed.
var debugComment string

func setDebugComment(s string) {
	mu.Lock()
	debugComment = s
	mu.Unlock()
}

func getDebugComment() string {
	mu.Lock()
	defer mu.Unlock()
	return debugComment
}

// debugReport builds the report and hands back the masker that produced it, so
// the user's comment and the issue title are masked with the same registrations
// rather than a fresh, empty one.
func debugReport() (string, *diagnostics.Masker) {
	in := buildDiagnostics()
	return diagnostics.Build(in), diagnostics.NewMaskerFor(in)
}
```

`NewMaskerFor` is already exported by Task 6. Add a test for the use it exists for, in `core/diagnostics/report_test.go`:

```go
// TestNewMaskerForMasksTheUsersOwnComment covers the case the exported
// constructor exists for: a user pastes the error they saw, and that error names
// their account.
func TestNewMaskerForMasksTheUsersOwnComment(t *testing.T) {
	m := NewMaskerFor(fullInput(t))
	got := m.Apply("it broke for vincent@fontrip.com in /Users/vincentkao/Library")
	if strings.Contains(got, "fontrip") || strings.Contains(got, "vincentkao") {
		t.Errorf("the comment kept an identifier: %q", got)
	}
}
```

- [ ] **Step 3: Add the macOS view**

In `reloadPanel`'s switch, beside `case "settings"`:

```go
	case "debug":
		report, _ := debugReport()
		htmlStr = panelui.RenderDebug(panelui.DebugVM{
			Report:  report,
			Comment: getDebugComment(),
			Status:  getStatus(),
		})
```

Add `"debug"` to the `currentView` comment at `main.go:43`.

- [ ] **Step 4: Mirror all of it on Windows**

Create `cmd/mcs-tray/paneldiagnostics_windows.go` with `panelBuildDiagnostics()`, identical to Step 1 except:

- `//go:build windows` first line.
- `panelPlat` instead of `plat`, and `panelMustFindProfiles()` instead of
  `mustFindProfiles()` — the name `panelBuildProfiles` already uses
  (`panel_windows.go:724`).
- `HomeReplacement: "%USERPROFILE%"`.
- `in.UserName = os.Getenv("USERNAME")`.
- `osVersion()` runs `cmd /c ver`:

```go
func osVersion() string {
	out, err := exec.Command("cmd", "/c", "ver").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
```

Then add the same three cases to `dispatchAction` (using `panelSetView`, `panelSetStatus`, and `panelSetDebugComment`/`panelGetDebugComment` guarded by `panelMu`, mirroring Step 2) and the same `case "debug"` to the Windows `reloadPanel`, with `openURL(...)` in place of `exec.Command("open", ...)`. Add `"debug"` to the `panelView` comment at `panel_windows.go:46`.

- [ ] **Step 5: Build and vet both targets**

Run:
```bash
gofmt -l . && go build ./... && go vet ./... && GOOS=windows go build ./... && GOOS=windows go vet ./... && go test ./... -count=1
```
Expected: no output from gofmt, all tests PASS.

- [ ] **Step 6: Run the real screen**

Run: `go run ./cmd/mcs-menubar`

Then: open the panel, gear, Debug info. Confirm on screen that the report lists every profile, that no address, UUID, home path, user name or host name appears anywhere in it, and that `[redacted: unregistered]` does **not** appear. Press Copy and check with `pbpaste`. Press Report a problem, read the dialog, confirm, and check the browser opens a GitHub new-issue page with a sensible title and the clipboard holding the report.

If `[redacted: unregistered]` appears, something reached the report without being registered — find it and register it in `buildDiagnostics`; do not loosen the sweep.

- [ ] **Step 7: Commit**

```bash
git add cmd/mcs-menubar/diagnostics.go cmd/mcs-menubar/main.go cmd/mcs-tray/paneldiagnostics_windows.go cmd/mcs-tray/panel_windows.go
git commit -m "feat: wire the Debug info screen into both hosts

Each host gathers its own Input, the way each already builds its own
SettingsVM. Masking is not among the things they gather: it happens once
inside Build, so a host cannot forget it."
```

---

### Task 11: Documentation

**Files:**
- Modify: `README.md`, `README.zh-TW.md`, `FILELIST.md`, `CHANGELOG.md`

**Interfaces:**
- Consumes: nothing.
- Produces: nothing.

- [ ] **Step 1: Add the FILELIST entries**

Add, in the order the existing file groups them:

```markdown
- `core/diagnostics/mask.go` — Stable-pseudonym masking for debug reports: registration by value, boundary-aware user names, home-prefix rewriting on both separators.
- `core/diagnostics/sweep.go` — Shape-based backstop that marks any email or UUID which escaped registration, so a missing rule fails a test instead of reaching a public issue.
- `core/diagnostics/report.go` — Builds the debug report: environment, profiles, path shape without path values, and the tail of each log file.
- `core/diagnostics/issue.go` — Builds the prefilled GitHub new-issue URL (masked, single-line, capped, escaped title).
- `platform/claudeversion.go` — Reads the Claude Desktop version and the bundled Claude Code CLI version from a profile directory.
- `internal/clip/clip_darwin.go` — macOS clipboard write (pbcopy), awaited before the browser opens.
- `cmd/mcs-menubar/diagnostics.go` — Gathers the macOS host's diagnostics Input.
- `internal/clip/clip_windows.go` — Windows clipboard write (Set-Clipboard, base64 payload), awaited before the browser opens.
- `internal/clip/clip_other.go` — Unsupported-platform clipboard stub.
- `cmd/mcs-tray/paneldiagnostics_windows.go` — Gathers the Windows host's diagnostics Input.
```

- [ ] **Step 2: Add the CHANGELOG entry**

Under `## [Unreleased]`, in an `### Added` section:

```markdown
- **A Debug info screen, and a way to report a problem from it.** Settings now
  shows what MCS knows about your machine: its own version, which Claude Desktop
  build you have, what each account looks like on disk, and the recent log.
  "Report a problem" copies that report and opens a new issue on the project's
  GitHub page for you to paste it into, so you see exactly what is published
  before anything is. Email addresses, account IDs, your user name and your home
  path are replaced with stable stand-ins first, and there is no way to turn that
  off.
```

And under `### Fixed`:

```markdown
- **There is a log file on macOS.** The menu-bar app never opened one, so
  everything it logged went nowhere and "Open log folder" showed an empty folder
  on a machine that had been running for months.
```

- [ ] **Step 3: Add a README section**

In `README.md`, after the section on syncing, before the troubleshooting or development section:

```markdown
### Reporting a problem

Settings → **Debug info** shows what the switcher knows about your machine:
versions, what each account looks like on disk, and the recent log. **Report a
problem** copies that report and opens a new GitHub issue for you to paste it
into.

Nothing is sent anywhere on its own. You see the report first, you paste it, and
you submit it under your own account. Email addresses, account IDs, your user
name and your home path are replaced with stand-ins such as `account-1` and
`org-A` before the report reaches the screen, so the relationships stay readable
while the values do not leave. Issues are public.
```

Then add the matching section to `README.zh-TW.md`, following that file's existing heading style.

- [ ] **Step 4: Verify the docs match the build**

Run: `go build ./... && go test ./... -count=1`
Expected: PASS. Then re-read each FILELIST line against `git status` — every new file must appear exactly once.

- [ ] **Step 5: Commit**

```bash
git add README.md README.zh-TW.md FILELIST.md CHANGELOG.md
git commit -m "docs: document the Debug info screen and the problem report"
```

---

## After the plan

Windows has not been exercised by any of this: the clipboard, the browser open,
`%USERPROFILE%` masking and `cmd /c ver` are all cross-compiled and unit-tested
only. Before releasing, add it to [issue #8](https://github.com/miou1107/multi-claude-switcher/issues/8)
or open a new one, with these checks:

1. Settings → Debug info opens and the report is populated (not `unknown` for
   every field).
2. No address, UUID, `C:\Users\<name>` or host name anywhere in it, and no
   `[redacted: unregistered]`.
   **Amended 2026-08-05:** `[redacted]` in the log sections is expected and not
   a failure — session IDs in log lines carry it, and no registration can cover
   them. Only `[redacted: unregistered]`, which appears above the log sections,
   means a field escaped registration.
3. Copy puts the report on the clipboard; paste it into Notepad and compare.
4. Report a problem opens the browser, the issue title is right, and the
   clipboard still holds the report at the moment the browser appears.
5. Both the standalone and Store builds, since `InstallKind` and the profile
   paths differ between them.
