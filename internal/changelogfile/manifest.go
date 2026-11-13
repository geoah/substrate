package changelogfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

// ManifestName is the manifest's file name at the top of a repository
// directory.
const ManifestName = "repository.json"

// ManifestFormat is the manifest format this package writes: 2, which names
// the repository by its id and keeps its name beside it (decision 0261). A
// format-1 manifest, written before repositories had ids, names the
// repository by the name alone; it is read so the boot can convert its
// directory (ManifestFormatBeforeIDs), and every other reader refuses it. A
// manifest naming any other format is refused rather than guessed at.
const ManifestFormat = 2

// ManifestFormatBeforeIDs is the format of a directory written before
// repositories had ids, named by the repository's name.
const ManifestFormatBeforeIDs = 1

var (
	// ErrManifestFormat is returned for a manifest whose format this package
	// does not read, and by WriteManifest for one that is not ManifestFormat.
	ErrManifestFormat = errors.New("changelogfile: manifest format is not one this package handles")
	// ErrManifestIncomplete is returned for a manifest with no authority, or a
	// format-2 manifest with no id.
	ErrManifestIncomplete = errors.New("changelogfile: manifest lacks an authority or an id")
	// ErrManifestAuthority is returned when the manifest's key is not the
	// name of the directory it sits in (its id, or the authority of a
	// format-1 manifest): the directory is keyed by it, and a renamed copy
	// would import under the wrong key.
	ErrManifestAuthority = errors.New("changelogfile: manifest key does not name its directory")
)

// Manifest is `repository.json`: what a restore needs to recreate the
// `repositories` row, and the directory's record of what a binary must
// understand to read it. It holds no head; the head is the last line of the
// active segment. The id is the repository's key (decision 0261): the
// directory name, the row's primary key and this field are one value. The
// authority is its name.
type Manifest struct {
	// Format is the format the manifest was read in, or the one to write:
	// ManifestFormat.
	Format int
	// ID is the repository's id; empty in a format-1 manifest.
	ID        string
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
	// that is never in the directory, bound to the repository's key: its id,
	// or the authority of a format-1 manifest.
	DEK []byte
	// DEKKeyID names the host key DEK is wrapped under, the
	// `repositories.dek_key_id` value: 16 hex digits of a one-way hash over
	// the key, never the key. Empty under no key (decision 0059).
	DEKKeyID string
}

// manifestWire is the JSON form of the manifest. CreatedAt is written in
// TSFormat, the precision the row holds, and read as any RFC 3339 time.
type manifestWire struct {
	Format int `json:"format"`
	// ID is written whenever the format has one, empty or not, and never in
	// format 1, which has no `id` key.
	ID                *string `json:"id,omitempty"`
	Authority         string  `json:"authority"`
	CreatedAt         string  `json:"createdAt"`
	ChangelogDialect  int     `json:"changelogDialect"`
	VocabularyDialect int     `json:"vocabularyDialect"`
	DEK               []byte  `json:"dek"`
	DEKKeyID          string  `json:"dekKeyId"`
}

// MarshalJSON renders the manifest in its file form.
func (m Manifest) MarshalJSON() ([]byte, error) {
	w := manifestWire{
		Format: m.Format, Authority: m.Authority,
		ChangelogDialect: m.ChangelogDialect, VocabularyDialect: m.VocabularyDialect, DEK: m.DEK,
		DEKKeyID: m.DEKKeyID,
	}
	if m.Format != ManifestFormatBeforeIDs {
		id := m.ID
		w.ID = &id
	}
	if !m.CreatedAt.IsZero() {
		w.CreatedAt = m.CreatedAt.UTC().Format(TSFormat)
	}
	return json.Marshal(w)
}

// UnmarshalJSON parses the file form, refusing any format but ManifestFormat
// and ManifestFormatBeforeIDs. The key set is closed: an unknown key is
// refused, because a format is defined by exactly its keys and a later format
// announces itself in `format`; a format-1 manifest carries no `id`.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	var probe struct {
		Format int `json:"format"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if probe.Format != ManifestFormat && probe.Format != ManifestFormatBeforeIDs {
		return fmt.Errorf("%w: got %d", ErrManifestFormat, probe.Format)
	}
	var w manifestWire
	if err := decodeClosed(data, &w); err != nil {
		return err
	}
	if w.Format == ManifestFormatBeforeIDs && w.ID != nil {
		return fmt.Errorf("%w: a format-1 manifest carries no id", ErrManifestFormat)
	}
	id := ""
	if w.ID != nil {
		id = *w.ID
	}
	created, err := parseManifestTime(w.CreatedAt)
	if err != nil {
		return err
	}
	*m = Manifest{
		Format: w.Format, ID: id, Authority: w.Authority,
		CreatedAt: created, ChangelogDialect: w.ChangelogDialect,
		VocabularyDialect: w.VocabularyDialect, DEK: w.DEK,
		DEKKeyID: w.DEKKeyID,
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

// Key is what names the manifest's directory: the id, or the authority of a
// format-1 manifest.
func (m Manifest) Key() string {
	if m.Format == ManifestFormatBeforeIDs {
		return m.Authority
	}
	return m.ID
}

// check refuses a manifest that does not name a repository, or whose key does
// not name the directory repoDir. The format is checked where it is read
// (UnmarshalJSON) and where it is written (WriteManifest).
func (m Manifest) check(repoDir string) error {
	if m.Authority == "" || (m.Format != ManifestFormatBeforeIDs && m.ID == "") {
		return ErrManifestIncomplete
	}
	if !vocabulary.ValidRepositoryAuthority(m.Authority) {
		return fmt.Errorf("%w: %q", ErrRepositoryAuthority, m.Authority)
	}
	if m.Format != ManifestFormatBeforeIDs && !vocabulary.ValidRepositoryID(m.ID) {
		return fmt.Errorf("%w: id %q", ErrRepositoryKey, m.ID)
	}
	if filepath.Base(filepath.Clean(repoDir)) != m.Key() {
		return fmt.Errorf("%w: %q in %s", ErrManifestAuthority, m.Key(), repoDir)
	}
	return nil
}

// ReadManifest reads and checks the manifest of the repository directory. A
// missing manifest is reported with an error that matches os.ErrNotExist, so
// the boot check can tell a directory with no manifest from a damaged one.
// The manifest returned carries the format it was read in.
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

// ReadManifestToKey reads the manifest of a directory still named by its
// repository's authority, as the boot finds one a release before ids wrote:
// a format-1 manifest, or the format-2 one the boot wrote into it before a
// crash cut the rename short. Both must name the directory by their
// authority; nothing but the boot's conversion reads a directory this way.
func ReadManifestToKey(repoDir string) (Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(repoDir, ManifestName))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, fmt.Errorf("changelogfile: decode %s: %w", ManifestName, err)
	}
	keyed := m
	keyed.Format = ManifestFormatBeforeIDs
	if err := keyed.check(repoDir); err != nil {
		return Manifest{}, err
	}
	if m.Format == ManifestFormat && !vocabulary.ValidRepositoryID(m.ID) {
		return Manifest{}, fmt.Errorf("%w: id %q", ErrRepositoryKey, m.ID)
	}
	return m, nil
}

// WriteManifestToKey writes a format-2 manifest into a directory still named
// by its repository's authority: the boot's conversion step between
// rewriting the manifest and renaming the directory to its id.
func WriteManifestToKey(repoDir string, m Manifest) error {
	if m.Format != ManifestFormat {
		return fmt.Errorf("%w: writing format %d, this package writes %d", ErrManifestFormat, m.Format, ManifestFormat)
	}
	if err := m.check(filepath.Join(filepath.Dir(filepath.Clean(repoDir)), m.ID)); err != nil {
		return err
	}
	if filepath.Base(filepath.Clean(repoDir)) != m.Authority {
		return fmt.Errorf("%w: %q in %s", ErrManifestAuthority, m.Authority, repoDir)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(repoDir, ManifestName, append(data, '\n'))
}

// WriteManifest checks the manifest and writes it atomically into the
// repository directory, replacing any manifest there. ManifestFormat is
// written; a format-1 manifest is written back only as it was read, by the
// offline rewrap of a directory from before ids, which the boot converts.
func WriteManifest(repoDir string, m Manifest) error {
	if m.Format != ManifestFormat && m.Format != ManifestFormatBeforeIDs {
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
