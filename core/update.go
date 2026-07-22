package core

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// LatestReleaseURL is the GitHub API endpoint for this project's latest release.
const LatestReleaseURL = "https://api.github.com/repos/miou1107/multi-claude-switcher/releases/latest"

// IsNewer reports whether the remote version string is strictly newer than the
// local one. Both may carry a leading "v" and a pre-release/build suffix
// (compared on major.minor.patch only).
func IsNewer(remote, local string) bool {
	return compareVersions(remote, local) > 0
}

func compareVersions(a, b string) int {
	pa, pb := parseVersion(a), parseVersion(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] > pb[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

func parseVersion(s string) [3]int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i] // drop pre-release / build metadata
	}
	var out [3]int
	for i, part := range strings.Split(s, ".") {
		if i >= 3 {
			break
		}
		out[i], _ = strconv.Atoi(part)
	}
	return out
}

// parseRelease extracts the tag and an asset-name -> download-URL map from a
// GitHub "latest release" API response body.
func parseRelease(data []byte) (tag string, assets map[string]string, err error) {
	var r struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", nil, err
	}
	m := make(map[string]string, len(r.Assets))
	for _, a := range r.Assets {
		m[a.Name] = a.URL
	}
	return r.TagName, m, nil
}

// LatestRelease fetches the latest release tag and its assets from GitHub.
func LatestRelease() (tag string, assets map[string]string, err error) {
	req, err := http.NewRequest(http.MethodGet, LatestReleaseURL, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "multi-claude-switcher")
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("github api returned %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}
	return parseRelease(data)
}

// DownloadTo streams a URL to a destination file (truncating it).
func DownloadTo(url, dest string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "multi-claude-switcher")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return f.Close()
}
