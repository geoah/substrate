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
// A manifest naming any other format is refused rather than guessed at: a
// format is defined by exactly its keys, and a later one announces itself
// here.
const ManifestFormat = 2

var (
	// ErrManifestFormat is returned for a manifest whose format this package
	// does not read, and by WriteManifest for one that is not ManifestFormat.
	ErrManifestFormat = errors.New("changelogfile: manifest format is not one this package handles")
	// ErrManifestIncomplete is returned for a manifest with no authority or
	// no username.
	ErrManifestIncomplete = errors.New("changelogfile: manifest lacks an authority or a username")
	// ErrManifestAuthority is returned when the manifest's authority is not
	// the name of the directory it sits in: the directory is keyed by the
	// authority, and a renamed copy would import under the wrong key.
	ErrManifestAuthority = errors.New("changelogfile: manifest authority does not name its directory")
)

// Manifest is `repository.json`: what a restore needs to recreate the
// `repositories` row, and the directory's record of what a binary must
// understand to read it. It holds no head; the head is the last line of the
// active segment. The authority is the repository's id (decision 0046), so
// there is no separate id field: the directory name, the row's primary key
// and this field are one value.
type Manifest struct {
	// Format is ManifestFormat, the one format read and written.
	Format    int
	Username  string
	Authority string
	CreatedAt time.Time
	// ChangelogDialect is the repository's `repositories.changelog_dialect`,
	// what a binary must replay to fold the segments.
	ChangelogDialect int
	// VocabularyDialect is the repository's `vocabulary_dialect` stamp, the
	// shape its stored declaration rows are in.
	VocabularyDialect int
	// DEK is the repository's data key wrapped under SUBSTRATE_CREDENTIAL_KEY,
	// the `repositories.dek` bytes; base64 on the wire. Ciphertext under a key
	// that is never in the directory.
	DEK []byte
	// DEKKeyID names the host key DEK is wrapped under, the
	// `repositories.dek_key_id` value: 16 hex digits of a one-way hash over
	// the key, never the key. Empty for a wrap written before the id was
	// recorded, or under no key (decision 0059).
	DEKKeyID string
	// SealedDEKOnly is `repositories.sealed_dek_only`: every file under
	// sealed/ is bound-framed ciphertext under DEK, so the engine refuses
	// every other form on this repository (0059).
	SealedDEKOnly bool
}

// manifestWire is the JSON form of the manifest. CreatedAt is written in
// TSFormat, the precision the row holds, and read as any RFC 3339 time.
type manifestWire struct {
	Format            int    `json:"format"`
	Username          string `json:"username"`
	Authority         string `json:"authority"`
	CreatedAt         string `json:"createdAt"`
	ChangelogDialect  int    `json:"changelogDialect"`
	VocabularyDialect int    `json:"vocabularyDialect"`
	DEK               []byte `json:"dek"`
	DEKKeyID          string `json:"dekKeyId"`
	SealedDEKOnly     bool   `json:"sealedDekOnly"`
}

// MarshalJSON renders the manifest in its file form.
func (m Manifest) MarshalJSON() ([]byte, error) {
	w := manifestWire{
		Format: m.Format, Username: m.Username, Authority: m.Authority,
		ChangelogDialect: m.ChangelogDialect, VocabularyDialect: m.VocabularyDialect, DEK: m.DEK,
		DEKKeyID: m.DEKKeyID, SealedDEKOnly: m.SealedDEKOnly,
	}
	if !m.CreatedAt.IsZero() {
		w.CreatedAt = m.CreatedAt.UTC().Format(TSFormat)
	}
	return json.Marshal(w)
}

// UnmarshalJSON parses the file form. The key set is closed: an unknown key
// is refused, because a format is defined by exactly its keys and a later
// format announces itself in `format`.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	var probe struct {
		Format int `json:"format"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if probe.Format != ManifestFormat {
		return fmt.Errorf("%w: got %d", ErrManifestFormat, probe.Format)
	}
	var w manifestWire
	if err := decodeClosed(data, &w); err != nil {
		return err
	}
	created, err := parseManifestTime(w.CreatedAt)
	if err != nil {
		return err
	}
	*m = Manifest{
		Format: w.Format, Username: w.Username, Authority: w.Authority,
		CreatedAt: created, ChangelogDialect: w.ChangelogDialect,
		VocabularyDialect: w.VocabularyDialect, DEK: w.DEK,
		DEKKeyID: w.DEKKeyID, SealedDEKOnly: w.SealedDEKOnly,
	}
	return nil
}

// decodeClosed decodes data into v, refusing any key v does not declare.
func decodeClosed(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func parseManifestTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("createdAt: %w", err)
	}
	return t.UTC(), nil
}

// check refuses a manifest that does not name a repository, or does not name
// the directory repoDir. The format is checked where it is read (UnmarshalJSON)
// and where it is written (WriteManifest).
func (m Manifest) check(repoDir string) error {
	if m.Authority == "" || m.Username == "" {
		return ErrManifestIncomplete
	}
	if err := checkRepositoryAuthority(m.Authority); err != nil {
		return err
	}
	if filepath.Base(filepath.Clean(repoDir)) != m.Authority {
		return fmt.Errorf("%w: authority %q in %s", ErrManifestAuthority, m.Authority, repoDir)
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
	if m.Format != ManifestFormat {
		return fmt.Errorf("%w: writing format %d, this package writes %d", ErrManifestFormat, m.Format, ManifestFormat)
	}
	if err := m.check(repoDir); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(repoDir, ManifestName, append(data, '\n'))
}
