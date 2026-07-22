//go:build windows

package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// makeTestZip writes a zip at path containing the given name->content entries.
func makeTestZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractUpdateArchiveAndFindBinary(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "update.zip")
	makeTestZip(t, zipPath, map[string]string{
		"mcs-tray.exe":  "MZ-fake-binary",
		"README.txt":    "hello",
		"sub/other.dll": "x",
	})

	dest := filepath.Join(dir, "extract")
	if err := extractUpdateArchive(zipPath, dest); err != nil {
		t.Fatalf("extractUpdateArchive: %v", err)
	}

	// The binary must be extracted with its bytes intact.
	got, err := os.ReadFile(filepath.Join(dest, "mcs-tray.exe"))
	if err != nil {
		t.Fatalf("reading extracted binary: %v", err)
	}
	if string(got) != "MZ-fake-binary" {
		t.Errorf("extracted binary content = %q", got)
	}

	bin, err := findTrayBinary(dest)
	if err != nil {
		t.Fatalf("findTrayBinary: %v", err)
	}
	if filepath.Base(bin) != "mcs-tray.exe" {
		t.Errorf("findTrayBinary = %q, want .../mcs-tray.exe", bin)
	}
}

func TestFindTrayBinaryMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "not-it.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := findTrayBinary(dir); err == nil {
		t.Fatal("expected an error when mcs-tray.exe is absent")
	}
}
