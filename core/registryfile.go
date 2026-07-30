package core

import (
	"os"
	"path/filepath"
)

// writeRegistryFile replaces path's contents with data, atomically.
//
// The staging file is created with a unique name rather than a fixed
// "path.tmp". MCS runs as more than one process — the tray and the panel are
// separate binaries and both write these registries — and a shared staging name
// means one process can truncate the file another is halfway through writing,
// then rename that mixture into place. A unique name per write makes the rename
// the only thing the two contend over, and a rename is atomic.
//
// The file is fsynced before the rename so a crash or power loss cannot leave
// the rename durable while the bytes it points at are not — that combination
// yields an empty registry, which for managed.json means every account
// disappears from the panel.
//
// A failed write removes its own staging file, so a repeatedly failing write
// cannot accumulate debris in the user's config directory.
func writeRegistryFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	// CreateTemp makes the file 0600. These registries hold profile names, not
	// secrets, and the rest of MCS writes them 0644; keep that consistent so a
	// registry written by one code path is not unreadable to a differently
	// privileged one.
	if err := tmp.Chmod(0o644); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
