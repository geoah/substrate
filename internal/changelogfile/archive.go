package changelogfile

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// THE ARCHIVE'S NAMES (decision 0279).
//
// A repository's archive in a store is one or more ARCHIVES, each one life
// of its copy under an id of its own that sorts by when it was made:
//
//	repositories/<repository id>/<archive id>/changelog/<dialect>/<piece>
//	repositories/<repository id>/<archive id>/sealed/<sealed object>
//	repositories/<repository id>/<archive id>/blobs/<digest>.<pass>
//	repositories/<repository id>/<archive id>/spool/<spool file>.<pass>
//	repositories/<repository id>/manifests/<archive id>/<epoch>-<hash>.json
//	repositories/<repository id>/manifests/<archive id>/deleted
//
// Every object's name carries the id of the pass that put it, and a
// manifest's its epoch and the hash of its bytes, so no name ever holds two
// contents: a put applied late writes bytes no other put writes under that
// name, and a delete applied late removes nothing a later pass put. The
// manifests and the deletion marks of every archive sit under one prefix,
// apart from the objects, so a pass lists them without listing every blob.

// DeletedMark is the name, beside an archive's manifests, of the mark a
// repository's deletion writes before it removes anything: an archive that
// carries it is never restored automatically.
const DeletedMark = "deleted"

var (
	reArchiveID = regexp.MustCompile(`^a[0-9a-f]{16}[0-9a-f]{8}$`)
	rePassID    = regexp.MustCompile(`^[0-9a-f]{16}$`)
	reManifest  = regexp.MustCompile(`^([0-9]{20})-([0-9a-f]{64})\.json$`)
	rePieceObj  = regexp.MustCompile(`^([0-9]{15})-([0-9]{15})-([0-9a-f]{16})-([0-9a-f]{16})\.ndjson$`)
)

// NewArchiveID mints an archive id: `a`, the time in nanoseconds as 16 hex
// digits, and 8 random hex digits. Its time is past every id in after, so an
// archive started on a clock that stepped back still sorts after the
// archives it follows.
func NewArchiveID(now time.Time, after ...string) (string, error) {
	t := uint64(now.UnixNano())
	for _, id := range after {
		if at, ok := archiveIDTime(id); ok && at >= t {
			t = at + 1
		}
	}
	var r [4]byte
	if _, err := rand.Read(r[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("a%016x%s", t, hex.EncodeToString(r[:])), nil
}

// archiveIDTime is the time an archive id carries.
func archiveIDTime(id string) (uint64, bool) {
	if !ValidArchiveID(id) {
		return 0, false
	}
	t, err := strconv.ParseUint(id[1:17], 16, 64)
	return t, err == nil
}

// ValidArchiveID reports whether s is an archive id.
func ValidArchiveID(s string) bool { return reArchiveID.MatchString(s) }

// NewPassID mints the id of one archive pass: 16 random hex digits.
func NewPassID() (string, error) {
	var r [8]byte
	if _, err := rand.Read(r[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(r[:]), nil
}

// ValidPassID reports whether s is a pass id.
func ValidPassID(s string) bool { return rePassID.MatchString(s) }

// RepositoryPrefix is the prefix of everything a store holds of one
// repository.
func RepositoryPrefix(repository string) string {
	return RepositoriesDir + "/" + repository + "/"
}

// ArchivePrefix is the prefix of one archive's objects.
func ArchivePrefix(repository, archive string) string {
	return RepositoryPrefix(repository) + archive + "/"
}

// ArchiveManifestsPrefix is the prefix of one archive's manifests and mark.
func ArchiveManifestsPrefix(repository, archive string) string {
	return RepositoryPrefix(repository) + ManifestsSubdir + "/" + archive + "/"
}

// ManifestsPrefix is the prefix of every archive's manifests and marks of
// one repository.
func ManifestsPrefix(repository string) string {
	return RepositoryPrefix(repository) + ManifestsSubdir + "/"
}

// ArchiveManifestName is the name, beside its archive's other manifests, of a
// manifest of epoch whose bytes are data: the epoch in 20 digits, so names
// sort as the epochs do, and the hash of the bytes.
func ArchiveManifestName(epoch int64, data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%020d-%s.json", epoch, hex.EncodeToString(sum[:]))
}

// ParseArchiveManifestName reads the epoch and the hash a manifest's name
// carries, or reports false for a name that is not one.
func ParseArchiveManifestName(name string) (epoch int64, hash string, ok bool) {
	m := reManifest.FindStringSubmatch(name)
	if m == nil {
		return 0, "", false
	}
	epoch, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || epoch < 1 {
		return 0, "", false
	}
	return epoch, m[2], true
}

// ManifestNamesBytes reports whether name is the name of a manifest whose
// bytes are data.
func ManifestNamesBytes(name string, data []byte) bool {
	epoch, _, ok := ParseArchiveManifestName(name)
	return ok && ArchiveManifestName(epoch, data) == name
}

// ArchiveKey is a key a store holds of a repository, read apart: a manifest
// or a mark (Manifests set, Name the file under the archive's manifests), or
// one of an archive's objects (Name its path under the archive).
type ArchiveKey struct {
	Repository, Archive, Name string
	Manifests                 bool
}

// ParseArchiveKey reads a key under RepositoriesDir, or reports false for
// one that is not an archive's.
func ParseArchiveKey(key string) (ArchiveKey, bool) {
	rest, ok := strings.CutPrefix(key, RepositoriesDir+"/")
	if !ok {
		return ArchiveKey{}, false
	}
	repository, rest, ok := strings.Cut(rest, "/")
	if !ok {
		return ArchiveKey{}, false
	}
	first, rest, ok := strings.Cut(rest, "/")
	if !ok || rest == "" {
		return ArchiveKey{}, false
	}
	if first == ManifestsSubdir {
		archive, name, ok := strings.Cut(rest, "/")
		if !ok || !ValidArchiveID(archive) || name == "" || strings.Contains(name, "/") {
			return ArchiveKey{}, false
		}
		return ArchiveKey{Repository: repository, Archive: archive, Name: name, Manifests: true}, true
	}
	if !ValidArchiveID(first) {
		return ArchiveKey{}, false
	}
	return ArchiveKey{Repository: repository, Archive: first, Name: rest}, true
}

// PieceObjectName is the name, under an archive, of the piece holding seqs
// first to last in dialect, put by pass, whose bytes hash to sum: the seqs
// in fifteen digits each, the pass, and the first 16 hex digits of the hash.
func PieceObjectName(dialect int, first, last int64, pass string, sum [32]byte) string {
	return fmt.Sprintf("%s/%d/%0*d-%0*d-%s-%s.ndjson", ChangelogSubdir, dialect,
		pieceDigits, first, pieceDigits, last, pass, hex.EncodeToString(sum[:8]))
}

// ParsePieceObjectName reads a piece's name under an archive.
func ParsePieceObjectName(name string) (dialect int, first, last int64, pass string, ok bool) {
	rest, ok := strings.CutPrefix(name, ChangelogSubdir+"/")
	if !ok {
		return 0, 0, 0, "", false
	}
	d, file, ok := strings.Cut(rest, "/")
	if !ok {
		return 0, 0, 0, "", false
	}
	dialect, err := strconv.Atoi(d)
	if err != nil || dialect < 0 || strconv.Itoa(dialect) != d {
		return 0, 0, 0, "", false
	}
	m := rePieceObj.FindStringSubmatch(file)
	if m == nil {
		return 0, 0, 0, "", false
	}
	first, err1 := strconv.ParseInt(m[1], 10, 64)
	last, err2 := strconv.ParseInt(m[2], 10, 64)
	if err1 != nil || err2 != nil || first < 1 || last < first {
		return 0, 0, 0, "", false
	}
	return dialect, first, last, m[3], true
}

// BlobObjectName is the name, under an archive, of a blob's bytes as pass
// put them. A blob's digest names its bytes; the pass makes the name one no
// other put writes.
func BlobObjectName(digest, pass string) string {
	return BlobsSubdir + "/" + digest + "." + pass
}

// ParseBlobObjectName reads a blob object's name under an archive.
func ParseBlobObjectName(name string) (digest, pass string, ok bool) {
	rest, ok := strings.CutPrefix(name, BlobsSubdir+"/")
	if !ok {
		return "", "", false
	}
	i := strings.LastIndexByte(rest, '.')
	if i <= 0 || strings.Contains(rest, "/") || !ValidPassID(rest[i+1:]) {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

// SealedObjectName is the name, under an archive, of one version of ref's
// sealed value as pass put it: its file name with the version's checksum and
// the pass before the extension.
func SealedObjectName(ref string, sum [32]byte, pass string) string {
	return SealedSubdir + "/" + strings.TrimSuffix(SealedFileName(ref), sealedSuffix) + "." + hex.EncodeToString(sum[:]) + "." + pass + sealedSuffix
}

// ParseSealedObjectName reads the checksum and the pass a sealed object's
// name carries, and holds the name to the ref it is selected for.
func ParseSealedObjectName(ref, name string) (sum [32]byte, pass string, ok bool) {
	stem := SealedSubdir + "/" + strings.TrimSuffix(SealedFileName(ref), sealedSuffix) + "."
	rest, ok := strings.CutPrefix(name, stem)
	if !ok {
		return sum, "", false
	}
	rest, ok = strings.CutSuffix(rest, sealedSuffix)
	if !ok {
		return sum, "", false
	}
	hexSum, pass, ok := strings.Cut(rest, ".")
	if !ok || len(hexSum) != 64 || !ValidPassID(pass) {
		return sum, "", false
	}
	if _, err := hex.Decode(sum[:], []byte(hexSum)); err != nil {
		return sum, "", false
	}
	return sum, pass, true
}

// SpoolObjectName is the name, under an archive, of a spooled request's file
// as pass put it.
func SpoolObjectName(file, pass string) string {
	return SpoolSubdir + "/" + file + "." + pass
}

// ParseSpoolObjectName reads the spool file a spool object holds.
func ParseSpoolObjectName(name string) (file, pass string, ok bool) {
	rest, ok := strings.CutPrefix(name, SpoolSubdir+"/")
	if !ok {
		return "", "", false
	}
	i := strings.LastIndexByte(rest, '.')
	if i <= 0 || strings.Contains(rest, "/") || !ValidPassID(rest[i+1:]) {
		return "", "", false
	}
	if _, _, err := ParseSpoolFileName(rest[:i]); err != nil {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}
