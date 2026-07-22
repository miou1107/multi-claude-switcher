//go:build !darwin && !windows

package core

import (
	"fmt"
	"runtime"
)

// Start-at-login is only implemented for macOS and Windows. On other operating
// systems (e.g. Linux, used by CI/dev builds) these are safe no-ops/errors so
// the package still builds.

func LoginItemEnabled() bool { return false }

func EnableLoginItem(exePath string) error {
	return fmt.Errorf("start at login is not supported on %s", runtime.GOOS)
}

func DisableLoginItem() error { return nil }
