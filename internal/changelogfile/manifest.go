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

// ManifestFormat is the manifest format this package writes. Format 2 adds
// `vocabularyDialect` to format 1's keys. Format 1, which v0.46.0 through
// v0.53.0 wrote, is still read (manifestFormatOne, and ReadLegacyManifest
// for v0.46.0's shape); a manifest naming any other format is refused rather
// than guessed at.
const ManifestFormat = 2

// manifestFormatOne is the format v0.47.0 through v0.53.0 wrote: format 2's
// keys without `vocabularyDialect`. `dekKeyId` and `sealedDekOnly` joined the
// set in v0.53.0 (decision 0059), so a format-1 manifest may carry them or
// not; one without them reads as an unnamed key and an unmarked store, which
// is what such a directory is. v0.46.0 wrote the same keys with an `id`
// beside them, which ReadLegacyManifest reads. Read, never written.
const manifestFormatOne = 1

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
	// Format is the format the manifest was read in, or the one to write:
	// ManifestFormat, or manifestFormatOne on a manifest v0.46.0 through
	// v0.53.0 wrote.
	Format    int
	Username  string
	Authority string
	CreatedAt time.Time
	// ChangelogDialect is the repository's `repositories.changelog_dialect`,
	// what a binary must replay to fold the segments.
	ChangelogDialect int
	// VocabularyDialect is the repository's `vocabulary_dialect` stamp, the
	// shape its stored declaration rows are in. 0 on a format-1 manifest,
	// which recorded none.
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
	// sealed/ is bound-framed ciphertext under DEK, with no plain and no
	// host-key-sealed payload left, so the engine refuses those forms on
	// this repository. False for a directory written before the marker
	// existed; the first open re-keys the store and sets it (0059).
	SealedDEKOnly bool
}

// manifestWire is the JSON form of format 2. CreatedAt is written in
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

// manifestWireOne is the JSON form of format 1: manifestWire without
// `vocabularyDialect`. Its key set is closed on its own, so a format-1
// document carrying the format-2 key is refused as unknown.
type manifestWireOne struct {
	Format           int    `json:"format"`
	Username         string `json:"username"`
	Authority        string `json:"authority"`
	CreatedAt        string `json:"createdAt"`
	ChangelogDialect int    `json:"changelogDialect"`
	DEK              []byte `json:"dek"`
	DEKKeyID         string `json:"dekKeyId"`
	SealedDEKOnly    bool   `json:"sealedDekOnly"`
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

// UnmarshalJSON parses the file form of format 2 or format 1, chosen by the
// `format` key. Each format's key set is closed: an unknown key is refused,
// because a format is defined by exactly its keys and a later format
// announces itself in `format`. The `id` key a pre-authority binary wrote is
// unknown here on purpose; ReadLegacyManifest reads that shape.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	var probe struct {
		Format int `json:"format"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	var w manifestWire
	switch probe.Format {
	case ManifestFormat:
		if err := decodeClosed(data, &w); err != nil {
			return err
		}
	case manifestFormatOne:
		var one manifestWireOne
		if err := decodeClosed(data, &one); err != nil {
			return err
		}
		w = manifestWire{
			Format: one.Format, Username: one.Username, Authority: one.Authority,
			CreatedAt: one.CreatedAt, ChangelogDialect: one.ChangelogDialect, DEK: one.DEK,
			DEKKeyID: one.DEKKeyID, SealedDEKOnly: one.SealedDEKOnly,
		}
	default:
		return fmt.Errorf("%w: got %d", ErrManifestFormat, probe.Format)
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
// and where it is written (WriteManifest), because the two accept different
// sets.
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

// WriteManifest checks the manifest and writes it atomically into the
// repository directory, replacing any manifest there. Only ManifestFormat is
// written: a manifest read in an older format is the caller's to bring up to
// date, because the keys the newer format adds are not this package's to
// invent.
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

// --- the pre-authority manifest ----------------------------------------------

// LegacyManifest is the manifest a binary from before the authority became
// the id wrote: format 1 with an `id` key holding a random id, which named
// the directory and was the additional data the DEK wrap was bound to. The
// engine reads one only to move the directory under its authority.
type LegacyManifest struct {
	// ID is the old random repository id, the directory's name.
	ID       string
	Manifest Manifest
}

// legacyManifestWire is manifestWireOne plus the `id` key.
type legacyManifestWire struct {
	Format           int    `json:"format"`
	ID               string `json:"id"`
	Username         string `json:"username"`
	Authority        string `json:"authority"`
	CreatedAt        string `json:"createdAt"`
	ChangelogDialect int    `json:"changelogDialect"`
	DEK              []byte `json:"dek"`
}

// ReadLegacyManifest reads the pre-authority manifest in repoDir. Its
// authority must be one a directory can be named by, since that is what the
// caller renames the directory to; whether the `id` names the directory is
// the caller's check, because the caller may be finishing a move that renamed
// the directory and then crashed before writing the new manifest. The same
// closed key set applies: this reads exactly the old shape, and the manifest
// it returns is format 1.
func ReadLegacyManifest(repoDir string) (LegacyManifest, error) {
	raw, err := os.ReadFile(filepath.Join(repoDir, ManifestName))
	if err != nil {
		return LegacyManifest{}, err
	}
	var w legacyManifestWire
	if err := decodeClosed(raw, &w); err != nil {
		return LegacyManifest{}, fmt.Errorf("changelogfile: decode %s: %w", ManifestName, err)
	}
	created, err := parseManifestTime(w.CreatedAt)
	if err != nil {
		return LegacyManifest{}, fmt.Errorf("changelogfile: decode %s: %w", ManifestName, err)
	}
	lm := LegacyManifest{
		ID: w.ID,
		Manifest: Manifest{
			Format: w.Format, Username: w.Username, Authority: w.Authority,
			CreatedAt: created, ChangelogDialect: w.ChangelogDialect, DEK: w.DEK,
		},
	}
	if lm.Manifest.Format != manifestFormatOne {
		return LegacyManifest{}, fmt.Errorf("%w: got %d", ErrManifestFormat, lm.Manifest.Format)
	}
	if lm.ID == "" || lm.Manifest.Authority == "" || lm.Manifest.Username == "" {
		return LegacyManifest{}, fmt.Errorf("%w (a pre-authority manifest needs an id too)", ErrManifestIncomplete)
	}
	if !reLegacyRepositoryID.MatchString(lm.ID) || lm.ID == "." || lm.ID == ".." {
		return LegacyManifest{}, fmt.Errorf("%w: %q", ErrLegacyRepositoryDir, lm.ID)
	}
	if err := checkRepositoryAuthority(lm.Manifest.Authority); err != nil {
		return LegacyManifest{}, err
	}
	return lm, nil
}
