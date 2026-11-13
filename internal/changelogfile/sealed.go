package changelogfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	// ErrSealedRef is returned for a ref that is not colon-separated labels of
	// the id alphabet, or that starts with a dot. A ref is a file name, so it
	// must be one path segment that no listing skips.
	ErrSealedRef = errors.New("changelogfile: sealed ref is not <label>(:<label>)*")
	// ErrSealedName is returned when a sealed file's name is not the file name
	// of the ref it holds: the file was renamed or its content replaced.
	ErrSealedName = errors.New("changelogfile: sealed file name does not match the ref it holds")
)

// reSealedRef matches every ref the engine mints: `secret:<hex>`,
// `secret:erased`, `auth:password:<hex>`, `auth:totp:<hex>` and a bare record
// id for an OAuth credential. Labels are the record id alphabet, so `/` and
// `\` can never appear, and the colon is the one separator the file name
// rewrites.
var reSealedRef = regexp.MustCompile(`^[A-Za-z0-9_.-]+(?::[A-Za-z0-9_.-]+)*$`)

// maxSealedRef bounds a ref so its file name stays well inside NAME_MAX.
const maxSealedRef = 200

const sealedSuffix = ".json"

// pendingSuffix marks a staged sealed file: the payload a write put on disk
// before its transaction committed, under `<file>.json.pending`. It is not
// the record until CommitSealed renames it over the `.json`, so a reader
// lists it with PendingSealed and never as a record, and a boot removes it.
const pendingSuffix = ".pending"

func checkSealedRef(ref string) error {
	if len(ref) > maxSealedRef || !reSealedRef.MatchString(ref) || strings.HasPrefix(ref, ".") {
		return fmt.Errorf("%w: %q", ErrSealedRef, ref)
	}
	return nil
}

// SealedFileName is the file that holds ref: the ref with every `:` replaced
// by `-`, plus `.json`.
func SealedFileName(ref string) string {
	return strings.ReplaceAll(ref, ":", "-") + sealedSuffix
}

// SealedRecord is one row of the sealed table as its file carries it. Payload
// is ciphertext under the repository's DEK, base64 on the wire.
type SealedRecord struct {
	Ref        string     `json:"ref"`
	RecordKind string     `json:"recordKind"`
	RecordID   string     `json:"recordId"`
	Payload    []byte     `json:"payload"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

// WriteSealed writes the record's file atomically into the repository's
// sealed directory, creating the directory when it is missing and replacing
// any file under the same ref.
func WriteSealed(repoDir string, rec SealedRecord) error {
	if err := checkSealedRef(rec.Ref); err != nil {
		return err
	}
	dir := SealedDir(repoDir)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return writeFileAtomic(dir, SealedFileName(rec.Ref), append(data, '\n'))
}

// PendingSealed lists the pending files under sealed/ by file name, sorted. A
// missing directory lists as empty.
func PendingSealed(repoDir string) ([]string, error) {
	entries, err := os.ReadDir(SealedDir(repoDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), sealedSuffix+pendingSuffix) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// DiscardPendingSealed removes every pending file under sealed/ and returns
// how many it removed: a staged write whose commit the directory never saw.
// A boot runs it after the table has been written out, so a pending file
// whose transaction did commit is already the record by then.
func DiscardPendingSealed(repoDir string) (int, error) {
	names, err := PendingSealed(repoDir)
	if err != nil || len(names) == 0 {
		return 0, err
	}
	dir := SealedDir(repoDir)
	for _, name := range names {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
	}
	return len(names), syncDir(dir)
}

// ReadSealed reads every sealed file of the repository, sorted by ref. A
// missing directory reads as empty. A `.json` file that does not decode, holds
// a ref outside the grammar, or is not named by its own ref is refused.
func ReadSealed(repoDir string) ([]SealedRecord, error) {
	dir := SealedDir(repoDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []SealedRecord
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, tmpPrefix) || !strings.HasSuffix(name, sealedSuffix) {
			continue
		}
		rec, err := readSealedFile(dir, name)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out, nil
}

// readSealedFile decodes one sealed file and holds it to its name.
func readSealedFile(dir, name string) (SealedRecord, error) {
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return SealedRecord{}, err
	}
	return decodeSealed(name, raw)
}

func decodeSealed(name string, raw []byte) (SealedRecord, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var rec SealedRecord
	if err := dec.Decode(&rec); err != nil {
		return SealedRecord{}, fmt.Errorf("changelogfile: decode sealed/%s: %w", name, err)
	}
	if err := checkSealedRef(rec.Ref); err != nil {
		return SealedRecord{}, fmt.Errorf("sealed/%s: %w", name, err)
	}
	if SealedFileName(rec.Ref) != name {
		return SealedRecord{}, fmt.Errorf("%w: sealed/%s holds ref %q", ErrSealedName, name, rec.Ref)
	}
	return rec, nil
}

// EncodeSealed renders a sealed record as its file holds it, for a store that
// is not a directory: the archive's (decision 0262).
func EncodeSealed(rec SealedRecord) (name string, data []byte, err error) {
	if err := checkSealedRef(rec.Ref); err != nil {
		return "", nil, err
	}
	data, err = json.Marshal(rec)
	if err != nil {
		return "", nil, err
	}
	return SealedFileName(rec.Ref), append(data, '\n'), nil
}

// DecodeSealed reads a sealed record from its file's bytes, refusing one that
// is not the record its name says.
func DecodeSealed(name string, data []byte) (SealedRecord, error) {
	return decodeSealed(name, data)
}

// EncodeSealedVersion renders rec as the archive holds it: its bytes and
// their checksum, which SealedObjectName puts in the object's name.
func EncodeSealedVersion(rec SealedRecord) (data []byte, sum [32]byte, err error) {
	if _, data, err = EncodeSealed(rec); err != nil {
		return nil, sum, err
	}
	return data, sha256.Sum256(data), nil
}

// DecodeSealedVersion reads the version of ref a manifest selects, refusing
// bytes that are not the ones sum names or a record that is not ref's.
func DecodeSealedVersion(ref string, sum [32]byte, data []byte) (SealedRecord, error) {
	if sha256.Sum256(data) != sum {
		return SealedRecord{}, fmt.Errorf("%w: the version of %s is not the bytes its name says", ErrSealedName, ref)
	}
	return decodeSealed(SealedFileName(ref), data)
}

// DeleteSealed removes the file under ref. A ref with no file is already
// deleted, so the call succeeds.
func DeleteSealed(repoDir, ref string) error {
	if err := checkSealedRef(ref); err != nil {
		return err
	}
	dir := SealedDir(repoDir)
	if err := os.Remove(filepath.Join(dir, SealedFileName(ref))); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	// The unlink is a directory change and is flushed like the rename: a
	// crash must not resurrect a deleted secret.
	return syncDir(dir)
}
