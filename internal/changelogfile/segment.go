package changelogfile

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// A segment is named by the seq of its first line, zero-padded to
// segmentDigits so lexical order is seq order. A finished segment has a
// sidecar `<name>.sha256` holding the lowercase hex SHA-256 of the whole file
// and a newline; the highest segment without one is the active segment.
const (
	segmentSuffix = ".ndjson"
	sidecarSuffix = ".sha256"
	segmentDigits = 15
)

var (
	// ErrSegmentName is returned when a `.ndjson` file in the changelog
	// directory is not named by a 15-digit seq.
	ErrSegmentName = errors.New("changelogfile: segment file is not named by a 15-digit seq")
	// ErrOrphanSidecar is returned when a `.sha256` sidecar has no segment.
	ErrOrphanSidecar = errors.New("changelogfile: sidecar has no segment")
	// ErrSegmentDigest is returned when a finished segment's bytes do not hash
	// to the digest its sidecar holds, or the sidecar is not a digest. The
	// error names the segment file.
	ErrSegmentDigest = errors.New("changelogfile: finished segment does not match its sidecar")
	// ErrSegmentOrder is returned when the segments are not contiguous: the
	// first segment does not start at seq 1, or a segment's first seq is not
	// the previous segment's last seq plus one.
	ErrSegmentOrder = errors.New("changelogfile: segments are not contiguous")
	// ErrSegmentUnfinished is returned when a segment below the highest has no
	// sidecar: only the highest segment may be active.
	ErrSegmentUnfinished = errors.New("changelogfile: a segment below the highest has no sidecar")
	// ErrSegmentEmpty is returned when a finished segment holds no line. The
	// writer never finishes an empty segment, so one is damage.
	ErrSegmentEmpty = errors.New("changelogfile: finished segment holds no entries")
)

// Segment is one changelog file as Segments lists it.
type Segment struct {
	// Name is the file name, `<first seq, 15 digits>.ndjson`.
	Name string
	// First is the seq of the segment's first line.
	First int64
	// Finished is whether the segment has a sidecar and so never changes.
	Finished bool
	// Size is the file's length in bytes.
	Size int64
}

// SegmentName is the file name of the segment whose first line is seq first.
func SegmentName(first int64) string {
	return fmt.Sprintf("%0*d%s", segmentDigits, first, segmentSuffix)
}

// sidecarName is the name of the sidecar that finishes segment name.
func sidecarName(name string) string { return name + sidecarSuffix }

// parseSegmentName recovers the first seq from a segment file name. Exactly
// 15 digits, first seq at least 1: a name that does not round-trip through
// SegmentName is refused, so two spellings can never name one segment.
func parseSegmentName(name string) (int64, bool) {
	stem, ok := strings.CutSuffix(name, segmentSuffix)
	if !ok || len(stem) != segmentDigits {
		return 0, false
	}
	first, err := strconv.ParseInt(stem, 10, 64)
	if err != nil || first < 1 || SegmentName(first) != name {
		return 0, false
	}
	return first, true
}

// Segments lists the segments in a changelog directory in seq order. A
// missing directory lists as empty. Files that are neither a segment nor a
// sidecar are ignored, as is anything half-written; a `.ndjson` whose name
// does not parse and a sidecar with no segment are refused.
func Segments(dir string) ([]Segment, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	byName := map[string]*Segment{}
	var sidecars []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, tmpPrefix) {
			continue
		}
		switch {
		case strings.HasSuffix(name, segmentSuffix+sidecarSuffix):
			sidecars = append(sidecars, strings.TrimSuffix(name, sidecarSuffix))
		case strings.HasSuffix(name, segmentSuffix):
			first, ok := parseSegmentName(name)
			if !ok {
				return nil, fmt.Errorf("%w: %s", ErrSegmentName, name)
			}
			info, err := e.Info()
			if err != nil {
				return nil, err
			}
			byName[name] = &Segment{Name: name, First: first, Size: info.Size()}
		}
	}
	for _, name := range sidecars {
		seg, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrOrphanSidecar, sidecarName(name))
		}
		seg.Finished = true
	}
	out := make([]Segment, 0, len(byName))
	for _, seg := range byName {
		out = append(out, *seg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].First < out[j].First })
	return out, nil
}

// digest is what one pass over a segment file yields: the hex SHA-256 of
// every byte, the number of newlines, and the last byte (0 for an empty
// file), so a finished segment's sidecar, line count and torn tail are all
// checked from a single read.
type digest struct {
	hex   string
	lines int64
	last  byte
}

// fileDigest hashes the whole file at path.
func fileDigest(path string) (digest, error) {
	f, err := os.Open(path)
	if err != nil {
		return digest{}, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	var d digest
	r := bufio.NewReaderSize(f, 1<<20)
	buf := make([]byte, 1<<20)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			h.Write(chunk)
			for _, c := range chunk {
				if c == '\n' {
					d.lines++
				}
			}
			d.last = chunk[n-1]
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return digest{}, err
		}
	}
	d.hex = hex.EncodeToString(h.Sum(nil))
	return d, nil
}

// readSidecar returns the digest a sidecar claims for segment name. A sidecar
// that is not one line of 64 lowercase hex digits is reported as a digest
// mismatch: nothing can match it.
func readSidecar(dir, name string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, sidecarName(name)))
	if err != nil {
		return "", err
	}
	want := strings.TrimRight(string(raw), "\r\n")
	if len(want) != sha256.Size*2 {
		return "", fmt.Errorf("%w: %s: sidecar is not a sha256 digest", ErrSegmentDigest, name)
	}
	if _, err := hex.DecodeString(want); err != nil || want != strings.ToLower(want) {
		return "", fmt.Errorf("%w: %s: sidecar is not a sha256 digest", ErrSegmentDigest, name)
	}
	return want, nil
}

// writeSidecar finishes segment name with the digest hexDigest.
func writeSidecar(dir, name, hexDigest string) error {
	return writeFileAtomic(dir, sidecarName(name), []byte(hexDigest+"\n"))
}
