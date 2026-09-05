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
