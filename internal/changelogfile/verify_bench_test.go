package changelogfile

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// benchPayload is a record-shaped payload of about size bytes: properties
// of every JSON type, a nested list, numbers the canonical form respells and
// text outside ASCII, so a line costs what a real one costs to check.
func benchPayload(seq int64, size int) json.RawMessage {
	notes := strings.Repeat("the rack layout moves to row 4; ", max(size/32, 1))
	payload := map[string]any{
		"props": map[string]any{
			"name":     fmt.Sprintf("task %d", seq),
			"done":     seq%2 == 0,
			"estimate": 1.50 + float64(seq%7),
			"budget":   json.Number("12.500e2"),
			"tags":     []any{"alpha", "beta", "γάμμα"},
			"notes":    notes,
			"assignee": map[string]any{"ref": fmt.Sprintf("ada.example.com/people/person/p%d", seq%13)},
		},
		"states":  map[string]any{"status": "open"},
		"version": seq,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return raw
}

// benchDir writes a changelog of n entries, each a transaction of its own
// with a payload of about payloadBytes, in segments of segmentBytes, and
// returns the directory and its size in bytes.
func benchDir(b *testing.B, n int64, payloadBytes int, segmentBytes int64) (string, int64) {
	b.Helper()
	dir := b.TempDir()
	w, err := OpenWriter(dir, WriterOptions{SegmentBytes: segmentBytes})
	if err != nil {
		b.Fatal(err)
	}
	const batch = 100
	entries := make([]Entry, 0, batch)
	for seq := int64(1); seq <= n; seq++ {
		entries = append(entries, Entry{
			Seq: seq, TS: time.Date(2026, 9, 29, 10, 0, 0, int(seq)*1000, time.UTC),
			Actor: "api", Principal: "k7abc", Op: "put",
			RecordID: fmt.Sprintf("rec%d", seq), Kind: "ada.example.com/tasks/task",
			Txn: seq, Payload: benchPayload(seq, payloadBytes),
		})
		if len(entries) == batch || seq == n {
			if err := w.Append(entries); err != nil {
				b.Fatal(err)
			}
			entries = entries[:0]
		}
	}
	if err := w.Close(); err != nil {
		b.Fatal(err)
	}
	segs, err := Segments(dir)
	if err != nil {
		b.Fatal(err)
	}
	var size int64
	for _, s := range segs {
		size += s.Size
	}
	return dir, size
}

// BenchmarkVerify is the whole check of a changelog directory: every
// finished segment against its sidecar and every line against its sum, the
// walk `repository verify` and the snapshot run (issue 761).
func BenchmarkVerify(b *testing.B) {
	const entries = 8000
	dir, size := benchDir(b, entries, 4096, 1<<20)
	b.SetBytes(size)
	b.ReportAllocs()
	for b.Loop() {
		rep, err := Verify(dir)
		if err != nil {
			b.Fatal(err)
		}
		if rep.Entries != entries {
			b.Fatalf("verified %d entries, want %d", rep.Entries, entries)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*entries), "ns/line")
}
