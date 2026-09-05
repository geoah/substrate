package changelogfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ManifestName is the manifest's file name at the top of a repository
// directory.
const ManifestName = "repository.json"

// ManifestFormat is the one manifest format this package reads and writes.
// A manifest naming another is refused rather than guessed at.
const ManifestFormat = 1

var (
	// ErrManifestFormat is returned for a manifest whose format is not
	// ManifestFormat.
	ErrManifestFormat = errors.New("changelogfile: manifest format is not 1")
	// ErrManifestIncomplete is returned for a manifest with no id or no
	// username.
	ErrManifestIncomplete = errors.New("changelogfile: manifest lacks an id or a username")
	// ErrManifestID is returned when the manifest's id is not the name of the
	// directory it sits in: the directory is keyed by the id, and a renamed
	// copy would import under the wrong key.
	ErrManifestID = errors.New("changelogfile: manifest id does not name its directory")
)

// Manifest is `repository.json`: what a restore needs to recreate the
// `repositories` row. It holds no head; the head is the last line of the
// active segment.
type Manifest struct {
	Format    int
	ID        string
	Username  string
	Authority string
	CreatedAt time.Time
	// ChangelogDialect is the repository's `repositories.changelog_dialect`.
	ChangelogDialect int
	// DEK is the repository's data key wrapped under SUBSTRATE_CREDENTIAL_KEY,
	// the `repositories.dek` bytes; base64 on the wire. Ciphertext under a key
	// that is never in the directory.
	DEK []byte
}

// manifestWire is the JSON form. CreatedAt is written in TSFormat, the
// precision the row holds, and read as any RFC 3339 time.
type manifestWire struct {
	Format           int    `json:"format"`
	ID               string `json:"id"`
	Username         string `json:"username"`
	Authority        string `json:"authority"`
	CreatedAt        string `json:"createdAt"`
	ChangelogDialect int    `json:"changelogDialect"`
	DEK              []byte `json:"dek"`
}

// MarshalJSON renders the manifest in its file form.
func (m Manifest) MarshalJSON() ([]byte, error) {
	w := manifestWire{
		Format: m.Format, ID: m.ID, Username: m.Username, Authority: m.Authority,
		ChangelogDialect: m.ChangelogDialect, DEK: m.DEK,
	}
	if !m.CreatedAt.IsZero() {
		w.CreatedAt = m.CreatedAt.UTC().Format(TSFormat)
	}
	return json.Marshal(w)
}

// UnmarshalJSON parses the file form. The key set is closed: an unknown key is
// refused, because format 1 is defined by exactly these keys and a later
// format announces itself in `format`.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var w manifestWire
	if err := dec.Decode(&w); err != nil {
		return err
	}
	var created time.Time
	if w.CreatedAt != "" {
		t, err := time.Parse(time.RFC3339Nano, w.CreatedAt)
		if err != nil {
			return fmt.Errorf("createdAt: %w", err)
		}
		created = t.UTC()
	}
	*m = Manifest{
		Format: w.Format, ID: w.ID, Username: w.Username, Authority: w.Authority,
		CreatedAt: created, ChangelogDialect: w.ChangelogDialect, DEK: w.DEK,
	}
	return nil
}

// check refuses a manifest this package will neither write nor act on, and
// one that does not name the directory repoDir.
func (m Manifest) check(repoDir string) error {
	if m.Format != ManifestFormat {
		return fmt.Errorf("%w: got %d", ErrManifestFormat, m.Format)
	}
	if m.ID == "" || m.Username == "" {
		return ErrManifestIncomplete
	}
	if err := checkRepositoryID(m.ID); err != nil {
		return err
	}
	if filepath.Base(filepath.Clean(repoDir)) != m.ID {
		return fmt.Errorf("%w: id %q in %s", ErrManifestID, m.ID, repoDir)
	}
	return nil
}

// ReadManifest reads and checks the manifest of the repository directory. A
// missing manifest is reported with an error that matches os.ErrNotExist, so
// the boot check can tell a directory with no manifest from a damaged one.
func ReadManifest(repoDir string) (Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(repoDir, ManifestName))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, fmt.Errorf("changelogfile: decode %s: %w", ManifestName, err)
	}
	if err := m.check(repoDir); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// WriteManifest checks the manifest and writes it atomically into the
// repository directory, replacing any manifest there.
func WriteManifest(repoDir string, m Manifest) error {
	if err := m.check(repoDir); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(repoDir, ManifestName, append(data, '\n'))
}
