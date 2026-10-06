package changelogfile

// The whole check of a changelog directory: every finished segment against
// its sidecar and every line against its sum, with the seqs gapless and every
// transaction whole. On a history of millions of lines it was hours, one line
// at a time on one goroutine through a decode and an encode of each (issue
// 761). A segment is checked on its own (a transaction never crosses one, and
// its first seq is in its name), so the segments are checked in parallel and
// held to each other in seq order; a finished segment's bytes are hashed for
// its sidecar in the same read that checks its lines; and a line is checked
// by lineChecker, without the round trip.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
)

// Report is what Verify counted before it returned.
type Report struct {
	// Segments is the number of segment files, finished and active.
	Segments int
	// Entries is the number of lines checked: every line on success, the
	// lines of the segments before the first error and of its own segment
	// before it otherwise. The lines of a segment VerifyOptions.Known vouched
	// for are not checked and not counted.
	Entries int64
	// Known is how many finished segments VerifyOptions.Known vouched for,
	// read at their first and last lines alone.
	Known int
	// Head is the seq of the last entry, 0 for an empty log; on an error,
	// the last seq of the segments before the one it is in.
	Head int64
	// TruncatedBytes and TruncatedEntries are the incomplete tail on the
	// active segment, left in place: Verify changes nothing. See
	// Log.TruncatedBytes.
	TruncatedBytes   int64
	TruncatedEntries int64
}

// Verify checks a changelog directory the way OpenReadOnly does and every
// line of it besides (VerifyDir), without changing the directory. The error
// is the first one in seq order; the report holds the counts before it.
func Verify(dir string) (Report, error) {
	_, r, err := VerifyDir(dir, VerifyOptions{})
	return r, err
}

// VerifyOptions tunes VerifyDir.
type VerifyOptions struct {
	// Progress, when not nil, is called after each segment is checked, in
	// seq order, with the segment, its last seq and the bytes checked so
	// far. It is called on the goroutine that called VerifyDir.
	Progress func(Position)
	// Known vouches for finished segments as OpenOptions.Known does: one
	// whose size and sidecar are the Known ones is held to them and read at
	// its first and last lines, not line by line. The verification is then
	// of everything else; a snapshot hands the segments its base holds.
	Known map[string]KnownSegment
}

// VerifyDir checks a changelog directory: every finished segment hashes to
// its sidecar, holds at least one line and ends in a newline; every line's
// checksum verifies; the seqs run gaplessly from 1 across the segments; and
// every line's `txn` fits the transaction around it, with no transaction
// crossing a segment. Each segment is read once, and as many are read at once
// as GOMAXPROCS allows. It changes nothing: an incomplete tail on the active
// segment is counted and left, as OpenReadOnly leaves it.
//
// It returns the Log OpenReadOnly would have opened, so a caller that goes on
// to read the directory (the table comparison of a verify) does not check it
// a second time. The error is the first in seq order, and the Log is nil
// with it.
func VerifyDir(dir string, opts VerifyOptions) (*Log, Report, error) {
	list, err := Segments(dir)
	if err != nil {
		return nil, Report{}, err
	}
	rep := Report{Segments: len(list)}
	l := &Log{dir: dir}
	var prevLast, checked int64
	err = inSegmentOrder(len(list), func(i int, lc *lineChecker) segmentCheck {
		return verifySegment(dir, list, i, lc, opts.Known)
	}, func(i int, c segmentCheck) error {
		s := list[i]
		if s.First != prevLast+1 {
			return fmt.Errorf("%w: %s starts at seq %d, the previous segment ends at %d", ErrSegmentOrder, s.Name, s.First, prevLast)
		}
		rep.Entries += c.lines
		if c.err != nil {
			return c.err
		}
		if c.seg.known {
			rep.Known++
		}
		prevLast = c.seg.last
		checked += s.Size
		if c.seg.end < s.Size {
			l.TruncatedBytes, l.TruncatedEntries = s.Size-c.seg.end, c.cut
		}
		l.segments = append(l.segments, c.seg)
		if opts.Progress != nil {
			opts.Progress(Position{Segment: s.Name, Seq: prevLast, Bytes: checked})
		}
		return nil
	})
	rep.Head = prevLast
	if err != nil {
		return nil, rep, err
	}
	l.head = prevLast
	rep.TruncatedBytes, rep.TruncatedEntries = l.TruncatedBytes, l.TruncatedEntries
	return l, rep, nil
}

// Verify walks every line of an opened Log, verifying each checksum, the seq
// sequence and the transaction framing, as VerifyDir does but without the
// sidecars: open held every finished segment to its own. It reads the
// segments in parallel and calls progress, when it is not nil, after each
// segment in seq order.
func (l *Log) Verify(progress func(Position)) (Report, error) {
	r := Report{Segments: len(l.segments), Head: l.head, TruncatedBytes: l.TruncatedBytes, TruncatedEntries: l.TruncatedEntries}
	var checked int64
	err := inSegmentOrder(len(l.segments), func(i int, lc *lineChecker) segmentCheck {
		seg := l.segments[i]
		n, err := l.checkSegmentLines(seg, lc)
		return segmentCheck{seg: seg, lines: n, err: err}
	}, func(i int, c segmentCheck) error {
		r.Entries += c.lines
		if c.err != nil {
			return c.err
		}
		checked += c.seg.end
		if progress != nil {
			progress(Position{Segment: c.seg.Name, Seq: c.seg.last, Bytes: checked})
		}
		return nil
	})
	return r, err
}

// checkSegmentLines checks the lines of one segment of an opened Log, up to
// its end, and that they are the seqs the Log's index gives it.
func (l *Log) checkSegmentLines(seg segment, lc *lineChecker) (int64, error) {
	f, err := os.Open(filepath.Join(l.dir, seg.Name))
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	n, err := checkLines(newLineReader(io.NewSectionReader(f, 0, seg.end), MaxLineBytes), seg.Name, seg.First, lc)
	if err != nil {
		return n, err
	}
	if want := seg.last - seg.First + 1; n != want {
		return n, fmt.Errorf("%w: %s holds %d lines, the open counted %d", ErrSeqGap, seg.Name, n, want)
	}
	return n, nil
}

// segmentCheck is what checking one segment found.
type segmentCheck struct {
	// seg is the segment as a Log indexes it: its last seq, its end and,
	// when finished, its digest.
	seg segment
	// lines is how many lines were checked before err, or all of them.
	lines int64
	// cut is how many complete lines the active segment's incomplete tail
	// holds (Log.TruncatedEntries).
	cut int64
	// reused is an open's: OpenOptions.Verified vouched for the segment.
	reused bool
	err    error
}

// verifySegment checks segment i of a listing in one read, or a finished
// one known vouches for at its first and last lines.
func verifySegment(dir string, list []Segment, i int, lc *lineChecker, known map[string]KnownSegment) segmentCheck {
	s := list[i]
	seg := segment{Segment: s, end: s.Size}
	path := filepath.Join(dir, s.Name)
	if k, ok := known[s.Name]; ok && s.Finished {
		seg, err := checkKnown(dir, seg, k, lc)
		return segmentCheck{seg: seg, err: err}
	}
	if !s.Finished {
		if i != len(list)-1 {
			return segmentCheck{seg: seg, err: fmt.Errorf("%w: %s", ErrSegmentUnfinished, s.Name)}
		}
		last, end, cut, err := scanActive(path, s.Name, s.First)
		if err != nil {
			return segmentCheck{seg: seg, err: err}
		}
		seg.last, seg.end = last, end
		return segmentCheck{seg: seg, lines: last - s.First + 1, cut: cut}
	}
	f, err := os.Open(path)
	if err != nil {
		return segmentCheck{seg: seg, err: err}
	}
	defer func() { _ = f.Close() }()
	d := newDigester()
	tee := io.TeeReader(f, d)
	n, lineErr := checkLines(newLineReader(tee, MaxLineBytes), s.Name, s.First, lc)
	// The bytes after a line that stopped the walk still go to the digest:
	// a segment that does not match its sidecar says so before any line
	// does, as open says it.
	if _, err := io.Copy(io.Discard, tee); err != nil {
		return segmentCheck{seg: seg, lines: n, err: err}
	}
	seg, err = holdToSidecar(dir, seg, d.digest())
	if err != nil {
		return segmentCheck{seg: seg, err: err}
	}
	return segmentCheck{seg: seg, lines: n, err: lineErr}
}

// checkLines checks every line lr yields as a line of the segment name whose
// first seq is first: its checksum, its seq, and its `txn` against the
// transaction around it, and that the last transaction ends in the segment.
// It returns how many lines it checked before the first error.
func checkLines(lr *lineReader, name string, first int64, lc *lineChecker) (int64, error) {
	expected := first
	var frame txnFrame
	var n int64
	for {
		line, start, complete, err := lr.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return n, fmt.Errorf("changelogfile: %s: line at byte %d: %w", name, start, err)
		}
		if !complete {
			return n, fmt.Errorf("%w: %s: torn line at byte %d", ErrSegmentDigest, name, start)
		}
		seq, txn, _, err := lc.check(line)
		if err != nil {
			return n, fmt.Errorf("changelogfile: %s: line at byte %d: %w", name, start, err)
		}
		if seq != expected {
			return n, fmt.Errorf("%w: %s has seq %d, want %d", ErrSeqGap, name, seq, expected)
		}
		if _, err := frame.next(seq, txn); err != nil {
			return n, fmt.Errorf("%s: %w", name, err)
		}
		expected++
		n++
	}
	if frame.open != 0 {
		return n, fmt.Errorf("%w: %s ends inside the transaction ending at seq %d", ErrTxnFraming, name, frame.open)
	}
	return n, nil
}

// inSegmentOrder runs check over segments 0..n-1 on up to GOMAXPROCS
// goroutines, each with a lineChecker of its own, and hands every result to
// each on the calling goroutine in index order. The first error each returns
// stops it: no segment after it is started, the ones already running finish,
// and the error is returned.
func inSegmentOrder(n int, check func(i int, lc *lineChecker) segmentCheck, each func(i int, c segmentCheck) error) error {
	if n == 0 {
		return nil
	}
	results := make([]segmentCheck, n)
	done := make([]chan struct{}, n)
	for i := range done {
		done[i] = make(chan struct{})
	}
	var next atomic.Int64
	var stopped atomic.Bool
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), n) {
		wg.Go(func() {
			lc := newLineChecker()
			for {
				i := int(next.Add(1) - 1)
				if i >= n {
					return
				}
				if !stopped.Load() {
					results[i] = check(i, lc)
				}
				close(done[i])
			}
		})
	}
	var err error
	for i := range n {
		<-done[i]
		if err = each(i, results[i]); err != nil {
			stopped.Store(true)
			break
		}
	}
	wg.Wait()
	return err
}

// SameAs holds l to src segment for segment: the same names and sizes, the
// same finished state and last seqs, the same digest for every finished
// segment, and the same bytes in the active one. A copy opened with
// OpenReadOnly has had every finished segment hashed against its own
// sidecar, so a copy that is also the same as the Log it was copied from is
// that Log's bytes, which is what a snapshot's read-back asks without
// walking a line again. The active segment has no digest to hold it to, so
// it is compared with the source's file, which must not have moved since
// src was opened.
func (l *Log) SameAs(src *Log) error {
	if len(l.segments) != len(src.segments) {
		return fmt.Errorf("%w: %d segments, the source has %d", ErrNotTheSource, len(l.segments), len(src.segments))
	}
	for i, seg := range l.segments {
		want := src.segments[i]
		switch {
		case seg.Name != want.Name:
			return fmt.Errorf("%w: segment %d is %s, the source's is %s", ErrNotTheSource, i, seg.Name, want.Name)
		case seg.Finished != want.Finished || seg.end != want.end || seg.last != want.last:
			return fmt.Errorf("%w: %s is not the source's segment", ErrNotTheSource, seg.Name)
		case seg.Finished && seg.digest != want.digest:
			return fmt.Errorf("%w: %s does not hash to the source's digest", ErrNotTheSource, seg.Name)
		case !seg.Finished:
			same, err := samePrefix(filepath.Join(l.dir, seg.Name), filepath.Join(src.dir, want.Name), seg.end)
			if err != nil {
				return err
			}
			if !same {
				return fmt.Errorf("%w: %s does not hold the source's bytes", ErrNotTheSource, seg.Name)
			}
		}
	}
	if l.head != src.head {
		return fmt.Errorf("%w: the head is %d, the source's is %d", ErrNotTheSource, l.head, src.head)
	}
	return nil
}

// samePrefix reports whether the first n bytes of the files at a and b are
// the same.
func samePrefix(a, b string, n int64) (bool, error) {
	fa, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer func() { _ = fa.Close() }()
	fb, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer func() { _ = fb.Close() }()
	ra, rb := io.NewSectionReader(fa, 0, n), io.NewSectionReader(fb, 0, n)
	bufA, bufB := make([]byte, 1<<20), make([]byte, 1<<20)
	for {
		na, errA := io.ReadFull(ra, bufA)
		nb, errB := io.ReadFull(rb, bufB)
		if na != nb || !bytes.Equal(bufA[:na], bufB[:nb]) {
			return false, nil
		}
		switch {
		case errors.Is(errA, io.EOF) || errors.Is(errA, io.ErrUnexpectedEOF):
			return errors.Is(errB, io.EOF) || errors.Is(errB, io.ErrUnexpectedEOF), nil
		case errA != nil:
			return false, errA
		case errB != nil:
			return false, errB
		}
	}
}

// ErrNotTheSource is returned by SameAs for a Log that is not the one it is
// held to.
var ErrNotTheSource = errors.New("changelogfile: the changelog is not the source's")

// SumCursor reads the seq and the sum each line of a Log carries, from one
// seq forward, without decoding or hashing the line: for a caller that has
// verified the Log (VerifyDir) and holds what the lines claim against
// something else, the table's stamped hashes. It checks only that the seqs
// continue. Close it when done.
type SumCursor struct {
	l        *Log
	idx      int
	r        *segmentReader
	expected int64
	before   int64
	pos      Position
}

// Sums positions a SumCursor just after seq after: its first line is
// after+1.
func (l *Log) Sums(after int64) *SumCursor {
	after = max(after, 0)
	idx := max(sort.Search(len(l.segments), func(i int) bool { return l.segments[i].First > after+1 })-1, 0)
	return &SumCursor{l: l, idx: idx, expected: after + 1, pos: Position{Seq: after}}
}

// Next returns the next line's seq and sum, or io.EOF past the head.
func (c *SumCursor) Next() (int64, [32]byte, error) {
	for c.expected <= c.l.head {
		if c.r == nil {
			for c.idx < len(c.l.segments) && c.l.segments[c.idx].last < c.expected {
				c.idx++
			}
			if c.idx >= len(c.l.segments) {
				break
			}
			seg := c.l.segments[c.idx]
			r, err := openSegment(c.l.dir, seg, c.expected-seg.First)
			if err != nil {
				return 0, [32]byte{}, err
			}
			c.r, c.pos.Segment = r, seg.Name
		}
		line, start, err := c.r.nextLine()
		if errors.Is(err, io.EOF) {
			c.before += c.r.off()
			_ = c.r.close()
			c.r = nil
			c.idx++
			continue
		}
		if err != nil {
			return 0, [32]byte{}, err
		}
		seq, sum, err := lineSum(line)
		if err != nil {
			return 0, sum, fmt.Errorf("changelogfile: %s: line at byte %d: %w", c.r.seg.Name, start, err)
		}
		if seq != c.expected {
			return 0, sum, fmt.Errorf("%w: %s has seq %d, want %d", ErrSeqGap, c.r.seg.Name, seq, c.expected)
		}
		c.expected++
		c.pos.Seq = seq
		return seq, sum, nil
	}
	return 0, [32]byte{}, io.EOF
}

// Position is where the cursor is, as Cursor.Position says it.
func (c *SumCursor) Position() Position {
	pos := c.pos
	pos.Bytes = c.before
	if c.r != nil {
		pos.Bytes += c.r.off()
	}
	return pos
}

// Close releases the open segment. A closed cursor reads nothing more.
func (c *SumCursor) Close() error {
	c.expected = c.l.head + 1
	if c.r == nil {
		return nil
	}
	err := c.r.close()
	c.r = nil
	return err
}
