package changelogfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// entryAt is a well-formed entry at seq, with a payload that names the seq so
// two entries never encode alike.
func entryAt(seq int64) Entry {
	return Entry{
		Seq: seq, TS: time.Date(2026, 9, 5, 10, 0, 0, int(seq)*1000, time.UTC),
		Actor: "api", Principal: "k7abc", Op: "put",
		RecordID: fmt.Sprintf("rec%d", seq), Kind: "ada.example.com/task",
		Payload: json.RawMessage(fmt.Sprintf(`{"seq":%d,"n":1.50}`, seq)),
	}
}

func entriesFrom(first, n int64) []Entry {
	out := make([]Entry, 0, n)
	for s := first; s < first+n; s++ {
		out = append(out, entryAt(s))
	}
	return out
}

// appendAll opens a writer with opts, appends the entries in one batch and
// closes it.
func appendAll(t *testing.T, dir string, opts WriterOptions, entries []Entry) {
	t.Helper()
	w, err := OpenWriter(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(entries); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// writeLines writes raw lines (each given a trailing newline) as the segment
// named by first, bypassing the writer's checks, so a test can plant damage.
func writeLines(t *testing.T, dir string, first int64, lines ...[]byte) string {
	t.Helper()
	if err := os.MkdirAll(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	var data []byte
	for _, l := range lines {
		data = append(data, l...)
		data = append(data, '\n')
	}
	path := filepath.Join(dir, SegmentName(first))
	if err := os.WriteFile(path, data, fileMode); err != nil {
		t.Fatal(err)
	}
	return path
}

func encodeLine(t *testing.T, e Entry) []byte {
	t.Helper()
	line, _, err := Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	return line
}

// finish writes a correct sidecar for the segment named by first.
func finish(t *testing.T, dir string, first int64) {
	t.Helper()
	name := SegmentName(first)
	d, err := fileDigest(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSidecar(dir, name, d.hex); err != nil {
		t.Fatal(err)
	}
}

func appendRaw(t *testing.T, path string, data []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, fileMode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func seqs(entries []Entry) []int64 {
	out := make([]int64, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Seq)
	}
	return out
}

func equalSeqs(a []int64, first, n int64) bool {
	if int64(len(a)) != n {
		return false
	}
	for i, s := range a {
		if s != first+int64(i) {
			return false
		}
	}
	return true
}
