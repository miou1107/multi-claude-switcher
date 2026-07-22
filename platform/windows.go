//go:build !darwin

package platform

import (
	"fmt"
)

type WindowsPlatform struct{}

func New() Platform {
	return &WindowsPlatform{}
}

func (w *WindowsPlatform) AppSupportDir() string {
	return ""
}

func (w *WindowsPlatform) FindProfiles() ([]*ProfileInfo, error) {
	return nil, fmt.Errorf("windows support is backlog")
}

func (w *WindowsPlatform) IsAppRunning() (bool, []string, error) {
	return false, nil, fmt.Errorf("windows support is backlog")
}

func (w *WindowsPlatform) DetectRunningProfile() (string, error) {
	return "", fmt.Errorf("windows support is backlog")
}

func (w *WindowsPlatform) TerminateApp() error {
	return fmt.Errorf("windows support is backlog")
}

func (w *WindowsPlatform) LaunchProfile(profilePath string) error {
	return fmt.Errorf("windows support is backlog")
}
