//go:build !darwin && !windows

package platform

// ClaudeApp: there is no Claude Desktop on this platform.
func ClaudeApp() (version, edition string) { return "", "" }
