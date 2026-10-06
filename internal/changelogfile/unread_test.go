package changelogfile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// threeLineSegments is a directory of two finished segments of three lines
// each (seqs 1-3 and 4-6) and an active one (7-8), so a byte can be damaged
// in a finished segment's middle line, which neither its first nor its last line holds.
func threeLineSegments(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeLines(t, dir, 1, encodeLine(t, entryAt(1)), encodeLine(t, entryAt(2)), encodeLine(t, entryAt(3)))
	finish(t, dir, 1)
	writeLines(t, dir, 4, encodeLine(t, entryAt(4)), encodeLine(t, entryAt(5)), encodeLine(t, entryAt(6)))
	finish(t, dir, 4)
	writeLines(t, dir, 7, encodeLine(t, entryAt(7)), encodeLine(t, entryAt(8)))
	return dir
}

// editSegment rewrites segment first with edit applied, keeping its sidecar.
func editSegment(t *testing.T, dir string, first int64, edit func([]byte) []byte) {
	t.Helper()
	path := filepath.Join(dir, SegmentName(first))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := edit(bytes.Clone(raw))
	if bytes.Equal(edited, raw) {
		t.Fatal("the edit changed nothing")
	}
	if err := os.WriteFile(path, edited, fileMode); err != nil {
		t.Fatal(err)
	}
}

// flipMiddle damages the middle line of a three-line segment, same length.
func flipMiddle(t *testing.T, dir string, first int64) {
	t.Helper()
	editSegment(t, dir, first, func(raw []byte) []byte {
		lines := bytes.SplitAfter(raw, []byte("\n"))
		lines[1] = bytes.Replace(lines[1], []byte(`"actor":"api"`), []byte(`"actor":"apj"`), 1)
		return bytes.Join(lines, nil)
	})
}

func digestOf(t *testing.T, dir string, first int64) KnownSegment {
	t.Helper()
	path := filepath.Join(dir, SegmentName(first))
	d, err := fileDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	return KnownSegment{Size: fileSize(t, path), Digest: d.hex}
}

// An open that trusts the sidecars reads each finished segment's first and last lines and
// leaves the rest to DigestUnread: a byte damaged in a middle line opens, is
// refused by a read of it, and is named by the digest. A clean directory
// digests clean, and every segment it digested is reported as such.
func TestTrustSidecarsLeavesTheDigestToDigestUnread(t *testing.T) {
	t.Run("damage in a middle line", func(t *testing.T) {
		dir := threeLineSegments(t)
		flipMiddle(t, dir, 4)
		if _, err := Open(dir); !errors.Is(err, ErrSegmentDigest) {
			t.Fatalf("a plain open: err = %v, want ErrSegmentDigest", err)
		}
		var progress []OpenProgress
		l, err := OpenWith(dir, OpenOptions{TrustSidecars: true, Progress: recordProgress(&progress)})
		if err != nil {
			t.Fatalf("the trusting open: %v", err)
		}
		if l.Head() != 8 || l.Unread() != 2 {
			t.Fatalf("head %d with %d unread, want 8 with 2", l.Head(), l.Unread())
		}
		for _, p := range progress {
			if p.Digested {
				t.Errorf("segment %s reported digested by an open that read only its first and last lines", p.Segment)
			}
		}
		if _, err := l.Read(4, 1); !errors.Is(err, ErrBadSum) {
			t.Fatalf("a read of the damaged line: err = %v, want ErrBadSum", err)
		}
		err = l.DigestUnread(context.Background(), DigestOptions{})
		if !errors.Is(err, ErrSegmentDigest) || !strings.Contains(err.Error(), SegmentName(4)) {
			t.Fatalf("digest: err = %v, want ErrSegmentDigest naming %s", err, SegmentName(4))
		}
	})
	t.Run("a clean directory", func(t *testing.T) {
		dir := threeLineSegments(t)
		l, err := OpenWith(dir, OpenOptions{TrustSidecars: true})
		if err != nil {
			t.Fatal(err)
		}
		var progress []OpenProgress
		if err := l.DigestUnread(context.Background(), DigestOptions{Progress: recordProgress(&progress)}); err != nil {
			t.Fatalf("digest: %v", err)
		}
		if len(progress) != 2 || !progress[1].Digested || progress[1].Segments != 2 || progress[1].Bytes != progress[1].TotalBytes {
			t.Fatalf("progress = %+v, want two digested segments ending at the total", progress)
		}
		// A Log handed on as Verified still owes the same digests.
		again, err := OpenWith(dir, OpenOptions{Verified: l, TrustSidecars: true})
		if err != nil || again.Unread() != 2 {
			t.Fatalf("reopen: %d unread, %v; want the two still owed", again.Unread(), err)
		}
	})
	t.Run("a done context", func(t *testing.T) {
		dir := threeLineSegments(t)
		l, err := OpenWith(dir, OpenOptions{TrustSidecars: true})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := l.DigestUnread(ctx, DigestOptions{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	})
}

// What the first and last lines show is still refused at the open: a finished segment cut
// short by a line keeps its sidecar and is refused by the next segment's
// first seq, a last line torn or damaged is refused, a first line that is not
// the seq the name says is refused, and an empty one is refused.
func TestTrustSidecarsRefusesWhatTheEndLinesShow(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func([]byte) []byte
		want error
	}{
		{
			name: "a line cut from the end",
			edit: func(raw []byte) []byte {
				lines := bytes.SplitAfter(raw, []byte("\n"))
				return bytes.Join(lines[:2], nil)
			},
			want: ErrSegmentOrder,
		},
		{
			name: "a torn last line",
			edit: func(raw []byte) []byte { return raw[:len(raw)-3] },
			want: ErrSegmentDigest,
		},
		{
			name: "a damaged last line",
			edit: func(raw []byte) []byte {
				i := bytes.LastIndex(raw, []byte(`"actor":"api"`))
				raw[i+len(`"actor":"ap`)] = 'j'
				return raw
			},
			want: ErrBadSum,
		},
		{
			name: "a damaged first line",
			edit: func(raw []byte) []byte {
				return bytes.Replace(raw, []byte(`"actor":"api"`), []byte(`"actor":"apj"`), 1)
			},
			want: ErrBadSum,
		},
		{
			name: "empty",
			edit: func([]byte) []byte { return []byte{} },
			want: ErrSegmentEmpty,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := threeLineSegments(t)
			editSegment(t, dir, 4, tc.edit)
			if _, err := OpenWith(dir, OpenOptions{TrustSidecars: true, ReadOnly: true}); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("a first line under another seq", func(t *testing.T) {
		dir := threeLineSegments(t)
		editSegment(t, dir, 4, func(raw []byte) []byte {
			lines := bytes.SplitAfter(raw, []byte("\n"))
			lines[0] = append(encodeLine(t, entryAt(40)), '\n')
			return bytes.Join(lines, nil)
		})
		if _, err := OpenWith(dir, OpenOptions{TrustSidecars: true, ReadOnly: true}); !errors.Is(err, ErrSeqGap) {
			t.Fatalf("err = %v, want ErrSeqGap", err)
		}
	})
}

// A last line longer than the first window read from the end is found by
// widening the window.
func TestTrustSidecarsFindsALongLastLine(t *testing.T) {
	dir := t.TempDir()
	long := entryAt(2)
	long.Payload = json.RawMessage(`{"blob":"` + strings.Repeat("x", 3*endLineWindow) + `"}`)
	writeLines(t, dir, 1, encodeLine(t, entryAt(1)), encodeLine(t, long))
	finish(t, dir, 1)
	writeLines(t, dir, 3, encodeLine(t, entryAt(3)))
	l, err := OpenWith(dir, OpenOptions{TrustSidecars: true})
	if err != nil {
		t.Fatal(err)
	}
	if l.Head() != 3 || l.Unread() != 1 {
		t.Fatalf("head %d with %d unread, want 3 with 1", l.Head(), l.Unread())
	}
	if err := l.DigestUnread(context.Background(), DigestOptions{}); err != nil {
		t.Fatal(err)
	}
}

// A Known segment is held to the size and digest the caller vouched for and
// read at its first and last lines; a Known name with another digest is refused, and
// KnownHead is where the run of them from seq 1 ends.
func TestOpenWithKnownHoldsTheSidecarToTheVouchedDigest(t *testing.T) {
	dir := threeLineSegments(t)
	known := map[string]KnownSegment{SegmentName(1): digestOf(t, dir, 1), SegmentName(4): digestOf(t, dir, 4)}
	flipMiddle(t, dir, 4)
	l, err := OpenWith(dir, OpenOptions{Known: known, ReadOnly: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	head, sum := l.KnownHead()
	if _, want, _ := Encode(entryAt(6)); head != 6 || sum != want {
		t.Fatalf("known head %d, want 6 with the line's sum", head)
	}
	if l.Unread() != 0 {
		t.Fatalf("%d unread: a known segment owes no digest", l.Unread())
	}

	wrong := map[string]KnownSegment{SegmentName(1): {Size: known[SegmentName(1)].Size, Digest: strings.Repeat("0", 64)}}
	if _, err := OpenWith(dir, OpenOptions{Known: wrong, ReadOnly: true}); !errors.Is(err, ErrSegmentDigest) {
		t.Fatalf("a known name with another digest: err = %v, want ErrSegmentDigest", err)
	}
	// Only the run from seq 1 is the known head: segment 1 unknown, none.
	l, err = OpenWith(dir, OpenOptions{Known: map[string]KnownSegment{SegmentName(4): known[SegmentName(4)]}, TrustSidecars: true, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if head, _ := l.KnownHead(); head != 0 {
		t.Fatalf("known head %d with segment 1 unknown, want 0", head)
	}
}

// VerifyDir with Known checks every line of what it does not vouch for and
// only the first and last lines of what it does.
func TestVerifyDirWithKnownChecksEverythingElse(t *testing.T) {
	dir := threeLineSegments(t)
	known := map[string]KnownSegment{SegmentName(1): digestOf(t, dir, 1)}
	flipMiddle(t, dir, 1)
	l, rep, err := VerifyDir(dir, VerifyOptions{Known: known})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if rep.Known != 1 || rep.Entries != 5 || rep.Head != 8 || l.Head() != 8 {
		t.Fatalf("report %+v, want 1 known segment and the other 5 lines checked to head 8", rep)
	}
	flipMiddle(t, dir, 4)
	if _, _, err := VerifyDir(dir, VerifyOptions{Known: known}); !errors.Is(err, ErrSegmentDigest) {
		t.Fatalf("a damaged segment nothing vouches for: err = %v, want ErrSegmentDigest", err)
	}
}

// SharedFinished is the run of finished segments from seq 1 that both
// directories hold under the same name, size and sidecar, read from the
// sidecars alone.
func TestSharedFinishedIsTheCommonRun(t *testing.T) {
	base := threeLineSegments(t)
	dir := t.TempDir()
	for _, name := range []string{SegmentName(1), SidecarName(SegmentName(1)), SegmentName(4), SidecarName(SegmentName(4))} {
		raw, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, fileMode); err != nil {
			t.Fatal(err)
		}
	}
	// dir went on: base's active segment finished in dir and dir has more.
	writeLines(t, dir, 7, encodeLine(t, entryAt(7)), encodeLine(t, entryAt(8)), encodeLine(t, entryAt(9)))
	finish(t, dir, 7)
	writeLines(t, dir, 10, encodeLine(t, entryAt(10)))

	got, err := SharedFinished(dir, base, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[SegmentName(1)] != digestOf(t, base, 1) || got[SegmentName(4)] != digestOf(t, base, 4) {
		t.Fatalf("shared = %+v, want segments 1 and 4 with base's digests", got)
	}
	// A head inside segment 4 vouches for segment 1 alone: what base holds
	// past its recorded head was written after it.
	if got, err := SharedFinished(dir, base, 5); err != nil || len(got) != 1 {
		t.Fatalf("shared under head 5 = %+v, %v; want segment 1 alone", got, err)
	}

	// A sidecar that differs ends the run there.
	if err := writeSidecar(base, SegmentName(1), strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	if got, err := SharedFinished(dir, base, 8); err != nil || len(got) != 0 {
		t.Fatalf("shared = %+v, %v; want none past a differing first segment", got, err)
	}
}

// CopyChangelogFrom links what is known and copies the rest, and the copy
// opens as the source with the linked segments known.
func TestCopyChangelogFromLinksTheKnownSegments(t *testing.T) {
	baseRepo, srcRepo, dstRepo := t.TempDir(), t.TempDir(), t.TempDir()
	base, src := ChangelogDir(baseRepo), ChangelogDir(srcRepo)
	for _, dir := range []string{base, src} {
		writeLines(t, dir, 1, encodeLine(t, entryAt(1)), encodeLine(t, entryAt(2)), encodeLine(t, entryAt(3)))
		finish(t, dir, 1)
	}
	writeLines(t, src, 4, encodeLine(t, entryAt(4)), encodeLine(t, entryAt(5)))
	finish(t, src, 4)
	writeLines(t, src, 6, encodeLine(t, entryAt(6)))

	writeLines(t, base, 4, encodeLine(t, entryAt(4)))
	known, err := SharedFinished(src, base, 4)
	if err != nil || len(known) != 1 {
		t.Fatalf("shared = %+v, %v", known, err)
	}
	report, err := CopyChangelogFrom(srcRepo, dstRepo, baseRepo, known)
	if err != nil {
		t.Fatal(err)
	}
	if report.Segments != 3 || len(report.Linked) != 1 || !report.Linked[SegmentName(1)] {
		t.Fatalf("report = %+v, want 3 segments with the first linked", report)
	}
	a, err := os.Stat(filepath.Join(base, SegmentName(1)))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(filepath.Join(ChangelogDir(dstRepo), SegmentName(1)))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(a, b) {
		t.Fatal("the known segment was copied, not linked")
	}

	srcLog, _, err := VerifyDir(src, VerifyOptions{Known: known})
	if err != nil {
		t.Fatal(err)
	}
	linked := map[string]KnownSegment{SegmentName(1): known[SegmentName(1)]}
	copied, err := OpenWith(ChangelogDir(dstRepo), OpenOptions{Known: linked, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := copied.SameAs(srcLog); err != nil {
		t.Fatalf("the copy is not the source: %v", err)
	}
}

// The last segment listed has no next one to hold its end to, so a finished
// last segment is digested whole: one that lost its last transaction with
// its sidecar kept would otherwise open as a shorter history.
func TestTrustSidecarsDigestsAFinishedLastSegment(t *testing.T) {
	dir := t.TempDir()
	writeLines(t, dir, 1, encodeLine(t, entryAt(1)), encodeLine(t, entryAt(2)))
	finish(t, dir, 1)
	writeLines(t, dir, 3, encodeLine(t, entryAt(3)), encodeLine(t, entryAt(4)), encodeLine(t, entryAt(5)))
	finish(t, dir, 3)
	l, err := OpenWith(dir, OpenOptions{TrustSidecars: true, ReadOnly: true})
	if err != nil || l.Head() != 5 || l.Unread() != 1 {
		t.Fatalf("open: head %d with %d unread, %v; want 5 with the first segment alone unread", l.Head(), l.Unread(), err)
	}
	editSegment(t, dir, 3, func(raw []byte) []byte {
		lines := bytes.SplitAfter(raw, []byte("\n"))
		return bytes.Join(lines[:2], nil)
	})
	for name, opts := range map[string]OpenOptions{
		"trusting the sidecars": {TrustSidecars: true, ReadOnly: true},
		"known":                 {Known: map[string]KnownSegment{SegmentName(3): {Size: fileSize(t, filepath.Join(dir, SegmentName(3))), Digest: mustSidecar(t, dir, 3)}}, ReadOnly: true},
	} {
		if _, err := OpenWith(dir, opts); !errors.Is(err, ErrSegmentDigest) {
			t.Errorf("%s: err = %v, want ErrSegmentDigest", name, err)
		}
	}
}

func mustSidecar(t *testing.T, dir string, first int64) string {
	t.Helper()
	d, err := ReadSidecar(dir, SegmentName(first))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// The digest paces each read as its caller says: the pace hook sees every
// read's bytes, their sum is the segments' bytes, and an error from it stops
// the digest with that error before the next read.
func TestDigestUnreadPacesEachRead(t *testing.T) {
	dir := threeLineSegments(t)
	l, err := OpenWith(dir, OpenOptions{TrustSidecars: true})
	if err != nil {
		t.Fatal(err)
	}
	var reads []int
	err = l.DigestUnread(context.Background(), DigestOptions{Pace: func(_ context.Context, n int) error {
		reads = append(reads, n)
		return nil
	}})
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	var total int64
	for _, n := range reads {
		total += int64(n)
	}
	if len(reads) < 2 || total != l.UnreadBytes() {
		t.Fatalf("the pace saw %d reads of %d bytes, want at least one per segment summing to %d", len(reads), total, l.UnreadBytes())
	}
	stop := errors.New("stop here")
	calls := 0
	err = l.DigestUnread(context.Background(), DigestOptions{Pace: func(context.Context, int) error {
		calls++
		return stop
	}})
	if !errors.Is(err, stop) || calls != 1 {
		t.Fatalf("digest: err = %v after %d pace calls, want the pace's error after its first", err, calls)
	}
}
