//go:build windows

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLooksLikeExecutable(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want bool
	}{
		{
			name: "PE header",
			in:   []byte{'M', 'Z', 0x90, 0x00},
			want: true,
		},
		{
			name: "exactly the signature",
			in:   []byte("MZ"),
			want: true,
		},
		{
			name: "HTML error page served with 200",
			in:   []byte("<!DOCTYPE html>"),
			want: false,
		},
		{
			name: "truncated to one byte",
			in:   []byte("M"),
			want: false,
		},
		{
			name: "empty download",
			in:   nil,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeExecutable(tc.in); got != tc.want {
				t.Errorf("looksLikeExecutable(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestInstallerFlagsAreUnattended guards the contract with
// packaging/windows-setup.iss: the updater runs the installer with nobody
// watching, so anything that could put a window or a prompt on screen — or
// merely /SILENT, which still shows a progress window — breaks the "no
// questions asked" upgrade.
func TestInstallerFlagsAreUnattended(t *testing.T) {
	flags := installerFlags()

	for _, required := range []string{"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART"} {
		if !slices.Contains(flags, required) {
			t.Errorf("installerFlags() = %v, missing %s", flags, required)
		}
	}
	if slices.Contains(flags, "/SILENT") {
		t.Errorf("installerFlags() = %v, /SILENT still shows a progress window; use /VERYSILENT", flags)
	}
}

func TestDownloadInstallerTo(t *testing.T) {
	const body = "MZ\x90\x00installer"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), updateDirName)
	// A leftover from a previous update: this process exits while the installer
	// is still running, so it never cleans up after itself and the next run has
	// to. Seed one and prove it is gone afterwards.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "stale.exe")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := downloadInstallerTo(dir, srv.URL)
	if err != nil {
		t.Fatalf("downloadInstallerTo() error = %v", err)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("reading downloaded installer: %v", err)
	}
	if string(data) != body {
		t.Errorf("downloaded %q, want %q", data, body)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale installer %s survived; the scratch dir was not cleared", stale)
	}
}

// TestDownloadInstallerToRejectsNonExecutable covers the case that actually
// bites: a proxy or captive portal answering 200 with an HTML page. Handing
// that to CreateProcess would fail confusingly, or worse, run something else.
func TestDownloadInstallerToRejectsNonExecutable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<!DOCTYPE html><title>Sign in to the network</title>"))
	}))
	defer srv.Close()

	if _, err := downloadInstallerTo(filepath.Join(t.TempDir(), updateDirName), srv.URL); err == nil {
		t.Error("downloadInstallerTo() accepted an HTML page as an installer")
	}
}
