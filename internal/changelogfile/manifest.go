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
	// ErrManifestIncomplete is returned for a manifest with no authority or
	// no username.
	ErrManifestIncomplete = errors.New("changelogfile: manifest lacks an authority or a username")
	// ErrManifestAuthority is returned when the manifest's authority is not
	// the name of the directory it sits in: the directory is keyed by the
	// authority, and a renamed copy would import under the wrong key.
	ErrManifestAuthority = errors.New("changelogfile: manifest authority does not name its directory")
)

// Manifest is `repository.json`: what a restore needs to recreate the
// `repositories` row. It holds no head; the head is the last line of the
// active segment. The authority is the repository's id (decision 0046), so
// there is no separate id field: the directory name, the row's primary key
// and this field are one value.
type Manifest struct {
	Format    int
	Username  string
	Authority string
	CreatedAt time.Time
	// ChangelogDialect is the repository's `repositories.changelog_dialect`.
	ChangelogDialect int
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

// manifestWire is the JSON form. CreatedAt is written in TSFormat, the
// precision the row holds, and read as any RFC 3339 time.
type manifestWire struct {
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
		ChangelogDialect: m.ChangelogDialect, DEK: m.DEK,
		DEKKeyID: m.DEKKeyID, SealedDEKOnly: m.SealedDEKOnly,
	}
	if !m.CreatedAt.IsZero() {
		w.CreatedAt = m.CreatedAt.UTC().Format(TSFormat)
	}
	return json.Marshal(w)
}

// UnmarshalJSON parses the file form. The key set is closed: an unknown key is
// refused, because format 1 is defined by exactly these keys and a later
// format announces itself in `format`. `dekKeyId` and `sealedDekOnly` joined
// the set after the first format-1 manifests were written, so a manifest
// without them reads as an unnamed key and an unmarked store, which is what
// such a directory is. The `id` key a pre-authority binary wrote is unknown
// here on purpose; ReadLegacyManifest reads that shape.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var w manifestWire
	if err := dec.Decode(&w); err != nil {
		return err
	}
	created, err := parseManifestTime(w.CreatedAt)
	if err != nil {
		return err
	}
	*m = Manifest{
		Format: w.Format, Username: w.Username, Authority: w.Authority,
		CreatedAt: created, ChangelogDialect: w.ChangelogDialect, DEK: w.DEK,
		DEKKeyID: w.DEKKeyID, SealedDEKOnly: w.SealedDEKOnly,
	}
	return nil
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

// check refuses a manifest this package will neither write nor act on, and
// one that does not name the directory repoDir.
func (m Manifest) check(repoDir string) error {
	if m.Format != ManifestFormat {
		return fmt.Errorf("%w: got %d", ErrManifestFormat, m.Format)
	}
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

// legacyManifestWire is manifestWire plus the `id` key.
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
// closed key set applies: this reads exactly the old shape.
func ReadLegacyManifest(repoDir string) (LegacyManifest, error) {
	raw, err := os.ReadFile(filepath.Join(repoDir, ManifestName))
	if err != nil {
		return LegacyManifest{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var w legacyManifestWire
	if err := dec.Decode(&w); err != nil {
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
	if lm.Manifest.Format != ManifestFormat {
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
