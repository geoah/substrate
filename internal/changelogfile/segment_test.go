package changelogfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSegmentNameRoundTrips(t *testing.T) {
	cases := []struct {
		first int64
		name  string
	}{
		{1, "000000000000001.ndjson"},
		{482113, "000000000482113.ndjson"},
		{999999999999999, "999999999999999.ndjson"},
	}
	for _, c := range cases {
		if got := SegmentName(c.first); got != c.name {
			t.Errorf("SegmentName(%d) = %s, want %s", c.first, got, c.name)
		}
		first, ok := parseSegmentName(c.name)
		if !ok || first != c.first {
			t.Errorf("parseSegmentName(%s) = %d, %v", c.name, first, ok)
		}
	}
	for _, bad := range []string{
		"0000000000001.ndjson",     // 13 digits
		"00000000000000001.ndjson", // 17 digits
		"000000000000000.ndjson",   // seq 0
		"00000000000000a.ndjson",   // not a digit
		"000000000000001.json",
		"000000000000001",
		"-00000000000001.ndjson",
	} {
		if _, ok := parseSegmentName(bad); ok {
			t.Errorf("%q must not parse", bad)
		}
	}
}

func TestSegmentsLists(t *testing.T) {
	dir := t.TempDir()
	writeLines(t, dir, 1, encodeLine(t, entryAt(1)), encodeLine(t, entryAt(2)))
	finish(t, dir, 1)
	writeLines(t, dir, 3, encodeLine(t, entryAt(3)))
	// Unrelated files and half-written ones are ignored.
	for _, name := range []string{"README", "notes.txt", tmpPrefix + "12345", "x.sha256"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), fileMode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.ndjson"), dirMode); err != nil {
		t.Fatal(err)
	}
	segs, err := Segments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 {
		t.Fatalf("got %d segments, want 2: %+v", len(segs), segs)
	}
	if segs[0].Name != SegmentName(1) || segs[0].First != 1 || !segs[0].Finished || segs[0].Size == 0 {
		t.Errorf("first segment = %+v", segs[0])
	}
	if segs[1].Name != SegmentName(3) || segs[1].First != 3 || segs[1].Finished {
		t.Errorf("second segment = %+v", segs[1])
	}
}

func TestSegmentsMissingDirIsEmpty(t *testing.T) {
	segs, err := Segments(filepath.Join(t.TempDir(), "absent"))
	if err != nil || len(segs) != 0 {
		t.Fatalf("got %v, %v", segs, err)
	}
}

func TestSegmentsRefusals(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  error
	}{
		{"unparseable segment", map[string]string{"1.ndjson": "{}\n"}, ErrSegmentName},
		{"segment named with letters", map[string]string{"00000000000000x.ndjson": ""}, ErrSegmentName},
		{"orphan sidecar", map[string]string{"000000000000001.ndjson.sha256": "abc\n"}, ErrOrphanSidecar},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range c.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), fileMode); err != nil {
					t.Fatal(err)
				}
			}
			_, err := Segments(dir)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestReadSidecarRefusesMalformed(t *testing.T) {
	dir := t.TempDir()
	name := SegmentName(1)
	for _, body := range []string{"", "abc\n", "ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789\n", "zz" + string(make([]byte, 62))} {
		if err := os.WriteFile(filepath.Join(dir, sidecarName(name)), []byte(body), fileMode); err != nil {
			t.Fatal(err)
		}
		if _, err := readSidecar(dir, name); !errors.Is(err, ErrSegmentDigest) {
			t.Errorf("sidecar %q: err = %v, want ErrSegmentDigest", body, err)
		}
	}
}
