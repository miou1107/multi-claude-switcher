package platform

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// copyTmpSuffix names the staging file a copy is written to before being moved
// into place. It deliberately does not end in .json: session files are found by
// that extension, so a staging file left behind by a killed process is invisible
// to every scan and sync rather than being mistaken for a conversation.
const copyTmpSuffix = ".mcs-copying"

// CopyDirMerge recursively copies src into dst, skipping any file dst already
// has, and returns how many files were copied.
//
// Merge, not replace: it is used to bring an account's saved conversations into a
// profile that may already hold some of them, and the copy must never overwrite
// what is already there.
//
// It lives here rather than in windows_msix.go, where it started, because both
// the Store build's post-sign-in migration and the standalone builds' recovery
// need it, and only one of those files compiles on macOS.
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
		if _, e := os.Lstat(target); e == nil {
			return nil // already there; never clobber
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

// copyFile copies one file, staging it and swapping it into place so an
// interruption cannot leave a damaged destination.
//
// Writing straight into the destination truncates it the instant the copy starts.
// A process that died partway therefore left a truncated file whose timestamp was
// the moment of the write, making it newer than its source — and sync keeps the
// newer copy, so the next run would treat the truncated file as the current
// version of that conversation. Staging removes that entirely: an interruption
// leaves the destination exactly as it was, plus a staging file that no scan looks
// at.
//
// It also carries the source's permissions and modification time across. Claude
// Desktop writes session files 0600; a fresh create would hand the staged file the
// process default, and the rename would carry that onto the destination, quietly
// widening access to every copied conversation.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// Read the mode off the open handle rather than the path: the file is already
	// open, so this cannot describe a different file than the one being copied.
	mode := os.FileMode(0o600) // conservative: never widen when the mode is unknown
	srcInfo, srcStatErr := in.Stat()
	if srcStatErr == nil {
		mode = srcInfo.Mode().Perm()
	}

	// Clear any staging file a previous run was killed before it could rename. It
	// carries the source's mode, so a read-only one would make the O_TRUNC below
	// fail on Windows and block this file from ever being copied again.
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
	if _, err := io.Copy(out, in); err != nil {
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
	// Before the swap, so the destination is never briefly wrong.
	if srcStatErr == nil {
		_ = os.Chtimes(tmp, srcInfo.ModTime(), srcInfo.ModTime())
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// RecoverySource is one profile holding part of an orphaned account's
// conversations, as scanned. It carries the path because a profile's folder name
// cannot be turned back into a path — the Store build's active profile lives in a
// directory named "Claude" whatever the profile is called — so callers pass the
// path they were given rather than rebuilding one.
type RecoverySource struct {
	Path string
	UUID string
}

// prepareRecoveryByCopy makes an orphaned account's conversations available in a
// new profile by copying its session buckets across.
//
// Copy, not move: until the user has signed in to the new profile the sources are
// the only copies that matter, and a failure here must lose nothing. Once the
// account is live in the new profile the sources' now-stale buckets are folded
// away by the scanner as duplicates of an account live elsewhere, so the user
// never sees them twice.
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
