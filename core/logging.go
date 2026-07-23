package core

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

// LogDir returns the directory where component logs are written
// (~/.multi-claude-switcher/logs), matching the backup root's home.
func LogDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "multi-claude-switcher-logs")
	}
	return filepath.Join(home, ".multi-claude-switcher", "logs")
}

// SetupLogging directs the standard logger to BOTH stderr and a persistent
// per-component log file (LogDir/<component>.log), so a background GUI such as
// the tray leaves a durable trail even with no console attached. It appends, so
// history survives restarts. Returns a closer for the file (safe to defer /
// ignore on a long-running process) and the log file path.
func SetupLogging(component string) (io.Closer, string, error) {
	dir := LogDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, "", err
	}
	path := filepath.Join(dir, component+".log")
	// 0600: logs record profile names / account UUIDs, so keep them owner-only.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, "", err
	}
	// Write to the FILE first, then stderr. A GUI build (-H=windowsgui) has no
	// valid stderr handle, so writing to it errors — and io.MultiWriter stops at
	// the first error, which would silently drop all file logging. Ordering the
	// file first, and swallowing stderr write errors, guarantees the durable log
	// is always written regardless of whether a console is attached.
	log.SetOutput(io.MultiWriter(f, ignoreWriteErr{os.Stderr}))
	log.SetFlags(log.LstdFlags)
	log.Printf("=== %s v%s started (log: %s) ===", component, Version, path)
	return f, path, nil
}

// ignoreWriteErr wraps a writer so its write errors are swallowed (reporting a
// full, successful write). Used for stderr, which is invalid in a GUI build and
// would otherwise abort an io.MultiWriter before the file writer runs.
type ignoreWriteErr struct{ w io.Writer }

func (e ignoreWriteErr) Write(p []byte) (int, error) {
	_, _ = e.w.Write(p)
	return len(p), nil
}
