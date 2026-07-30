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

const (
	// archiveRenameAttempts and archiveRenameDelay bound the retry on a locked
	// directory. Claude Desktop can still be releasing file handles for a moment
	// after it exits, and a rename that fails for that reason succeeds shortly
	// after. Mirrors platform/windows_msix.go's renameWithRetry, which cannot be
	// reused here because that file is windows-only.
	archiveRenameAttempts = 40
	archiveRenameDelay    = 500 * time.Millisecond
	// archiveCollisionLimit bounds the search for an unused archive name. It is far
	// above any plausible number of archives of one profile in one second; reaching
	// it means something is wrong with the archive root, and reporting that beats
	// looping.
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
// the processes exit. A cross-volume rename is not.
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
