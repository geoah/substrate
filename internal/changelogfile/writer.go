package changelogfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultSegmentBytes is the segment size the writer rotates at when the
// options name none: 256 MiB, the SUBSTRATE_CHANGELOG_SEGMENT_BYTES default.
const DefaultSegmentBytes int64 = 256 << 20

// ErrWriterClosed is returned by Append after Close.
var ErrWriterClosed = errors.New("changelogfile: writer is closed")

// WriterOptions tunes a Writer.
type WriterOptions struct {
	// SegmentBytes is the size at or past which the active segment is
	// finished after an append; DefaultSegmentBytes when not positive.
	SegmentBytes int64
}

// Writer appends entries to a repository's changelog directory. It is not safe
// for concurrent use: the engine serializes appends per repository already
// (one writer's total order, decision 0017), and a second writer on one
// directory would interleave lines.
//
// After an I/O error the writer refuses every later Append with that error.
// Reopen it: OpenWriter rescans the directory and cuts the torn tail the
// failed write may have left.
type Writer struct {
	dir          string
	segmentBytes int64
	head         int64
	truncated    int64
	// file is the active segment, nil when none is open: a fresh directory,
	// or right after a rotation. The next Append creates the segment named
	// head+1, so an empty segment never exists on disk.
	file   *os.File
	name   string
	size   int64
	failed error
	closed bool
}

// OpenWriter opens the changelog directory for appending, creating the
// directory when it is missing. It runs Open first, so the directory is
// checked and a torn tail is cut before the first append, and the head is
// the last verified seq.
func OpenWriter(dir string, opts WriterOptions) (*Writer, error) {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("changelogfile: create the changelog directory: %w", err)
	}
	l, err := Open(dir)
	if err != nil {
		return nil, err
	}
	w := &Writer{dir: dir, segmentBytes: opts.SegmentBytes, head: l.head, truncated: l.TruncatedBytes}
	if w.segmentBytes <= 0 {
		w.segmentBytes = DefaultSegmentBytes
	}
	if n := len(l.segments); n > 0 && !l.segments[n-1].Finished {
		active := l.segments[n-1]
		f, err := os.OpenFile(filepath.Join(dir, active.Name), os.O_WRONLY|os.O_APPEND, fileMode)
		if err != nil {
			return nil, err
		}
		w.file, w.name, w.size = f, active.Name, active.end
	}
	return w, nil
}

// Head is the seq of the last entry written or found, 0 for an empty log.
func (w *Writer) Head() int64 { return w.head }

// TruncatedBytes is the torn tail OpenWriter cut from the active segment, 0
// when it found none.
func (w *Writer) TruncatedBytes() int64 { return w.truncated }

// Append encodes the entries and writes them as lines in one write, then
// fsyncs. The first entry's seq must be Head()+1 and each following one must
// add one, or ErrSeqGap is returned and nothing is written. After a
// successful append at or past SegmentBytes the active segment is finished:
// its sidecar is written and the next Append starts a new segment.
func (w *Writer) Append(entries []Entry) error {
	if w.closed {
		return ErrWriterClosed
	}
	if w.failed != nil {
		return w.failed
	}
	if len(entries) == 0 {
		return nil
	}
	var buf bytes.Buffer
	next := w.head + 1
	for _, e := range entries {
		if e.Seq != next {
			return fmt.Errorf("%w: got seq %d, want %d (the head is %d)", ErrSeqGap, e.Seq, next, w.head)
		}
		line, _, err := Encode(e)
		if err != nil {
			return err
		}
		if len(line) > MaxLineBytes {
			return fmt.Errorf("%w: seq %d is %d bytes", ErrLineTooLong, e.Seq, len(line))
		}
		buf.Write(line)
		buf.WriteByte('\n')
		next++
	}
	created := false
	if w.file == nil {
		name := SegmentName(w.head + 1)
		f, err := os.OpenFile(filepath.Join(w.dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
		if err != nil {
			return fmt.Errorf("changelogfile: create segment: %w", err)
		}
		w.file, w.name, w.size = f, name, 0
		created = true
	}
	if _, err := w.file.Write(buf.Bytes()); err != nil {
		return w.fail(created, fmt.Errorf("changelogfile: write %s: %w", w.name, err))
	}
	if err := w.file.Sync(); err != nil {
		return w.fail(created, fmt.Errorf("changelogfile: fsync %s: %w", w.name, err))
	}
	if created {
		if err := syncDir(w.dir); err != nil {
			return w.fail(false, fmt.Errorf("changelogfile: fsync directory: %w", err))
		}
	}
	w.head = next - 1
	w.size += int64(buf.Len())
	if w.size >= w.segmentBytes {
		return w.rotate()
	}
	return nil
}

// fail records an I/O error, rolls the active file back to the bytes that
// were durable before the append (removing it outright when the append
// created it, so no empty segment stays behind), and closes it.
func (w *Writer) fail(created bool, err error) error {
	w.failed = err
	if w.file != nil {
		if created {
			_ = w.file.Close()
			_ = os.Remove(filepath.Join(w.dir, w.name))
		} else {
			_ = w.file.Truncate(w.size)
			_ = w.file.Sync()
			_ = w.file.Close()
		}
		w.file = nil
	}
	return err
}

// rotate finishes the active segment: closes it (Append already fsynced it),
// hashes the whole file and writes the sidecar atomically. The next segment is
// created by the next Append.
func (w *Writer) rotate() error {
	if err := w.file.Close(); err != nil {
		return w.fail(false, fmt.Errorf("changelogfile: close %s: %w", w.name, err))
	}
	w.file = nil
	d, err := fileDigest(filepath.Join(w.dir, w.name))
	if err != nil {
		return w.fail(false, err)
	}
	if err := writeSidecar(w.dir, w.name, d.hex); err != nil {
		return w.fail(false, fmt.Errorf("changelogfile: write sidecar for %s: %w", w.name, err))
	}
	w.name, w.size = "", 0
	return nil
}

// Close releases the active segment. It is idempotent; the segment stays
// active, the next OpenWriter appends to it.
func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
