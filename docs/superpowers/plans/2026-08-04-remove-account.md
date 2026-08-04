# Remove an account — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user take an account off the switcher, archiving its profile folder instead of deleting it.

**Architecture:** A new `core.RemoveProfile` runs the tail of the merge path that is already in production: resolve, refuse, archive by rename, then clean the registries. A new `platform.PrepareDelete` puts the one platform-specific refusal (the Windows Store shared slot) in the platform layer. The panel's Rename screen becomes an Account settings screen carrying the red remove button, and a dedicated result screen reports where the folder went or that nothing moved.

**Tech Stack:** Go 1.x, no new dependencies. macOS WKWebView host (`cmd/mcs-menubar`) and Windows WebView2 host (`cmd/mcs-tray`) both render `internal/panelui`.

**Spec:** `docs/superpowers/specs/2026-08-04-remove-account-design.md`

## Global Constraints

- Nothing is ever deleted. No `os.RemoveAll` on a profile directory anywhere in this work.
- Registries (`managed.json`, `names.json`, `pending.json`) are written **after** the folder has moved, never before.
- No em dash (`—`) in any user-facing string. `internal/panelui/emdash_test.go` enforces this over the rendered HTML; it does not cover Go error strings, so watch those by hand.
- Repo output is English. Comments explain *why*, matching the density of the surrounding file.
- Both hosts must stay in step. Every renderer change lands in `cmd/mcs-menubar/main.go` and `cmd/mcs-tray/panel_windows.go` in the same task, or the Windows build breaks.
- Windows-only code cannot be run on the dev machine. Verify it compiles with `GOOS=windows go build ./...` and mark the behaviour as needing a real Windows check.
- Every commit updates `README.md`, `README.zh-TW.md`, `FILELIST.md`, `CHANGELOG.md` where they are affected.
- Do not bump the version. That is Task 8 and it needs the maintainer's approval first.

---

### Task 1: `PrepareDelete` on the platform layer

Resolves an identity to the directory to archive, and refuses when that directory may not be renamed away. Separate from `PrepareArchive`, which takes a *keeper* to swap into the Store slot; a removal has no keeper.

**Files:**
- Modify: `platform/platform.go` (the `Platform` interface, beside `PrepareArchive`)
- Modify: `platform/darwin.go`
- Modify: `platform/windows.go`
- Modify: `platform/unsupported.go`
- Modify: `core/switch_test.go` (`mockPlatform`)
- Test: `platform/darwin_test.go`, `platform/windows_msix_test.go`

**Interfaces:**
- Produces: `PrepareDelete(identity string) (path string, err error)` on `platform.Platform`.

- [ ] **Step 1: Write the failing macOS test**

Append to `platform/darwin_test.go`:

```go
func TestPrepareDeleteResolvesUnderAppSupport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	d := &DarwinPlatform{}
	got, err := d.PrepareDelete("Claude_Work")
	if err != nil {
		t.Fatalf("PrepareDelete: %v", err)
	}
	want := filepath.Join(home, "Library", "Application Support", "Claude_Work")
	if got != want {
		t.Fatalf("PrepareDelete = %q, want %q", got, want)
	}
}
```

If `darwin_test.go` already has a helper that redirects `HOME`, use it instead of `t.Setenv` and match the existing test's style. Read the top of the file first.

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./platform/ -run TestPrepareDelete -v`
Expected: FAIL, `d.PrepareDelete undefined`.

- [ ] **Step 3: Add the interface method**

In `platform/platform.go`, directly below the `PrepareArchive` declaration and its comment:

```go
	// PrepareDelete resolves an identity to the directory that would be archived
	// by a removal, and refuses when that directory may not be renamed away.
	//
	// It is not PrepareArchive with one argument. PrepareArchive takes a KEEPER to
	// swap into the Store build's shared slot; a removal has no keeper, so there is
	// nothing to swap and the only honest answer for the slot occupant is a
	// refusal. Passing the active identity as a stand-in would read as a swap that
	// never happens.
	//
	// Caller need not have terminated Claude: a removal may not target the profile
	// in use, so the directory this returns is never open.
	PrepareDelete(identity string) (path string, err error)
```

- [ ] **Step 4: Implement it on all four platforms**

`platform/darwin.go`, below `PrepareArchive`:

```go
// PrepareDelete has nothing to refuse here: every profile is its own directory,
// so any of them can be renamed away without disturbing the others.
func (d *DarwinPlatform) PrepareDelete(identity string) (string, error) {
	appSup := d.AppSupportDir()
	if appSup == "" {
		return "", fmt.Errorf("could not determine user home directory")
	}
	return filepath.Join(appSup, identity), nil
}
```

`platform/windows.go`, below `PrepareArchive`:

```go
// PrepareDelete refuses the Store build's slot occupant. The active profile is
// addressed through one shared directory that state.json names; renaming it away
// would leave state.json pointing at nothing, and unlike a merge there is no
// keeper to swap in first. The standalone build has no shared slot, so it only
// resolves.
func (w *WindowsPlatform) PrepareDelete(identity string) (string, error) {
	if !w.isMSIX() {
		root := w.AppSupportDir()
		if root == "" {
			return "", fmt.Errorf("could not determine %%APPDATA%% directory")
		}
		return filepath.Join(root, identity), nil
	}
	roaming := msixRoamingDir()
	if roaming == "" {
		return "", fmt.Errorf("Store Claude Desktop data directory not found")
	}
	if strings.EqualFold(readMSIXStateIn(roaming).Current, identity) {
		return "", fmt.Errorf("%s is the account in use. Switch to another account first, then remove it", DisplayNameOf(identity))
	}
	return msixProfilePath(roaming, identity), nil
}
```

`DisplayNameOf` does not exist in `platform`; `core.DisplayName` does, and `platform` must not import `core`. Use the raw identity instead and let the caller phrase it:

```go
		return "", fmt.Errorf("%q is the account in use. Switch to another account first, then remove it", identity)
```

`platform/unsupported.go`:

```go
func (p *unsupportedPlatform) PrepareDelete(identity string) (string, error) {
	return "", notSupported()
}
```

`core/switch_test.go`, on `mockPlatform`, following the `prepareArchive` field pattern already there. Add a field:

```go
	// prepareDelete lets a test decide what PrepareDelete hands back, which is how
	// the Store build's refusal of the slot occupant gets represented.
	prepareDelete func(identity string) (string, error)
```

and the method beside `PrepareArchive`:

```go
func (m *mockPlatform) PrepareDelete(identity string) (string, error) {
	if m.prepareDelete != nil {
		return m.prepareDelete(identity)
	}
	return filepath.Join(m.appSupport, identity), nil
}
```

Read `mockPlatform` first: use whatever field it actually holds for the app-support root rather than assuming `m.appSupport`, and match how `PrepareArchive` builds its default.

- [ ] **Step 5: Run the macOS test**

Run: `go test ./platform/ ./core/ -run 'TestPrepareDelete|TestSwitch' -v`
Expected: PASS.

- [ ] **Step 6: Write the Windows Store test**

Append to `platform/windows_msix_test.go`. Read the file first and reuse its existing fixture helper for writing a `state.json` under a temp roaming dir rather than writing one by hand.

```go
func TestPrepareDeleteRefusesSlotOccupant(t *testing.T) {
	roaming := msixFixture(t, "Work") // whatever the file's helper is called
	w := &WindowsPlatform{}
	if _, err := w.PrepareDelete("Work"); err == nil {
		t.Fatal("PrepareDelete accepted the slot occupant; it must refuse")
	}
	got, err := w.PrepareDelete("Personal")
	if err != nil {
		t.Fatalf("PrepareDelete on a parked profile: %v", err)
	}
	want := filepath.Join(msixContainerDir(roaming), "Personal")
	if got != want {
		t.Fatalf("PrepareDelete = %q, want %q", got, want)
	}
}
```

The Store fixture may need `msixRoamingDir` redirected; follow exactly what the neighbouring tests in that file do.

- [ ] **Step 7: Verify the Windows build compiles**

Run: `GOOS=windows go build ./... && GOOS=windows go vet ./...`
Expected: no output.

This is the only check available here. Record in the commit message that the Store refusal is unverified on a real machine.

- [ ] **Step 8: Commit**

```bash
git add platform/ core/switch_test.go
git commit -m "feat(platform): PrepareDelete resolves a profile for removal

Separate from PrepareArchive, which takes a keeper to swap into the Store
build's shared slot. A removal has no keeper, so for the slot occupant the
only honest answer is a refusal rather than a swap that never happens.

The Store refusal is compile-checked only; it needs a real Windows run."
```

---

### Task 2: `core.RemoveProfile` — the happy path

**Files:**
- Create: `core/removeprofile.go`
- Create: `core/removeprofile_test.go`

**Interfaces:**
- Consumes: `platform.Platform.PrepareDelete` (Task 1), `ArchiveProfile(identity, profilePath, archiveRoot string) (string, error)`, `RemoveManaged(identity string) error`, `SetProfileName(identity, name string) error`, `RemovePending(folder string) error`, `LoadActiveProfile() string`.
- Produces: `RemoveProfile(plat platform.Platform, identity string) (archivedTo string, err error)`. `archivedTo` is the full path the folder landed at.

- [ ] **Step 1: Write the failing test**

Create `core/removeprofile_test.go`. Reuse `withStubbedNames` from `core/merge_test.go` (same package) and follow `mergePlatform` for the fake. Read `core/merge_test.go` first — the fake platform there is the model to copy.

```go
package core

import (
	"os"
	"path/filepath"
	"testing"
)

// removeFixture builds one profile directory holding a session file, and returns
// its path plus the archive root.
func removeFixture(t *testing.T, identity string) (root, profilePath, archiveRoot string) {
	t.Helper()
	root = t.TempDir()
	profilePath = filepath.Join(root, identity)
	archiveRoot = filepath.Join(root, "archive")
	bucket := filepath.Join(profilePath, "claude-code-sessions", "uuid-1")
	if err := os.MkdirAll(bucket, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucket, "local_x.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, profilePath, archiveRoot
}

func TestRemoveProfileArchivesAndClearsRegistries(t *testing.T) {
	withStubbedNames(t)
	withStubbedManaged(t) // add this helper if core has none; see step 2
	withStubbedPending(t)
	withStubbedActive(t)

	root, profilePath, archiveRoot := removeFixture(t, "Claude_Old")
	plat := newRemovePlatform(t, root, archiveRoot, "Claude_Old")

	if err := SetManaged([]string{"Claude_Old", "Claude_Keep"}); err != nil {
		t.Fatal(err)
	}
	if err := SetProfileName("Claude_Old", "Old one"); err != nil {
		t.Fatal(err)
	}
	if err := AddPending("Claude_Old", ""); err != nil {
		t.Fatal(err)
	}

	dest, err := RemoveProfile(plat, "Claude_Old")
	if err != nil {
		t.Fatalf("RemoveProfile: %v", err)
	}
	if _, err := os.Stat(profilePath); !os.IsNotExist(err) {
		t.Fatal("the profile directory is still in the scan path")
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("archived copy is not at %s: %v", dest, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "claude-code-sessions", "uuid-1", "local_x.json")); err != nil {
		t.Fatalf("the conversation did not travel with the folder: %v", err)
	}
	managed, _ := LoadManaged()
	for _, m := range managed {
		if m == "Claude_Old" {
			t.Fatal("managed.json still lists the removed profile")
		}
	}
	if n := DisplayName("Claude_Old"); n != "Claude_Old" {
		t.Fatalf("display name survived removal: %q", n)
	}
	for _, p := range LoadPending() {
		if p == "Claude_Old" {
			t.Fatal("pending.json still lists the removed profile")
		}
	}
}
```

`LoadPending`'s return type must be checked before writing that loop; `core/pending.go` is the reference. Adjust the assertion to whatever it actually returns.

- [ ] **Step 2: Add the missing test helpers**

`core/merge_test.go` has `withStubbedNames`. `core/managed_test.go` and `core/activeprofile_test.go` redirect their own paths inline. Rather than duplicating, add the three missing helpers to `core/removeprofile_test.go` in the same shape as `withStubbedNames`:

```go
func withStubbedManaged(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	orig := managedPath
	managedPath = func() string { return filepath.Join(dir, "managed.json") }
	t.Cleanup(func() { managedPath = orig })
}

func withStubbedPending(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	orig := pendingPath
	pendingPath = func() string { return filepath.Join(dir, "pending.json") }
	t.Cleanup(func() { pendingPath = orig })
}

func withStubbedActive(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	orig := activeProfilePath
	activeProfilePath = func() string { return filepath.Join(dir, "active.json") }
	t.Cleanup(func() { activeProfilePath = orig })
}
```

Confirm the real var names in `core/managed.go` and `core/pending.go` before writing these. If a helper of the same name already exists elsewhere in package `core`, use that one — a duplicate will not compile.

**This matters beyond tidiness.** A test in this package once wrote to the user's real home directory and renamed a live profile. Every registry a test touches must be redirected, or the test damages the machine it runs on.

Also add the fake platform:

```go
// removePlatform resolves the identity back to the fixture's directory. Modelled
// on mergePlatform in merge_test.go: enough of a platform to exercise the order
// of operations, without pretending to be an OS.
type removePlatform struct {
	*mockPlatform
	root, archiveRoot string
	refuse            error
}

func newRemovePlatform(t *testing.T, root, archiveRoot, identity string) *removePlatform {
	t.Helper()
	return &removePlatform{mockPlatform: &mockPlatform{}, root: root, archiveRoot: archiveRoot}
}

func (p *removePlatform) PrepareDelete(identity string) (string, error) {
	if p.refuse != nil {
		return "", p.refuse
	}
	return filepath.Join(p.root, identity), nil
}

func (p *removePlatform) ArchiveDir() string { return p.archiveRoot }
```

`mockPlatform`'s zero value may not be usable directly. Read `core/switch_test.go` and construct it the way `mergePlatform` does.

- [ ] **Step 3: Run it and watch it fail**

Run: `go test ./core/ -run TestRemoveProfile -v`
Expected: FAIL, `undefined: RemoveProfile`.

- [ ] **Step 4: Implement**

Create `core/removeprofile.go`:

```go
package core

import (
	"fmt"
	"log"
	"os"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// RemoveProfile takes an account off the switcher by moving its profile
// directory into the archive root, and returns where it landed.
//
// Nothing is deleted. The directory moves untouched, which is also why no backup
// is taken: a merge snapshots the profile it keeps because a merge writes into
// it, and this writes nothing.
//
// Order is refuse, resolve, move, then update state. The registries are written
// only once the folder has really left the scan path: a folder unmanaged while
// still in place is hidden from the panel and back on the next Rescan, so
// "removed" would not stay removed.
func RemoveProfile(plat platform.Platform, identity string) (string, error) {
	name := DisplayName(identity)

	// The account in use, whether or not Claude is running. A check against the
	// running process would open the moment Claude is closed, and renaming the
	// directory the user is about to reopen is exactly what must not happen.
	// Refusing this is also what buys not having to quit Claude for a removal:
	// no other profile's directory is held by anything.
	if identity == LoadActiveProfile() {
		return "", fmt.Errorf("%s is the account you are using. Switch to another account first, then remove it", name)
	}

	path, err := plat.PrepareDelete(identity)
	if err != nil {
		return "", err
	}
	if fi, statErr := os.Stat(path); statErr != nil || !fi.IsDir() {
		return "", fmt.Errorf("%s is no longer there. Run Rescan", name)
	}

	dest, err := ArchiveProfile(identity, path, plat.ArchiveDir())
	if err != nil {
		// Nothing moved and no registry has been touched, so the account is still
		// listed and a retry is safe.
		return "", err
	}

	// From here the folder is gone from the scan path, so every registry naming it
	// is now wrong. Report the first failure but keep going: stopping at one would
	// leave the others describing a profile that no longer exists, which is worse
	// than the failure being reported.
	var registryErr error
	if err := RemoveManaged(identity); err != nil {
		registryErr = fmt.Errorf("removed, but the managed list still lists it: %w", err)
		log.Printf("remove: %v", registryErr)
	}
	// The display name goes with the profile. Left behind, it would be inherited by
	// any later profile that happened to reuse the identity.
	if err := SetProfileName(identity, ""); err != nil {
		log.Printf("remove: could not clear the display name for %q: %v", identity, err)
	}
	// And its pending entry. Pending entries are pruned only on sign-in, and a
	// removed profile never appears in FindProfiles again, so an entry left here
	// would render a sign-in prompt the user could never clear.
	if err := RemovePending(identity); err != nil {
		log.Printf("remove: could not clear the pending entry for %q: %v", identity, err)
	}
	return dest, registryErr
}
```

- [ ] **Step 5: Run it**

Run: `go test ./core/ -run TestRemoveProfile -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add core/removeprofile.go core/removeprofile_test.go
git commit -m "feat(core): RemoveProfile archives a profile and clears its registries

Reuses the tail of the merge path that is already in production. Registries
are written only after the folder has left the scan path: unmanaging a folder
still in place hides it and it returns on the next Rescan.

No backup is taken. The archive is the backup, because the directory moves
untouched; a merge snapshots only because a merge writes into what it keeps."
```

---

### Task 3: `core.RemoveProfile` — refusals, and a failed move leaving state intact

The part a real filesystem will not produce on demand, and the part where a wrong order does permanent damage.

**Files:**
- Modify: `core/removeprofile_test.go`

**Interfaces:**
- Consumes: `RemoveProfile` (Task 2), `renameProfile` (the injectable `os.Rename` var in `core/archive.go`), `archiveRenameDelay`.

- [ ] **Step 1: Write the failing tests**

Append to `core/removeprofile_test.go`:

```go
func TestRemoveProfileRefusesTheAccountInUse(t *testing.T) {
	withStubbedNames(t)
	withStubbedManaged(t)
	withStubbedPending(t)
	withStubbedActive(t)

	root, profilePath, archiveRoot := removeFixture(t, "Claude_Live")
	plat := newRemovePlatform(t, root, archiveRoot, "Claude_Live")
	if err := SaveActiveProfile("Claude_Live"); err != nil {
		t.Fatal(err)
	}
	if err := SetManaged([]string{"Claude_Live"}); err != nil {
		t.Fatal(err)
	}

	if _, err := RemoveProfile(plat, "Claude_Live"); err == nil {
		t.Fatal("RemoveProfile removed the account in use")
	}
	if _, err := os.Stat(profilePath); err != nil {
		t.Fatalf("the profile directory was disturbed by a refused removal: %v", err)
	}
	managed, _ := LoadManaged()
	if len(managed) != 1 || managed[0] != "Claude_Live" {
		t.Fatalf("a refused removal changed the managed list: %v", managed)
	}
}

func TestRemoveProfileRefusesAProfileThatIsGone(t *testing.T) {
	withStubbedNames(t)
	withStubbedManaged(t)
	withStubbedPending(t)
	withStubbedActive(t)

	root := t.TempDir()
	plat := newRemovePlatform(t, root, filepath.Join(root, "archive"), "Claude_Ghost")
	if _, err := RemoveProfile(plat, "Claude_Ghost"); err == nil {
		t.Fatal("RemoveProfile accepted an identity with no directory behind it")
	}
}

// The registries must survive a move that fails. If they were written first, a
// locked directory would leave the folder in place AND unlisted: invisible in the
// panel, back on the next Rescan, with its display name gone.
func TestRemoveProfileKeepsRegistriesWhenTheMoveFails(t *testing.T) {
	withStubbedNames(t)
	withStubbedManaged(t)
	withStubbedPending(t)
	withStubbedActive(t)

	root, profilePath, archiveRoot := removeFixture(t, "Claude_Stuck")
	plat := newRemovePlatform(t, root, archiveRoot, "Claude_Stuck")
	if err := SetManaged([]string{"Claude_Stuck", "Claude_Keep"}); err != nil {
		t.Fatal(err)
	}
	if err := SetProfileName("Claude_Stuck", "Stuck one"); err != nil {
		t.Fatal(err)
	}

	origRename, origDelay := renameProfile, archiveRenameDelay
	renameProfile = func(from, to string) error { return errors.New("in use by another process") }
	archiveRenameDelay = time.Millisecond
	t.Cleanup(func() { renameProfile, archiveRenameDelay = origRename, origDelay })

	if _, err := RemoveProfile(plat, "Claude_Stuck"); err == nil {
		t.Fatal("RemoveProfile reported success on a failed move")
	}
	if _, err := os.Stat(profilePath); err != nil {
		t.Fatalf("the profile directory vanished on a failed move: %v", err)
	}
	managed, _ := LoadManaged()
	found := false
	for _, m := range managed {
		if m == "Claude_Stuck" {
			found = true
		}
	}
	if !found {
		t.Fatal("a failed move unmanaged the profile; it is now hidden but still on disk")
	}
	if n := DisplayName("Claude_Stuck"); n != "Stuck one" {
		t.Fatalf("a failed move cleared the display name: %q", n)
	}
}
```

Add `errors` and `time` to the test file's imports.

- [ ] **Step 2: Run them**

Run: `go test ./core/ -run TestRemoveProfile -v`
Expected: PASS, all four. They test the implementation from Task 2 and should need no production change. If one fails, the order of operations in `RemoveProfile` is wrong — fix `removeprofile.go`, not the test.

- [ ] **Step 3: Run the whole suite**

Run: `go test ./...`
Expected: PASS. `archiveRenameDelay` and `renameProfile` are package-level vars; confirm no other test in `core` runs in parallel with these and reads them.

- [ ] **Step 4: Commit**

```bash
git add core/removeprofile_test.go
git commit -m "test(core): removal refuses the live account and survives a failed move

The failed-move case is the one a real filesystem will not produce on demand
and the one where a wrong order does permanent damage: registries written
first would leave the folder in place and unlisted, invisible in the panel
but back on the next Rescan."
```

---

### Task 4: Account settings screen with the remove button

The Rename screen becomes the per-account screen and gains the red section.

**Files:**
- Modify: `internal/panelui/render.go` (`RenderRename` → `RenderAccount`, new `AccountVM`, new `askRemove` JS)
- Modify: `internal/panelui/render_test.go`
- Modify: `cmd/mcs-menubar/main.go` (call site in the `"rename"` view case)
- Modify: `cmd/mcs-tray/panel_windows.go` (same)

**Interfaces:**
- Produces:
  ```go
  type AccountVM struct {
      Folder  string // the identity, the key every action carries
      Name    string // display name
      Convos  int    // conversations in its own account bucket
      Current bool   // the account in use: remove is disabled
      OnlyOne bool   // the only profile listed: remove is hidden
  }
  func RenderAccount(vm AccountVM) string
  ```
  and the panel action `removeProfile` carrying the folder as its argument.

- [ ] **Step 1: Write the failing tests**

Append to `internal/panelui/render_test.go`:

```go
func TestRenderAccountOffersRemove(t *testing.T) {
	h := RenderAccount(AccountVM{Folder: "Claude_Old", Name: "Old one", Convos: 34})
	if !strings.Contains(h, "Remove this account") {
		t.Fatal("no remove button on the account screen")
	}
	if !strings.Contains(h, "sbtn danger") {
		t.Fatal("the remove button is not styled as destructive")
	}
	if !strings.Contains(h, "Account settings") {
		t.Fatal("the screen is still titled as rename-only")
	}
	if !strings.Contains(h, "archived, not deleted") {
		t.Fatal("the screen does not say the folder is kept")
	}
}

func TestRenderAccountDisablesRemoveForTheAccountInUse(t *testing.T) {
	h := RenderAccount(AccountVM{Folder: "Claude_Live", Name: "Live", Convos: 12, Current: true})
	if !strings.Contains(h, "disabled") {
		t.Fatal("remove is not disabled for the account in use")
	}
	if !strings.Contains(h, "Switch to another account first") {
		t.Fatal("no reason given for the disabled button")
	}
}

func TestRenderAccountHidesRemoveWhenItIsTheOnlyProfile(t *testing.T) {
	h := RenderAccount(AccountVM{Folder: "Claude", Name: "Claude", Convos: 5, OnlyOne: true})
	if strings.Contains(h, "Remove this account") {
		t.Fatal("removing the last profile would leave an empty panel with no way back")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./internal/panelui/ -run TestRenderAccount -v`
Expected: FAIL, `undefined: RenderAccount`.

- [ ] **Step 3: Implement the renderer**

In `internal/panelui/render.go`, replace `RenderRename` with `RenderAccount`. Keep the input field, its id `rn`, `renameSave`, and the focus/select script exactly as they are: Esc-inside-a-text-input already depends on that id.

```go
// AccountVM drives the per-account screen: renaming, and removal.
type AccountVM struct {
	Folder  string
	Name    string
	Convos  int
	Current bool
	OnlyOne bool
}

// RenderAccount is the in-panel screen reached from the pencil on an account row.
// It was the Rename screen; removal lives at the bottom of it rather than as a bin
// icon beside the pencil, because two small adjacent icons is the arrangement most
// likely to be mis-tapped, and the delete-button rule is red and away from edit.
func RenderAccount(vm AccountVM) string {
	esc := html.EscapeString

	remove := ""
	if !vm.OnlyOne {
		btn := fmt.Sprintf(`<button class="sbtn danger" data-folder="%s" data-name="%s" data-convos="%d" onclick="askRemove(this)">Remove this account</button>`,
			esc(vm.Folder), esc(vm.Name), vm.Convos)
		note := `<div class="hint">Removing takes this account off the list. Its folder is archived, not deleted.</div>`
		if vm.Current {
			btn = `<button class="sbtn danger" disabled>Remove this account</button>`
			note = `<div class="hint">Switch to another account first, then you can remove it.</div>`
		}
		remove = `<div class="dangerzone">` + note + btn + `</div>`
	}

	body := `<div class="header">
  <button class="back" onclick="send('showList','')">‹</button>
  <div class="htext"><h1>Account settings</h1><p>Rename or remove this account</p></div>
</div>
<input id="rn" class="rninput" type="text" value="` + esc(vm.Name) + `" placeholder="Display name">
<div class="footer">
  <button class="btn btn-light" onclick="send('showList','')">Cancel</button>
  <button class="btn btn-primary" data-folder="` + esc(vm.Folder) + `" onclick="renameSave(this.dataset.folder)">Save</button>
</div>` + remove + `
<script>var e=document.getElementById('rn'); e.focus(); e.select();</script>`
	return shell(body)
}
```

Add the CSS beside the other panel rules, following the existing style (see `.dbgnote` for the pattern):

```
.dangerzone{border-top:1px solid #ece9f4;margin-top:18px;padding-top:14px}
```

Add the JS beside `askReport`:

```js
  // The folder and name travel as data-* and are read back with dataset, never
  // interpolated into the inline handler: a name with an apostrophe would
  // otherwise break the parse (the v0.9.1 bug).
  function askRemove(el){
    var n = parseInt(el.dataset.convos, 10) || 0;
    var what = n === 1 ? 'its 1 conversation' : 'all ' + n + ' conversations';
    askConfirm('removeProfile', el.dataset.folder, 'Remove '+el.dataset.name+'?',
      'It disappears from the switcher. Its folder, with '+what+', moves to the archive folder you can open from Settings.',
      'Remove',
      'To use this account again you have to sign in to it again.');
  }
```

- [ ] **Step 4: Update both hosts' call sites**

`cmd/mcs-menubar/main.go`, in the `"rename"` view case:

```go
	case "rename":
		mu.Lock()
		f := renameFolder
		mu.Unlock()
		htmlStr = panelui.RenderAccount(accountVM(f))
```

and add the helper beside `buildProfiles`:

```go
// accountVM finds the row the account screen is about. Built from the same list
// the panel shows, so the conversation count on the confirmation is the number
// the user was already looking at.
func accountVM(folder string) panelui.AccountVM {
	profiles := buildProfiles()
	vm := panelui.AccountVM{Folder: folder, Name: core.DisplayName(folder), OnlyOne: len(profiles) <= 1}
	for _, p := range profiles {
		if p.Folder == folder {
			vm.Name, vm.Convos, vm.Current = p.Name, p.Convos, p.Current
			break
		}
	}
	return vm
}
```

Make the identical change in `cmd/mcs-tray/panel_windows.go`, using that file's own profile builder (it has one at the top of its render switch; read it and match).

- [ ] **Step 5: Run the tests and both builds**

Run: `go test ./... && go build ./... && GOOS=windows go build ./...`
Expected: PASS, no output from the builds.

- [ ] **Step 6: Commit**

```bash
git add internal/panelui/ cmd/
git commit -m "feat(panel): account screen carries the remove button

The Rename screen becomes Account settings and gains a red remove section
below a rule, rather than a bin icon beside the pencil: two small adjacent
icons is the arrangement most likely to be mis-tapped.

Disabled with a reason for the account in use, absent entirely when it is the
only profile listed, which would otherwise leave an empty panel."
```

---

### Task 5: The result screen

**Files:**
- Modify: `internal/panelui/render.go` (`RemovedVM`, `RenderRemoved`)
- Modify: `internal/panelui/render_test.go`

**Interfaces:**
- Produces:
  ```go
  type RemovedVM struct {
      Folder     string // for Try again after a failure
      Name       string
      Convos     int
      ArchiveDir string // base name of where it landed; empty on failure
      Err        string // empty on success
  }
  func RenderRemoved(vm RemovedVM) string
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestRenderRemovedNamesWhereItWent(t *testing.T) {
	h := RenderRemoved(RemovedVM{Name: "Old one", Convos: 34, ArchiveDir: "Claude_Old-20260804-142233"})
	if !strings.Contains(h, "Old one removed") {
		t.Fatal("the result screen does not say what happened")
	}
	if !strings.Contains(h, "Claude_Old-20260804-142233") {
		t.Fatal("the result screen does not name the archived folder")
	}
	if !strings.Contains(h, "openArchive") {
		t.Fatal("no way to go and look at the archived folder")
	}
}

func TestRenderRemovedSaysNothingMovedOnFailure(t *testing.T) {
	h := RenderRemoved(RemovedVM{Folder: "Claude_Old", Name: "Old one",
		Err: "Claude may still be holding its files."})
	if !strings.Contains(h, "was not removed") {
		t.Fatal("a failure does not read as a failure")
	}
	if !strings.Contains(h, "still on your list") {
		t.Fatal("a failure does not say the account survived")
	}
	if !strings.Contains(h, "Claude may still be holding its files.") {
		t.Fatal("the underlying reason is not shown")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./internal/panelui/ -run TestRenderRemoved -v`
Expected: FAIL, `undefined: RenderRemoved`.

- [ ] **Step 3: Implement**

```go
// RemovedVM drives the screen shown after a removal, in either outcome.
type RemovedVM struct {
	Folder     string
	Name       string
	Convos     int
	ArchiveDir string
	Err        string
}

// RenderRemoved reports the outcome on its own screen rather than as a line at
// the top of a changed list. A removal that reports itself in one line is the
// case where the user cannot tell whether it happened, which for a destructive-
// looking action is the one thing the screen has to answer.
func RenderRemoved(vm RemovedVM) string {
	esc := html.EscapeString

	if vm.Err != "" {
		body := `<div class="header">
  <button class="back" onclick="send('showList','')">‹</button>
  <div class="htext"><h1>` + esc(vm.Name) + ` was not removed</h1><p>Nothing was moved</p></div>
</div>
<div class="errbox">` + esc(vm.Err) + `</div>
<div class="hint">The account is still on your list, so you can try again.</div>
<div class="footer">
  <button class="btn btn-light" onclick="send('showList','')">Back</button>
  <button class="btn btn-primary" data-folder="` + esc(vm.Folder) + `" onclick="send('removeProfile',this.dataset.folder)">Try again</button>
</div>`
		return shell(body)
	}

	what := "Its conversations are untouched"
	if vm.Convos == 1 {
		what = "Its 1 conversation is untouched"
	} else if vm.Convos > 1 {
		what = fmt.Sprintf("Its %d conversations are untouched", vm.Convos)
	}
	body := `<div class="header">
  <div class="htext"><h1>` + esc(vm.Name) + ` removed</h1><p>It is off the switcher</p></div>
</div>
<div class="hint">` + esc(what) + `, in a folder called <b>` + esc(vm.ArchiveDir) + `</b> inside your archive.</div>
<button class="sbtn" onclick="send('openArchive','')">Open archive folder</button>
<div class="footer">
  <button class="btn btn-primary" onclick="send('showList','')">Done</button>
</div>`
	return shell(body)
}
```

Check `.errbox` and `.hint` exist in the stylesheet; both are used by `RenderNewProfile` already. If the header without a back button looks wrong against the other screens, keep the back button on the success screen too.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/panelui/ -v`
Expected: PASS, including `TestNoEmDash`.

- [ ] **Step 5: Commit**

```bash
git add internal/panelui/
git commit -m "feat(panel): a result screen for a removal

Success names the archived folder and offers to open it; failure says plainly
that nothing moved and the account is still listed. A removal reported as one
line at the top of a changed list is the case where the user cannot tell
whether it happened."
```

---

### Task 6: macOS host wiring

**Files:**
- Modify: `cmd/mcs-menubar/main.go`

**Interfaces:**
- Consumes: `core.RemoveProfile` (Task 2), `panelui.RenderRemoved` (Task 5).

- [ ] **Step 1: Add the state**

Beside `renameFolder` and the other view state, under the same `mu`:

```go
// removedVM holds the outcome of the last removal, drawn by the "removed" view.
// Under mu with the rest of the view state.
var removedVM panelui.RemovedVM
```

- [ ] **Step 2: Add the action**

In `goPanelAction`'s switch, beside `mergeConfirm`. Copy the busy guard and goroutine shape from the `backup` / `mergeConfirm` cases in the same file rather than inventing one; the guard is what stops two long actions overlapping.

```go
	case "removeProfile":
		if getBusy() {
			return
		}
		folder := arg
		vm := accountVM(folder)
		setBusy(true)
		go func() {
			defer setBusy(false)
			out := panelui.RemovedVM{Folder: folder, Name: vm.Name, Convos: vm.Convos}
			dest, err := core.RemoveProfile(plat, folder)
			if err != nil {
				out.Err = err.Error()
			} else {
				out.ArchiveDir = filepath.Base(dest)
			}
			mu.Lock()
			removedVM = out
			currentView = "removed"
			mu.Unlock()
			reloadPanel()
		}()
```

`accountVM` is read before the goroutine starts, because after the removal the profile is gone and its name and count are no longer resolvable.

- [ ] **Step 3: Add the view case**

```go
	case "removed":
		mu.Lock()
		vm := removedVM
		mu.Unlock()
		htmlStr = panelui.RenderRemoved(vm)
```

- [ ] **Step 4: Build and run by hand**

Run: `go build ./... && go test ./...`
Expected: PASS.

Then build the app bundle the way the repo already does (check `Makefile` or the release workflow for the exact target) and click through: pencil on a non-current account, Remove, confirm, read the result screen, open the archive folder and see the directory. Then Rescan and confirm the account does not come back.

`LSUIElement` menu-bar apps cannot be driven by the screenshot tooling. This step is done by hand and the result recorded in the commit message.

- [ ] **Step 5: Commit**

```bash
git add cmd/mcs-menubar/
git commit -m "feat(macos): wire up account removal

The account's name and conversation count are read before the goroutine
starts: after the removal the profile is gone and neither is resolvable.

Verified by hand on a real profile: removed, archive folder holds the
directory, Rescan does not bring it back."
```

---

### Task 7: Windows host wiring

**Files:**
- Modify: `cmd/mcs-tray/panel_windows.go`

**Interfaces:**
- Consumes: the same as Task 6.

- [ ] **Step 1: Mirror Task 6**

Make the identical change in `cmd/mcs-tray/panel_windows.go`: the `removedVM` global under that file's mutex, the `removeProfile` action in its switch beside `mergeConfirm`, and the `"removed"` case in its render switch. Use that file's own busy guard, status setters and profile builder; the two hosts share the renderer, not their plumbing.

- [ ] **Step 2: Verify it compiles**

Run: `GOOS=windows go build ./... && GOOS=windows go vet ./... && go test ./...`
Expected: no output from the builds, PASS from the tests.

- [ ] **Step 3: Commit**

```bash
git add cmd/mcs-tray/
git commit -m "feat(windows): wire up account removal

Compile-checked only. Both the standalone and Store paths need a real
Windows run, and the Store build's refusal of the slot occupant is the part
with no coverage here at all."
```

---

### Task 8: Docs, changelog, and the version

**Files:**
- Modify: `README.md`, `README.zh-TW.md`, `FILELIST.md`, `CHANGELOG.md`
- Modify: `core/version.go` (only after the maintainer approves the minor bump)

- [ ] **Step 1: Document the feature**

In both READMEs, beside however renaming and merging are described today, add what removal does and what it refuses. Say plainly that the folder is archived rather than deleted, that the archive folder is reachable from Settings, and that the account in use cannot be removed. Keep the zh-TW file in step: it is a translation, not a shorter version.

- [ ] **Step 2: FILELIST**

Add `core/removeprofile.go` and this plan file, in the shape the surrounding entries use.

- [ ] **Step 3: CHANGELOG**

Add the entry under a new `0.13.0` heading, matching the format of the `0.12.0` entry above it.

- [ ] **Step 4: Ask before bumping the version**

**Stop here and ask the maintainer.** The house rule is that minor and major bumps are confirmed first; only patch bumps are automatic. Once approved, set `core.Version` to `0.13.0` and confirm the CHANGELOG heading matches.

- [ ] **Step 5: Commit**

```bash
git add README.md README.zh-TW.md FILELIST.md CHANGELOG.md core/version.go
git commit -m "docs: remove-an-account, and 0.13.0"
```

---

## Verification before calling this done

- [ ] `go test ./...` passes.
- [ ] `gofmt -l .` prints nothing. A permanently non-empty list is a broken alarm.
- [ ] `go build ./...` and `GOOS=windows go build ./...` both succeed.
- [ ] `go vet ./...` and `GOOS=windows go vet ./...` are clean.
- [ ] No `os.RemoveAll` was added against a profile directory.
- [ ] The macOS flow was clicked through by hand, including the archive folder and a Rescan afterwards.
- [ ] The Windows flow is filed as an issue for a real-machine check, naming both the standalone and Store builds.
- [ ] `superpowers:verification-before-completion`, then `superpowers:requesting-code-review`, then `superpowers:receiving-code-review`. These are not optional.
