package changelogfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriterStartsEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "changelog")
	w, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	if w.Head() != 0 {
		t.Fatalf("head = %d, want 0", w.Head())
	}
	// No segment exists until the first append: an empty segment never does.
	if segs, _ := Segments(dir); len(segs) != 0 {
		t.Fatalf("a fresh writer created %v", segs)
	}
	if err := w.Append(nil); err != nil {
		t.Fatal(err)
	}
	if segs, _ := Segments(dir); len(segs) != 0 {
		t.Fatalf("an empty append created %v", segs)
	}
	if err := w.Append(entriesFrom(1, 3)); err != nil {
		t.Fatal(err)
	}
	if w.Head() != 3 {
		t.Fatalf("head = %d, want 3", w.Head())
	}
	segs, err := Segments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || segs[0].Name != "000000000000001.ndjson" || segs[0].Finished {
		t.Fatalf("segments = %+v", segs)
	}
	info, err := os.Stat(filepath.Join(dir, segs[0].Name))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != fileMode {
		t.Fatalf("segment mode = %v, want %v", info.Mode().Perm(), fileMode)
	}
}

func TestWriterReopensAtHead(t *testing.T) {
	dir := t.TempDir()
	appendAll(t, dir, WriterOptions{}, entriesFrom(1, 5))
	w, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	if w.Head() != 5 {
		t.Fatalf("head = %d, want 5", w.Head())
	}
	if err := w.Append(entriesFrom(6, 2)); err != nil {
		t.Fatal(err)
	}
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.Head() != 7 {
		t.Fatalf("log head = %d, want 7", l.Head())
	}
	got, err := l.Read(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !equalSeqs(seqs(got), 1, 7) {
		t.Fatalf("read %v", seqs(got))
	}
}

func TestWriterRefusesGapsAndRepeats(t *testing.T) {
	cases := []struct {
		name    string
		entries []Entry
	}{
		{"gap at start", entriesFrom(4, 1)},
		{"repeat of head", entriesFrom(2, 1)},
		{"seq zero", entriesFrom(0, 1)},
		{"gap inside the batch", []Entry{entryAt(3), entryAt(5)}},
		{"repeat inside the batch", []Entry{entryAt(3), entryAt(3)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			appendAll(t, dir, WriterOptions{}, entriesFrom(1, 2))
			before := fileSize(t, filepath.Join(dir, SegmentName(1)))
			w, err := OpenWriter(dir, WriterOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = w.Close() }()
			if err := w.Append(c.entries); !errors.Is(err, ErrSeqGap) {
				t.Fatalf("err = %v, want ErrSeqGap", err)
			}
			if w.Head() != 2 {
				t.Fatalf("head moved to %d", w.Head())
			}
			if after := fileSize(t, filepath.Join(dir, SegmentName(1))); after != before {
				t.Fatalf("a refused append wrote %d bytes", after-before)
			}
			// The writer is still usable after a refused batch.
			if err := w.Append(entriesFrom(3, 1)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWriterRefusesAnUnencodableEntry(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	bad := entryAt(1)
	bad.Payload = json.RawMessage(`{"a":`)
	if err := w.Append([]Entry{entryAt(1), bad}); err == nil {
		t.Fatal("a batch with an unencodable entry must be refused")
	}
	if segs, _ := Segments(dir); len(segs) != 0 {
		t.Fatalf("a refused first append created %v", segs)
	}
}

func TestWriterRotatesAtSegmentBytes(t *testing.T) {
	dir := t.TempDir()
	line := encodeLine(t, entryAt(1))
	// Two lines fill a segment; the third starts the next one.
	opts := WriterOptions{SegmentBytes: int64(2 * (len(line) + 1))}
	w, err := OpenWriter(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	if err := w.Append(entriesFrom(1, 2)); err != nil {
		t.Fatal(err)
	}
	segs, err := Segments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || !segs[0].Finished {
		t.Fatalf("after filling the first segment: %+v", segs)
	}
	// Rotation opens the next segment lazily: nothing empty is on disk.
	if _, err := os.Stat(filepath.Join(dir, SegmentName(3))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("segment 3 exists before anything was written to it: %v", err)
	}
	side, err := os.ReadFile(filepath.Join(dir, SegmentName(1)+".sha256"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := fileDigest(filepath.Join(dir, SegmentName(1)))
	if err != nil {
		t.Fatal(err)
	}
	if string(side) != d.hex+"\n" {
		t.Fatalf("sidecar = %q, want %q", side, d.hex+"\n")
	}
	if err := w.Append(entriesFrom(3, 1)); err != nil {
		t.Fatal(err)
	}
	segs, err = Segments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 || segs[1].Name != "000000000000003.ndjson" || segs[1].Finished {
		t.Fatalf("after the third append: %+v", segs)
	}
	// A batch that lands past the boundary rotates once, after the whole
	// batch: lines are never split across segments.
	if err := w.Append(entriesFrom(4, 3)); err != nil {
		t.Fatal(err)
	}
	segs, err = Segments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 || !segs[1].Finished {
		t.Fatalf("after the fourth append: %+v", segs)
	}
	if err := w.Append(entriesFrom(7, 1)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.Head() != 7 {
		t.Fatalf("head = %d, want 7", l.Head())
	}
	segs, _ = Segments(dir)
	if len(segs) != 3 || segs[2].Name != "000000000000007.ndjson" {
		t.Fatalf("segments = %+v", segs)
	}
}

func TestWriterReopensAfterRotationWithNoActiveSegment(t *testing.T) {
	dir := t.TempDir()
	line := encodeLine(t, entryAt(1))
	opts := WriterOptions{SegmentBytes: int64(len(line) + 1)}
	appendAll(t, dir, opts, entriesFrom(1, 1))
	segs, _ := Segments(dir)
	if len(segs) != 1 || !segs[0].Finished {
		t.Fatalf("segments = %+v", segs)
	}
	w, err := OpenWriter(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	if w.Head() != 1 {
		t.Fatalf("head = %d, want 1", w.Head())
	}
	if err := w.Append(entriesFrom(2, 1)); err != nil {
		t.Fatal(err)
	}
	segs, _ = Segments(dir)
	if len(segs) != 2 || segs[1].Name != SegmentName(2) {
		t.Fatalf("segments = %+v", segs)
	}
}

func TestWriterCutsATornTailOnOpen(t *testing.T) {
	dir := t.TempDir()
	appendAll(t, dir, WriterOptions{}, entriesFrom(1, 2))
	path := filepath.Join(dir, SegmentName(1))
	clean := fileSize(t, path)
	torn := encodeLine(t, entryAt(3))
	appendRaw(t, path, torn[:len(torn)/2])
	w, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	if w.Head() != 2 || w.TruncatedBytes() != int64(len(torn)/2) {
		t.Fatalf("head = %d, truncated = %d", w.Head(), w.TruncatedBytes())
	}
	if got := fileSize(t, path); got != clean {
		t.Fatalf("file is %d bytes after open, want %d", got, clean)
	}
	if err := w.Append(entriesFrom(3, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(dir); err != nil {
		t.Fatal(err)
	}
}

func TestWriterClosed(t *testing.T) {
	w, err := OpenWriter(t.TempDir(), WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if err := w.Append(entriesFrom(1, 1)); !errors.Is(err, ErrWriterClosed) {
		t.Fatalf("err = %v, want ErrWriterClosed", err)
	}
}

func TestWriterRefusesADamagedDirectory(t *testing.T) {
	dir := t.TempDir()
	writeLines(t, dir, 1, encodeLine(t, entryAt(1)))
	finish(t, dir, 1)
	appendRaw(t, filepath.Join(dir, SegmentName(1)), []byte("x"))
	if _, err := OpenWriter(dir, WriterOptions{}); !errors.Is(err, ErrSegmentDigest) {
		t.Fatalf("err = %v, want ErrSegmentDigest", err)
	}
}

func TestWriterRefusesALineOverTheCap(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates a 64 MiB payload")
	}
	dir := t.TempDir()
	w, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	e := entryAt(1)
	e.Payload = json.RawMessage(`"` + strings.Repeat("a", MaxLineBytes) + `"`)
	if err := w.Append([]Entry{e}); !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("err = %v, want ErrLineTooLong", err)
	}
	if segs, _ := Segments(dir); len(segs) != 0 {
		t.Fatalf("a refused append created %v", segs)
	}
}

// One writer per directory: a second OpenWriter on a locked directory is
// refused with ErrLocked and the directory is untouched; once the first is
// closed the second opens at the same head. The lock file is not a segment.
func TestWriterLockRefusesASecondWriter(t *testing.T) {
	dir := t.TempDir()
	first, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Append(entriesFrom(1, 2)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, LockFileName)); err != nil {
		t.Fatalf("no lock file after OpenWriter: %v", err)
	}
	if _, err := OpenWriter(dir, WriterOptions{}); !errors.Is(err, ErrLocked) {
		t.Fatalf("second writer: err = %v, want ErrLocked", err)
	}
	// A read-only open and a verify pass beside a live writer, and neither
	// lists the lock file as a segment.
	if l, err := OpenReadOnly(dir); err != nil || l.Head() != 2 {
		t.Fatalf("OpenReadOnly beside a writer: head %d, err %v", l.Head(), err)
	}
	if rep, err := Verify(dir); err != nil || rep.Segments != 1 || rep.Entries != 2 {
		t.Fatalf("Verify beside a writer: %+v, %v", rep, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatalf("after Close the second writer must open: %v", err)
	}
	defer func() { _ = second.Close() }()
	if second.Head() != 2 {
		t.Fatalf("head = %d, want 2", second.Head())
	}
	if err := second.Append(entriesFrom(3, 1)); err != nil {
		t.Fatal(err)
	}
}

// A torn tail is cut under the writer lock: a second process that finds one
// while the writer holds the lock is refused rather than allowed to truncate
// a line the writer may still be writing.
func TestOpenRefusesToCutATornTailUnderAnotherWriter(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	if err := w.Append(entriesFrom(1, 1)); err != nil {
		t.Fatal(err)
	}
	torn := encodeLine(t, entryAt(2))
	appendRaw(t, filepath.Join(dir, SegmentName(1)), torn[:len(torn)/2])
	if _, err := Open(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("Open with a torn tail under another writer: err = %v, want ErrLocked", err)
	}
	// Read-only sees the tail and leaves it.
	l, err := OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.Head() != 1 || l.TruncatedBytes != int64(len(torn)/2) {
		t.Fatalf("head %d, truncated %d", l.Head(), l.TruncatedBytes)
	}
}

// A Log whose directory moved between Open and Writer is refused: a writer
// opened at a stale head would write seqs the file already holds.
func TestLogWriterRefusesAStaleSnapshot(t *testing.T) {
	dir := t.TempDir()
	appendAll(t, dir, WriterOptions{}, entriesFrom(1, 2))
	stale, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	appendAll(t, dir, WriterOptions{}, entriesFrom(3, 1))
	if _, err := stale.Writer(WriterOptions{}); !errors.Is(err, ErrLogStale) {
		t.Fatalf("err = %v, want ErrLogStale", err)
	}
	fresh, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fresh.Close() }()
	if fresh.Head() != 3 {
		t.Fatalf("head = %d, want 3", fresh.Head())
	}
}

// AppendLines writes what Encode produced byte for byte, holds the same seq
// rule, and refuses a line over the cap without writing anything.
func TestWriterAppendLines(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	lines := []Line{{Seq: 1, Bytes: encodeLine(t, entryAt(1))}, {Seq: 2, Bytes: encodeLine(t, entryAt(2))}}
	if err := w.AppendLines(lines); err != nil {
		t.Fatal(err)
	}
	if err := w.AppendLines([]Line{{Seq: 4, Bytes: encodeLine(t, entryAt(4))}}); !errors.Is(err, ErrSeqGap) {
		t.Fatalf("gap: err = %v, want ErrSeqGap", err)
	}
	before := fileSize(t, filepath.Join(dir, SegmentName(1)))
	over := Line{Seq: 3, Bytes: make([]byte, MaxLineBytes+1)}
	if err := w.AppendLines([]Line{over}); !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("over the cap: err = %v, want ErrLineTooLong", err)
	}
	if after := fileSize(t, filepath.Join(dir, SegmentName(1))); after != before {
		t.Fatalf("a refused line wrote %d bytes", after-before)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := l.Read(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !equalSeqs(seqs(got), 1, 2) {
		t.Fatalf("read %v", seqs(got))
	}
	want := append(append(append([]byte{}, lines[0].Bytes...), '\n'), append(lines[1].Bytes, '\n')...)
	whole, err := os.ReadFile(filepath.Join(dir, SegmentName(1)))
	if err != nil {
		t.Fatal(err)
	}
	if string(whole) != string(want) {
		t.Fatal("AppendLines did not write the given bytes verbatim")
	}
}

// linesOf encodes entries as the Lines the engine hands the writer.
func linesOf(t *testing.T, entries []Entry) []Line {
	t.Helper()
	out := make([]Line, 0, len(entries))
	for _, e := range entries {
		out = append(out, Line{Seq: e.Seq, Txn: e.Txn, Bytes: encodeLine(t, e)})
	}
	return out
}

// A prepared transaction is on disk and is not history: a read-only open
// counts every byte of it as the incomplete tail and stops before it, and
// the head does not move. Commit writes one byte, the final newline, and
// the same open then reads the whole transaction.
func TestAPreparedTransactionIsCutUntilCommitted(t *testing.T) {
	dir := t.TempDir()
	appendAll(t, dir, WriterOptions{}, entriesFrom(1, 2))
	w, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	lines := linesOf(t, txnFrom(3, 3))
	before := fileSize(t, filepath.Join(dir, SegmentName(1)))
	if err := w.Prepare(lines); err != nil {
		t.Fatal(err)
	}
	if w.Head() != 2 {
		t.Fatalf("head moved to %d at prepare", w.Head())
	}
	var want int64
	for _, l := range lines {
		want += int64(len(l.Bytes)) + 1
	}
	if got := fileSize(t, filepath.Join(dir, SegmentName(1))) - before; got != want-1 {
		t.Fatalf("prepare wrote %d bytes, want %d (every byte but the last newline)", got, want-1)
	}
	ro, err := OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ro.Head() != 2 || ro.TruncatedBytes != want-1 || ro.TruncatedEntries != 2 {
		t.Fatalf("read-only open of a prepared transaction: head %d, truncated %d bytes, %d entries",
			ro.Head(), ro.TruncatedBytes, ro.TruncatedEntries)
	}
	if err := w.Prepare(linesOf(t, entriesFrom(6, 1))); !errors.Is(err, ErrTxnPending) {
		t.Fatalf("a second prepare: err = %v, want ErrTxnPending", err)
	}
	if err := w.Commit(); err != nil {
		t.Fatal(err)
	}
	if w.Head() != 5 {
		t.Fatalf("head = %d after commit, want 5", w.Head())
	}
	if got := fileSize(t, filepath.Join(dir, SegmentName(1))) - before; got != want {
		t.Fatalf("commit left %d bytes, want %d", got, want)
	}
	if err := w.Commit(); !errors.Is(err, ErrNoTxnPending) {
		t.Fatalf("a second commit: err = %v, want ErrNoTxnPending", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.TruncatedBytes != 0 {
		t.Fatalf("open cut %d bytes from a committed transaction", l.TruncatedBytes)
	}
	got, err := l.Read(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !equalSeqs(seqs(got), 1, 5) {
		t.Fatalf("read %v", seqs(got))
	}
}

// A writer closed with a transaction prepared (the process died between the
// prepare and its commit) leaves the tail on disk, and the next Open cuts it
// whole: nothing of the transaction becomes history, and the next writer
// continues from the seq before it.
func TestAPreparedTransactionLeftByACrashIsCutAtTheNextOpen(t *testing.T) {
	dir := t.TempDir()
	appendAll(t, dir, WriterOptions{}, entriesFrom(1, 2))
	w, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Prepare(linesOf(t, txnFrom(3, 2))); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w2, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w2.Close() }()
	if w2.Head() != 2 || w2.TruncatedBytes() == 0 {
		t.Fatalf("after the crash: head %d, truncated %d", w2.Head(), w2.TruncatedBytes())
	}
	if err := w2.AppendLines(linesOf(t, entriesFrom(3, 1))); err != nil {
		t.Fatal(err)
	}
	if w2.Head() != 3 {
		t.Fatalf("head = %d, want 3", w2.Head())
	}
}

// Abort puts the segment back as it was, on an existing segment and on one
// the prepare created (which is removed, so no empty segment stays), and
// the writer goes on appending.
func TestAbortLeavesTheSegmentAsItWas(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	if err := w.Abort(); !errors.Is(err, ErrNoTxnPending) {
		t.Fatalf("abort with nothing prepared: err = %v, want ErrNoTxnPending", err)
	}
	if err := w.Prepare(linesOf(t, entriesFrom(1, 1))); err != nil {
		t.Fatal(err)
	}
	if err := w.Abort(); err != nil {
		t.Fatal(err)
	}
	if segs, _ := Segments(dir); len(segs) != 0 {
		t.Fatalf("an aborted prepare left %v", segs)
	}
	if err := w.AppendLines(linesOf(t, entriesFrom(1, 2))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, SegmentName(1))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Prepare(linesOf(t, txnFrom(3, 2))); err != nil {
		t.Fatal(err)
	}
	if err := w.Abort(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("abort did not put the segment back")
	}
	if err := w.AppendLines(linesOf(t, entriesFrom(3, 1))); err != nil {
		t.Fatal(err)
	}
	if w.Head() != 3 {
		t.Fatalf("head = %d, want 3", w.Head())
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := l.Read(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !equalSeqs(seqs(got), 1, 3) || l.TruncatedBytes != 0 {
		t.Fatalf("read %v, truncated %d", seqs(got), l.TruncatedBytes)
	}
}

// The segment finishes at Commit, never at Prepare: a transaction that
// carries the segment past SegmentBytes is whole in it, and a sidecar is
// written only for bytes that are history.
func TestCommitFinishesTheSegmentWhenDue(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenWriter(dir, WriterOptions{SegmentBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	if err := w.Prepare(linesOf(t, txnFrom(1, 2))); err != nil {
		t.Fatal(err)
	}
	segs, err := Segments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || segs[0].Finished {
		t.Fatalf("after prepare: %+v", segs)
	}
	if err := w.Commit(); err != nil {
		t.Fatal(err)
	}
	segs, err = Segments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || !segs[0].Finished {
		t.Fatalf("after commit: %+v", segs)
	}
	if err := w.AppendLines(linesOf(t, entriesFrom(3, 1))); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.Head() != 3 || len(l.segments) != 2 {
		t.Fatalf("head %d, %d segments", l.Head(), len(l.segments))
	}
}
