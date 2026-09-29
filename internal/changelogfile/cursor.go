package changelogfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Position is where a reader of a Log is: the segment it is in, the last seq
// it handed out and how many bytes it has read across every segment so far.
// A long walk reports it, so an operator watching a verify or a rebuild of a
// history with millions of entries sees it move.
type Position struct {
	Segment string
	Seq     int64
	Bytes   int64
}

// segmentReader decodes one segment's lines in order, up to the segment's
// end, skipping the first skip lines without decoding them. It holds the
// file open between calls, which is what lets a paged read of a segment
// cost one read of it.
type segmentReader struct {
	seg  segment
	f    *os.File
	lr   *lineReader
	skip int64
}

func openSegment(dir string, seg segment, skip int64) (*segmentReader, error) {
	f, err := os.Open(filepath.Join(dir, seg.Name))
	if err != nil {
		return nil, err
	}
	return &segmentReader{
		seg: seg, f: f, skip: skip,
		lr: newLineReader(io.NewSectionReader(f, 0, seg.end), MaxLineBytes),
	}, nil
}

// next returns the next entry, or io.EOF at the segment's end. A torn line
// inside the segment's end is damage, because open bounded the end at the
// last complete transaction.
func (r *segmentReader) next() (Entry, error) {
	line, start, err := r.nextLine()
	if err != nil {
		return Entry{}, err
	}
	e, _, err := Decode(line)
	if err != nil {
		return Entry{}, fmt.Errorf("changelogfile: %s: line at byte %d: %w", r.seg.Name, start, err)
	}
	return e, nil
}

// nextLine is next without the decode: the line's bytes, valid until the
// next call, and the offset it starts at.
func (r *segmentReader) nextLine() ([]byte, int64, error) {
	for {
		line, start, complete, err := r.lr.next()
		if errors.Is(err, io.EOF) {
			return nil, start, io.EOF
		}
		if err != nil {
			return nil, start, fmt.Errorf("changelogfile: %s: line at byte %d: %w", r.seg.Name, start, err)
		}
		if !complete {
			return nil, start, fmt.Errorf("%w: %s: torn line at byte %d", ErrSegmentDigest, r.seg.Name, start)
		}
		if r.skip > 0 {
			r.skip--
			continue
		}
		return line, start, nil
	}
}

// off is how many bytes of the segment have been read.
func (r *segmentReader) off() int64 { return r.lr.off }

func (r *segmentReader) close() error { return r.f.Close() }

// Cursor reads a Log forward from one seq, a page at a time, and keeps the
// segment it is in open between pages. A caller that pages through a history
// with Read opens the segment again on every page and skips to the page from
// byte 0, so a 256 MB segment read in pages of 500 is read hundreds of times
// over; a Cursor reads each segment once. Every line's checksum is verified
// and every seq must continue the one before it. Close it when done.
type Cursor struct {
	l *Log
	// idx is the segment r reads, or the next one to open.
	idx int
	r   *segmentReader
	// expected is the seq the next line must carry.
	expected int64
	// before is the size of every segment the cursor has finished with.
	before int64
	pos    Position
}

// Cursor positions a reader just after seq after: its first entry is after+1.
func (l *Log) Cursor(after int64) *Cursor {
	if after < 0 {
		after = 0
	}
	// The segment holding seq after+1: the last one whose first seq is at
	// or below it.
	idx := sort.Search(len(l.segments), func(i int) bool { return l.segments[i].First > after+1 }) - 1
	if idx < 0 {
		idx = 0
	}
	return &Cursor{l: l, idx: idx, expected: after + 1, pos: Position{Seq: after}}
}

// Next returns the next entries in order, at most limit of them (every
// remaining one when limit is not positive); none at the head.
func (c *Cursor) Next(limit int) ([]Entry, error) {
	return c.read(func(n int, _ Entry) bool { return limit <= 0 || n < limit })
}

// Until returns the next entries in order, through seq inclusive; none when
// the cursor is already past it.
func (c *Cursor) Until(seq int64) ([]Entry, error) {
	if seq < c.expected {
		return nil, nil
	}
	return c.read(func(_ int, e Entry) bool { return e.Seq < seq })
}

// Position is where the cursor is: the segment it last read from, the last
// seq it returned and the bytes read so far.
func (c *Cursor) Position() Position {
	pos := c.pos
	pos.Bytes = c.before
	if c.r != nil {
		pos.Bytes += c.r.off()
	}
	return pos
}

// Close releases the open segment. A closed cursor reads nothing more.
func (c *Cursor) Close() error {
	// Closed whether or not a segment is open: before the first read, and
	// between two segments, there is none, and the cursor must still stop.
	c.expected = c.l.head + 1
	if c.r == nil {
		return nil
	}
	err := c.r.close()
	c.r = nil
	return err
}

// read collects entries while more says so, given how many are collected and
// the last one, and stops at the head.
func (c *Cursor) read(more func(n int, e Entry) bool) ([]Entry, error) {
	var out []Entry
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
				return out, err
			}
			c.r, c.pos.Segment = r, seg.Name
		}
		e, err := c.r.next()
		if errors.Is(err, io.EOF) {
			c.before += c.r.off()
			_ = c.r.close()
			c.r = nil
			c.idx++
			continue
		}
		if err != nil {
			return out, err
		}
		if e.Seq != c.expected {
			return out, fmt.Errorf("%w: %s has seq %d, want %d", ErrSeqGap, c.r.seg.Name, e.Seq, c.expected)
		}
		out = append(out, e)
		c.expected++
		c.pos.Seq = e.Seq
		if !more(len(out), e) {
			break
		}
	}
	return out, nil
}
