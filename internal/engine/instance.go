package engine

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// instanceFile names the file under the data root that holds this server's
// instance id. It lives beside `repositories/`, not in the database: the data
// root is the truth a wiped database is imported from, and a server whose
// database is rebuilt is still the same server to every domain whose
// `_substrate` record names it (decision 0208).
const instanceFile = "instance"

// loadInstanceID reads the data root's instance id, minting and writing one
// on the first boot. A read-only process never writes, and answers "" when no
// server has minted one yet.
func loadInstanceID(root string, readOnly bool) (string, error) {
	path := filepath.Join(root, instanceFile)
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		id := strings.TrimSpace(string(raw))
		if !validInstanceID(id) {
			return "", fmt.Errorf("substrate/engine: %s holds %q, not an instance id: remove the file and a new one is minted at the next boot", path, id)
		}
		return id, nil
	case !errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("substrate/engine: read %s: %w", path, err)
	case readOnly:
		return "", nil
	}
	id, err := newInstanceID()
	if err != nil {
		return "", err
	}
	// Written whole to a uniquely named temporary file, then linked to the
	// final name, which fails when the name exists. Two first boots racing
	// over one data root therefore persist one id, and the loser answers the
	// winner's: rename would let the second replace the first after the first
	// had already answered its own.
	tmp, err := os.CreateTemp(root, instanceFile+"-*.tmp")
	if err != nil {
		return "", fmt.Errorf("substrate/engine: write %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(id + "\n"); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("substrate/engine: write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("substrate/engine: write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("substrate/engine: write %s: %w", tmp.Name(), err)
	}
	if err := os.Link(tmp.Name(), path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return loadInstanceID(root, true)
		}
		// A data root without hard links (an SMB share, some volume
		// drivers) publishes the id by an exclusive create: still one
		// winner, though a crash between the create and the write leaves a
		// file the next boot refuses, naming it.
		if err := writeExclusive(path, id+"\n"); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return loadInstanceID(root, true)
			}
			return "", fmt.Errorf("substrate/engine: write %s: %w", path, err)
		}
	}
	return id, nil
}

// writeExclusive creates path, which must not exist, holding content.
func writeExclusive(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// newInstanceID mints 26 characters of lowercase base32 over 16 random bytes.
func newInstanceID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return strings.ToLower(b32.EncodeToString(raw)), nil
}

func validInstanceID(id string) bool {
	if len(id) < 16 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// InstanceID is this server's instance id, minted once per data root.
func (s *service) InstanceID() string { return s.instanceID }
