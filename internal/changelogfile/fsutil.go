package changelogfile

import (
	"os"
	"path/filepath"
)

// dirMode and fileMode keep a repository's directory readable by the
// substrate's own user and nobody else: the changelog carries every record in
// the clear, and on a shared box the mode is the only thing between another
// local account and the whole repository. They match internal/blobbytes/fs.go
// because the blobs live one directory over.
const (
	dirMode  os.FileMode = 0o700
	fileMode os.FileMode = 0o600
)

// tmpPrefix marks a half-written file. Every listing skips the prefix, so a
// crash between CreateTemp and Rename leaves nothing a reader mistakes for a
// segment, a sidecar, a manifest or a sealed record.
const tmpPrefix = ".incoming-"

// writeFileAtomic writes data to <dir>/<name> through a temporary file in the
// same directory and a rename, so a reader sees either the old file, the new
// file, or none, never a prefix of one. The file and then the directory are
// fsynced, so the name is durable when this returns.
func writeFileAtomic(dir, name string, data []byte) error {
	tmp, err := os.CreateTemp(dir, tmpPrefix+"*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(fileMode); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		return err
	}
	return syncDir(dir)
}

// syncDir flushes a directory entry (a create, a rename, an unlink) to disk.
// Without it the file's contents are durable but the name pointing at them may
// not be.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}
