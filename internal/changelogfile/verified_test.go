package changelogfile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// flipInSegment edits one byte inside a line of segment first, keeping the
// file's size and its sidecar, so only a read of the bytes can tell.
func flipInSegment(t *testing.T, dir string, first int64) {
	t.Helper()
	path := filepath.Join(dir, SegmentName(first))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := bytes.Replace(raw, []byte(`"actor":"api"`), []byte(`"actor":"apj"`), 1)
	if bytes.Equal(edited, raw) {
		t.Fatal("nothing to flip")
	}
	if err := os.WriteFile(path, edited, fileMode); err != nil {
		t.Fatal(err)
	}
}

// recordProgress collects every OpenProgress an open reports.
func recordProgress(got *[]OpenProgress) func(OpenProgress) {
	return func(p OpenProgress) { *got = append(*got, p) }
}

// A Log handed back as Verified vouches for its finished segments: the next
// open reads none of their bytes. The proof is a byte flipped inside a
// finished segment after the first open, which a plain Open refuses and the
// vouched open does not see.
func TestOpenWithVerifiedReadsNoFinishedSegmentAgain(t *testing.T) {
	dir := threeSegments(t)
	first, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	flipInSegment(t, dir, 3)
	if _, err := Open(dir); !errors.Is(err, ErrSegmentDigest) {
		t.Fatalf("a plain open: err = %v, want ErrSegmentDigest", err)
	}
	var progress []OpenProgress
	l, err := OpenWith(dir, OpenOptions{Verified: first, Progress: recordProgress(&progress)})
	if err != nil {
		t.Fatalf("the vouched open: %v", err)
	}
	if l.Head() != 7 {
		t.Fatalf("head = %d, want 7", l.Head())
	}
	if len(progress) != 3 {
		t.Fatalf("progress reported %d segments, want 3: %+v", len(progress), progress)
	}
	var total int64
	for _, first := range []int64{1, 3, 5} {
		total += fileSize(t, filepath.Join(dir, SegmentName(first)))
	}
	var bytesSoFar int64
	for i, p := range progress {
		bytesSoFar += fileSize(t, filepath.Join(dir, p.Segment))
		finished := i < 2
		if p.Finished != finished || p.Reused != finished {
			t.Errorf("segment %s: finished %v reused %v, want both %v", p.Segment, p.Finished, p.Reused, finished)
		}
		if p.Segments != i+1 || p.TotalSegments != 3 || p.Bytes != bytesSoFar || p.TotalBytes != total {
			t.Errorf("segment %s: counts %+v, want %d of 3 segments, %d of %d bytes", p.Segment, p, i+1, bytesSoFar, total)
		}
	}
	// The vouched Log reads like any other, from the index the first open
	// built, and a read still holds every line to its checksum: the flipped
	// line is refused when it is read.
	got, err := l.Read(0, 2)
	if err != nil || !equalSeqs(seqs(got), 1, 2) {
		t.Fatalf("read = %v, %v", seqs(got), err)
	}
	if _, err := l.Read(2, 2); !errors.Is(err, ErrBadSum) {
		t.Fatalf("read of the flipped line: err = %v, want ErrBadSum", err)
	}
}

// What the Verified Log does not vouch for is checked as Open checks it: the
// active segment every time, a segment that finished since, a finished
// segment whose size changed, a sidecar that no longer names the digest, and
// every segment when the Log is over another directory.
func TestOpenWithVerifiedChecksWhatItDoesNotVouchFor(t *testing.T) {
	t.Run("the active segment", func(t *testing.T) {
		dir := threeSegments(t)
		first, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		flipInSegment(t, dir, 5)
		if _, err := OpenWith(dir, OpenOptions{Verified: first}); !errors.Is(err, ErrBadSum) {
			t.Fatalf("err = %v, want ErrBadSum", err)
		}
	})
	t.Run("a segment finished since", func(t *testing.T) {
		dir := threeSegments(t)
		first, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		finish(t, dir, 5)
		flipInSegment(t, dir, 5)
		var progress []OpenProgress
		if _, err := OpenWith(dir, OpenOptions{Verified: first, Progress: recordProgress(&progress)}); !errors.Is(err, ErrSegmentDigest) {
			t.Fatalf("err = %v, want ErrSegmentDigest", err)
		}
		if len(progress) != 2 || !progress[0].Reused || !progress[1].Reused {
			t.Fatalf("the two segments the Log checked were not reused: %+v", progress)
		}
	})
	t.Run("a finished segment whose size changed", func(t *testing.T) {
		dir := threeSegments(t)
		first, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		appendRaw(t, filepath.Join(dir, SegmentName(3)), []byte("\n"))
		if _, err := OpenWith(dir, OpenOptions{Verified: first}); !errors.Is(err, ErrSegmentDigest) {
			t.Fatalf("err = %v, want ErrSegmentDigest", err)
		}
	})
	t.Run("a sidecar rewritten", func(t *testing.T) {
		dir := threeSegments(t)
		first, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeSidecar(dir, SegmentName(1), string(bytes.Repeat([]byte("0"), 64))); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenWith(dir, OpenOptions{Verified: first}); !errors.Is(err, ErrSegmentDigest) {
			t.Fatalf("err = %v, want ErrSegmentDigest", err)
		}
	})
	t.Run("a Log over another directory", func(t *testing.T) {
		dir, other := threeSegments(t), threeSegments(t)
		first, err := Open(other)
		if err != nil {
			t.Fatal(err)
		}
		flipInSegment(t, dir, 3)
		var progress []OpenProgress
		if _, err := OpenWith(dir, OpenOptions{Verified: first, Progress: recordProgress(&progress)}); !errors.Is(err, ErrSegmentDigest) {
			t.Fatalf("err = %v, want ErrSegmentDigest", err)
		}
		if len(progress) != 1 || progress[0].Reused {
			t.Fatalf("a Log over another directory vouched: %+v", progress)
		}
	})
}

// The writer's own rotation is what finishes a segment in practice: a Log
// opened before the rotation vouches for the segments that were finished
// then, and the segment the writer finished since is digested.
func TestOpenWithVerifiedAfterARotation(t *testing.T) {
	dir := t.TempDir()
	opts := WriterOptions{SegmentBytes: 1}
	appendAll(t, dir, opts, entriesFrom(1, 1))
	appendAll(t, dir, opts, entriesFrom(2, 1))
	first, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	appendAll(t, dir, opts, entriesFrom(3, 1))
	var progress []OpenProgress
	l, err := OpenWith(dir, OpenOptions{Verified: first, Progress: recordProgress(&progress)})
	if err != nil {
		t.Fatal(err)
	}
	if l.Head() != 3 {
		t.Fatalf("head = %d, want 3", l.Head())
	}
	var reused []bool
	for _, p := range progress {
		reused = append(reused, p.Reused)
	}
	if len(reused) != 3 || !reused[0] || !reused[1] || reused[2] {
		t.Fatalf("reused = %v, want [true true false]", reused)
	}
	w, err := l.Writer(opts)
	if err != nil {
		t.Fatalf("a vouched Log backs a writer: %v", err)
	}
	if err := w.Append(entriesFrom(4, 1)); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	if rep, err := Verify(dir); err != nil || rep.Head != 4 {
		t.Fatalf("verify after the append: %+v, %v", rep, err)
	}
}
