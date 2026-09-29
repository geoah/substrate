package changelogfile

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	// ErrLogStale is returned by Log.Writer when the directory no longer
	// matches the Log's snapshot: another writer appended, rotated or repaired
	// between Open and Writer. The caller opens the directory again.
	ErrLogStale = errors.New("changelogfile: the changelog directory changed since it was opened")
)

// segment is a listed Segment plus what Open learned by reading it.
type segment struct {
	Segment
	// last is the seq of the segment's last line, First-1 when it has none.
	last int64
	// end is how many bytes of the file hold complete transactions. It is the
	// size, except on an active segment with an incomplete tail that
	// OpenReadOnly left in place.
	end int64
	// digest is the hex SHA-256 a finished segment was checked against, so a
	// later open handed this Log (OpenOptions.Verified) can hold the sidecar
	// to it without reading the segment.
	digest string
}

// Log is a changelog directory as Open found it: a snapshot. Head and the
// segment index are fixed at Open, so lines a Writer appends afterwards are
// not read until the directory is opened again.
type Log struct {
	dir      string
	segments []segment
	head     int64
	// TruncatedBytes is the length of the incomplete tail on the active
	// segment: every byte after the last line that ends a transaction, which
	// is a torn last line and, before it, the complete lines of the same
	// unfinished transaction. A crash between the writer's write and its
	// fsync leaves such a tail. Open cut it from the file; OpenReadOnly and
	// Verify only counted it.
	TruncatedBytes int64
	// TruncatedEntries is how many complete lines the tail held: the entries
	// of the unfinished transaction, which never became history.
	TruncatedEntries int64
	// repaired records that the incomplete tail, if any, was cut: only such
	// a Log may back a Writer, because an append after a torn line would
	// glue two half-lines into one unreadable one, and an append after a
	// half transaction would make it whole history.
	repaired bool
}

// Open reads a changelog directory and checks it: every finished segment
// hashes to its sidecar, the segments are contiguous from seq 1, and every
// line of the active segment decodes with a verified checksum, a gapless seq
// and a `txn` that fits the transaction around it. The one damage it repairs
// is an incomplete final transaction on the active segment: a torn last line
// and any complete lines of the same transaction before it, which it
// truncates away together and reports in TruncatedBytes and
// TruncatedEntries. The head is then the last seq of the last complete
// transaction, so a caller that holds the whole transaction elsewhere (the
// `changelog` table) appends it again from there, and one that does not has
// lost exactly the transaction that never finished. Any other damage is a
// named error, and nothing is changed. A missing directory opens as an empty
// log with head 0.
//
// The truncation is a write, so it is done under the directory's writer lock
// and refused with ErrLocked while another process holds it: a tail that
// looks incomplete to a second process may be one the live writer is still
// writing.
func Open(dir string) (*Log, error) { return OpenWith(dir, OpenOptions{}) }

// OpenReadOnly reads and checks a changelog directory exactly as Open does
// but changes nothing: an incomplete tail is counted in TruncatedBytes and
// TruncatedEntries and left in place, and Read and Walk stop before it. It is
// for readers that must not write, an operator's verify or an inspection of a
// directory another process may be appending to. A Log opened this way cannot
// back a Writer.
func OpenReadOnly(dir string) (*Log, error) { return OpenWith(dir, OpenOptions{ReadOnly: true}) }

// OpenOptions tunes OpenWith.
type OpenOptions struct {
	// ReadOnly opens as OpenReadOnly does: an incomplete tail is counted and
	// left in place, and the Log cannot back a Writer.
	ReadOnly bool
	// Verified is a Log this process opened earlier over the same
	// directory. A finished segment it holds as finished, under the same
	// name and size, is taken as checked without reading it again: a
	// finished segment never changes, so the digest that Log checked is
	// still its digest, and the sidecar is held to that digest instead.
	// Every other finished segment is digested and the active segment is
	// always scanned, so what Verified does not vouch for is checked as
	// Open checks it. A Log over another directory vouches for nothing.
	Verified *Log
	// Progress, when not nil, is called after each segment is checked, in
	// seq order.
	Progress func(OpenProgress)
}

// OpenProgress is where an open is in its check of a directory. Digesting
// every finished segment of a long history takes minutes, and the caller
// reports it so whoever waits on the open sees it move.
type OpenProgress struct {
	// Segment is the segment just checked.
	Segment string
	// Finished is whether it is a finished segment, checked against its
	// sidecar, rather than the active one, scanned line by line.
	Finished bool
	// Reused is whether OpenOptions.Verified vouched for it, so none of its
	// bytes were read.
	Reused bool
	// Segments and Bytes are how many segments, and how many of their
	// bytes, are checked so far, this one included; the totals are the
	// directory's.
	Segments      int
	TotalSegments int
	Bytes         int64
	TotalBytes    int64
}

// OpenWith is Open, or OpenReadOnly when opts.ReadOnly is set, with the
// options' reuse of an earlier check and progress reports.
func OpenWith(dir string, opts OpenOptions) (*Log, error) {
	list, err := Segments(dir)
	if err != nil {
		return nil, err
	}
	var totalBytes int64
	for _, s := range list {
		totalBytes += s.Size
	}
	vouched := opts.Verified.finishedByName(dir)
	l := &Log{dir: dir, repaired: !opts.ReadOnly}
	var prevLast, checkedBytes int64
	for i, s := range list {
		if s.First != prevLast+1 {
			return nil, fmt.Errorf("%w: %s starts at seq %d, the previous segment ends at %d", ErrSegmentOrder, s.Name, s.First, prevLast)
		}
		seg := segment{Segment: s, end: s.Size}
		path := filepath.Join(dir, s.Name)
		reused := false
		if s.Finished {
			if known, ok := vouched[s.Name]; ok && known.Size == s.Size && known.First == s.First {
				want, err := readSidecar(dir, s.Name)
				if err != nil {
					return nil, err
				}
				if want != known.digest {
					return nil, fmt.Errorf("%w: %s", ErrSegmentDigest, s.Name)
				}
				seg.last, seg.digest, reused = known.last, known.digest, true
			} else if seg, err = checkFinished(dir, seg); err != nil {
				return nil, err
			}
		} else {
			if i != len(list)-1 {
				return nil, fmt.Errorf("%w: %s", ErrSegmentUnfinished, s.Name)
			}
			last, end, cut, err := scanActive(path, s.Name, s.First)
			if err != nil {
				return nil, err
			}
			seg.last, seg.end = last, end
			if end < s.Size {
				l.TruncatedBytes, l.TruncatedEntries = s.Size-end, cut
				if !opts.ReadOnly {
					if err := truncateLocked(dir, path, end); err != nil {
						return nil, fmt.Errorf("changelogfile: %s: cut incomplete tail: %w", s.Name, err)
					}
					seg.Size = end
				}
			}
		}
		prevLast = seg.last
		l.segments = append(l.segments, seg)
		checkedBytes += s.Size
		if opts.Progress != nil {
			opts.Progress(OpenProgress{
				Segment: s.Name, Finished: s.Finished, Reused: reused,
				Segments: i + 1, TotalSegments: len(list),
				Bytes: checkedBytes, TotalBytes: totalBytes,
			})
		}
	}
	l.head = prevLast
	return l, nil
}

// checkFinished digests a finished segment and holds it to its sidecar: the
// same bytes, at least one line, and a final newline. It returns the segment
// with its last seq and its digest.
func checkFinished(dir string, seg segment) (segment, error) {
	d, err := fileDigest(filepath.Join(dir, seg.Name))
	if err != nil {
		return seg, err
	}
	want, err := readSidecar(dir, seg.Name)
	if err != nil {
		return seg, err
	}
	if d.hex != want {
		return seg, fmt.Errorf("%w: %s", ErrSegmentDigest, seg.Name)
	}
	if d.lines == 0 {
		return seg, fmt.Errorf("%w: %s", ErrSegmentEmpty, seg.Name)
	}
	if d.last != '\n' {
		return seg, fmt.Errorf("%w: %s: torn final line", ErrSegmentDigest, seg.Name)
	}
	seg.last, seg.digest = seg.First+d.lines-1, d.hex
	return seg, nil
}

// finishedByName is the finished segments a Log checked, by name, for an
// open of dir it is handed as OpenOptions.Verified; none for a nil Log or one
// over another directory.
func (l *Log) finishedByName(dir string) map[string]segment {
	if l == nil || filepath.Clean(l.dir) != filepath.Clean(dir) {
		return nil
	}
	out := make(map[string]segment, len(l.segments))
	for _, seg := range l.segments {
		if seg.Finished && seg.digest != "" {
			out[seg.Name] = seg
		}
	}
	return out
}

// scanActive decodes every complete line of the active segment, checking
// checksums, that seqs run gaplessly from first and that each line's `txn`
// fits the transaction around it. It returns the last seq of the last
// complete transaction, the offset just past that transaction's last newline,
// and how many complete lines lie after it: those lines and the torn line
// after them, if any, are the incomplete tail.
func scanActive(path, name string, first int64) (last, end, cut int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, 0, err
	}
	defer func() { _ = f.Close() }()
	lr := newLineReader(f, MaxLineBytes)
	expected := first
	last = first - 1
	var frame txnFrame
	for {
		line, start, complete, err := lr.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, 0, 0, fmt.Errorf("changelogfile: %s: line at byte %d: %w", name, start, err)
		}
		if !complete {
			// The torn line: the only one a crash leaves half written, and
			// the only one not held to the checksum.
			break
		}
		e, _, err := Decode(line)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("changelogfile: %s: line at byte %d: %w", name, start, err)
		}
		if e.Seq != expected {
			return 0, 0, 0, fmt.Errorf("%w: %s: line at byte %d has seq %d, want %d", ErrSeqGap, name, start, e.Seq, expected)
		}
		ends, err := frame.next(e.Seq, e.Txn)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("%s: line at byte %d: %w", name, start, err)
		}
		expected++
		if ends {
			last, end, cut = e.Seq, lr.off, 0
		} else {
			cut++
		}
	}
	return last, end, cut, nil
}

// truncateLocked cuts path to size bytes under the directory's writer lock, so
// a live writer's active segment is never cut from under it.
func truncateLocked(dir, path string, size int64) error {
	lock, err := lockDir(dir)
	if err != nil {
		return err
	}
	defer func() { _ = lock.release() }()
	return truncateFile(path, size)
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

// unchanged reports whether the directory still lists exactly the segments
// this Log was opened over, by name, state and size. It is Writer's check that
// nothing appended, rotated or repaired between Open and the lock.
func (l *Log) unchanged() (bool, error) {
	now, err := Segments(l.dir)
	if err != nil {
		return false, err
	}
	if len(now) != len(l.segments) {
		return false, nil
	}
	for i, s := range now {
		was := l.segments[i].Segment
		if s.Name != was.Name || s.Finished != was.Finished || s.Size != was.Size {
			return false, nil
		}
	}
	return true, nil
}

// Read returns the entries with seq greater than after, in order, at most
// limit of them (every one when limit is not positive). It opens only the
// segments that hold the range and skips lines below the range without
// decoding them, which a segment's gapless seqs make sound; every returned
// line's checksum is verified. It is one page: a caller that pages through
// the whole log holds a Cursor instead, which reads each segment once.
func (l *Log) Read(after int64, limit int) ([]Entry, error) {
	c := l.Cursor(after)
	defer func() { _ = c.Close() }()
	return c.Next(limit)
}

// Walk streams every entry from seq 1 to the head, verifying each line's
// checksum, the seq sequence and the transaction framing (every line fits the
// transaction before it, and no transaction crosses a segment), and stops at
// the first error fn returns.
func (l *Log) Walk(fn func(Entry) error) error {
	return l.walk(func(e Entry, _ Position) error { return fn(e) })
}

// walk is Walk with the reader's position beside each entry, for a caller
// that reports progress.
func (l *Log) walk(fn func(Entry, Position) error) error {
	var expected int64 = 1
	var frame txnFrame
	var before int64
	for _, seg := range l.segments {
		err := l.scanSegment(seg, 0, func(e Entry, off int64) (bool, error) {
			if e.Seq != expected {
				return false, fmt.Errorf("%w: %s has seq %d, want %d", ErrSeqGap, seg.Name, e.Seq, expected)
			}
			if _, err := frame.next(e.Seq, e.Txn); err != nil {
				return false, fmt.Errorf("%s: %w", seg.Name, err)
			}
			expected++
			return true, fn(e, Position{Segment: seg.Name, Seq: e.Seq, Bytes: before + off})
		})
		if err != nil {
			return err
		}
		if frame.open != 0 {
			return fmt.Errorf("%w: %s ends inside the transaction ending at seq %d", ErrTxnFraming, seg.Name, frame.open)
		}
		before += seg.end
	}
	return nil
}

// scanSegment reads one segment up to its end, skips the first skip lines
// without decoding them, and hands every following entry to fn, with the
// offset just past its line, until fn returns false.
func (l *Log) scanSegment(seg segment, skip int64, fn func(Entry, int64) (bool, error)) error {
	r, err := openSegment(l.dir, seg, skip)
	if err != nil {
		return err
	}
	defer func() { _ = r.close() }()
	for {
		e, err := r.next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		more, err := fn(e, r.off())
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
	// TruncatedBytes and TruncatedEntries are the incomplete tail on the
	// active segment, left in place: Verify changes nothing. See
	// Log.TruncatedBytes.
	TruncatedBytes   int64
	TruncatedEntries int64
}

// Verify checks a changelog directory the way Open does and then walks every
// line, verifying each checksum, without changing the directory. The error is
// the first one met; the report holds the counts up to it.
func Verify(dir string) (Report, error) {
	l, err := OpenReadOnly(dir)
	if err != nil {
		return Report{}, err
	}
	return l.Verify(nil)
}

// Verify walks every line of an opened Log, verifying each checksum, the seq
// sequence and the transaction framing, and calls progress, when it is not
// nil, with the position after each entry. A caller that already holds the
// Log verifies it here rather than through the package's Verify, which opens
// the directory again and digests every finished segment a second time.
func (l *Log) Verify(progress func(Position)) (Report, error) {
	r := Report{Segments: len(l.segments), Head: l.head, TruncatedBytes: l.TruncatedBytes, TruncatedEntries: l.TruncatedEntries}
	err := l.walk(func(_ Entry, pos Position) error {
		r.Entries++
		if progress != nil {
			progress(pos)
		}
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
