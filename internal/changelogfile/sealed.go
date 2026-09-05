package changelogfile

import (
	"bytes"
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
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		var rec SealedRecord
		if err := dec.Decode(&rec); err != nil {
			return nil, fmt.Errorf("changelogfile: decode sealed/%s: %w", name, err)
		}
		if err := checkSealedRef(rec.Ref); err != nil {
			return nil, fmt.Errorf("sealed/%s: %w", name, err)
		}
		if SealedFileName(rec.Ref) != name {
			return nil, fmt.Errorf("%w: sealed/%s holds ref %q", ErrSealedName, name, rec.Ref)
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out, nil
}
