//go:build !darwin && !windows

package platform

import (
	"fmt"
	"runtime"
)

// unsupportedPlatform is the fallback for operating systems that are neither
// macOS nor Windows (e.g. Linux, used by CI/dev builds). Every operation fails
// with a clear "not supported" error rather than pretending to work.
type unsupportedPlatform struct{}

func New() Platform {
	return &unsupportedPlatform{}
}

func notSupported() error {
	return fmt.Errorf("Claude Desktop profile switching is not supported on %s", runtime.GOOS)
}

func (p *unsupportedPlatform) AppSupportDir() string { return "" }

func (p *unsupportedPlatform) FindProfiles() ([]*ProfileInfo, error) { return nil, notSupported() }

func (p *unsupportedPlatform) IsAppRunning() (bool, []string, error) {
	return false, nil, notSupported()
}

func (p *unsupportedPlatform) DetectRunningProfile() (string, error) { return "", notSupported() }

func (p *unsupportedPlatform) TerminateApp() error { return notSupported() }

func (p *unsupportedPlatform) LaunchProfile(profilePath string) error { return notSupported() }
