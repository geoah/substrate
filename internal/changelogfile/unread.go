package changelogfile

// A finished segment never changes, and digesting every one of a long
// history is most of what an open of it costs: 6.5 minutes of a 16 GB
// repository's boot, and an hour of a snapshot of it (issues 761 and 825).
// So an open can take a finished segment without hashing its bytes: on its
// own sidecar's word (OpenOptions.TrustSidecars), which leaves the digest
// owed to Log.DigestUnread, or on a digest the caller vouches for
// (OpenOptions.Known), which a snapshot's base was given when it was written.
// Either way the open reads the segment's first and last lines: they hold the
// name to the seq the segment starts at, give the seq it ends at for the
// contiguity check, carry checksums of their own, and catch a segment cut
// short or torn. A damaged byte between them is what only the digest finds.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// KnownSegment is a finished segment as a caller vouches for it: its length
// and the digest its bytes were checked against elsewhere. An open handed
// one (OpenOptions.Known) holds the segment's size and sidecar to it and
// reads only the segment's first and last lines.
type KnownSegment struct {
	Size   int64
	Digest string
}

// endLineWindow is the first read from the end of a segment looking for the
// start of its last line; a longer line widens the read until it is found.
const endLineWindow = 64 << 10

// checkEndLines reads a finished segment's first and last lines: each verifies
// against its own sum, the first is the seq the name says, and the last ends
// a transaction (none crosses a segment) and ends in the file's final
// newline. It returns the segment with its last seq and that line's sum. A
// segment of one line reads that line twice.
func checkEndLines(dir string, seg segment, lc *lineChecker) (segment, error) {
	if seg.Size == 0 {
		return seg, fmt.Errorf("%w: %s", ErrSegmentEmpty, seg.Name)
	}
	f, err := os.Open(filepath.Join(dir, seg.Name))
	if err != nil {
		return seg, err
	}
	defer func() { _ = f.Close() }()

	lr := newLineReader(io.NewSectionReader(f, 0, seg.Size), MaxLineBytes)
	line, _, complete, err := lr.next()
	if err != nil {
		return seg, fmt.Errorf("changelogfile: %s: the first line: %w", seg.Name, err)
	}
	if !complete {
		return seg, fmt.Errorf("%w: %s: torn final line", ErrSegmentDigest, seg.Name)
	}
	seq, _, _, err := lc.check(line)
	if err != nil {
		return seg, fmt.Errorf("changelogfile: %s: the first line: %w", seg.Name, err)
	}
	if seq != seg.First {
		return seg, fmt.Errorf("%w: %s has seq %d, want %d", ErrSeqGap, seg.Name, seq, seg.First)
	}

	last, err := lastLine(f, seg.Size)
	if err != nil {
		return seg, fmt.Errorf("changelogfile: %s: %w", seg.Name, err)
	}
	if last == nil {
		return seg, fmt.Errorf("%w: %s: torn final line", ErrSegmentDigest, seg.Name)
	}
	seq, txn, sum, err := lc.check(last)
	if err != nil {
		return seg, fmt.Errorf("changelogfile: %s: the last line: %w", seg.Name, err)
	}
	if seq < seg.First {
		return seg, fmt.Errorf("%w: %s ends at seq %d, before its first", ErrSeqGap, seg.Name, seq)
	}
	if err := checkTxn(seq, txn); err != nil {
		return seg, fmt.Errorf("%s: %w", seg.Name, err)
	}
	if !endsTransaction(seq, txn) {
		return seg, fmt.Errorf("%w: %s ends inside the transaction ending at seq %d", ErrTxnFraming, seg.Name, txn)
	}
	seg.last, seg.lastSum = seq, sum
	return seg, nil
}

// lastLine returns the last line of a file of size bytes, without its
// newline, or nil when the file does not end in one. It reads backwards from
// the end, widening the window until the newline before the line is found or
// the line is the file's first, and refuses a line longer than MaxLineBytes
// as the forward reader does.
func lastLine(f *os.File, size int64) ([]byte, error) {
	var tail [1]byte
	if _, err := f.ReadAt(tail[:], size-1); err != nil {
		return nil, err
	}
	if tail[0] != '\n' {
		return nil, nil
	}
	end := size - 1
	for window := int64(endLineWindow); ; window *= 2 {
		start := max(end-window, 0)
		buf := make([]byte, end-start)
		if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		for i := len(buf) - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				return buf[i+1:], nil
			}
		}
		if start == 0 {
			return buf, nil
		}
		if end-start > MaxLineBytes {
			return nil, ErrLineTooLong
		}
	}
}

// Unread is how many finished segments the open took on their sidecars'
// word (OpenOptions.TrustSidecars) and so owes a digest: what DigestUnread
// reads.
func (l *Log) Unread() int {
	n := 0
	for _, seg := range l.segments {
		if seg.unread {
			n++
		}
	}
	return n
}

// DigestUnread hashes every finished segment the open took on its sidecar's
// word and holds it to that sidecar as Open would have: the same digest, as
// many lines as its seqs span, and a final newline. It reads one segment at a
// time, so it can run beside the writer without starving it, stops at the
// first segment that does not match with ErrSegmentDigest naming it, and
// stops with ctx's error once ctx is done. progress, when not nil, is called
// after each segment. The Log is not changed: a segment it took on its
// sidecar's word stays so for any later open handed it as Verified.
func (l *Log) DigestUnread(ctx context.Context, progress func(OpenProgress)) error {
	var total, done int64
	pending := make([]segment, 0, len(l.segments))
	for _, seg := range l.segments {
		if seg.unread {
			pending = append(pending, seg)
			total += seg.Size
		}
	}
	for i, seg := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		d, err := fileDigestContext(ctx, filepath.Join(l.dir, seg.Name))
		if err != nil {
			return err
		}
		switch {
		case d.hex != seg.digest:
			return fmt.Errorf("%w: %s", ErrSegmentDigest, seg.Name)
		case d.last != '\n':
			return fmt.Errorf("%w: %s: torn final line", ErrSegmentDigest, seg.Name)
		case d.lines != seg.last-seg.First+1:
			return fmt.Errorf("%w: %s holds %d lines, its seqs span %d", ErrSegmentDigest, seg.Name, d.lines, seg.last-seg.First+1)
		}
		done += seg.Size
		if progress != nil {
			progress(OpenProgress{
				Segment: seg.Name, Finished: true, Digested: true,
				Segments: i + 1, TotalSegments: len(pending),
				Bytes: done, TotalBytes: total,
			})
		}
	}
	return nil
}

// KnownHead is the last seq, and that line's sum, of the run of segments
// from seq 1 the open took on a digest the caller vouched for
// (OpenOptions.Known): 0 when the first segment is not one. A caller that
// compares the lines with something else (a verify's table pass) starts
// after it.
func (l *Log) KnownHead() (int64, [32]byte) {
	var head int64
	var sum [32]byte
	for _, seg := range l.segments {
		if !seg.known {
			break
		}
		head, sum = seg.last, seg.lastSum
	}
	return head, sum
}

// fileDigestContext is fileDigest, stopping with ctx's error once ctx is
// done.
func fileDigestContext(ctx context.Context, path string) (digest, error) {
	f, err := os.Open(path)
	if err != nil {
		return digest{}, err
	}
	defer func() { _ = f.Close() }()
	d := newDigester()
	if _, err := io.CopyBuffer(d, ctxReader{ctx: ctx, r: f}, make([]byte, 1<<20)); err != nil {
		return digest{}, err
	}
	return d.digest(), nil
}

// ctxReader is a reader that fails with its context's error once the
// context is done.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r ctxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// SharedFinished is the run of finished segments from seq 1 that the
// changelog directory dir holds exactly as the changelog directory base
// does: the same name, the same size and the same sidecar digest, read from
// the two sidecars and no segment byte. A base segment counts only when the
// base's next segment starts at or before head+1, so it ends at or before
// head: a snapshot's base vouches for the history up to the head its
// snapshot.json records and for nothing written past it. The run stops at
// the first segment that differs, that base does not hold finished, or that
// is base's last. A snapshot hands the run to OpenOptions.Known and
// VerifyOptions.Known: base was checked when it was written, and a segment
// with the digest base's was checked against holds the bytes base holds.
func SharedFinished(dir, base string, head int64) (map[string]KnownSegment, error) {
	ours, err := Segments(dir)
	if err != nil {
		return nil, err
	}
	theirs, err := Segments(base)
	if err != nil {
		return nil, err
	}
	out := map[string]KnownSegment{}
	for i, s := range ours {
		if i+1 >= len(theirs) {
			break
		}
		b, next := theirs[i], theirs[i+1]
		if !s.Finished || !b.Finished || b.Name != s.Name || b.Size != s.Size || next.First > head+1 {
			break
		}
		want, err := readSidecar(dir, s.Name)
		if err != nil {
			return nil, err
		}
		got, err := readSidecar(base, s.Name)
		if err != nil {
			return nil, err
		}
		if got != want {
			break
		}
		out[s.Name] = KnownSegment{Size: s.Size, Digest: want}
	}
	return out, nil
}
