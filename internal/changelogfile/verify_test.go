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

// flipLineByte edits one byte inside the first line of segment first,
// keeping the line well-formed JSON, so only its checksum can tell.
func flipLineByte(t *testing.T, dir string, first int64) {
	t.Helper()
	path := filepath.Join(dir, SegmentName(first))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := bytes.Replace(raw, []byte(`"actor":"api"`), []byte(`"actor":"apj"`), 1)
	if err := os.WriteFile(path, edited, fileMode); err != nil {
		t.Fatal(err)
	}
}

// cutFile shortens segment first to n bytes.
func cutFile(t *testing.T, dir string, first, n int64) {
	t.Helper()
	if err := os.Truncate(filepath.Join(dir, SegmentName(first)), n); err != nil {
		t.Fatal(err)
	}
}

// The walk is faster and it catches what it caught: a byte flipped inside a
// line, a truncated segment, a wrong sidecar and a seq gap each fail
// VerifyDir, Verify and a walk of an opened Log (where the open itself does
// not refuse first), naming the damage.
func TestVerifyRefusesDamage(t *testing.T) {
	cases := []struct {
		name   string
		damage func(t *testing.T, dir string)
		want   error
		text   string
	}{
		{
			name:   "a byte flipped in a finished segment",
			damage: func(t *testing.T, dir string) { flipLineByte(t, dir, 3) },
			want:   ErrSegmentDigest, text: SegmentName(3),
		},
		{
			name: "a byte flipped in a finished segment, its sidecar forged",
			damage: func(t *testing.T, dir string) {
				flipLineByte(t, dir, 3)
				finish(t, dir, 3)
			},
			want: ErrBadSum, text: SegmentName(3),
		},
		{
			name:   "a byte flipped in the active segment",
			damage: func(t *testing.T, dir string) { flipLineByte(t, dir, 5) },
			want:   ErrBadSum, text: SegmentName(5),
		},
		{
			name: "a finished segment truncated mid-line",
			damage: func(t *testing.T, dir string) {
				cutFile(t, dir, 3, fileSize(t, filepath.Join(dir, SegmentName(3)))-10)
			},
			want: ErrSegmentDigest, text: SegmentName(3),
		},
		{
			name: "a finished segment truncated mid-line, its sidecar forged",
			damage: func(t *testing.T, dir string) {
				cutFile(t, dir, 3, fileSize(t, filepath.Join(dir, SegmentName(3)))-10)
				finish(t, dir, 3)
			},
			want: ErrSegmentDigest, text: "torn final line",
		},
		{
			name: "a finished segment truncated to its first line, its sidecar forged",
			damage: func(t *testing.T, dir string) {
				cutFile(t, dir, 3, int64(len(encodeLine(t, entryAt(3)))+1))
				finish(t, dir, 3)
			},
			want: ErrSegmentOrder, text: SegmentName(5),
		},
		{
			name: "a wrong sidecar",
			damage: func(t *testing.T, dir string) {
				if err := writeSidecar(dir, SegmentName(1), strings.Repeat("0", 64)); err != nil {
					t.Fatal(err)
				}
			},
			want: ErrSegmentDigest, text: SegmentName(1),
		},
		{
			name: "a sidecar that is not a digest",
			damage: func(t *testing.T, dir string) {
				if err := writeSidecar(dir, SegmentName(1), "nope"); err != nil {
					t.Fatal(err)
				}
			},
			want: ErrSegmentDigest, text: SegmentName(1),
		},
		{
			name: "a seq gap inside a finished segment",
			damage: func(t *testing.T, dir string) {
				writeLines(t, dir, 3, encodeLine(t, entryAt(3)), encodeLine(t, entryAt(5)))
				finish(t, dir, 3)
			},
			want: ErrSeqGap, text: "seq 5, want 4",
		},
		{
			name: "a seq gap inside the active segment",
			damage: func(t *testing.T, dir string) {
				writeLines(t, dir, 5, encodeLine(t, entryAt(5)), encodeLine(t, entryAt(7)))
			},
			want: ErrSeqGap, text: "seq 7, want 6",
		},
		{
			name: "a seq gap between segments",
			damage: func(t *testing.T, dir string) {
				for _, name := range []string{SegmentName(3), SidecarName(SegmentName(3))} {
					if err := os.Remove(filepath.Join(dir, name)); err != nil {
						t.Fatal(err)
					}
				}
			},
			want: ErrSegmentOrder, text: SegmentName(5),
		},
		{
			name: "a transaction that crosses a finished segment",
			damage: func(t *testing.T, dir string) {
				e := entryAt(4)
				e.Txn = 5
				writeLines(t, dir, 3, encodeLine(t, entryAt(3)), encodeLine(t, e))
				finish(t, dir, 3)
			},
			want: ErrTxnFraming, text: SegmentName(3),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := threeSegments(t)
			c.damage(t, dir)
			check := func(how string, err error) {
				t.Helper()
				if !errors.Is(err, c.want) || !strings.Contains(err.Error(), c.text) {
					t.Errorf("%s: err = %v, want %v naming %q", how, err, c.want, c.text)
				}
			}
			l, rep, err := VerifyDir(dir, VerifyOptions{})
			check("VerifyDir", err)
			if l != nil {
				t.Errorf("VerifyDir returned a Log with its error")
			}
			if rep.Segments == 0 {
				t.Errorf("the report counts no segments: %+v", rep)
			}
			_, err = Verify(dir)
			check("Verify", err)
			// A walk of an opened Log meets the damage the open does not.
			if opened, err := OpenReadOnly(dir); err == nil {
				_, err = opened.Verify(nil)
				check("Log.Verify", err)
			}
		})
	}
}

// Two damaged segments are reported in seq order, however the parallel
// walk happens to finish them, with the counts of what came before.
func TestVerifyReportsTheFirstDamageInSeqOrder(t *testing.T) {
	dir := t.TempDir()
	opts := WriterOptions{SegmentBytes: 1}
	for seq := int64(1); seq <= 12; seq++ {
		appendAll(t, dir, opts, entriesFrom(seq, 1))
	}
	flipLineByte(t, dir, 9)
	flipLineByte(t, dir, 4)
	for range 20 {
		_, rep, err := VerifyDir(dir, VerifyOptions{})
		if !errors.Is(err, ErrSegmentDigest) || !strings.Contains(err.Error(), SegmentName(4)) {
			t.Fatalf("err = %v, want the digest of %s", err, SegmentName(4))
		}
		if rep.Entries != 3 || rep.Head != 3 || rep.Segments != 12 {
			t.Fatalf("report = %+v, want 3 entries before the damage", rep)
		}
	}
}

// VerifyDir returns the Log OpenReadOnly opens: the same index, head and
// incomplete tail, and a Log that reads.
func TestVerifyDirReturnsTheLogOpenReadOnlyOpens(t *testing.T) {
	dir := threeSegments(t)
	appendRaw(t, filepath.Join(dir, SegmentName(5)), encodeLine(t, txnFrom(8, 2)[0]))
	appendRaw(t, filepath.Join(dir, SegmentName(5)), []byte("\n{\"torn"))
	opened, err := OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	verified, rep, err := VerifyDir(dir, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := verified.SameAs(opened); err != nil {
		t.Fatalf("the verified Log is not the opened one: %v", err)
	}
	if verified.TruncatedBytes != opened.TruncatedBytes || verified.TruncatedEntries != 1 || verified.repaired {
		t.Fatalf("tail = %d bytes, %d entries, repaired %v; the open's is %d bytes",
			verified.TruncatedBytes, verified.TruncatedEntries, verified.repaired, opened.TruncatedBytes)
	}
	want := Report{Segments: 3, Entries: 7, Head: 7, TruncatedBytes: opened.TruncatedBytes, TruncatedEntries: 1}
	if rep != want {
		t.Fatalf("report = %+v, want %+v", rep, want)
	}
	got, err := verified.Read(0, 0)
	if err != nil || !equalSeqs(seqs(got), 1, 7) {
		t.Fatalf("read = %v, %v", seqs(got), err)
	}
	if _, err := verified.Writer(WriterOptions{}); !errors.Is(err, ErrLogNotRepaired) {
		t.Fatalf("a verified Log backed a writer: %v", err)
	}
}

// SameAs holds a copy to its source: a copy of the bytes is the same, and a
// segment the copy holds differently is named, even when its own sidecar
// was rewritten to match it.
func TestSameAsHoldsACopyToItsSource(t *testing.T) {
	src := threeSegments(t)
	srcLog, _, err := VerifyDir(src, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	copyOf := func(t *testing.T) string {
		t.Helper()
		dstRepo := filepath.Join(t.TempDir(), "repo")
		if err := os.MkdirAll(ChangelogDir(dstRepo), dirMode); err != nil {
			t.Fatal(err)
		}
		segs, err := Segments(src)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range segs {
			names := []string{s.Name}
			if s.Finished {
				names = append(names, SidecarName(s.Name))
			}
			for _, name := range names {
				raw, err := os.ReadFile(filepath.Join(src, name))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(ChangelogDir(dstRepo), name), raw, fileMode); err != nil {
					t.Fatal(err)
				}
			}
		}
		return ChangelogDir(dstRepo)
	}
	same := copyOf(t)
	copied, err := OpenReadOnly(same)
	if err != nil {
		t.Fatal(err)
	}
	if err := copied.SameAs(srcLog); err != nil {
		t.Fatalf("a byte copy is not the source: %v", err)
	}

	forged := copyOf(t)
	flipLineByte(t, forged, 3)
	finish(t, forged, 3)
	forgedLog, err := OpenReadOnly(forged)
	if err != nil {
		t.Fatalf("the forged copy does not open: %v", err)
	}
	if err := forgedLog.SameAs(srcLog); !errors.Is(err, ErrNotTheSource) || !strings.Contains(err.Error(), SegmentName(3)) {
		t.Fatalf("err = %v, want ErrNotTheSource naming %s", err, SegmentName(3))
	}

	short := copyOf(t)
	if err := os.Remove(filepath.Join(short, SegmentName(5))); err != nil {
		t.Fatal(err)
	}
	shortLog, err := OpenReadOnly(short)
	if err != nil {
		t.Fatal(err)
	}
	if err := shortLog.SameAs(srcLog); !errors.Is(err, ErrNotTheSource) {
		t.Fatalf("a copy missing its active segment: err = %v, want ErrNotTheSource", err)
	}
}

// A SumCursor reads every line's seq and sum from any seq on, across
// segments, the sums Encode stamped.
func TestSumCursorReadsTheStampedSums(t *testing.T) {
	dir := threeSegments(t)
	l, _, err := VerifyDir(dir, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, after := range []int64{0, 1, 2, 4, 6, 7} {
		c := l.Sums(after)
		var got []int64
		for {
			seq, sum, err := c.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("after %d: %v", after, err)
			}
			if _, want, err := Encode(entryAt(seq)); err != nil || sum != want {
				t.Fatalf("after %d: seq %d carries %x, Encode stamps %x (%v)", after, seq, sum, want, err)
			}
			got = append(got, seq)
		}
		if !equalSeqs(got, after+1, 7-after) {
			t.Fatalf("after %d: read %v", after, got)
		}
		if pos := c.Position(); pos.Seq != 7 && after < 7 {
			t.Fatalf("after %d: position %+v", after, pos)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		if _, _, err := c.Next(); !errors.Is(err, io.EOF) {
			t.Fatalf("a closed cursor read on: %v", err)
		}
	}
}
