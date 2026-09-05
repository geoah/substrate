package changelogfile

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// MaxLineBytes caps one line. A reader that met a longer one would either
// buffer without bound or split a record in two, so both sides refuse it: the
// writer before it writes, the reader before it decodes.
const MaxLineBytes = 64 << 20

var (
	// ErrLineTooLong is returned for a line longer than MaxLineBytes.
	ErrLineTooLong = errors.New("changelogfile: line exceeds MaxLineBytes")
	// ErrSeqGap is returned when a seq does not continue the one before it:
	// on write, an entry whose seq is not the head plus one (a gap or a
	// repeat); on read, a line whose seq is not the previous line's plus one.
	ErrSeqGap = errors.New("changelogfile: seq does not continue the previous one")
)

// segment is a listed Segment plus what Open learned by reading it.
type segment struct {
	Segment
	// last is the seq of the segment's last line, First-1 when it has none.
	last int64
	// end is how many bytes of the file hold complete lines. It is the size,
	// except on an active segment with a torn tail that Verify left in place.
	end int64
}

// Log is a changelog directory as Open found it: a snapshot. Head and the
// segment index are fixed at Open, so lines a Writer appends afterwards are
// not read until the directory is opened again.
type Log struct {
	dir      string
	segments []segment
	head     int64
	// TruncatedBytes is the length of the torn tail on the active segment:
	// the bytes after its last newline, which a crash mid-append leaves
	// behind. Open cut them from the file; OpenReadOnly and Verify only
	// counted them.
	TruncatedBytes int64
	// repaired records that the torn tail, if any, was cut: only such a Log
	// may back a Writer, because an append after a torn tail would glue two
	// half-lines into one unreadable one.
	repaired bool
}

// Open reads a changelog directory and checks it: every finished segment
// hashes to its sidecar, the segments are contiguous from seq 1, and every
// line of the active segment decodes with a verified checksum and a gapless
// seq. The one damage it repairs is a torn final line on the active segment,
// which it truncates away and reports in TruncatedBytes. Any other damage is
// a named error, and nothing is changed. A missing directory opens as an
// empty log with head 0.
func Open(dir string) (*Log, error) { return open(dir, true) }

// OpenReadOnly reads and checks a changelog directory exactly as Open does
// but changes nothing: a torn tail is counted in TruncatedBytes and left in
// place, and Read and Walk stop before it. It is for readers that must not
// write, an operator's verify or an inspection of a directory another process
// may be appending to. A Log opened this way cannot back a Writer.
func OpenReadOnly(dir string) (*Log, error) { return open(dir, false) }

func open(dir string, repair bool) (*Log, error) {
	list, err := Segments(dir)
	if err != nil {
		return nil, err
	}
	l := &Log{dir: dir, repaired: repair}
	var prevLast int64
	for i, s := range list {
		if s.First != prevLast+1 {
			return nil, fmt.Errorf("%w: %s starts at seq %d, the previous segment ends at %d", ErrSegmentOrder, s.Name, s.First, prevLast)
		}
		seg := segment{Segment: s, end: s.Size}
		path := filepath.Join(dir, s.Name)
		if s.Finished {
			d, err := fileDigest(path)
			if err != nil {
				return nil, err
			}
			want, err := readSidecar(dir, s.Name)
			if err != nil {
				return nil, err
			}
			if d.hex != want {
				return nil, fmt.Errorf("%w: %s", ErrSegmentDigest, s.Name)
			}
			if d.lines == 0 {
				return nil, fmt.Errorf("%w: %s", ErrSegmentEmpty, s.Name)
			}
			if d.last != '\n' {
				return nil, fmt.Errorf("%w: %s: torn final line", ErrSegmentDigest, s.Name)
			}
			seg.last = s.First + d.lines - 1
		} else {
			if i != len(list)-1 {
				return nil, fmt.Errorf("%w: %s", ErrSegmentUnfinished, s.Name)
			}
			last, end, err := scanActive(path, s.Name, s.First)
			if err != nil {
				return nil, err
			}
			seg.last, seg.end = last, end
			if end < s.Size {
				l.TruncatedBytes = s.Size - end
				if repair {
					if err := truncateFile(path, end); err != nil {
						return nil, fmt.Errorf("changelogfile: %s: cut torn tail: %w", s.Name, err)
					}
					seg.Size = end
				}
			}
		}
		prevLast = seg.last
		l.segments = append(l.segments, seg)
	}
	l.head = prevLast
	return l, nil
}

// scanActive decodes every complete line of the active segment, checking
// checksums and that seqs run gaplessly from first. It returns the last seq
// and the offset just past the last newline: bytes after it are a torn tail.
func scanActive(path, name string, first int64) (last, end int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = f.Close() }()
	lr := newLineReader(f, MaxLineBytes)
	expected := first
	for {
		line, start, complete, err := lr.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, 0, fmt.Errorf("changelogfile: %s: line at byte %d: %w", name, start, err)
		}
		if !complete {
			// The torn tail: the only line a crash leaves behind, and the
			// only one not held to the checksum.
			break
		}
		e, _, err := Decode(line)
		if err != nil {
			return 0, 0, fmt.Errorf("changelogfile: %s: line at byte %d: %w", name, start, err)
		}
		if e.Seq != expected {
			return 0, 0, fmt.Errorf("%w: %s: line at byte %d has seq %d, want %d", ErrSeqGap, name, start, e.Seq, expected)
		}
		expected++
		end = lr.off
	}
	return expected - 1, end, nil
}

// truncateFile cuts path to size bytes and fsyncs it.
func truncateFile(path string, size int64) error {
	f, err := os.OpenFile(path, os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := f.Truncate(size); err != nil {
		return err
	}
	return f.Sync()
}

// Head is the seq of the last entry, 0 for an empty log.
func (l *Log) Head() int64 { return l.head }

// Read returns the entries with seq greater than after, in order, at most
// limit of them (every one when limit is not positive). It opens only the
// segments that hold the range and skips lines below the range without
// decoding them, which a segment's gapless seqs make sound; every returned
// line's checksum is verified.
func (l *Log) Read(after int64, limit int) ([]Entry, error) {
	if after < 0 {
		after = 0
	}
	if after >= l.head {
		return nil, nil
	}
	// The segment holding seq after+1: the last one whose first seq is at
	// or below it.
	idx := sort.Search(len(l.segments), func(i int) bool { return l.segments[i].First > after+1 }) - 1
	var out []Entry
	expected := after + 1
	for i := idx; i < len(l.segments); i++ {
		seg := l.segments[i]
		if seg.last < expected {
			continue
		}
		skip := expected - seg.First
		err := l.scanSegment(seg, skip, func(e Entry) (bool, error) {
			if e.Seq != expected {
				return false, fmt.Errorf("%w: %s has seq %d, want %d", ErrSeqGap, seg.Name, e.Seq, expected)
			}
			out = append(out, e)
			expected++
			return expected <= l.head && (limit <= 0 || len(out) < limit), nil
		})
		if err != nil {
			return nil, err
		}
		if expected > l.head || (limit > 0 && len(out) >= limit) {
			break
		}
	}
	return out, nil
}

// Walk streams every entry from seq 1 to the head, verifying each line's
// checksum and the seq sequence, and stops at the first error fn returns.
func (l *Log) Walk(fn func(Entry) error) error {
	var expected int64 = 1
	for _, seg := range l.segments {
		err := l.scanSegment(seg, 0, func(e Entry) (bool, error) {
			if e.Seq != expected {
				return false, fmt.Errorf("%w: %s has seq %d, want %d", ErrSeqGap, seg.Name, e.Seq, expected)
			}
			expected++
			return true, fn(e)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// scanSegment reads one segment up to its end, skips the first skip lines
// without decoding them, and hands every following entry to fn until fn
// returns false.
func (l *Log) scanSegment(seg segment, skip int64, fn func(Entry) (bool, error)) error {
	f, err := os.Open(filepath.Join(l.dir, seg.Name))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	lr := newLineReader(io.NewSectionReader(f, 0, seg.end), MaxLineBytes)
	for {
		line, start, complete, err := lr.next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("changelogfile: %s: line at byte %d: %w", seg.Name, start, err)
		}
		if !complete {
			return fmt.Errorf("%w: %s: torn line at byte %d", ErrSegmentDigest, seg.Name, start)
		}
		if skip > 0 {
			skip--
			continue
		}
		e, _, err := Decode(line)
		if err != nil {
			return fmt.Errorf("changelogfile: %s: line at byte %d: %w", seg.Name, start, err)
		}
		more, err := fn(e)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
}

// Report is what Verify counted before it returned.
type Report struct {
	// Segments is the number of segment files, finished and active.
	Segments int
	// Entries is the number of lines Verify decoded and checked.
	Entries int64
	// Head is the seq of the last entry, 0 for an empty log.
	Head int64
	// TruncatedBytes is the length of a torn tail on the active segment, left
	// in place: Verify changes nothing.
	TruncatedBytes int64
}

// Verify checks a changelog directory the way Open does and then walks every
// line, verifying each checksum, without changing the directory. The error is
// the first one met; the report holds the counts up to it.
func Verify(dir string) (Report, error) {
	l, err := open(dir, false)
	if err != nil {
		return Report{}, err
	}
	r := Report{Segments: len(l.segments), Head: l.head, TruncatedBytes: l.TruncatedBytes}
	err = l.Walk(func(Entry) error {
		r.Entries++
		return nil
	})
	return r, err
}

// lineReader yields newline-terminated lines from a stream, tracking the byte
// offset of each so an error can name where it was found. It holds one line
// at a time, never the file.
type lineReader struct {
	r   *bufio.Reader
	off int64
	max int64
}

func newLineReader(r io.Reader, maxLine int64) *lineReader {
	return &lineReader{r: bufio.NewReaderSize(r, 64<<10), max: maxLine}
}

// next returns the next line without its newline and the offset it started
// at. complete is false for a final line that has no newline (a torn tail);
// io.EOF is returned when nothing remains.
func (lr *lineReader) next() (line []byte, start int64, complete bool, err error) {
	start = lr.off
	var acc []byte
	for {
		chunk, err := lr.r.ReadSlice('\n')
		acc = append(acc, chunk...)
		lr.off += int64(len(chunk))
		if int64(len(acc)) > lr.max+1 {
			return nil, start, false, ErrLineTooLong
		}
		switch {
		case err == nil:
			return acc[:len(acc)-1], start, true, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if len(acc) == 0 {
				return nil, start, false, io.EOF
			}
			return acc, start, false, nil
		default:
			return nil, start, false, err
		}
	}
}
