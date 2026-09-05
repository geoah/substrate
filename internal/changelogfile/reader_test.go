package changelogfile

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// threeSegments builds a log of 7 entries in segments [1,2] [3,4] [5,6,7],
// the first two finished, the last active.
func threeSegments(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeLines(t, dir, 1, encodeLine(t, entryAt(1)), encodeLine(t, entryAt(2)))
	finish(t, dir, 1)
	writeLines(t, dir, 3, encodeLine(t, entryAt(3)), encodeLine(t, entryAt(4)))
	finish(t, dir, 3)
	writeLines(t, dir, 5, encodeLine(t, entryAt(5)), encodeLine(t, entryAt(6)), encodeLine(t, entryAt(7)))
	return dir
}

func TestOpenEmpty(t *testing.T) {
	for _, dir := range []string{t.TempDir(), filepath.Join(t.TempDir(), "absent")} {
		l, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		if l.Head() != 0 {
			t.Fatalf("head = %d", l.Head())
		}
		got, err := l.Read(0, 10)
		if err != nil || len(got) != 0 {
			t.Fatalf("read = %v, %v", got, err)
		}
		if err := l.Walk(func(Entry) error { t.Fatal("walked an entry"); return nil }); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadPagesAcrossSegments(t *testing.T) {
	dir := threeSegments(t)
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.Head() != 7 {
		t.Fatalf("head = %d, want 7", l.Head())
	}
	cases := []struct {
		after int64
		limit int
		first int64
		n     int64
	}{
		{0, 3, 1, 3},  // within and across the first boundary
		{3, 3, 4, 3},  // starts inside the second segment, crosses into the third
		{1, 0, 2, 6},  // no limit: everything after 1
		{6, 10, 7, 1}, // the last entry alone
		{7, 10, 0, 0}, // at the head
		{9, 10, 0, 0}, // past the head
		{-5, 2, 1, 2}, // a negative after reads from the start
		{4, 1, 5, 1},  // exactly the first line of a segment
	}
	for _, c := range cases {
		got, err := l.Read(c.after, c.limit)
		if err != nil {
			t.Fatalf("Read(%d, %d): %v", c.after, c.limit, err)
		}
		if !equalSeqs(seqs(got), c.first, c.n) {
			t.Errorf("Read(%d, %d) = %v, want %d from %d", c.after, c.limit, seqs(got), c.n, c.first)
		}
	}
	// Entries come back whole, with the payload in canonical form.
	got, err := l.Read(4, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].RecordID != "rec5" || string(got[0].Payload) != `{"n":1.5E0,"seq":5E0}` {
		t.Fatalf("entry 5 = %+v", got[0])
	}
}

func TestWalkVisitsEverySegment(t *testing.T) {
	dir := threeSegments(t)
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var seen []int64
	if err := l.Walk(func(e Entry) error { seen = append(seen, e.Seq); return nil }); err != nil {
		t.Fatal(err)
	}
	if !equalSeqs(seen, 1, 7) {
		t.Fatalf("walked %v", seen)
	}
	stop := errors.New("stop")
	n := 0
	err = l.Walk(func(Entry) error {
		n++
		if n == 3 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) || n != 3 {
		t.Fatalf("walk stopped with %v after %d", err, n)
	}
}

func TestOpenRefusesADigestMismatch(t *testing.T) {
	dir := threeSegments(t)
	path := filepath.Join(dir, SegmentName(3))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// A one-byte edit inside a finished segment, its line still well-formed
	// as JSON: the sidecar catches it before any line is decoded.
	edited := bytes.Replace(raw, []byte(`"actor":"api"`), []byte(`"actor":"apj"`), 1)
	if err := os.WriteFile(path, edited, fileMode); err != nil {
		t.Fatal(err)
	}
	_, err = Open(dir)
	if !errors.Is(err, ErrSegmentDigest) || !strings.Contains(err.Error(), SegmentName(3)) {
		t.Fatalf("err = %v, want ErrSegmentDigest naming %s", err, SegmentName(3))
	}
}

func TestOpenRefusesATornTailOnAFinishedSegment(t *testing.T) {
	dir := threeSegments(t)
	path := filepath.Join(dir, SegmentName(3))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw[:len(raw)-5], fileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); !errors.Is(err, ErrSegmentDigest) {
		t.Fatalf("err = %v, want ErrSegmentDigest", err)
	}
	// Even with a sidecar forged over the torn file, the missing newline is
	// refused.
	finish(t, dir, 3)
	if _, err := Open(dir); !errors.Is(err, ErrSegmentDigest) {
		t.Fatalf("forged sidecar: err = %v, want ErrSegmentDigest", err)
	}
}

func TestOpenCutsATornTailOnTheActiveSegment(t *testing.T) {
	dir := threeSegments(t)
	path := filepath.Join(dir, SegmentName(5))
	clean := fileSize(t, path)
	torn := encodeLine(t, entryAt(8))
	appendRaw(t, path, torn[:len(torn)-1])
	// Verify reports the tail and leaves it.
	rep, err := Verify(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Head != 7 || rep.Entries != 7 || rep.Segments != 3 || rep.TruncatedBytes != int64(len(torn)-1) {
		t.Fatalf("report = %+v", rep)
	}
	if fileSize(t, path) != clean+int64(len(torn)-1) {
		t.Fatal("Verify changed the file")
	}
	// Open cuts it.
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.Head() != 7 || l.TruncatedBytes != int64(len(torn)-1) {
		t.Fatalf("head = %d, truncated = %d", l.Head(), l.TruncatedBytes)
	}
	if fileSize(t, path) != clean {
		t.Fatalf("file is %d bytes, want %d", fileSize(t, path), clean)
	}
	got, err := l.Read(6, 5)
	if err != nil || !equalSeqs(seqs(got), 7, 1) {
		t.Fatalf("read = %v, %v", seqs(got), err)
	}
	l, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.TruncatedBytes != 0 {
		t.Fatalf("a second open reports %d truncated bytes", l.TruncatedBytes)
	}
}

func TestOpenReadOnlyLeavesATornTailAndRefusesAWriter(t *testing.T) {
	dir := threeSegments(t)
	path := filepath.Join(dir, SegmentName(5))
	clean := fileSize(t, path)
	torn := encodeLine(t, entryAt(8))
	appendRaw(t, path, torn[:len(torn)-1])
	l, err := OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.Head() != 7 || l.TruncatedBytes != int64(len(torn)-1) {
		t.Fatalf("head = %d, truncated = %d", l.Head(), l.TruncatedBytes)
	}
	if fileSize(t, path) != clean+int64(len(torn)-1) {
		t.Fatal("OpenReadOnly changed the file")
	}
	// Reads stop before the torn tail.
	got, err := l.Read(0, 0)
	if err != nil || !equalSeqs(seqs(got), 1, 7) {
		t.Fatalf("read = %v, %v", seqs(got), err)
	}
	if _, err := l.Writer(WriterOptions{}); !errors.Is(err, ErrLogNotRepaired) {
		t.Fatalf("Writer over a read-only log: err = %v, want ErrLogNotRepaired", err)
	}
	// Open repairs, and a writer over that log continues at the head.
	l, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w, err := l.Writer(WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(entriesFrom(8, 1)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if rep, err := Verify(dir); err != nil || rep.Head != 8 || rep.TruncatedBytes != 0 {
		t.Fatalf("after the append: report = %+v, err = %v", rep, err)
	}
}

func TestOpenTornTailThatIsTheWholeActiveSegment(t *testing.T) {
	dir := t.TempDir()
	writeLines(t, dir, 1, encodeLine(t, entryAt(1)))
	finish(t, dir, 1)
	torn := encodeLine(t, entryAt(2))
	if err := os.WriteFile(filepath.Join(dir, SegmentName(2)), torn[:10], fileMode); err != nil {
		t.Fatal(err)
	}
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.Head() != 1 || l.TruncatedBytes != 10 {
		t.Fatalf("head = %d, truncated = %d", l.Head(), l.TruncatedBytes)
	}
	// The now-empty active segment is where the writer continues.
	w, err := OpenWriter(dir, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	if err := w.Append(entriesFrom(2, 1)); err != nil {
		t.Fatal(err)
	}
	if rep, err := Verify(dir); err != nil || rep.Head != 2 || rep.Segments != 2 {
		t.Fatalf("report = %+v, %v", rep, err)
	}
}

func TestOpenRefusesDamagedLines(t *testing.T) {
	cases := []struct {
		name  string
		lines func(t *testing.T) [][]byte
		want  error
		text  string
	}{
		{
			"gap in seqs",
			func(t *testing.T) [][]byte { return [][]byte{encodeLine(t, entryAt(1)), encodeLine(t, entryAt(3))} },
			ErrSeqGap, "seq 3, want 2",
		},
		{
			"repeated seq",
			func(t *testing.T) [][]byte { return [][]byte{encodeLine(t, entryAt(1)), encodeLine(t, entryAt(1))} },
			ErrSeqGap, "",
		},
		{
			"bad sum",
			func(t *testing.T) [][]byte {
				l := encodeLine(t, entryAt(1))
				return [][]byte{bytes.Replace(l, []byte(`"actor":"api"`), []byte(`"actor":"apj"`), 1)}
			},
			ErrBadSum, "line at byte 0",
		},
		{
			"not JSON",
			func(t *testing.T) [][]byte { return [][]byte{encodeLine(t, entryAt(1)), []byte("garbage")} },
			nil, "line at byte",
		},
		{
			"blank line",
			func(t *testing.T) [][]byte { return [][]byte{encodeLine(t, entryAt(1)), {}} },
			nil, "line at byte",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeLines(t, dir, 1, c.lines(t)...)
			_, err := Open(dir)
			if err == nil {
				t.Fatal("opened a damaged active segment")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if !strings.Contains(err.Error(), c.text) {
				t.Fatalf("err = %v, want it to mention %q", err, c.text)
			}
		})
	}
}

func TestOpenRefusesSegmentLayoutErrors(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T, dir string)
		want  error
	}{
		{
			"first segment not at seq 1",
			func(t *testing.T, dir string) { writeLines(t, dir, 2, encodeLine(t, entryAt(2))) },
			ErrSegmentOrder,
		},
		{
			"segment starts past the previous end",
			func(t *testing.T, dir string) {
				writeLines(t, dir, 1, encodeLine(t, entryAt(1)))
				finish(t, dir, 1)
				writeLines(t, dir, 3, encodeLine(t, entryAt(3)))
			},
			ErrSegmentOrder,
		},
		{
			"segment starts before the previous end",
			func(t *testing.T, dir string) {
				writeLines(t, dir, 1, encodeLine(t, entryAt(1)), encodeLine(t, entryAt(2)), encodeLine(t, entryAt(3)))
				finish(t, dir, 1)
				writeLines(t, dir, 3, encodeLine(t, entryAt(3)))
			},
			ErrSegmentOrder,
		},
		{
			"segment named by a seq its first line does not carry",
			func(t *testing.T, dir string) { writeLines(t, dir, 1, encodeLine(t, entryAt(5))) },
			ErrSeqGap,
		},
		{
			"unfinished segment below the highest",
			func(t *testing.T, dir string) {
				writeLines(t, dir, 1, encodeLine(t, entryAt(1)))
				writeLines(t, dir, 2, encodeLine(t, entryAt(2)))
			},
			ErrSegmentUnfinished,
		},
		{
			"finished segment with no lines",
			func(t *testing.T, dir string) {
				writeLines(t, dir, 1)
				finish(t, dir, 1)
			},
			ErrSegmentEmpty,
		},
		{
			"orphan sidecar",
			func(t *testing.T, dir string) {
				writeLines(t, dir, 1, encodeLine(t, entryAt(1)))
				if err := os.WriteFile(filepath.Join(dir, SegmentName(2)+".sha256"), []byte("x\n"), fileMode); err != nil {
					t.Fatal(err)
				}
			},
			ErrOrphanSidecar,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			c.build(t, dir)
			if _, err := Open(dir); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if _, err := Verify(dir); !errors.Is(err, c.want) {
				t.Fatalf("verify: err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestVerifyCounts(t *testing.T) {
	dir := threeSegments(t)
	rep, err := Verify(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Report{Segments: 3, Entries: 7, Head: 7}
	if rep != want {
		t.Fatalf("report = %+v, want %+v", rep, want)
	}
	rep, err = Verify(filepath.Join(dir, "absent"))
	if err != nil || rep != (Report{}) {
		t.Fatalf("empty: %+v, %v", rep, err)
	}
}

func TestLogIsASnapshot(t *testing.T) {
	dir := t.TempDir()
	appendAll(t, dir, WriterOptions{}, entriesFrom(1, 2))
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	appendAll(t, dir, WriterOptions{}, entriesFrom(3, 2))
	got, err := l.Read(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if l.Head() != 2 || !equalSeqs(seqs(got), 1, 2) {
		t.Fatalf("head = %d, read %v", l.Head(), seqs(got))
	}
}

func TestLineReader(t *testing.T) {
	lr := newLineReader(strings.NewReader("ab\n\ncdef\ntail"), 1<<20)
	type step struct {
		line     string
		start    int64
		complete bool
	}
	want := []step{{"ab", 0, true}, {"", 3, true}, {"cdef", 4, true}, {"tail", 9, false}}
	for i, w := range want {
		line, start, complete, err := lr.next()
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if string(line) != w.line || start != w.start || complete != w.complete {
			t.Fatalf("step %d = %q, %d, %v; want %+v", i, line, start, complete, w)
		}
	}
	if _, _, _, err := lr.next(); !errors.Is(err, io.EOF) {
		t.Fatalf("after the tail: %v", err)
	}
	// A line over the cap is refused where it starts, even when it spans
	// several buffer fills.
	long := strings.Repeat("x", 200<<10)
	lr = newLineReader(strings.NewReader("ok\n"+long+"\n"), 100<<10)
	if _, _, _, err := lr.next(); err != nil {
		t.Fatal(err)
	}
	_, start, _, err := lr.next()
	if !errors.Is(err, ErrLineTooLong) || start != 3 {
		t.Fatalf("long line: err = %v at %d", err, start)
	}
	// A line exactly at the cap passes.
	lr = newLineReader(strings.NewReader(strings.Repeat("y", 100<<10)+"\n"), 100<<10)
	if line, _, _, err := lr.next(); err != nil || len(line) != 100<<10 {
		t.Fatalf("line at the cap: %d, %v", len(line), err)
	}
}
