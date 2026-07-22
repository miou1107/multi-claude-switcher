package core

// Version is the single source of truth for the product version. Keep it in
// sync with the latest CHANGELOG.md entry (see also the release iron rule about
// version numbers). Both the CLI and the tray import this value.
//
// It is a var (not a const) so release builds can override it via linker flags:
//
//	go build -ldflags "-X github.com/miou1107/multi-claude-switcher/core.Version=0.5.0"
//
// The GitHub release workflow injects the git tag here; local builds use the
// default below.
var Version = "0.5.0"
