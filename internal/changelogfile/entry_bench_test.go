package changelogfile

import (
	"testing"
	"time"
)

// BenchmarkEncode is one entry rendered as its canonical line with its
// checksum, which every write pays per changelog entry before the commit.
func BenchmarkEncode(b *testing.B) {
	for _, size := range []struct {
		name  string
		bytes int
	}{{"small", 256}, {"4KiB", 4096}, {"64KiB", 64 << 10}} {
		b.Run(size.name, func(b *testing.B) {
			e := Entry{
				Seq: 42, TS: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC),
				Actor: "api", Principal: "k7abc", Op: "put",
				RecordID: "rec42", Kind: "ada.example.com/tasks/task",
				Txn: 42, Payload: benchPayload(42, size.bytes),
			}
			b.SetBytes(int64(len(e.Payload)))
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := Encode(e); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
