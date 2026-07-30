package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/miou1107/multi-claude-switcher/platform"
)

type BackupManager struct {
	BackupRootDir string
}

func NewBackupManager(rootDir string) *BackupManager {
	if rootDir == "" {
		home, _ := os.UserHomeDir()
		rootDir = filepath.Join(home, ".multi-claude-switcher", "backups")
	}
	return &BackupManager{BackupRootDir: rootDir}
}

func (bm *BackupManager) CreateBackup(profilePath string) (string, error) {
	sessionsDir := platform.GetProfileSessionsDir(profilePath)
	if fi, err := os.Stat(sessionsDir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("sessions directory does not exist: %s", sessionsDir)
	}

	backupDir, err := bm.freeBackupDir(filepath.Base(profilePath))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup root: %w", err)
	}

	targetSessionsDir := filepath.Join(backupDir, "claude-code-sessions")
	if err := copyDir(sessionsDir, targetSessionsDir); err != nil {
		return "", fmt.Errorf("failed to copy sessions dir: %w", err)
	}

	// Record what was captured, so the next automatic backup can tell whether the
	// profile has changed since. Best effort: a snapshot without one is simply
	// never reused.
	if fp, fpErr := sessionsFingerprint(sessionsDir); fpErr == nil {
		_ = os.WriteFile(filepath.Join(backupDir, backupFingerprintName), []byte(fp), 0644)
	}

	return backupDir, nil
}

// backupFingerprintName is where CreateBackup records a description of what it
// captured, next to the copied sessions.
const backupFingerprintName = "fingerprint.txt"

// freeBackupDir picks an unused directory for a new snapshot.
//
// The name is timestamped to the second, so two backups of one profile inside the
// same second used to land on the same path — and copyDir merges rather than
// replaces, so the "snapshot" became a mixture of two different states, with files
// deleted in between still present. A backup that is not a faithful copy of one
// moment is worse than useless, because it is trusted. Two syncs a second apart is
// an ordinary thing for a user to do.
func (bm *BackupManager) freeBackupDir(profileName string) (string, error) {
	base := fmt.Sprintf("%s_%s", profileName, time.Now().Format("20060102_150405"))
	for i := 1; i <= backupCollisionLimit; i++ {
		dir := filepath.Join(bm.BackupRootDir, base)
		if i > 1 {
			dir = filepath.Join(bm.BackupRootDir, fmt.Sprintf("%s-%d", base, i))
		}
		_, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			return dir, nil
		}
		if err != nil {
			// Anything other than "not there" will not change by looping.
			return "", fmt.Errorf("check backup destination %s: %w", dir, err)
		}
	}
	return "", fmt.Errorf("too many backups named %q already — clear out %s", base, bm.BackupRootDir)
}

// backupCollisionLimit bounds the search for an unused snapshot name. It is far
// above any plausible number of backups of one profile in one second.
const backupCollisionLimit = 100

// sessionsFingerprint describes a session tree cheaply enough to run before every
// backup: every file's path, size and modification time.
//
// Content hashing would be more exact and far too slow — a profile holds hundreds
// of megabytes. Size and mtime are enough here because MCS is the only thing that
// writes into these trees other than Claude Desktop itself, and both rewrite a
// session file wholesale rather than editing it in place. copyFile preserves
// mtimes, so a snapshot's fingerprint stays comparable with its source.
//
// EVERY file counts except the operating system's own metadata. The tree is not all
// conversations: Claude keeps extensionless "deleted_<uuid>" markers beside them,
// which is how a deleted conversation is recorded. A fingerprint that only looked at
// *.json would call a tree with a fresh deletion marker unchanged, so the automatic
// backup would reuse a snapshot from before the deletion and the state actually about
// to be overwritten would never be captured. copyDir copies everything, so the
// fingerprint covers everything.
//
// .DS_Store is the exception, because it is not Claude's data and it appears without
// anybody's involvement: browsing the backups folder in Finder writes one into the
// snapshot, which would make the snapshot stop matching the profile and stop reuse
// working for that profile from then on.
func sessionsFingerprint(sessionsDir string) (string, error) {
	var lines []string
	err := filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || isOSMetadataFile(info.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(sessionsDir, path)
		if relErr != nil {
			return relErr
		}
		lines = append(lines, fmt.Sprintf("%s|%d|%d", filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines) // walk order is not guaranteed stable across filesystems
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:]), nil
}

// newestBackupFor returns the most recent snapshot directory for a profile, or ""
// when there is none.
//
// Getting this wrong is not loud. Reuse would just pick a snapshot that does not
// match, take a full copy instead, and the only symptom would be the unbounded
// growth this was added to stop, quietly coming back. Two things it must get right:
//
//   - The profile name has to match EXACTLY, not by prefix. "Claude" and
//     "Claude_Work" are MCS's own default names, and a prefix match on "Claude_"
//     also matches every "Claude_Work_…" snapshot.
//   - The same-second counter has to compare as a number. "-10" sorts before "-2"
//     as text.
func (bm *BackupManager) newestBackupFor(profileName string) string {
	entries, err := os.ReadDir(bm.BackupRootDir)
	if err != nil {
		return ""
	}
	newest, newestStamp, newestSeq := "", "", 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		profile, stamp, seq, ok := parseBackupName(e.Name())
		if !ok || profile != profileName {
			continue
		}
		if stamp > newestStamp || (stamp == newestStamp && seq > newestSeq) {
			newest, newestStamp, newestSeq = e.Name(), stamp, seq
		}
	}
	if newest == "" {
		return ""
	}
	return filepath.Join(bm.BackupRootDir, newest)
}

// parseBackupName splits a snapshot directory name into the profile it belongs to,
// its timestamp, and its same-second counter.
//
// The shape is "<profile>_<YYYYMMDD>_<HHMMSS>" with an optional "-<n>". It is parsed
// from the right, because the part that varies is the fixed-width end: the profile
// name is user-chosen and may itself contain both underscores and dashes ("work-2"
// is a legal name), so scanning from the left cannot tell where it stops. ok is
// false for anything that is not a snapshot name at all.
func parseBackupName(name string) (profile, stamp string, seq int, ok bool) {
	seq = 1
	if i := strings.LastIndex(name, "-"); i >= 0 {
		if n, err := strconv.Atoi(name[i+1:]); err == nil {
			seq = n
			name = name[:i]
		}
	}
	// The last two underscore-separated fields are the date and the time.
	iTime := strings.LastIndex(name, "_")
	if iTime <= 0 {
		return "", "", 0, false
	}
	iDate := strings.LastIndex(name[:iTime], "_")
	if iDate <= 0 {
		return "", "", 0, false
	}
	date, clock := name[iDate+1:iTime], name[iTime+1:]
	if !isDigits(date, 8) || !isDigits(clock, 6) {
		return "", "", 0, false
	}
	return name[:iDate], date + "_" + clock, seq, true
}

// isOSMetadataFile reports whether a filename is something the operating system
// dropped in rather than something Claude wrote. These must not count towards a
// fingerprint: they appear and disappear on their own, so treating them as content
// makes a snapshot stop matching the profile for reasons that have nothing to do
// with the user's conversations.
func isOSMetadataFile(name string) bool {
	switch name {
	case ".DS_Store", "Thumbs.db", "desktop.ini":
		return true
	}
	return strings.HasPrefix(name, "._") // macOS AppleDouble sidecars
}

func isDigits(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ProfileHasSessions reports whether a profile holds a session tree at all, i.e.
// whether there is anything a backup could protect. Callers that want an
// unconditional snapshot (the user asked for one) pair this with CreateBackup, so
// they skip empty profiles without inheriting BackupIfHasData's reuse.
func ProfileHasSessions(profilePath string) bool {
	fi, err := os.Stat(platform.GetProfileSessionsDir(profilePath))
	return err == nil && fi.IsDir()
}

// BackupIfHasData backs up the profile only when it actually holds sessions.
// It returns ("", nil) when there is nothing to back up (no sessions dir), the
// backup path on success, and ("", err) on a genuine backup failure. Callers
// that are about to overwrite the profile MUST abort on a non-nil error so real
// data is never overwritten without a backup.
//
// When the profile is byte-for-byte where it was at the last snapshot, that
// snapshot is returned instead of a fresh copy being made. This is the automatic
// safety net that runs before every switch, sync and restore, and the panel takes
// one on every Sync click; copying a few hundred megabytes again to protect a state
// already protected is how one machine accumulated 65 near-identical snapshots
// totalling 1.6 GB. Nothing is deleted to achieve this — the copy is simply not
// made. An explicit backup requested by the user still always copies
// (see CreateBackup).
func (bm *BackupManager) BackupIfHasData(profilePath string) (string, error) {
	sessionsDir := platform.GetProfileSessionsDir(profilePath)
	if fi, err := os.Stat(sessionsDir); err != nil || !fi.IsDir() {
		return "", nil // nothing to lose
	}
	if existing := bm.reusableBackup(profilePath, sessionsDir); existing != "" {
		return existing, nil
	}
	return bm.CreateBackup(profilePath)
}

// reusableBackup returns the newest snapshot of this profile when it still matches
// the profile exactly, or "" when there is none, it cannot be read, or anything has
// changed. Every uncertainty resolves to "" so the caller takes a fresh backup: the
// cost of a needless copy is disk, and the cost of a wrongly skipped one is the
// user's conversations.
func (bm *BackupManager) reusableBackup(profilePath, sessionsDir string) string {
	newest := bm.newestBackupFor(filepath.Base(profilePath))
	if newest == "" {
		return ""
	}
	recorded, err := os.ReadFile(filepath.Join(newest, backupFingerprintName))
	if err != nil {
		return "" // pre-dates fingerprints, or unreadable
	}
	current, err := sessionsFingerprint(sessionsDir)
	if err != nil {
		return ""
	}
	if strings.TrimSpace(string(recorded)) != current {
		return ""
	}
	// The recorded fingerprint describes the profile, so confirm the snapshot
	// itself still matches it too. A snapshot the user has moved or pruned files
	// out of protects nothing, and its fingerprint file would not know.
	stored, err := sessionsFingerprint(filepath.Join(newest, "claude-code-sessions"))
	if err != nil || stored != current {
		return ""
	}
	return newest
}

func (bm *BackupManager) ListBackups() ([]string, error) {
	if _, err := os.Stat(bm.BackupRootDir); os.IsNotExist(err) {
		return nil, nil
	}
	entries, err := os.ReadDir(bm.BackupRootDir)
	if err != nil {
		return nil, err
	}
	var backups []string
	for _, entry := range entries {
		if entry.IsDir() {
			backups = append(backups, filepath.Join(bm.BackupRootDir, entry.Name()))
		}
	}
	return backups, nil
}

func (bm *BackupManager) RestoreBackup(backupPath, targetProfilePath string) error {
	backupSessionsDir := filepath.Join(backupPath, "claude-code-sessions")
	if fi, err := os.Stat(backupSessionsDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("invalid backup directory: %s", backupPath)
	}

	targetSessionsDir := platform.GetProfileSessionsDir(targetProfilePath)

	// A successful restore overwrites (and then discards) whatever the target
	// currently holds. Snapshot the current target first so the restore is
	// itself reversible — restoring the wrong backup must not be a one-way loss.
	// Abort if the snapshot fails: never discard live data without a recoverable
	// backup (same invariant as switch/sync).
	if _, err := bm.BackupIfHasData(targetProfilePath); err != nil {
		return fmt.Errorf("refusing to restore: failed to back up the current target first: %w", err)
	}

	// Stage the restore into a temp dir first. A mid-copy failure (disk full,
	// permissions) then leaves the existing target untouched instead of half
	// destroyed.
	tmpDir := targetSessionsDir + ".restoring"
	_ = os.RemoveAll(tmpDir)
	if err := copyDir(backupSessionsDir, tmpDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("failed to stage restore: %w", err)
	}

	// Move the current target aside, then swap in the fully-staged copy. If the
	// final swap fails, roll the original back into place.
	oldDir := targetSessionsDir + ".old"
	_ = os.RemoveAll(oldDir)
	if _, err := os.Stat(targetSessionsDir); err == nil {
		if err := os.Rename(targetSessionsDir, oldDir); err != nil {
			_ = os.RemoveAll(tmpDir)
			return fmt.Errorf("failed to move current target aside: %w", err)
		}
	}
	if err := os.Rename(tmpDir, targetSessionsDir); err != nil {
		_ = os.Rename(oldDir, targetSessionsDir) // best-effort rollback
		_ = os.RemoveAll(tmpDir)                 // don't leak the staged copy
		return fmt.Errorf("failed to swap in restored sessions: %w", err)
	}
	_ = os.RemoveAll(oldDir)
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}

		return copyFile(path, targetPath)
	})
}

// copyTmpSuffix names the staging file every copy is written through. It
// deliberately does not end in ".json": a process killed mid-copy leaves the
// staging file inside a session bucket, and sync walks buckets copying *.json
// (core/sync.go), so a ".json" staging name would be carried into the other
// profile as if it were a conversation.
const copyTmpSuffix = ".mcstmp"

// copyRename is os.Rename behind a var so a test can force the final swap to fail
// and prove the destination is left exactly as it was.
var copyRename = os.Rename

// copyFile copies src to dst, staging through a temporary file so the destination
// is only ever replaced whole.
//
// The staging matters more than it looks. Writing straight into dst through
// os.Create truncates it immediately, so a copy interrupted partway — the process
// killed, Quit clicked during a sync, the machine losing power — left a truncated
// file behind. And a truncated file's mtime is the moment it was written, which
// makes it NEWER than its source; sync keeps whichever copy of a record is newer,
// so the next sync would treat the truncated file as the current version of that
// conversation and keep it. An interruption became permanent damage. Staging turns
// the same interruption into "nothing happened", at the cost of one rename.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	// Match the source's permissions. Claude Desktop writes session files 0600, and
	// a fresh os.Create would give the staged file the process default (0644 under a
	// typical umask) which the rename then carries onto the destination — quietly
	// making every synced conversation group- and world-readable. Writing straight
	// into an existing destination used to keep its mode by accident; staging has to
	// do it on purpose.
	//
	// Read the mode off the open handle rather than the path: the file is already
	// open, so this cannot describe a different file than the one being copied, and
	// it works when the path is no longer resolvable.
	mode := os.FileMode(0600) // conservative: never widen when the mode is unknown
	srcInfo, srcStatErr := in.Stat()
	if srcStatErr == nil {
		mode = srcInfo.Mode().Perm()
	}

	// Clear any staging file a previous run was killed before it could rename. It
	// carries the source's mode, so a read-only one would make the O_TRUNC below
	// fail on Windows and block this file from ever syncing again.
	tmp := dst + copyTmpSuffix
	_ = os.Remove(tmp)

	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	// O_CREATE applies the umask, so set the mode explicitly.
	if err := out.Chmod(mode); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	// Preserve the source modification time, on the staging file so the
	// destination is never briefly visible with the wrong one (rename carries it
	// across). Two things depend on it: sync decides which of two copies of a
	// record is current by comparing mtimes (core/sync.go), and the scanner reports
	// each account's "last updated" from the newest session file. A copy that reset
	// them to "now" would make every copied file look newer than its source on the
	// next comparison, and every restored account look freshly used.
	if srcStatErr == nil {
		_ = os.Chtimes(tmp, srcInfo.ModTime(), srcInfo.ModTime())
	}

	if err := copyRename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
