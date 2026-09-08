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

// ErrTxnIncomplete is returned by Append and AppendLines for a batch whose
// last line does not end its transaction. One append is one write and one
// fsync, and the segment rotates only after it, so a batch of whole
// transactions is what keeps a transaction inside one write and one segment;
// the writer refuses to start what it cannot finish.
var ErrTxnIncomplete = errors.New("changelogfile: append does not end at a transaction boundary")

// ErrTxnPending is returned by Prepare, Append and AppendLines while a
// prepared transaction awaits its Commit or Abort: the segment's tail is that
// transaction's bytes, and a second one written after them would be read as
// part of it.
var ErrTxnPending = errors.New("changelogfile: a prepared transaction is pending")

// ErrNoTxnPending is returned by Commit and Abort when nothing is prepared.
var ErrNoTxnPending = errors.New("changelogfile: no prepared transaction")

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
// A Writer holds the directory's exclusive advisory lock (LockFileName) from
// Log.Writer until Close, so a second process that opens a writer on the same
// directory gets ErrLocked rather than a chance to interleave.
//
// After an I/O error the writer refuses every later Append with that error.
// Reopen it: OpenWriter rescans the directory and cuts the torn tail the
// failed write may have left.
type Writer struct {
	dir          string
	segmentBytes int64
	head         int64
	truncated    int64
	lock         *dirLock
	// file is the active segment, nil when none is open: a fresh directory,
	// or right after a rotation. The next Append creates the segment named
	// head+1, so the writer never creates an empty segment; the one empty
	// segment that can exist is an active one Open cut back to nothing (an
	// unfinished transaction that was its whole content), which the next
	// Append fills.
	file   *os.File
	name   string
	size   int64
	failed error
	closed bool
	// pending is the transaction Prepare wrote and Commit has not ended: its
	// bytes are on disk, its final newline is not. head and size describe the
	// segment without it until Commit.
	pending *prepared
}

// prepared is what Commit needs to end a Prepare: the head the transaction
// brings the changelog to, the bytes it adds with its final newline, and whether
// Prepare created the segment (so Abort removes it rather than cutting it).
type prepared struct {
	head    int64
	bytes   int64
	created bool
}

// Line is one entry as Encode rendered it, without the trailing newline, for
// AppendLines: a caller that encoded the entry to stamp its checksum hands the
// same bytes to the file rather than encoding twice. Txn is the entry's Txn,
// so the writer holds the batch to transaction boundaries without decoding
// the bytes.
type Line struct {
	Seq   int64
	Txn   int64
	Bytes []byte
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
	return l.Writer(opts)
}

// ErrLogNotRepaired is returned by Log.Writer for a Log that OpenReadOnly
// produced: its torn tail, if any, is still on disk, and appending after it
// would corrupt the segment.
var ErrLogNotRepaired = errors.New("changelogfile: a read-only log cannot back a writer")

// Writer opens a Writer over a Log that Open produced, positioned at the
// Log's head, so a caller that has already scanned the directory (to compare
// its tail with the table, say) does not scan it a second time. It takes the
// directory's writer lock first and then checks that the directory still
// lists what the Log saw: a snapshot another writer has moved past is refused
// with ErrLogStale, because a writer opened at a stale head would write seqs
// the file already holds. The Log is a snapshot: after the first Append it no
// longer describes the directory.
func (l *Log) Writer(opts WriterOptions) (*Writer, error) {
	if !l.repaired {
		return nil, ErrLogNotRepaired
	}
	if err := os.MkdirAll(l.dir, dirMode); err != nil {
		return nil, fmt.Errorf("changelogfile: create the changelog directory: %w", err)
	}
	lock, err := lockDir(l.dir)
	if err != nil {
		return nil, err
	}
	same, err := l.unchanged()
	if err != nil {
		_ = lock.release()
		return nil, err
	}
	if !same {
		_ = lock.release()
		return nil, fmt.Errorf("%w: %s", ErrLogStale, l.dir)
	}
	w := &Writer{dir: l.dir, segmentBytes: opts.SegmentBytes, head: l.head, truncated: l.TruncatedBytes, lock: lock}
	if w.segmentBytes <= 0 {
		w.segmentBytes = DefaultSegmentBytes
	}
	if n := len(l.segments); n > 0 && !l.segments[n-1].Finished {
		active := l.segments[n-1]
		f, err := os.OpenFile(filepath.Join(l.dir, active.Name), os.O_WRONLY|os.O_APPEND, fileMode)
		if err != nil {
			_ = lock.release()
			return nil, err
		}
		w.file, w.name, w.size = f, active.Name, active.end
	}
	return w, nil
}

// Head is the seq of the last entry written or found, 0 for an empty log.
func (w *Writer) Head() int64 { return w.head }

// Active is the segment the writer holds open, by name, with the bytes of it
// that are committed history: the file's length while no transaction is
// prepared, and the length before the prepared bytes while one is. ok is
// false when no segment is open: a fresh directory, or the moment after a
// rotation and before the next Prepare creates one. The export pins its point
// through this under the engine's writer mutex, where no prepare is in
// flight, so the size it takes ends at a transaction boundary.
func (w *Writer) Active() (name string, size int64, ok bool) {
	if w.file == nil {
		return "", 0, false
	}
	return w.name, w.size, true
}

// Err is the writer's standing refusal: the I/O error that failed it, or
// ErrWriterClosed, or nil while it still appends. A failed writer stays
// failed until the process reopens the directory, so a caller that would
// retry against it asks for a restart instead.
func (w *Writer) Err() error { return w.ready() }

// TruncatedBytes is the torn tail OpenWriter cut from the active segment, 0
// when it found none.
func (w *Writer) TruncatedBytes() int64 { return w.truncated }

// Append encodes the entries and writes them as lines in one write, then
// fsyncs. The first entry's seq must be Head()+1 and each following one must
// add one, or ErrSeqGap is returned and nothing is written. The entries must
// be whole transactions: each line continues the transaction before it or
// starts one (ErrTxnFraming), and the last one ends its transaction
// (ErrTxnIncomplete). After a successful append at or past SegmentBytes the
// active segment is finished: its sidecar is written and the next Append
// starts a new segment. A transaction therefore never crosses a segment,
// however large it is.
func (w *Writer) Append(entries []Entry) error {
	if err := w.ready(); err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	lines := make([]Line, 0, len(entries))
	for _, e := range entries {
		line, _, err := Encode(e)
		if err != nil {
			return err
		}
		lines = append(lines, Line{Seq: e.Seq, Txn: e.Txn, Bytes: line})
	}
	return w.AppendLines(lines)
}

// AppendLines is Append for entries the caller has already encoded with
// Encode. The same checks apply: gapless seqs from Head()+1, no line over
// MaxLineBytes, whole transactions, nothing written when any is refused. It
// is Prepare followed by Commit, so two fsyncs: a caller with no commit point
// of its own between them (the boot's table-to-file catch-up) pays for a
// step it does not use.
func (w *Writer) AppendLines(lines []Line) error {
	if len(lines) == 0 {
		return w.ready()
	}
	if err := w.Prepare(lines); err != nil {
		return err
	}
	return w.Commit()
}

// Prepare writes the lines as AppendLines would, every byte but the last
// line's newline, and fsyncs. On disk the transaction is then an incomplete
// tail: Open cuts it whole and OpenReadOnly stops before it (0057), so
// nothing reads it as history until Commit writes the newline, and Head does
// not move until then. A caller that commits elsewhere between the two (the
// engine's Postgres transaction) has the bytes durable before its commit and
// a file that claims nothing that commit did not make true. The lines are
// checked as AppendLines checks them, and an I/O failure rolls the segment
// back and refuses every later call, as an append's does.
func (w *Writer) Prepare(lines []Line) error {
	if err := w.ready(); err != nil {
		return err
	}
	if w.pending != nil {
		return ErrTxnPending
	}
	if len(lines) == 0 {
		return errors.New("changelogfile: prepare of no lines")
	}
	var buf bytes.Buffer
	next := w.head + 1
	var frame txnFrame
	for i, l := range lines {
		if l.Seq != next {
			return fmt.Errorf("%w: got seq %d, want %d (the head is %d)", ErrSeqGap, l.Seq, next, w.head)
		}
		if len(l.Bytes) > MaxLineBytes {
			return fmt.Errorf("%w: seq %d is %d bytes", ErrLineTooLong, l.Seq, len(l.Bytes))
		}
		if _, err := frame.next(l.Seq, l.Txn); err != nil {
			return err
		}
		buf.Write(l.Bytes)
		if i < len(lines)-1 {
			buf.WriteByte('\n')
		}
		next++
	}
	if frame.open != 0 {
		return fmt.Errorf("%w: the last line is seq %d and its transaction ends at %d", ErrTxnIncomplete, next-1, frame.open)
	}
	created := false
	if w.file == nil {
		// O_APPEND as Writer opens an existing segment: after Abort cuts the
		// file, the next write must land at the new end and not at the old
		// offset with a hole of zeros before it.
		name := SegmentName(w.head + 1)
		f, err := os.OpenFile(filepath.Join(w.dir, name), os.O_WRONLY|os.O_APPEND|os.O_CREATE|os.O_EXCL, fileMode)
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
	w.pending = &prepared{head: next - 1, bytes: int64(buf.Len()) + 1, created: created}
	return nil
}

// Commit writes the newline that ends the prepared transaction and fsyncs;
// from that byte on the transaction is history. It then advances the head
// and, at or past SegmentBytes, finishes the segment. A failure rolls the
// segment back to the bytes before Prepare and refuses every later call: the
// transaction is not in the file, and a caller that committed it elsewhere
// appends it again from there at the next open.
func (w *Writer) Commit() error {
	if err := w.ready(); err != nil {
		return err
	}
	p := w.pending
	if p == nil {
		return ErrNoTxnPending
	}
	if _, err := w.file.Write([]byte{'\n'}); err != nil {
		return w.fail(p.created, fmt.Errorf("changelogfile: write %s: %w", w.name, err))
	}
	if err := w.file.Sync(); err != nil {
		return w.fail(p.created, fmt.Errorf("changelogfile: fsync %s: %w", w.name, err))
	}
	w.pending = nil
	w.head = p.head
	w.size += p.bytes
	if w.size >= w.segmentBytes {
		return w.rotate()
	}
	return nil
}

// Abort cuts the prepared transaction's bytes back off the segment and
// fsyncs, removing the segment when Prepare created it, so the file holds
// what it held before Prepare. The writer stays usable: the caller's commit
// failed for a reason of its own, not the disk's. A failure to cut is an I/O
// failure and is treated as one.
func (w *Writer) Abort() error {
	if err := w.ready(); err != nil {
		return err
	}
	p := w.pending
	if p == nil {
		return ErrNoTxnPending
	}
	w.pending = nil
	if p.created {
		if err := w.file.Close(); err != nil {
			return w.fail(false, fmt.Errorf("changelogfile: close %s: %w", w.name, err))
		}
		name := w.name
		w.file, w.name, w.size = nil, "", 0
		if err := os.Remove(filepath.Join(w.dir, name)); err != nil {
			return w.fail(false, fmt.Errorf("changelogfile: remove %s: %w", name, err))
		}
		if err := syncDir(w.dir); err != nil {
			return w.fail(false, fmt.Errorf("changelogfile: fsync directory: %w", err))
		}
		return nil
	}
	if err := w.file.Truncate(w.size); err != nil {
		return w.fail(false, fmt.Errorf("changelogfile: cut %s: %w", w.name, err))
	}
	if err := w.file.Sync(); err != nil {
		return w.fail(false, fmt.Errorf("changelogfile: fsync %s: %w", w.name, err))
	}
	return nil
}

// ready is the standing refusal every call checks first: closed or failed.
// Prepare alone also refuses while a transaction is pending.
func (w *Writer) ready() error {
	if w.closed {
		return ErrWriterClosed
	}
	return w.failed
}

// fail records an I/O error, rolls the active file back to the bytes that
// were durable before the append or the prepare (removing it outright when
// that created it, so no empty segment stays behind), and closes it.
func (w *Writer) fail(created bool, err error) error {
	w.failed = err
	w.pending = nil
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

// Close releases the active segment and the directory's writer lock. It is
// idempotent; the segment stays active, the next OpenWriter appends to it.
func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	var err error
	if w.file != nil {
		err = w.file.Close()
		w.file = nil
	}
	if lerr := w.lock.release(); err == nil {
		err = lerr
	}
	w.lock = nil
	return err
}
