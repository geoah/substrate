package changelogfile

import (
	"reflect"
	"testing"
)

// A paged walk through a Cursor returns exactly what one Read of everything
// returns, whatever the page size, and reads each segment's bytes once: the
// position's byte count after the walk is the segments' total size, not a
// multiple of it (issue 745, where a Read per page read each 256 MB segment
// hundreds of times).
func TestCursorPagesReadEachSegmentOnce(t *testing.T) {
	dir := threeSegments(t)
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, err := l.Read(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, s := range l.segments {
		total += s.end
	}
	for _, page := range []int{1, 2, 3, 7, 100, 0} {
		c := l.Cursor(0)
		var got []Entry
		for {
			es, err := c.Next(page)
			if err != nil {
				t.Fatalf("page %d: %v", page, err)
			}
			if len(es) == 0 {
				break
			}
			if page > 0 && len(es) > page {
				t.Fatalf("page %d returned %d entries", page, len(es))
			}
			got = append(got, es...)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("page %d: cursor = %v, Read = %v", page, seqs(got), seqs(want))
		}
		pos := c.Position()
		if pos.Bytes != total || pos.Seq != 7 || pos.Segment != SegmentName(5) {
			t.Errorf("page %d: position = %+v, want %d bytes, seq 7, %s", page, pos, total, SegmentName(5))
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		if es, err := c.Next(1); err != nil || len(es) != 0 {
			t.Fatalf("a closed cursor read %v, %v", seqs(es), err)
		}
	}
}

// Until reads through one seq and no further, so a caller pairing the file
// with table pages that end at arbitrary seqs reads exactly the page's lines.
func TestCursorUntilAndStartingPoints(t *testing.T) {
	dir := threeSegments(t)
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := l.Cursor(0)
	defer func() { _ = c.Close() }()
	steps := []struct {
		until int64
		first int64
		n     int64
	}{
		{2, 1, 2},   // inside the first segment
		{2, 0, 0},   // already past it: nothing
		{5, 3, 3},   // across two boundaries
		{100, 6, 2}, // past the head: the rest
		{100, 0, 0}, // at the head
	}
	for _, s := range steps {
		got, err := c.Until(s.until)
		if err != nil {
			t.Fatalf("Until(%d): %v", s.until, err)
		}
		if !equalSeqs(seqs(got), s.first, s.n) {
			t.Errorf("Until(%d) = %v, want %d from %d", s.until, seqs(got), s.n, s.first)
		}
	}
	starts := []struct {
		after int64
		limit int
		first int64
		n     int64
	}{
		{3, 3, 4, 3},  // starts inside the second segment
		{4, 1, 5, 1},  // exactly the first line of a segment
		{6, 10, 7, 1}, // the last entry alone
		{7, 10, 0, 0}, // at the head
		{9, 10, 0, 0}, // past the head
		{-5, 2, 1, 2}, // a negative after reads from the start
	}
	for _, s := range starts {
		c := l.Cursor(s.after)
		got, err := c.Next(s.limit)
		if err != nil {
			t.Fatalf("Cursor(%d).Next(%d): %v", s.after, s.limit, err)
		}
		if !equalSeqs(seqs(got), s.first, s.n) {
			t.Errorf("Cursor(%d).Next(%d) = %v, want %d from %d", s.after, s.limit, seqs(got), s.n, s.first)
		}
		_ = c.Close()
	}
	// Closed before its first read, when no segment is open yet, and closed
	// mid-walk with one open, a cursor reads nothing more.
	unread := l.Cursor(0)
	if err := unread.Close(); err != nil {
		t.Fatal(err)
	}
	if es, err := unread.Next(0); err != nil || len(es) != 0 {
		t.Fatalf("a cursor closed before its first read returned %v, %v", seqs(es), err)
	}
	between := l.Cursor(0)
	if _, err := between.Until(2); err != nil {
		t.Fatal(err)
	}
	if err := between.Close(); err != nil {
		t.Fatal(err)
	}
	if es, err := between.Until(7); err != nil || len(es) != 0 {
		t.Fatalf("a cursor closed between segments returned %v, %v", seqs(es), err)
	}
}

// Log.Verify and VerifyDir report a position after every segment, in seq
// order, at the segment's last seq and with the byte count running across
// segments, and count the same.
func TestLogVerifyReportsPositions(t *testing.T) {
	dir := threeSegments(t)
	l, err := OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, s := range l.segments {
		total += s.end
	}
	check := func(name string, positions []Position) {
		t.Helper()
		want := []struct {
			segment string
			seq     int64
		}{{SegmentName(1), 2}, {SegmentName(3), 4}, {SegmentName(5), 7}}
		if len(positions) != len(want) {
			t.Fatalf("%s: %d positions, want %d: %+v", name, len(positions), len(want), positions)
		}
		for i, p := range positions {
			if p.Segment != want[i].segment || p.Seq != want[i].seq {
				t.Errorf("%s: position %d = %+v, want %s at seq %d", name, i, p, want[i].segment, want[i].seq)
			}
			if i > 0 && p.Bytes <= positions[i-1].Bytes {
				t.Errorf("%s: position %d did not move: %+v after %+v", name, i, p, positions[i-1])
			}
		}
		if last := positions[len(positions)-1]; last.Bytes != total {
			t.Errorf("%s: last position = %+v, want %d bytes", name, last, total)
		}
	}
	var positions []Position
	r, err := l.Verify(func(p Position) { positions = append(positions, p) })
	if err != nil {
		t.Fatal(err)
	}
	if r.Entries != 7 || r.Head != 7 || r.Segments != 3 {
		t.Fatalf("report = %+v", r)
	}
	check("Log.Verify", positions)
	positions = nil
	_, whole, err := VerifyDir(dir, VerifyOptions{Progress: func(p Position) { positions = append(positions, p) }})
	if err != nil || whole != r {
		t.Fatalf("VerifyDir = %+v, %v; Log.Verify = %+v", whole, err, r)
	}
	check("VerifyDir", positions)
}
