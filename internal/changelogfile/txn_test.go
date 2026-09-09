package changelogfile

// Transaction framing (decision 0057): every line names the seq its
// transaction ends at, an append is whole transactions, and recovery cuts an
// unfinished transaction back to the line before its first entry.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// A three-entry transaction cut at every byte offset opens at the head before
// it or, only when nothing is missing, at its last seq; never inside it. The
// read-only open is checked at every offset; the repairing open and the
// re-append (one fsync each) at the offsets that matter, each line's first
// byte, its last byte and its newline. The same writer appends the
// transaction again whole and the file is byte for byte what it was, which is
// what the boot does from the table.
func TestOpenCutsBackToTheLastCompleteTransaction(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(txnFrom(1, 2)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, SegmentName(1))
	prefix := fileSize(t, path)
	if err := w.Append(txnFrom(3, 3)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	whole, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	boundary := map[int64]bool{}
	for start := prefix; start < int64(len(whole)); {
		end := start + int64(bytes.IndexByte(whole[start:], '\n')) + 1
		for _, off := range []int64{start, start + 1, end - 1, end} {
			if off < int64(len(whole)) {
				boundary[off] = true
			}
		}
		start = end
	}
	for cut := prefix; cut < int64(len(whole)); cut++ {
		if err := os.WriteFile(path, whole[:cut], fileMode); err != nil {
			t.Fatal(err)
		}
		wantEntries := int64(bytes.Count(whole[prefix:cut], []byte{'\n'}))
		// Read-only: counted and left in place, reads stop before it.
		ro, err := OpenReadOnly(dir)
		if err != nil {
			t.Fatalf("cut at %d: OpenReadOnly: %v", cut, err)
		}
		if ro.Head() != 2 || ro.TruncatedBytes != cut-prefix || ro.TruncatedEntries != wantEntries {
			t.Fatalf("cut at %d: read-only head = %d, truncated = %d bytes / %d entries; want 2, %d, %d",
				cut, ro.Head(), ro.TruncatedBytes, ro.TruncatedEntries, cut-prefix, wantEntries)
		}
		if got, err := ro.Read(0, 0); err != nil || !equalSeqs(seqs(got), 1, 2) {
			t.Fatalf("cut at %d: read-only Read = %v, %v", cut, seqs(got), err)
		}
		if fileSize(t, path) != cut {
			t.Fatalf("cut at %d: OpenReadOnly changed the file", cut)
		}
		if !boundary[cut] {
			continue
		}
		// Open cuts the whole unfinished transaction.
		l, err := Open(dir)
		if err != nil {
			t.Fatalf("cut at %d: Open: %v", cut, err)
		}
		if l.Head() != 2 || l.TruncatedBytes != cut-prefix || l.TruncatedEntries != wantEntries {
			t.Fatalf("cut at %d: head = %d, truncated = %d bytes / %d entries", cut, l.Head(), l.TruncatedBytes, l.TruncatedEntries)
		}
		if fileSize(t, path) != prefix {
			t.Fatalf("cut at %d: Open left %d bytes, want %d", cut, fileSize(t, path), prefix)
		}
		// The transaction appends again from the head Open reports, whole.
		w, err := l.Writer(WriterOptions{})
		if err != nil {
			t.Fatalf("cut at %d: writer: %v", cut, err)
		}
		if w.Head() != 2 {
			t.Fatalf("cut at %d: writer head = %d", cut, w.Head())
		}
		if err := w.Append(txnFrom(3, 3)); err != nil {
			t.Fatalf("cut at %d: re-append: %v", cut, err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, whole) {
			t.Fatalf("cut at %d: the re-appended segment is not byte for byte the original", cut)
		}
	}
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.Head() != 5 || l.TruncatedBytes != 0 || l.TruncatedEntries != 0 {
		t.Fatalf("the whole file: head = %d, truncated = %d / %d", l.Head(), l.TruncatedBytes, l.TruncatedEntries)
	}
}

// An unfinished transaction that is the whole active segment is cut to an
// empty segment, the head is the finished segment's last seq, and a writer
// continues into the empty segment.
func TestOpenCutsAnUnfinishedTransactionThatIsTheWholeActiveSegment(t *testing.T) {
	dir := t.TempDir()
	appendAll(t, dir, WriterOptions{SegmentBytes: 1}, txnFrom(1, 2))
	if segs, _ := Segments(dir); len(segs) != 1 || !segs[0].Finished {
		t.Fatalf("segments = %+v, want one finished", segs)
	}
	// Two complete lines of a three-line transaction, and no third.
	tx := txnFrom(3, 3)
	path := writeLines(t, dir, 3, encodeLine(t, tx[0]), encodeLine(t, tx[1]))
	size := fileSize(t, path)
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.Head() != 2 || l.TruncatedBytes != size || l.TruncatedEntries != 2 {
		t.Fatalf("head = %d, truncated = %d / %d; want 2, %d, 2", l.Head(), l.TruncatedBytes, l.TruncatedEntries, size)
	}
	if fileSize(t, path) != 0 {
		t.Fatalf("the active segment holds %d bytes after the cut", fileSize(t, path))
	}
	w, err := l.Writer(WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(tx); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	rep, err := Verify(dir)
	if err != nil || rep.Head != 5 || rep.Entries != 5 || rep.TruncatedBytes != 0 {
		t.Fatalf("verify after the re-append: %+v, %v", rep, err)
	}
}

// The frame is part of the line: `txn` moves the checksum, and a line
// carrying none does not encode at all.
func TestTxnMovesTheSum(t *testing.T) {
	a := entryAt(1)
	b := a
	b.Txn = 2
	_, sa, err := Encode(a)
	if err != nil {
		t.Fatal(err)
	}
	_, sb, err := Encode(b)
	if err != nil {
		t.Fatal(err)
	}
	if sa == sb {
		t.Fatal("txn is not covered by the checksum")
	}
	unframed := a
	unframed.Txn = 0
	if _, _, err := Encode(unframed); !errors.Is(err, ErrTxnFraming) {
		t.Fatalf("encoding a line with no txn: err = %v, want ErrTxnFraming", err)
	}
	line, _, err := Encode(a)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := Decode(line)
	if err != nil {
		t.Fatal(err)
	}
	if got.Txn != 1 {
		t.Fatalf("decoded txn = %d", got.Txn)
	}
}

// A `txn` below the line's own seq is refused on both sides, before any
// checksum is consulted.
func TestTxnBeforeSeqIsRefused(t *testing.T) {
	e := entryAt(2)
	e.Txn = 1
	if _, _, err := Encode(e); !errors.Is(err, ErrTxnFraming) {
		t.Fatalf("Encode: err = %v, want ErrTxnFraming", err)
	}
	raw, err := json.Marshal(map[string]any{
		"seq": 2, "txn": 1, "ts": "2026-09-05T10:00:00.000000Z", "actor": "api", "principal": "k",
		"op": "put", "recordId": "r", "kind": "ada.example.com/task", "payload": map[string]any{},
		"sum": "sha256:" + fmt.Sprintf("%064d", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Decode(raw); !errors.Is(err, ErrTxnFraming) {
		t.Fatalf("Decode: err = %v, want ErrTxnFraming", err)
	}
}

// The writer takes whole transactions only: a batch that leaves one open, or
// changes transaction inside one, writes nothing and stays usable.
func TestWriterRefusesABatchThatIsNotWholeTransactions(t *testing.T) {
	changed := txnFrom(3, 3)
	changed[1].Txn = 6
	dropped := txnFrom(3, 3)
	dropped[1].Txn = 0
	cases := []struct {
		name    string
		entries []Entry
		want    error
	}{
		{"open at the end", txnFrom(3, 3)[:2], ErrTxnIncomplete},
		{"one line of a longer transaction", txnFrom(3, 2)[:1], ErrTxnIncomplete},
		{"txn changes inside", changed, ErrTxnFraming},
		{"unframed line inside", dropped, ErrTxnFraming},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			appendAll(t, dir, WriterOptions{}, txnFrom(1, 2))
			path := filepath.Join(dir, SegmentName(1))
			before := fileSize(t, path)
			w, err := OpenWriter(dir, WriterOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = w.Close() }()
			if err := w.Append(c.entries); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if w.Head() != 2 || fileSize(t, path) != before {
				t.Fatalf("a refused batch moved the head to %d or wrote %d bytes", w.Head(), fileSize(t, path)-before)
			}
			// Whole transactions, a one-line one beside a longer one (the
			// boot writing out a table with both), are taken.
			if err := w.Append(append(entriesFrom(3, 1), txnFrom(4, 2)...)); err != nil {
				t.Fatal(err)
			}
			if w.Head() != 5 {
				t.Fatalf("head = %d, want 5", w.Head())
			}
		})
	}
}

// AppendLines trusts the Txn on each Line for the same checks.
func TestAppendLinesHoldsTransactionBoundaries(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	tx := txnFrom(1, 2)
	lines := []Line{{Seq: 1, Txn: 2, Bytes: encodeLine(t, tx[0])}}
	if err := w.AppendLines(lines); !errors.Is(err, ErrTxnIncomplete) {
		t.Fatalf("err = %v, want ErrTxnIncomplete", err)
	}
	if segs, _ := Segments(dir); len(segs) != 0 {
		t.Fatalf("a refused first append created %v", segs)
	}
	lines = append(lines, Line{Seq: 2, Txn: 2, Bytes: encodeLine(t, tx[1])})
	if err := w.AppendLines(lines); err != nil {
		t.Fatal(err)
	}
	if w.Head() != 2 {
		t.Fatalf("head = %d", w.Head())
	}
}

// The segment rotates only after an append, so a transaction larger than
// SegmentBytes lands in one segment, and the next transaction opens the next.
func TestATransactionLargerThanSegmentBytesLandsInOneSegment(t *testing.T) {
	dir := t.TempDir()
	oneLine := int64(len(encodeLine(t, entryAt(1)))) + 1
	// A segment size one line can exceed, so a writer that rotated inside a
	// batch would split the transaction after its first line.
	w, err := OpenWriter(dir, WriterOptions{SegmentBytes: oneLine})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(txnFrom(1, 4)); err != nil {
		t.Fatal(err)
	}
	segs, err := Segments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || !segs[0].Finished || segs[0].Size < 4*oneLine {
		t.Fatalf("after one four-line transaction: %+v", segs)
	}
	if err := w.Append(txnFrom(5, 2)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	segs, err = Segments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 || segs[1].Name != SegmentName(5) {
		t.Fatalf("after the second transaction: %+v", segs)
	}
	rep, err := Verify(dir)
	if err != nil || rep.Head != 6 || rep.Entries != 6 {
		t.Fatalf("verify: %+v, %v", rep, err)
	}
}

// A transaction that crosses a segment boundary is damage no writer produces:
// Open trusts the finished segment's sidecar and the active segment starts a
// transaction cleanly, so it opens; Verify decodes everything and names it.
func TestVerifyRefusesATransactionCrossingASegment(t *testing.T) {
	dir := t.TempDir()
	tx := txnFrom(1, 3)
	writeLines(t, dir, 1, encodeLine(t, tx[0]), encodeLine(t, tx[1]))
	finish(t, dir, 1)
	writeLines(t, dir, 3, encodeLine(t, tx[2]))
	l, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if l.Head() != 3 {
		t.Fatalf("head = %d", l.Head())
	}
	rep, err := Verify(dir)
	if !errors.Is(err, ErrTxnFraming) || rep.Entries != 2 {
		t.Fatalf("verify: %+v, %v; want ErrTxnFraming after 2 entries", rep, err)
	}
	if !bytes.Contains([]byte(err.Error()), []byte(SegmentName(1))) {
		t.Fatalf("the error does not name the segment: %v", err)
	}
}

// A line whose `txn` is not the transaction the line before it left open is
// refused by the active-segment scan, not cut. A line carrying no `txn` at all
// is refused a step earlier, where it is encoded (TestTxnMovesTheSum), so it
// cannot be written here.
func TestOpenRefusesTransactionFramingDamage(t *testing.T) {
	entries := txnFrom(1, 3)
	entries[1].Txn = 4
	dir := t.TempDir()
	path := writeLines(t, dir, 1, encodeLine(t, entries[0]), encodeLine(t, entries[1]), encodeLine(t, entries[2]))
	size := fileSize(t, path)
	if _, err := Open(dir); !errors.Is(err, ErrTxnFraming) {
		t.Fatalf("Open: err = %v, want ErrTxnFraming", err)
	}
	if fileSize(t, path) != size {
		t.Fatal("a refused open changed the file")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
