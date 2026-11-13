package changelogfile

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
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

// ManifestFormatArchive is the archive's manifest (decision 0262): format 2
// and the mark, the head the archive holds every line up to and the checksum
// of the line at it. An archive pass writes it last, so what it names is
// there.
const ManifestFormatArchive = 3

// ManifestsSubdir holds, under a repository's prefix in a store, every
// archive's manifests and deletion mark (archive.go).
const ManifestsSubdir = "manifests"

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
	// The rest is format 3's alone: one archive's manifest (archive.go),
	// the repository as one database snapshot at Head saw it.
	//
	// Archive is the id of the archive the manifest is of, and Pass the id
	// of the pass that published it.
	Archive, Pass string
	// Head is the archive's mark: every line from 1 to it is in Pieces, and
	// every blob and sealed value the records at it hold is in the archive
	// but those MissingBlobs and PendingBlobs name. 0 is an archive of no
	// lines.
	Head int64
	// HeadSum is the checksum of the line at Head, `sha256:` and hex. Empty
	// when Head is 0.
	HeadSum string
	// Pieces is the history from seq 1 to Head, in order: each piece's name
	// under the archive (PieceObjectName) and the checksum of its bytes.
	Pieces []ArchivePiece
	// Sealed selects the version of each sealed value: ref to the name of
	// its object under the archive (SealedObjectName).
	Sealed map[string]string
	// Spool names the objects holding the webhook requests answered and not
	// yet recorded (SpoolObjectName).
	Spool []string
	// MissingBlobs names the stored blobs the records at Head hold whose
	// bytes the server did not have when the pass ran, and PendingBlobs
	// those it has not copied yet: a pass copies a bounded amount and
	// publishes. Every later pass tries both again. A blob is selected by
	// its digest: any object under it (BlobObjectName) holds its bytes.
	MissingBlobs, PendingBlobs []string
	// Epoch is the manifest's place among its archive's manifests, the
	// number its name carries (ArchiveManifestName).
	Epoch int64
}

// ArchivePiece is one piece a manifest selects: its name under the archive,
// the checksum of its bytes and their length.
type ArchivePiece struct {
	Name string `json:"name"`
	Sum  string `json:"sum"`
	Size int64  `json:"size"`
}

// Chain is the manifest's pieces read apart, each held to its place: the
// first starts at seq 1, each starts where the one before ended, all in the
// manifest's dialect, and the last ends at Head.
func (m Manifest) Chain() ([]Piece, error) {
	chain := make([]Piece, 0, len(m.Pieces))
	next := int64(1)
	for _, p := range m.Pieces {
		dialect, first, last, _, ok := ParsePieceObjectName(p.Name)
		if !ok || dialect != m.ChangelogDialect || first != next || last > m.Head {
			return nil, fmt.Errorf("%w: piece %q is not the one after seq %d in dialect %d", ErrManifestFormat, p.Name, next-1, m.ChangelogDialect)
		}
		if !reSum.MatchString(p.Sum) || p.Size < 0 {
			return nil, fmt.Errorf("%w: piece %q names checksum %q and size %d", ErrManifestFormat, p.Name, p.Sum, p.Size)
		}
		chain = append(chain, Piece{First: first, Last: last, Key: p.Name})
		next = last + 1
	}
	if next != m.Head+1 {
		return nil, fmt.Errorf("%w: its pieces end at seq %d, its head is %d", ErrManifestFormat, next-1, m.Head)
	}
	return chain, nil
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
	// The rest is written in format 3 and nowhere else.
	Archive      *string           `json:"archive,omitempty"`
	Pass         *string           `json:"pass,omitempty"`
	Head         *int64            `json:"head,omitempty"`
	HeadSum      *string           `json:"headSum,omitempty"`
	Pieces       []ArchivePiece    `json:"pieces,omitempty"`
	Sealed       map[string]string `json:"sealed,omitempty"`
	Spool        []string          `json:"spool,omitempty"`
	MissingBlobs []string          `json:"missingBlobs,omitempty"`
	PendingBlobs []string          `json:"pendingBlobs,omitempty"`
	Epoch        *int64            `json:"epoch,omitempty"`
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
	if m.Format == ManifestFormatArchive {
		head := m.Head
		w.Head = &head
		if m.Head > 0 {
			sum := m.HeadSum
			w.HeadSum = &sum
		}
		archive, pass := m.Archive, m.Pass
		w.Archive, w.Pass = &archive, &pass
		w.Pieces, w.Sealed, w.Spool, w.MissingBlobs, w.PendingBlobs = m.Pieces, m.Sealed, m.Spool, m.MissingBlobs, m.PendingBlobs
		epoch := m.Epoch
		w.Epoch = &epoch
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
	if probe.Format != ManifestFormat && probe.Format != ManifestFormatBeforeIDs && probe.Format != ManifestFormatArchive {
		return fmt.Errorf("%w: got %d", ErrManifestFormat, probe.Format)
	}
	var w manifestWire
	if err := decodeClosed(data, &w); err != nil {
		return err
	}
	if w.Format == ManifestFormatBeforeIDs && w.ID != nil {
		return fmt.Errorf("%w: a format-1 manifest carries no id", ErrManifestFormat)
	}
	var head int64
	var headSum string
	switch {
	case w.Format != ManifestFormatArchive && (w.Archive != nil || w.Pass != nil || w.Head != nil || w.HeadSum != nil || w.Pieces != nil ||
		w.Sealed != nil || w.Spool != nil || w.MissingBlobs != nil || w.PendingBlobs != nil || w.Epoch != nil):
		return fmt.Errorf("%w: a format-%d manifest carries nothing of an archive's", ErrManifestFormat, w.Format)
	case w.Format == ManifestFormatArchive:
		if w.Archive == nil || !ValidArchiveID(*w.Archive) || w.Pass == nil || !ValidPassID(*w.Pass) {
			return fmt.Errorf("%w: an archive's manifest names its archive and its pass", ErrManifestFormat)
		}
		if w.Head == nil || *w.Head < 0 {
			return fmt.Errorf("%w: an archive's manifest names its head", ErrManifestFormat)
		}
		if w.Epoch == nil || *w.Epoch < 1 {
			return fmt.Errorf("%w: an archive's manifest names its epoch", ErrManifestFormat)
		}
		head = *w.Head
		if w.HeadSum != nil {
			headSum = *w.HeadSum
		}
		if (head > 0) != (headSum != "") || (headSum != "" && !reSum.MatchString(headSum)) {
			return fmt.Errorf("%w: head %d with headSum %q", ErrManifestFormat, head, headSum)
		}
		for ref, name := range w.Sealed {
			if err := checkSealedRef(ref); err != nil {
				return fmt.Errorf("%w: sealed: %w", ErrManifestFormat, err)
			}
			if _, _, ok := ParseSealedObjectName(ref, name); !ok {
				return fmt.Errorf("%w: sealed %s names object %q", ErrManifestFormat, ref, name)
			}
		}
		for _, name := range w.Spool {
			if _, _, ok := ParseSpoolObjectName(name); !ok {
				return fmt.Errorf("%w: spool object %q", ErrManifestFormat, name)
			}
		}
		for _, digest := range append(slices.Clone(w.MissingBlobs), w.PendingBlobs...) {
			if digest == "" || strings.ContainsAny(digest, "/\\.") {
				return fmt.Errorf("%w: blob %q", ErrManifestFormat, digest)
			}
		}
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
		DEKKeyID: w.DEKKeyID, Head: head, HeadSum: headSum,
		Pieces: w.Pieces, Sealed: w.Sealed, Spool: w.Spool, MissingBlobs: w.MissingBlobs, PendingBlobs: w.PendingBlobs,
	}
	if w.Epoch != nil {
		m.Epoch = *w.Epoch
	}
	if w.Archive != nil {
		m.Archive, m.Pass = *w.Archive, *w.Pass
	}
	if m.Format == ManifestFormatArchive {
		if _, err := m.Chain(); err != nil {
			return err
		}
	}
	return nil
}

// reSum is a line's checksum as the manifest names it.
var reSum = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// SumText is a checksum as a line and the manifest spell it.
func SumText(sum [32]byte) string { return sumPrefix + hex.EncodeToString(sum[:]) }

// ParseSum reads a checksum SumText spelled.
func ParseSum(text string) ([32]byte, error) {
	var sum [32]byte
	if !reSum.MatchString(text) {
		return sum, fmt.Errorf("%w: %q is not a checksum", ErrManifestFormat, text)
	}
	if _, err := hex.Decode(sum[:], []byte(text[len(sumPrefix):])); err != nil {
		return sum, err
	}
	return sum, nil
}

// EncodeArchiveManifest renders an archive's manifest, format 3, checked.
func EncodeArchiveManifest(m Manifest) ([]byte, error) {
	if m.Format != ManifestFormatArchive {
		return nil, fmt.Errorf("%w: an archive's manifest is format %d, not %d", ErrManifestFormat, ManifestFormatArchive, m.Format)
	}
	if m.Epoch < 1 {
		return nil, fmt.Errorf("%w: an archive's manifest names its epoch", ErrManifestFormat)
	}
	if !ValidArchiveID(m.Archive) || !ValidPassID(m.Pass) {
		return nil, fmt.Errorf("%w: an archive's manifest names its archive and its pass", ErrManifestFormat)
	}
	if err := m.checkKey(); err != nil {
		return nil, err
	}
	if _, err := m.Chain(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// DecodeArchiveManifest reads an archive's manifest, refusing any format but
// 3 and one that names another repository than id.
func DecodeArchiveManifest(data []byte, id string) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("changelogfile: decode %s: %w", ManifestName, err)
	}
	if m.Format != ManifestFormatArchive {
		return Manifest{}, fmt.Errorf("%w: an archive's manifest is format %d, this one is %d", ErrManifestFormat, ManifestFormatArchive, m.Format)
	}
	if err := m.checkKey(); err != nil {
		return Manifest{}, err
	}
	if m.ID != id {
		return Manifest{}, fmt.Errorf("%w: %q under %s", ErrManifestAuthority, m.ID, id)
	}
	return m, nil
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
	if err := m.checkKey(); err != nil {
		return err
	}
	if filepath.Base(filepath.Clean(repoDir)) != m.Key() {
		return fmt.Errorf("%w: %q in %s", ErrManifestAuthority, m.Key(), repoDir)
	}
	return nil
}

// checkKey refuses a manifest that does not name a repository.
func (m Manifest) checkKey() error {
	if m.Authority == "" || (m.Format != ManifestFormatBeforeIDs && m.ID == "") {
		return ErrManifestIncomplete
	}
	if !vocabulary.ValidRepositoryAuthority(m.Authority) {
		return fmt.Errorf("%w: %q", ErrRepositoryAuthority, m.Authority)
	}
	if m.Format != ManifestFormatBeforeIDs && !vocabulary.ValidRepositoryID(m.ID) {
		return fmt.Errorf("%w: id %q", ErrRepositoryKey, m.ID)
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
// written; a format-1 manifest is written only as it was read, for a
// directory from before ids, which the boot converts.
func WriteManifest(repoDir string, m Manifest) error {
	if m.Format != ManifestFormat && m.Format != ManifestFormatBeforeIDs && m.Format != ManifestFormatArchive {
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

// ErrArchiveDeleted is the refusal of an archive whose newest life carries
// its repository's deletion mark.
var ErrArchiveDeleted = errors.New("changelogfile: the archive's newest life ends in its repository's deletion")

// ReadArchiveManifestDir reads, from a repository's directory in an archive
// on a file system (`repositories/<id>/` of an export extracted, or of a
// directory store), the newest archive's newest manifest, held to its name,
// and returns it with that name. Two manifests of the highest epoch, and a
// newest archive that carries the deletion mark, are refused.
func ReadArchiveManifestDir(repoDir string) (Manifest, string, error) {
	dir := filepath.Join(repoDir, ManifestsSubdir)
	archives, err := os.ReadDir(dir)
	if err != nil {
		return Manifest{}, "", err
	}
	newest := ""
	for _, e := range archives {
		if e.IsDir() && ValidArchiveID(e.Name()) && e.Name() > newest {
			newest = e.Name()
		}
	}
	if newest == "" {
		return Manifest{}, "", fmt.Errorf("%s holds no archive: %w", dir, os.ErrNotExist)
	}
	entries, err := os.ReadDir(filepath.Join(dir, newest))
	if err != nil {
		return Manifest{}, "", err
	}
	var top int64
	var names []string
	for _, e := range entries {
		if e.Name() == DeletedMark {
			return Manifest{}, "", fmt.Errorf("%w: %s", ErrArchiveDeleted, newest)
		}
		epoch, _, ok := ParseArchiveManifestName(e.Name())
		switch {
		case !ok:
		case epoch > top:
			top, names = epoch, []string{e.Name()}
		case epoch == top:
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return Manifest{}, "", fmt.Errorf("archive %s in %s holds no manifest: %w", newest, dir, os.ErrNotExist)
	}
	if len(names) > 1 {
		return Manifest{}, "", fmt.Errorf("%w: archive %s holds two manifests of epoch %d", ErrManifestFormat, newest, top)
	}
	raw, err := os.ReadFile(filepath.Join(dir, newest, names[0]))
	if err != nil {
		return Manifest{}, "", err
	}
	if !ManifestNamesBytes(names[0], raw) {
		return Manifest{}, "", fmt.Errorf("%w: %s is not the manifest its name says", ErrManifestFormat, names[0])
	}
	m, err := DecodeArchiveManifest(raw, filepath.Base(filepath.Clean(repoDir)))
	if err != nil {
		return Manifest{}, "", err
	}
	if m.Archive != newest || m.Epoch != top {
		return Manifest{}, "", fmt.Errorf("%w: %s/%s names archive %s, epoch %d", ErrManifestFormat, newest, names[0], m.Archive, m.Epoch)
	}
	return m, names[0], nil
}

// WriteArchiveManifestDir writes m beside its archive's manifests in a
// repository's directory on a file system, under the name of its epoch and
// its bytes, and returns that name.
func WriteArchiveManifestDir(repoDir string, m Manifest) (string, error) {
	data, err := EncodeArchiveManifest(m)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(repoDir, ManifestsSubdir, m.Archive)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return "", err
	}
	name := ArchiveManifestName(m.Epoch, data)
	return name, writeFileAtomic(dir, name, data)
}
