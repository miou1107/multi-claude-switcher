package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// mockPlatform is a test double for platform.Platform.
type mockPlatform struct {
	running      bool
	launched     bool
	launchedPath string // the profile launched LAST
	// launchedPaths is every profile launched, in order. Closing Claude Desktop
	// closes every profile at once, so "what got reopened" is a set, not one path.
	launchedPaths []string
	terminated    bool
	detected      string   // DetectRunningProfile result
	detectedAll   []string // DetectRunningProfiles result; nil falls back to detected
	appSupport    string   // AppSupportDir result
	// launchErr makes LaunchProfile fail for particular profiles, which is how the
	// difference between "the target never opened" and "an account that was open
	// did not come back" gets tested.
	launchErr map[string]error
	// onTerminate runs at the moment Claude is closed, so a test can inspect state
	// in the window where Claude is shut and MCS has not yet reopened it.
	onTerminate func()

	// createdIdentity and createdPath are separate on purpose. They differ on the
	// Store build, where the identity comes from state.json and the directory is
	// the shared slot, so a mock that conflated them could not catch a caller
	// deriving one from the other.
	createdName     string // the cleaned name the caller passed in
	createdIdentity string
	createdPath     string
	preparedSources []platform.RecoverySource
	prepareErr      error
	archiveRoot     string
	// prepareArchive lets a test decide what PrepareArchive hands back, which is
	// how the Store build's swap — where both paths move — gets represented.
	prepareArchive func(keep, archive string) (string, string, error)
	// prepareRemove lets a test decide what PrepareRemove hands back, which is how
	// the Store build's refusals get represented.
	prepareRemove func(identity string) (string, error)
}

func (m *mockPlatform) CreateProfile(clean string) (string, string, error) {
	m.createdName = clean
	return m.createdIdentity, m.createdPath, nil
}

func (m *mockPlatform) PrepareRecovery(newProfilePath string, sources []platform.RecoverySource) error {
	m.preparedSources = sources
	return m.prepareErr
}

func (m *mockPlatform) PrepareArchive(keepIdentity, archiveIdentity string) (string, string, error) {
	if m.prepareArchive != nil {
		return m.prepareArchive(keepIdentity, archiveIdentity)
	}
	return keepIdentity, archiveIdentity, nil
}

func (m *mockPlatform) PrepareRemove(identity string) (string, error) {
	if m.prepareRemove != nil {
		return m.prepareRemove(identity)
	}
	return filepath.Join(m.appSupport, identity), nil
}

func (m *mockPlatform) ArchiveDir() string { return m.archiveRoot }

// InstallKind is fixed to "macos" because no test here exercises install-kind
// branching; it only needs to satisfy platform.Platform.
func (m *mockPlatform) InstallKind() string { return "macos" }

func (m *mockPlatform) AppSupportDir() string                          { return m.appSupport }
func (m *mockPlatform) FindProfiles() ([]*platform.ProfileInfo, error) { return nil, nil }
func (m *mockPlatform) IsAppRunning() (bool, []string, error)          { return m.running, nil, nil }
func (m *mockPlatform) TerminateApp() error {
	m.terminated = true
	m.running = false
	if m.onTerminate != nil {
		m.onTerminate()
	}
	return nil
}
func (m *mockPlatform) DetectRunningProfile() (string, error) { return m.detected, nil }

// DetectRunningProfiles reports detectedAll when a test sets it, and otherwise
// falls back to the single `detected` profile, so the tests that only care about
// one running profile stay as they are.
func (m *mockPlatform) DetectRunningProfiles() ([]string, error) {
	if m.detectedAll != nil {
		return m.detectedAll, nil
	}
	if m.detected == "" {
		return nil, nil
	}
	return []string{m.detected}, nil
}

func (m *mockPlatform) LaunchProfile(profilePath string) error {
	if err, ok := m.launchErr[profilePath]; ok {
		return err
	}
	m.launched = true
	m.launchedPath = profilePath
	m.launchedPaths = append(m.launchedPaths, profilePath)
	return nil
}

// TestSafeSwitchReopensOtherRunningProfilesButNotTheSource covers a switch made
// while a third account is also open. Closing Claude Desktop closes every
// profile, so the bystander must come back; the profile being switched away from
// must not, or the switch did not switch anything.
func TestSafeSwitchReopensOtherRunningProfilesButNotTheSource(t *testing.T) {
	withStubbedSettings(t)
	tempDir := t.TempDir()

	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	other := filepath.Join(tempDir, "Other")
	for _, p := range []string{src, dst, other} {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}

	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	mp := &mockPlatform{running: true, detectedAll: []string{src, other}}
	s := NewSwitcher(mp, bm)

	if err := s.SafeSwitch(src, dst, ""); err != nil {
		t.Fatalf("SafeSwitch failed: %v", err)
	}
	assertLaunched(t, mp.launchedPaths, dst, other)
}

// TestSafeSwitchLaunchesAnAlreadyRunningTargetOnce covers switching to an account
// that is already open, which is ordinary: the target is both the thing to launch
// and something that was running, and launching it twice would leave the user
// with two windows on one account.
func TestSafeSwitchLaunchesAnAlreadyRunningTargetOnce(t *testing.T) {
	withStubbedSettings(t)
	tempDir := t.TempDir()

	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	for _, p := range []string{src, dst} {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}

	mp := &mockPlatform{running: true, detectedAll: []string{src, dst}}
	s := NewSwitcher(mp, NewBackupManager(filepath.Join(tempDir, "backups")))

	if err := s.SafeSwitch(src, dst, ""); err != nil {
		t.Fatalf("SafeSwitch failed: %v", err)
	}
	assertLaunched(t, mp.launchedPaths, dst)
}

// TestSafeSwitchMatchesProfilesByPathNotBySpelling: the paths a caller passes come
// from user input (mcs switch <path>) while the running ones come from the
// platform, so the same directory routinely arrives spelled two ways. Comparing
// them as raw strings would classify the source as a bystander and reopen the
// account the user just switched away from.
func TestSafeSwitchMatchesProfilesByPathNotBySpelling(t *testing.T) {
	withStubbedSettings(t)
	tempDir := t.TempDir()

	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	for _, p := range []string{src, dst} {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}

	mp := &mockPlatform{running: true, detectedAll: []string{src}}
	s := NewSwitcher(mp, NewBackupManager(filepath.Join(tempDir, "backups")))

	// Same directory, spelled with a trailing separator and a "." segment.
	if err := s.SafeSwitch(filepath.Join(src, ".")+string(filepath.Separator), dst, ""); err != nil {
		t.Fatalf("SafeSwitch failed: %v", err)
	}
	assertLaunched(t, mp.launchedPaths, dst)
}

// TestSafeSwitchReportsSuccessWhenOnlyABystanderFailsToReopen: the switch did
// happen, and saying otherwise makes every caller announce a failure and leave the
// account list pointing at the old account.
func TestSafeSwitchReportsSuccessWhenOnlyABystanderFailsToReopen(t *testing.T) {
	withStubbedSettings(t)
	tempDir := t.TempDir()

	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	other := filepath.Join(tempDir, "Other")
	for _, p := range []string{src, dst, other} {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}

	mp := &mockPlatform{
		running:     true,
		detectedAll: []string{src, other},
		launchErr:   map[string]error{other: errors.New("no such application")},
	}
	s := NewSwitcher(mp, NewBackupManager(filepath.Join(tempDir, "backups")))

	if err := s.SafeSwitch(src, dst, ""); err != nil {
		t.Fatalf("the switch reached its target, so it must not report failure: %v", err)
	}
}

// TestSafeSwitchFailsWhenTheTargetCannotBeOpened is the other half: if the account
// the user asked for never opened, the switch failed and must say so.
func TestSafeSwitchFailsWhenTheTargetCannotBeOpened(t *testing.T) {
	withStubbedSettings(t)
	tempDir := t.TempDir()

	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	for _, p := range []string{src, dst} {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}

	mp := &mockPlatform{
		running:     true,
		detectedAll: []string{src},
		launchErr:   map[string]error{dst: errors.New("no such application")},
	}
	s := NewSwitcher(mp, NewBackupManager(filepath.Join(tempDir, "backups")))

	if err := s.SafeSwitch(src, dst, ""); err == nil {
		t.Fatal("the target never opened, so the switch must report failure")
	}
}

// TestSafeSwitchLaunchesWhenTargetNotLoggedIn verifies that switching to a
// fresh profile with no account yet (no config.json) skips the sync but still
// launches it — so `switch` can be used to open a profile in order to log in.
func TestSafeSwitchLaunchesWhenTargetNotLoggedIn(t *testing.T) {
	withStubbedSettings(t)
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

	// The folder exists but holds no config.json, so nobody is signed in to it.
	// It has to exist: switching to a folder that is not there is now refused, or a
	// mistyped name would silently create a profile the user never asked for.
	dst := filepath.Join(tempDir, "Dst")
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}
	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	mp := &mockPlatform{}
	s := NewSwitcher(mp, bm)

	if err := s.SafeSwitch(src, dst, ""); err != nil {
		t.Fatalf("expected switch to a not-logged-in target to succeed (skip sync, still launch), got %v", err)
	}
	if !mp.launched {
		t.Error("target profile must still be launched even though sync was skipped")
	}
}

// TestSafeSwitchSkipsSyncButStillLaunchesWhenBackupFails verifies that a failed
// backup skips the session union (never write without a backup) yet still opens
// the target profile.
//
// It used to return early instead, which read as "abort the switch" but was not
// one: Claude Desktop has already been terminated by then, and the launch is the
// only thing that brings it back. The user was left with Claude shut, holding an
// error, with no indication of which account they would land in if they opened it
// by hand. Nothing is written when the sync is skipped, so launching is safe, and
// it is what the user actually asked for. The error still comes back so the
// skipped sync is not silent.
//
// Backup only runs when auto sync is ON and both profiles are logged in, so this
// test turns auto sync ON.
func TestSafeSwitchSkipsSyncButStillLaunchesWhenBackupFails(t *testing.T) {
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

	if err := s.SafeSwitch(src, dst, ""); err == nil {
		t.Fatal("expected the skipped sync to be reported, got nil error")
	}
	if !mp.launched {
		t.Error("Claude was terminated, so the target must still be launched — otherwise the user is stranded with Claude shut")
	}
	if mp.launchedPath != dst {
		t.Errorf("launched %q, want the switch target %q", mp.launchedPath, dst)
	}
	got, readErr := os.ReadFile(dstFile)
	if readErr != nil {
		t.Fatalf("target file disappeared: %v", readErr)
	}
	if string(got) != string(original) {
		t.Errorf("target file was overwritten despite backup failure: got %q", got)
	}
}

// TestSafeSwitchOffMovesNoData verifies that with auto sync OFF (the
// default), SafeSwitch is a pure account switch — no session data moves.
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

	if err := s.SafeSwitch(src, dst, ""); err != nil {
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

// TestSafeSwitchOnUnionsBothAccounts verifies that with auto sync ON,
// SafeSwitch backs up and unions both accounts' sessions bidirectionally.
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

	if err := s.SafeSwitch(src, dst, ""); err != nil {
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

// TestSafeSwitchProceedsWhenTargetIsEmpty verifies a brand-new target profile
// (no sessions dir, nothing to lose) does not block the switch.
func TestSafeSwitchProceedsWhenTargetIsEmpty(t *testing.T) {
	withStubbedSettings(t)
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

	dst := filepath.Join(tempDir, "Dst") // no sessions dir at all
	writeAccountConfig(t, dst, "uuid1")
	bm := NewBackupManager(filepath.Join(tempDir, "backups"))
	mp := &mockPlatform{}
	s := NewSwitcher(mp, bm)

	if err := s.SafeSwitch(src, dst, ""); err != nil {
		t.Fatalf("expected switch to succeed for empty target, got %v", err)
	}
	if !mp.launched {
		t.Error("expected target profile to be launched")
	}
}

// TestManualAlignExposesTheRelaunchItOwesWhileClaudeIsClosed is the regression
// test for clicking Quit during a sync.
//
// ManualAlign closes Claude Desktop, does its work, and reopens it. Between those
// two moments Claude is shut and only MCS knows which profile to reopen. If MCS
// exits in that window — the panel's Quit handler calls TerminateApp on itself and
// does not check whether an operation is in flight — the goroutine doing the work
// dies and Claude is never reopened. The user is left with no Claude and no MCS,
// and nothing on screen said why.
//
// So the owed relaunch has to be visible from outside for as long as it is owed.
func TestManualAlignExposesTheRelaunchItOwesWhileClaudeIsClosed(t *testing.T) {
	withStubbedSettings(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "Src")
	dst := filepath.Join(dir, "Dst")
	writeAccountConfig(t, src, "uuid1")
	writeAccountConfig(t, dst, "uuid2")

	mp := &mockPlatform{running: true, detected: src}
	s := NewSwitcher(mp, NewBackupManager(filepath.Join(dir, "backups")))

	var owedMidFlight []string
	mp.onTerminate = func() {
		// Exactly the moment Claude is closed. This is the window Quit lands in.
		owedMidFlight = s.PendingRelaunch()
	}

	if _, err := s.ManualAlign(src, dst); err != nil {
		t.Fatalf("ManualAlign: %v", err)
	}
	if len(owedMidFlight) != 1 || owedMidFlight[0] != src {
		t.Fatalf("owed relaunch while closed = %q, want %q", owedMidFlight, []string{src})
	}
	if got := s.PendingRelaunch(); len(got) != 0 {
		t.Fatalf("nothing is owed once Claude has been reopened, got %q", got)
	}
}

// TestClaimPendingRelaunchHandsItOutOnlyOnce stops MCS and its own operation from
// both reopening Claude, which would leave the user with two windows.
func TestClaimPendingRelaunchHandsItOutOnlyOnce(t *testing.T) {
	s := NewSwitcher(&mockPlatform{}, NewBackupManager(t.TempDir()))
	s.notePendingRelaunch("/some/profile")

	if got := s.ClaimPendingRelaunch(); len(got) != 1 || got[0] != "/some/profile" {
		t.Fatalf("first claim = %q", got)
	}
	if got := s.ClaimPendingRelaunch(); len(got) != 0 {
		t.Fatalf("second claim = %q, want it already taken", got)
	}
}

// TestManualAlignOwesNothingWhenClaudeWasNotRunning: with nothing closed there is
// nothing to reopen, and a Quit in that window must not launch Claude at all.
func TestManualAlignOwesNothingWhenClaudeWasNotRunning(t *testing.T) {
	withStubbedSettings(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "Src")
	dst := filepath.Join(dir, "Dst")
	writeAccountConfig(t, src, "uuid1")
	writeAccountConfig(t, dst, "uuid2")

	mp := &mockPlatform{running: false}
	s := NewSwitcher(mp, NewBackupManager(filepath.Join(dir, "backups")))
	if _, err := s.ManualAlign(src, dst); err != nil {
		t.Fatalf("ManualAlign: %v", err)
	}
	if got := s.PendingRelaunch(); len(got) != 0 {
		t.Fatalf("owed = %q, want nothing", got)
	}
	if mp.launched {
		t.Fatal("Claude was not running, so it must not be launched")
	}
}

// TestSafeSwitchValidatesTargetBeforeClosingClaude
//
// SafeSwitch closed the running Claude as its very first act, before checking that
// the target profile was even a directory. So `mcs switch NoSuchProfile AlsoNoSuch`
// killed the app the user was working in and then failed — and the same is true for
// any caller passing a stale or mistyped folder. Closing somebody's app is the last
// thing to do after every check has passed, not the first.
func TestSafeSwitchValidatesTargetBeforeClosingClaude(t *testing.T) {
	root := t.TempDir()
	mp := &mockPlatform{running: true}
	s := NewSwitcher(mp, NewBackupManager(filepath.Join(root, "backups")))

	err := s.SafeSwitch(filepath.Join(root, "Claude"), filepath.Join(root, "NoSuchProfile"), "")

	if err == nil {
		t.Fatal("switching to a profile that is not there must fail")
	}
	if mp.terminated {
		t.Error("Claude must not be closed before the target is known to exist")
	}
	if mp.launched {
		t.Error("nothing should have been launched")
	}
	if p := s.PendingRelaunch(); len(p) != 0 {
		t.Errorf("nothing was closed, so nothing is owed a relaunch, got %q", p)
	}
}

// TestSafeSwitchValidatesTargetIsADirectory guards the same gate against a file
// sitting where a profile folder should be.
func TestSafeSwitchValidatesTargetIsADirectory(t *testing.T) {
	root := t.TempDir()
	notADir := filepath.Join(root, "Claude_File")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mp := &mockPlatform{running: true}
	s := NewSwitcher(mp, NewBackupManager(filepath.Join(root, "backups")))

	if err := s.SafeSwitch(filepath.Join(root, "Claude"), notADir, ""); err == nil {
		t.Fatal("a file is not a profile")
	}
	if mp.terminated {
		t.Error("Claude must not be closed for a target that is not a directory")
	}
}

// TestSafeSwitchRecordsTheAccountItPutTheUserOn: the switch is the moment MCS
// knows which account the user is on, so it is the moment worth recording. Left
// to the hosts, the CLI and any other caller would move the user without saying
// so, and the next switch would close the wrong account.
func TestSafeSwitchRecordsTheAccountItPutTheUserOn(t *testing.T) {
	withStubbedSettings(t)
	withStubbedActiveProfile(t)
	tempDir := t.TempDir()

	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	for _, p := range []string{src, dst} {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}

	mp := &mockPlatform{running: true, detectedAll: []string{src}}
	s := NewSwitcher(mp, NewBackupManager(filepath.Join(tempDir, "backups")))

	if err := s.SafeSwitch(src, dst, "Dst"); err != nil {
		t.Fatalf("SafeSwitch: %v", err)
	}
	if got := LoadActiveProfile(); got != "Dst" {
		t.Errorf("active account recorded as %q, want %q", got, "Dst")
	}
}

// TestSafeSwitchRecordsEvenWhenTheSyncFailed: the sync failing does not undo the
// switch. Claude is up on the target, so that is where the user is, and a record
// that disagrees would send the NEXT switch after the wrong account.
func TestSafeSwitchRecordsEvenWhenTheSyncFailed(t *testing.T) {
	withStubbedSettings(t)
	withStubbedActiveProfile(t)
	if err := SetAutoSyncOnSwitch(true); err != nil { // ON so the backup step runs
		t.Fatal(err)
	}
	tempDir := t.TempDir()

	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	writeAccountConfig(t, src, "uuid1")
	writeAccountConfig(t, dst, "uuid2")
	// Both ends need data, or there is nothing to back up and the align has
	// nothing to fail at.
	writeSessionFile(t, src, filepath.Join("uuid1", "local_src.json"), `{"src":1}`, time.Now())
	writeSessionFile(t, dst, filepath.Join("uuid2", "local_dst.json"), `{"dst":1}`, time.Now())

	// A regular file where the backup root needs a directory: the align fails,
	// the switch itself does not.
	blocker := filepath.Join(tempDir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	mp := &mockPlatform{running: true, detectedAll: []string{src}}
	s := NewSwitcher(mp, NewBackupManager(filepath.Join(blocker, "backups")))

	if err := s.SafeSwitch(src, dst, "Dst"); err == nil {
		t.Fatal("the failed sync must still be reported")
	}
	if !mp.launched {
		t.Fatal("the target must still be launched")
	}
	if got := LoadActiveProfile(); got != "Dst" {
		t.Errorf("active account recorded as %q, want %q — the user IS on the target", got, "Dst")
	}
}

// TestSafeSwitchRecordsNothingWhenTheTargetNeverOpened: no launch, no move, so
// the previous record is still the truth.
func TestSafeSwitchRecordsNothingWhenTheTargetNeverOpened(t *testing.T) {
	withStubbedSettings(t)
	withStubbedActiveProfile(t)
	tempDir := t.TempDir()

	src := filepath.Join(tempDir, "Src")
	dst := filepath.Join(tempDir, "Dst")
	for _, p := range []string{src, dst} {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := SaveActiveProfile("Src"); err != nil {
		t.Fatal(err)
	}

	mp := &mockPlatform{
		running:     true,
		detectedAll: []string{src},
		launchErr:   map[string]error{dst: errors.New("no such application")},
	}
	s := NewSwitcher(mp, NewBackupManager(filepath.Join(tempDir, "backups")))

	if err := s.SafeSwitch(src, dst, "Dst"); err == nil {
		t.Fatal("the target never opened, so the switch must fail")
	}
	if got := LoadActiveProfile(); got != "Src" {
		t.Errorf("active account = %q, want the untouched %q", got, "Src")
	}
}
