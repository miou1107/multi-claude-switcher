# Manual Align + Auto Sync-on-Switch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a manual directional "Align" tray action and an opt-in "Auto Sync on Switch" toggle, so a user can copy one account's Code sessions into another on demand, or have every switch keep both accounts identical.

**Architecture:** Reuse the tested `core.SyncSessions` (directional, re-buckets into the target account, additive/newer-wins). Add a bidirectional union wrapper, a persisted settings store for the toggle, a `Switcher.ManualAlign` controller that closes → backs up → syncs → reopens the same account, and revise `SafeSwitch` to move data only when the toggle is ON. Tray wiring adds a checkbox (with an enable-time warning) and a directional submenu.

**Tech Stack:** Go 1.22, `github.com/getlantern/systray`, macOS `osascript` for native dialogs.

## Global Constraints

- Product version is the single source of truth in `core/version.go` (`var Version`); keep `core.Version`, `CHANGELOG.md`, and the git tag in sync at release.
- Repo output (code, comments, docs, commit messages, UI copy) is in **English**.
- Git commits: **never** add `Co-Authored-By`; contributors show Vin only.
- macOS only; unsigned (no Apple Developer account). Baseline macOS 11.
- **Phase 1 scope = Code sessions only** (`claude-code-sessions`). Agent Mode is a separate gated Phase 2, not in this work.
- **Auto sync defaults OFF** (privacy: cross-account conversation merge is opt-in).
- **Safety invariants (never violate):** never write into a running Claude Desktop (terminate first); back up every profile before it is written; sync is additive and newer-wins (never a silent overwrite).
- After code changes, keep `README.md`, `FILELIST.md`, `CHANGELOG.md` in sync.
- Do NOT push or tag a release; that is user-triggered (`v*` tag → CI).

---

### Task 1: Settings store (`core/settings.go`)

Persist two booleans to `~/.multi-claude-switcher/settings.json`, mirroring the `core/names.go` and `core/loginitem.go` patterns. The path is a `var` so tests can redirect it.

**Files:**
- Create: `core/settings.go`
- Test: `core/settings_test.go`

**Interfaces:**
- Produces: `core.AutoSyncOnSwitch() bool`, `core.SetAutoSyncOnSwitch(bool) error`, `core.AutoSyncWarningDismissed() bool`, `core.SetAutoSyncWarningDismissed(bool) error`; test-only stub var `settingsPath func() string`.

- [ ] **Step 1: Write the failing test**

```go
// core/settings_test.go
package core

import (
	"path/filepath"
	"testing"
)

// withStubbedSettings redirects the settings file to a temp dir so tests never
// touch the real ~/.multi-claude-switcher/settings.json (same idea as
// loginitem_test.go's stubbed dir).
func withStubbedSettings(t *testing.T) {
	t.Helper()
	orig := settingsPath
	dir := t.TempDir()
	settingsPath = func() string { return filepath.Join(dir, "settings.json") }
	t.Cleanup(func() { settingsPath = orig })
}

func TestSettingsDefaultFalse(t *testing.T) {
	withStubbedSettings(t)
	if AutoSyncOnSwitch() {
		t.Error("autoSyncOnSwitch should default false when no file exists")
	}
	if AutoSyncWarningDismissed() {
		t.Error("autoSyncWarningDismissed should default false when no file exists")
	}
}

func TestSettingsRoundTripAndNoClobber(t *testing.T) {
	withStubbedSettings(t)
	if err := SetAutoSyncOnSwitch(true); err != nil {
		t.Fatal(err)
	}
	if !AutoSyncOnSwitch() {
		t.Error("expected autoSyncOnSwitch true after set")
	}
	// Writing the second flag must not clobber the first.
	if err := SetAutoSyncWarningDismissed(true); err != nil {
		t.Fatal(err)
	}
	if !AutoSyncOnSwitch() {
		t.Error("setting warning-dismissed clobbered autoSyncOnSwitch")
	}
	if !AutoSyncWarningDismissed() {
		t.Error("expected autoSyncWarningDismissed true after set")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/ -run TestSettings -v`
Expected: FAIL — `undefined: settingsPath` / `undefined: AutoSyncOnSwitch`.

- [ ] **Step 3: Write minimal implementation**

```go
// core/settings.go
package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var settingsMu sync.Mutex

// settingsPath is where user settings are stored. It is a var so tests can
// redirect it to a temp dir (same pattern as loginitem.go's launchAgentsDir).
var settingsPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "multi-claude-switcher-settings.json")
	}
	return filepath.Join(home, ".multi-claude-switcher", "settings.json")
}

type settings struct {
	AutoSyncOnSwitch         bool `json:"autoSyncOnSwitch"`
	AutoSyncWarningDismissed bool `json:"autoSyncWarningDismissed"`
}

func loadSettingsLocked() settings {
	var s settings
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		return s // defaults (all false) when absent/unreadable
	}
	_ = json.Unmarshal(data, &s)
	return s
}

func saveSettingsLocked(s settings) error {
	if err := os.MkdirAll(filepath.Dir(settingsPath()), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// Atomic write: a crash mid-write must not corrupt the existing settings.
	tmp := settingsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, settingsPath())
}

// AutoSyncOnSwitch reports whether switching should bidirectionally sync.
func AutoSyncOnSwitch() bool {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return loadSettingsLocked().AutoSyncOnSwitch
}

// SetAutoSyncOnSwitch persists the auto sync-on-switch toggle.
func SetAutoSyncOnSwitch(v bool) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	s := loadSettingsLocked()
	s.AutoSyncOnSwitch = v
	return saveSettingsLocked(s)
}

// AutoSyncWarningDismissed reports whether the enable-time warning is suppressed.
func AutoSyncWarningDismissed() bool {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return loadSettingsLocked().AutoSyncWarningDismissed
}

// SetAutoSyncWarningDismissed persists the "don't ask again" choice.
func SetAutoSyncWarningDismissed(v bool) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	s := loadSettingsLocked()
	s.AutoSyncWarningDismissed = v
	return saveSettingsLocked(s)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./core/ -run TestSettings -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add core/settings.go core/settings_test.go
git commit -m "feat: settings store for auto sync toggle and warning-dismissed flag"
```

---

### Task 2: Bidirectional union (`core.SyncBidirectional`)

**Files:**
- Modify: `core/sync.go` (append function at end)
- Test: `core/sync_test.go` (append test)

**Interfaces:**
- Consumes: `SyncSessions(src, dst string) (*SyncReport, error)` (existing).
- Produces: `core.SyncBidirectional(profileA, profileB string) error`.

- [ ] **Step 1: Write the failing test**

```go
// append to core/sync_test.go
func TestSyncBidirectionalUnion(t *testing.T) {
	tempDir := t.TempDir()
	a := filepath.Join(tempDir, "A")
	b := filepath.Join(tempDir, "B")
	writeAccountConfig(t, a, "a-uuid")
	writeAccountConfig(t, b, "b-uuid")
	// Each account has one distinct session under its OWN account bucket.
	writeSessionFile(t, a, filepath.Join("a-uuid", "local_a.json"), `{"v":"A"}`, time.Now())
	writeSessionFile(t, b, filepath.Join("b-uuid", "local_b.json"), `{"v":"B"}`, time.Now())

	if err := SyncBidirectional(a, b); err != nil {
		t.Fatalf("SyncBidirectional failed: %v", err)
	}

	// After union, BOTH accounts hold BOTH sessions, each under its own bucket.
	for _, want := range []string{
		filepath.Join(platformSessions(a), "a-uuid", "local_a.json"),
		filepath.Join(platformSessions(a), "a-uuid", "local_b.json"),
		filepath.Join(platformSessions(b), "b-uuid", "local_a.json"),
		filepath.Join(platformSessions(b), "b-uuid", "local_b.json"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected %s after bidirectional union: %v", want, err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/ -run TestSyncBidirectionalUnion -v`
Expected: FAIL — `undefined: SyncBidirectional`.

- [ ] **Step 3: Write minimal implementation**

```go
// append to core/sync.go
// SyncBidirectional makes both profiles' Code sessions converge to the union of
// the two. It syncs source->target first (so the target then holds the union),
// then target->source, leaving both accounts with A ∪ B. SyncSessions is
// additive and skips identical files, so this is safe and idempotent. Both
// profiles must be logged in (SyncSessions errors otherwise).
func SyncBidirectional(profileA, profileB string) error {
	if _, err := SyncSessions(profileA, profileB); err != nil {
		return err
	}
	if _, err := SyncSessions(profileB, profileA); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./core/ -run TestSyncBidirectionalUnion -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add core/sync.go core/sync_test.go
git commit -m "feat: SyncBidirectional union helper (both accounts converge)"
```

---

### Task 3: Revise `SafeSwitch` to honor the toggle

Switch moves data ONLY when auto sync is ON and both profiles are logged in; then it backs up BOTH profiles (bidirectional writes both) and unions them. OFF = pure switch, no write, no backup. This changes existing behavior (today's switch always one-way syncs), so existing switch tests are updated too.

**Files:**
- Modify: `core/switch.go` (rewrite `SafeSwitch` body)
- Test: `core/switch_test.go` (stub settings in all tests; update backup-fail test; add OFF/ON tests)

**Interfaces:**
- Consumes: `AutoSyncOnSwitch()` (Task 1), `SyncBidirectional` (Task 2), `platform.GetProfileAccountUUID`, `BackupManager.BackupIfHasData`, `Platform.{IsAppRunning,TerminateApp,LaunchProfile}`.

- [ ] **Step 1: Update the shared mock and existing tests, add new tests**

Add a `detected` field to `mockPlatform` (needed by Task 4 too) and make every switch test deterministic by stubbing settings. Replace the three existing tests' setup and add two new ones.

```go
// core/switch_test.go — update the mock:
type mockPlatform struct {
	running      bool
	launched     bool
	launchedPath string
	terminated   bool
	detected     string // DetectRunningProfile result
}

func (m *mockPlatform) DetectRunningProfile() (string, error) { return m.detected, nil }
```

```go
// core/switch_test.go — replace TestSafeSwitchAbortsWhenBackupFails with the
// ON-path version (backup only happens when auto sync is ON and both logged in):
func TestSafeSwitchAbortsWhenBackupFails(t *testing.T) {
	withStubbedSettings(t)
	if err := SetAutoSyncOnSwitch(true); err != nil { // ON so the backup step runs
		t.Fatal(err)
	}
	tempDir := t.TempDir()

	src := filepath.Join(tempDir, "Src")
	writeAccountConfig(t, src, "uuid1")
	srcSessions := filepath.Join(platform.GetProfileSessionsDir(src), "uuid1")
	if err := os.MkdirAll(srcSessions, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcSessions, "local_src.json"), []byte(`{"src":1}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Target has real data we must not lose.
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, dst, "uuid2")
	dstSessions := filepath.Join(platform.GetProfileSessionsDir(dst), "uuid2")
	if err := os.MkdirAll(dstSessions, 0755); err != nil {
		t.Fatal(err)
	}
	dstFile := filepath.Join(dstSessions, "local_dst.json")
	original := []byte(`{"dst":"precious"}`)
	if err := os.WriteFile(dstFile, original, 0644); err != nil {
		t.Fatal(err)
	}

	// Force backup to fail: a regular file where the backup root needs a dir.
	blocker := filepath.Join(tempDir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	bm := NewBackupManager(filepath.Join(blocker, "backups"))

	mp := &mockPlatform{}
	s := NewSwitcher(mp, bm)

	if err := s.SafeSwitch(src, dst); err == nil {
		t.Fatal("expected SafeSwitch to abort when backup fails, got nil error")
	}
	if mp.launched {
		t.Error("target profile must NOT be launched after a failed backup")
	}
	got, readErr := os.ReadFile(dstFile)
	if readErr != nil {
		t.Fatalf("target file disappeared: %v", readErr)
	}
	if string(got) != string(original) {
		t.Errorf("target file was overwritten despite backup failure: got %q", got)
	}
}
```

```go
// core/switch_test.go — add `withStubbedSettings(t)` as the FIRST line of
// TestSafeSwitchLaunchesWhenTargetNotLoggedIn and TestSafeSwitchProceedsWhenTargetIsEmpty
// so they don't read the real settings file. Default (OFF) is fine for both;
// they still assert the target launches.

// New: OFF is a pure switch — no session data moves.
func TestSafeSwitchOffMovesNoData(t *testing.T) {
	withStubbedSettings(t) // default OFF
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "src-uuid")
	writeAccountConfig(t, dst, "dst-uuid")
	writeSessionFile(t, src, filepath.Join("src-uuid", "local_a.json"), `{"v":"A"}`, time.Now())

	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	mp := &mockPlatform{}
	s := NewSwitcher(mp, bm)

	if err := s.SafeSwitch(src, dst); err != nil {
		t.Fatalf("pure switch should succeed: %v", err)
	}
	if !mp.launched {
		t.Error("target must still be launched on a pure switch")
	}
	// The source session must NOT have been copied into the target.
	if _, err := os.Stat(filepath.Join(platformSessions(dst), "dst-uuid", "local_a.json")); err == nil {
		t.Error("OFF switch moved session data — it must be a pure switch")
	}
}

// New: ON unions both accounts.
func TestSafeSwitchOnUnionsBothAccounts(t *testing.T) {
	withStubbedSettings(t)
	if err := SetAutoSyncOnSwitch(true); err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "src-uuid")
	writeAccountConfig(t, dst, "dst-uuid")
	writeSessionFile(t, src, filepath.Join("src-uuid", "local_a.json"), `{"v":"A"}`, time.Now())
	writeSessionFile(t, dst, filepath.Join("dst-uuid", "local_b.json"), `{"v":"B"}`, time.Now())

	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	mp := &mockPlatform{}
	s := NewSwitcher(mp, bm)

	if err := s.SafeSwitch(src, dst); err != nil {
		t.Fatalf("ON switch failed: %v", err)
	}
	if !mp.launched {
		t.Error("target must be launched")
	}
	for _, want := range []string{
		filepath.Join(platformSessions(dst), "dst-uuid", "local_a.json"),
		filepath.Join(platformSessions(src), "src-uuid", "local_b.json"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected union file %s: %v", want, err)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/ -run TestSafeSwitch -v`
Expected: FAIL — new tests reference behavior not yet implemented (OFF still one-way syncs; `detected` field / settings gating not wired). Compilation of the mock change is fine; the OFF/ON assertions fail.

- [ ] **Step 3: Rewrite `SafeSwitch`**

```go
// core/switch.go — replace the SafeSwitch function body:

// SafeSwitch closes the running app, optionally aligns sessions, then launches
// the target. Data is moved ONLY when auto sync is ON and both profiles are
// logged in: then it backs up BOTH profiles (bidirectional align writes both)
// and unions their sessions. With auto sync OFF (default) the switch moves no
// data at all — a pure account switch.
func (s *Switcher) SafeSwitch(srcProfilePath, dstProfilePath string) error {
	log.Printf("[Safe Switch] Starting switch from %s to %s...", srcProfilePath, dstProfilePath)

	// Step 1: close any running Claude Desktop (never write into a live profile).
	running, procs, err := s.Platform.IsAppRunning()
	if err != nil {
		return fmt.Errorf("failed to check running processes: %w", err)
	}
	if running {
		log.Printf("[Safe Switch] Terminating %d running Claude process(es)...", len(procs))
		if err := s.Platform.TerminateApp(); err != nil {
			return fmt.Errorf("failed to terminate Claude process: %w", err)
		}
	}

	// Step 2: align only when the user opted in AND both profiles are logged in.
	if AutoSyncOnSwitch() {
		_, srcErr := platform.GetProfileAccountUUID(srcProfilePath)
		_, dstErr := platform.GetProfileAccountUUID(dstProfilePath)
		if srcErr != nil || dstErr != nil {
			log.Printf("[Safe Switch] Auto sync on, but a profile has no account yet (src: %v, dst: %v). Skipping align.", srcErr, dstErr)
		} else {
			// Bidirectional align writes into BOTH profiles, so back up both.
			if _, err := s.BackupManager.BackupIfHasData(srcProfilePath); err != nil {
				return fmt.Errorf("aborting switch: failed to back up source profile (refusing to overwrite without a backup): %w", err)
			}
			if _, err := s.BackupManager.BackupIfHasData(dstProfilePath); err != nil {
				return fmt.Errorf("aborting switch: failed to back up target profile (refusing to overwrite without a backup): %w", err)
			}
			log.Printf("[Safe Switch] Auto sync on: unioning sessions between both accounts...")
			if err := SyncBidirectional(srcProfilePath, dstProfilePath); err != nil {
				return fmt.Errorf("failed to auto sync sessions: %w", err)
			}
		}
	} else {
		log.Printf("[Safe Switch] Auto sync off: pure switch, no session data moved.")
	}

	// Step 3: launch the target profile.
	log.Printf("[Safe Switch] Launching Claude Desktop profile: %s...", dstProfilePath)
	if err := s.Platform.LaunchProfile(dstProfilePath); err != nil {
		return fmt.Errorf("failed to launch target profile: %w", err)
	}

	log.Printf("[Safe Switch] Switch completed successfully!")
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/ -run TestSafeSwitch -v`
Expected: PASS (all five: NotLoggedIn, AbortsWhenBackupFails, ProceedsWhenTargetIsEmpty, OffMovesNoData, OnUnionsBothAccounts).

- [ ] **Step 5: Commit**

```bash
git add core/switch.go core/switch_test.go
git commit -m "feat: switch aligns bidirectionally only when auto sync is on"
```

---

### Task 4: `Switcher.ManualAlign` controller (`core/align.go`)

Directional align that leaves the user on the account they started on.

**Files:**
- Create: `core/align.go`
- Test: `core/align_test.go`

**Interfaces:**
- Consumes: `Platform.{DetectRunningProfile,IsAppRunning,TerminateApp,LaunchProfile}`, `BackupManager.BackupIfHasData`, `SyncSessions`.
- Produces: `func (s *Switcher) ManualAlign(srcProfilePath, dstProfilePath string) (*SyncReport, error)`.

- [ ] **Step 1: Write the failing tests**

```go
// core/align_test.go
package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManualAlignReturnsToRunningProfile(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "src-uuid")
	writeAccountConfig(t, dst, "dst-uuid")
	writeSessionFile(t, src, filepath.Join("src-uuid", "local_a.json"), `{"v":"work"}`, time.Now())

	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	mp := &mockPlatform{running: true, detected: src} // user is on src
	s := NewSwitcher(mp, bm)

	report, err := s.ManualAlign(src, dst)
	if err != nil {
		t.Fatalf("ManualAlign failed: %v", err)
	}
	if !mp.terminated {
		t.Error("expected Claude Desktop to be closed before writing")
	}
	if mp.launchedPath != src {
		t.Errorf("expected to reopen the running profile %q, got %q", src, mp.launchedPath)
	}
	if report.CopiedCount != 1 {
		t.Errorf("expected 1 session copied, got %d", report.CopiedCount)
	}
	// Session must be re-homed under the TARGET account bucket.
	want := filepath.Join(platformSessions(dst), "dst-uuid", "local_a.json")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected session at %s: %v", want, err)
	}
}

func TestManualAlignNoRelaunchWhenNothingRunning(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "src-uuid")
	writeAccountConfig(t, dst, "dst-uuid")
	writeSessionFile(t, src, filepath.Join("src-uuid", "local_a.json"), `{"v":"x"}`, time.Now())

	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	mp := &mockPlatform{running: false, detected: ""}
	s := NewSwitcher(mp, bm)

	if _, err := s.ManualAlign(src, dst); err != nil {
		t.Fatalf("ManualAlign failed: %v", err)
	}
	if mp.launched {
		t.Error("must not launch anything when nothing was running")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/ -run TestManualAlign -v`
Expected: FAIL — `s.ManualAlign undefined`.

- [ ] **Step 3: Write the implementation**

```go
// core/align.go
package core

import "fmt"

// ManualAlign copies one profile's Code sessions into another WITHOUT changing
// which account is active. It remembers the running profile, closes Claude
// Desktop (never write into a live profile), backs up the target, syncs
// source->target (re-bucketed under the target account), then relaunches the
// profile that was running so the user is left exactly where they started.
func (s *Switcher) ManualAlign(srcProfilePath, dstProfilePath string) (*SyncReport, error) {
	// Remember which profile to reopen (the one the user is on now).
	relaunch, _ := s.Platform.DetectRunningProfile()

	running, _, err := s.Platform.IsAppRunning()
	if err != nil {
		return nil, fmt.Errorf("failed to check running processes: %w", err)
	}
	if running {
		if err := s.Platform.TerminateApp(); err != nil {
			return nil, fmt.Errorf("failed to close Claude Desktop: %w", err)
		}
	}

	// Never overwrite the target's data without a backup.
	if _, err := s.BackupManager.BackupIfHasData(dstProfilePath); err != nil {
		return nil, fmt.Errorf("aborting align: failed to back up target profile: %w", err)
	}

	report, err := SyncSessions(srcProfilePath, dstProfilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to sync sessions: %w", err)
	}

	// Put the user back on the account they were using (if any).
	if relaunch != "" {
		if err := s.Platform.LaunchProfile(relaunch); err != nil {
			return report, fmt.Errorf("sync done but could not reopen Claude Desktop (%s): %w", relaunch, err)
		}
	}
	return report, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/ -run TestManualAlign -v`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git add core/align.go core/align_test.go core/switch_test.go
git commit -m "feat: ManualAlign — directional sync that returns you to your account"
```

---

### Task 5: Tray "Auto Sync on Switch" toggle + enable warning

**Files:**
- Create: `cmd/mcs-tray/autosync.go`
- Test: `cmd/mcs-tray/autosync_test.go`
- Modify: `cmd/mcs-tray/main.go` (add menu item + handler)

**Interfaces:**
- Consumes: `core.AutoSyncOnSwitch/SetAutoSyncOnSwitch/AutoSyncWarningDismissed/SetAutoSyncWarningDismissed`, existing `osaQuote`, `notify`.
- Produces: `shouldWarnAutoSync(enabling, dismissed bool) bool`, `parseAutoSyncChoice(out string, runErr error) autoSyncChoice`, `askEnableAutoSync() autoSyncChoice`, `toggleAutoSync(*systray.MenuItem)`.

- [ ] **Step 1: Write the failing test**

```go
// cmd/mcs-tray/autosync_test.go
package main

import (
	"errors"
	"testing"
)

func TestShouldWarnAutoSync(t *testing.T) {
	cases := []struct{ enabling, dismissed, want bool }{
		{true, false, true},   // enabling, not dismissed -> warn
		{true, true, false},   // enabling, dismissed -> no warn
		{false, false, false}, // disabling -> never warn
		{false, true, false},
	}
	for _, c := range cases {
		if got := shouldWarnAutoSync(c.enabling, c.dismissed); got != c.want {
			t.Errorf("shouldWarnAutoSync(%v,%v)=%v want %v", c.enabling, c.dismissed, got, c.want)
		}
	}
}

func TestParseAutoSyncChoice(t *testing.T) {
	if parseAutoSyncChoice("", errors.New("cancelled")) != choiceCancel {
		t.Error("non-zero exit (cancel button) must map to choiceCancel")
	}
	if parseAutoSyncChoice("button returned:Enable\n", nil) != choiceEnable {
		t.Error("Enable button must map to choiceEnable")
	}
	if parseAutoSyncChoice("button returned:Enable, don't ask again\n", nil) != choiceEnableDontAsk {
		t.Error("don't-ask button must map to choiceEnableDontAsk")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/mcs-tray/ -run 'TestShouldWarn|TestParseAutoSync' -v`
Expected: FAIL — undefined `shouldWarnAutoSync` / `parseAutoSyncChoice` / `choiceCancel`.

- [ ] **Step 3: Write the implementation**

```go
// cmd/mcs-tray/autosync.go
package main

import (
	"log"
	"os/exec"
	"strings"

	"github.com/getlantern/systray"
	"github.com/miou1107/multi-claude-switcher/core"
)

// autoSyncChoice is the user's response to the enable-time warning.
type autoSyncChoice int

const (
	choiceCancel autoSyncChoice = iota
	choiceEnable
	choiceEnableDontAsk
)

// shouldWarnAutoSync reports whether to show the enable-time warning: only when
// turning the toggle ON and the user has not dismissed it. Turning OFF never warns.
func shouldWarnAutoSync(enabling, dismissed bool) bool {
	return enabling && !dismissed
}

// parseAutoSyncChoice maps an osascript `display dialog` result to a choice.
// The cancel button makes osascript exit non-zero (runErr != nil); otherwise
// stdout is "button returned:<label>".
func parseAutoSyncChoice(out string, runErr error) autoSyncChoice {
	if runErr != nil {
		return choiceCancel
	}
	if strings.Contains(strings.ToLower(out), "don't ask") {
		return choiceEnableDontAsk
	}
	return choiceEnable
}

// askEnableAutoSync shows the enable-time warning and returns the user's choice.
func askEnableAutoSync() autoSyncChoice {
	msg := "With this on, every account switch bidirectionally syncs — both accounts' conversations will merge. Enable?"
	script := "display dialog " + osaQuote(msg) +
		` buttons {"Cancel", "Enable", "Enable, don't ask again"}` +
		` default button "Enable" cancel button "Cancel" with title "Multi-Claude Switcher"`
	out, err := exec.Command("osascript", "-e", script).Output()
	return parseAutoSyncChoice(string(out), err)
}

// toggleAutoSync flips the auto sync-on-switch setting and syncs the menu
// checkbox. Enabling shows a one-time warning (unless previously dismissed).
func toggleAutoSync(m *systray.MenuItem) {
	if core.AutoSyncOnSwitch() {
		if err := core.SetAutoSyncOnSwitch(false); err != nil {
			log.Printf("Disable auto sync failed: %v", err)
			notify("Couldn't update Auto Sync", err.Error())
			return
		}
		m.Uncheck()
		log.Println("Auto sync on switch disabled")
		return
	}

	if shouldWarnAutoSync(true, core.AutoSyncWarningDismissed()) {
		switch askEnableAutoSync() {
		case choiceCancel:
			log.Println("Auto sync enable cancelled by user")
			return
		case choiceEnableDontAsk:
			if err := core.SetAutoSyncWarningDismissed(true); err != nil {
				log.Printf("Could not persist warning-dismissed: %v", err)
			}
		case choiceEnable:
			// proceed
		}
	}

	if err := core.SetAutoSyncOnSwitch(true); err != nil {
		log.Printf("Enable auto sync failed: %v", err)
		notify("Couldn't update Auto Sync", err.Error())
		return
	}
	m.Check()
	log.Println("Auto sync on switch enabled")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/mcs-tray/ -run 'TestShouldWarn|TestParseAutoSync' -v`
Expected: PASS.

- [ ] **Step 5: Wire the menu item into `onReady`**

In `cmd/mcs-tray/main.go`, add the checkbox next to `mLogin` (after line 75) and a handler goroutine next to the login handler (after the `mLogin` goroutine, ~line 169):

```go
// in onReady, after mLogin:
mAutoSync := systray.AddMenuItemCheckbox("Auto Sync on Switch", "Keep both accounts' sessions identical on every switch", core.AutoSyncOnSwitch())
```

```go
// in onReady, after the mLogin ClickedCh goroutine:
go func() {
	for range mAutoSync.ClickedCh {
		toggleAutoSync(mAutoSync)
	}
}()
```

- [ ] **Step 6: Verify it builds and all tests pass**

Run: `go build ./... && go test ./...`
Expected: build OK; all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/mcs-tray/autosync.go cmd/mcs-tray/autosync_test.go cmd/mcs-tray/main.go
git commit -m "feat: tray Auto Sync on Switch toggle with one-time enable warning"
```

---

### Task 6: Tray "Sync sessions →" directional submenu (manual align)

**Files:**
- Modify: `cmd/mcs-tray/main.go` (submenu in `onReady`, handlers, `confirmAlign` helper)

**Interfaces:**
- Consumes: `switcher.ManualAlign` (Task 4), `core.DisplayName`, `platform.ProfileInfo`, existing `osaQuote`, `notify`, `switcher`.

- [ ] **Step 1: Add the submenu and handlers in `onReady`**

Place after the Profiles section (after the `systray.AddSeparator()` at line 70), before the Actions section:

```go
// Manual align: copy one account's sessions into another WITHOUT switching.
mSync := systray.AddMenuItem("Sync sessions →", "Copy one account's sessions into another (without switching accounts)")
type alignPair struct{ src, dst *platform.ProfileInfo }
alignItems := map[*systray.MenuItem]alignPair{}
var shown []*platform.ProfileInfo
for _, p := range profiles {
	if p.HasSessionsDir || p.Name == "Claude" || p.Name == "Claude_Profile2" {
		shown = append(shown, p)
	}
}
for _, a := range shown {
	for _, b := range shown {
		if a.Path == b.Path {
			continue
		}
		label := fmt.Sprintf("From %s → To %s", core.DisplayName(a.Name), core.DisplayName(b.Name))
		child := mSync.AddSubMenuItem(label, "Copy the first account's sessions into the second")
		alignItems[child] = alignPair{src: a, dst: b}
	}
}
```

Add the handlers after the profile-switch handler loop (after ~line 108):

```go
for item, pair := range alignItems {
	go func(m *systray.MenuItem, pr alignPair) {
		for range m.ClickedCh {
			if !confirmAlign(core.DisplayName(pr.src.Name), core.DisplayName(pr.dst.Name)) {
				log.Printf("Align %s -> %s cancelled by user.", pr.src.Name, pr.dst.Name)
				continue
			}
			report, err := switcher.ManualAlign(pr.src.Path, pr.dst.Path)
			if err != nil {
				log.Printf("Manual align error: %v", err)
				notify("Align failed", err.Error())
				continue
			}
			log.Printf("Align %s -> %s: %d copied, %d skipped, %d conflict(s).", pr.src.Name, pr.dst.Name, report.CopiedCount, report.SkippedCount, report.ConflictCount)
			notify("Align complete", fmt.Sprintf("%d copied, %d skipped, %d conflict(s).", report.CopiedCount, report.SkippedCount, report.ConflictCount))
		}
	}(item, pair)
}
```

- [ ] **Step 2: Add the `confirmAlign` helper**

Add near `confirmSwitch` in `cmd/mcs-tray/main.go`:

```go
// confirmAlign asks before a manual align, which closes and reopens Claude
// Desktop on the SAME account (it copies data, it does not switch accounts).
func confirmAlign(src, dst string) bool {
	msg := fmt.Sprintf("Copy %q's sessions into %q? Claude Desktop will be closed, synced, and reopened on the account you're using now.", src, dst)
	script := fmt.Sprintf(`display dialog %s buttons {"Cancel", "Sync"} default button "Sync" cancel button "Cancel" with title "Multi-Claude Switcher"`, osaQuote(msg))
	return exec.Command("osascript", "-e", script).Run() == nil
}
```

- [ ] **Step 3: Verify it builds and all tests pass**

Run: `go build ./... && go test ./...`
Expected: build OK; all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/mcs-tray/main.go
git commit -m "feat: tray Sync-sessions submenu for manual directional align"
```

---

### Task 7: Docs, version bump, and verification

**Files:**
- Modify: `core/version.go` (`0.6.1` → `0.7.0`)
- Modify: `CHANGELOG.md` (new `[0.7.0]` entry)
- Modify: `README.md` (document both features + behavior change)
- Modify: `FILELIST.md` (new files)

- [ ] **Step 1: Bump the version**

In `core/version.go`: `var Version = "0.7.0"`.

- [ ] **Step 2: Add the CHANGELOG entry**

Prepend under the top `# CHANGELOG` header:

```markdown
## [0.7.0] - 2026-07-22

### Added
- **Manual "Sync sessions →" tray submenu:** copy one account's Code sessions
  into another **without switching accounts** — it closes Claude Desktop, backs
  up the target, syncs (re-bucketed under the target account), and reopens the
  account you were already on (`core/align.go` `Switcher.ManualAlign`,
  `cmd/mcs-tray/main.go`).
- **"Auto Sync on Switch" toggle (default OFF):** when on, every switch
  bidirectionally unions both accounts' Code sessions so they converge to the
  same history; safe because both profiles are closed during the switch window.
  Enabling shows a one-time warning (with an "Enable, don't ask again" option),
  since it merges one account's conversations into the other
  (`core/settings.go`, `core/sync.go` `SyncBidirectional`,
  `cmd/mcs-tray/autosync.go`).

### Changed
- **Switching no longer auto-syncs by default.** Previously every switch ran a
  one-way session sync; now a switch moves **no** session data unless
  "Auto Sync on Switch" is enabled. This makes cross-account conversation
  merging an explicit opt-in (`core/switch.go`).

### Notes
- Scope is Code sessions (`claude-code-sessions`) only. Agent Mode / Cowork
  sessions (`local-agent-mode-sessions`) are not synced; that is a separate,
  display-verification-gated follow-up. Regular chat is server-side per account
  and cannot be synced locally.
```

- [ ] **Step 3: Add a README section**

Add a "Syncing sessions between accounts" subsection documenting: (a) the manual "Sync sessions →" submenu (copies one account's sessions into another without switching), (b) the "Auto Sync on Switch" toggle (default off; on = every switch keeps both accounts identical; one-time warning), and (c) the behavior note that a plain switch moves no session data unless the toggle is on. State plainly that only the Code sessions sync, and that regular chat stays per account. Match the existing README voice.

- [ ] **Step 4: Update FILELIST**

Add these lines under the relevant sections:

```markdown
- `core/settings.go` — User settings store (~/.multi-claude-switcher/settings.json): auto sync toggle + warning-dismissed flag.
- `core/settings_test.go` — Unit tests for settings round-trip, defaults, and no-clobber.
- `core/align.go` — Manual directional align (ManualAlign): close → backup → sync → reopen the same account.
- `core/align_test.go` — Unit tests for ManualAlign (returns to running profile; no relaunch when nothing ran).
- `cmd/mcs-tray/autosync.go` — Tray Auto Sync toggle: enable-time warning dialog and choice parsing.
- `cmd/mcs-tray/autosync_test.go` — Unit tests for the warning-gating and dialog-choice parsing helpers.
```

Also update the `core/sync.go` and `core/switch.go` descriptions to mention `SyncBidirectional` and the toggle-gated align.

- [ ] **Step 5: Verify the whole build**

Run:
```bash
gofmt -l core/ cmd/
go vet ./...
go test ./...
```
Expected: `gofmt -l` prints nothing; `go vet` clean; `go test ./...` all PASS.

- [ ] **Step 6: Build the .app locally**

Run: `./scripts/package-app.sh 0.7.0`
Expected: `dist/Multi-Claude-Switcher_0.7.0_macos.zip` produced.

- [ ] **Step 7: Commit**

```bash
git add core/version.go CHANGELOG.md README.md FILELIST.md
git commit -m "docs: document manual align + auto sync, bump to 0.7.0"
```

- [ ] **Step 8: Manual on-device verification (REQUIRED — not inside the Claude Desktop Code tab)**

Run the built app from `dist/` (or install it) in a standalone environment. Verify:
1. **Manual align:** while on the personal account, pick `From Company → To Personal`, confirm the dialog closes/reopens Claude, and after relaunch the target account shows the source's Code sessions, and the **active account is unchanged** (still personal).
2. **Auto sync toggle OFF (default):** switch accounts; confirm no session data moved (each account still shows only its own).
3. **Auto sync enable warning:** click the checkbox; confirm the 3-button warning appears; "Enable, don't ask again" enables and suppresses the warning on the next enable.
4. **Auto sync ON:** switch both ways; confirm both accounts converge to the same Code-session history.
5. Clean up any throwaway test sessions/backups created during verification.

Report actual results (per verification-before-completion). Do **not** push or tag — the release is user-triggered.

---

## Self-Review

**Spec coverage:**
- §3 mechanics (directional/bidirectional, additive) → Tasks 2, 4. ✅
- §4.1 manual align (close→backup→sync→reopen R, submenu) → Tasks 4, 6. ✅
- §4.2 auto sync toggle (default off, bidirectional at switch, both-closed safety, behavior change) → Tasks 3, 5. ✅
- §4.2 enable warning + "don't ask again" (3-button) → Task 5. ✅
- §4.3 settings store (two flags) → Task 1. ✅
- §5 safety (never live, backup before write, additive) → Tasks 3, 4 (reuse existing guarantees). ✅
- §6 Phase 2 agent mode → explicitly out of scope (CHANGELOG note, Task 7). ✅
- §7 testing → each task is TDD; on-device verification in Task 7 Step 8. ✅

**Placeholder scan:** README Step 3 describes content rather than pasting final prose — acceptable, as the README voice/section layout is established and the required facts are enumerated; the implementer writes to the existing style. All code steps contain complete code.

**Type consistency:** `SyncBidirectional(a, b string) error`, `ManualAlign(src, dst string) (*SyncReport, error)`, `AutoSyncOnSwitch() bool`, `autoSyncChoice`/`choiceCancel|choiceEnable|choiceEnableDontAsk`, `mockPlatform.detected` — used identically across tasks. `SyncReport` fields (`CopiedCount`, `SkippedCount`, `ConflictCount`) match `core/sync.go`. ✅
