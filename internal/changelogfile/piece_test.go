package changelogfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// pieceLines encodes the entries for seqs first..last, one transaction per
// entry except where txns names a transaction's last seq for a seq.
func pieceLines(t *testing.T, first, last int64, txns map[int64]int64) []Line {
	t.Helper()
	var out []Line
	for seq := first; seq <= last; seq++ {
		txn := seq
		if v, ok := txns[seq]; ok {
			txn = v
		}
		line, _, err := Encode(Entry{
			Seq: seq, Txn: txn, TS: time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC),
			Actor: "api", Op: "put", Kind: "ada.example.com/tasks/task", RecordID: "r1",
			Payload: json.RawMessage(`{"created":true}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, Line{Seq: seq, Txn: txn, Bytes: line})
	}
	return out
}

func TestEncodeAndReadAPiece(t *testing.T) {
	lines := pieceLines(t, 5, 8, map[int64]int64{6: 7})
	data, err := EncodePiece(lines)
	if err != nil {
		t.Fatal(err)
	}
	var seen []int64
	if err := ReadPiece(bytes.NewReader(data), 5, 8, func(e Entry, line []byte, _ [32]byte) error {
		seen = append(seen, e.Seq)
		if !bytes.Equal(line, lines[e.Seq-5].Bytes) {
			t.Errorf("seq %d reads back as other bytes", e.Seq)
		}
		return nil
	}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(seen) != 4 {
		t.Fatalf("read %v", seen)
	}

	// A piece that would end inside a transaction, or skip a seq, is not
	// written.
	if _, err := EncodePiece(pieceLines(t, 5, 6, map[int64]int64{6: 7})); !errors.Is(err, ErrTxnIncomplete) {
		t.Fatalf("a piece ending inside a transaction: %v", err)
	}
	gap := append(pieceLines(t, 5, 5, nil), pieceLines(t, 7, 7, nil)...)
	if _, err := EncodePiece(gap); err == nil {
		t.Fatal("a piece with a gap was written")
	}

	nothing := func(Entry, []byte, [32]byte) error { return nil }
	damaged := map[string][]byte{
		"cut short":           data[:len(data)-1],
		"a line missing":      data[:bytes.IndexByte(data, '\n')+1],
		"another range":       data,
		"an edited line":      bytes.Replace(data, []byte(`"r1"`), []byte(`"r2"`), 1),
		"a line too many":     append(append([]byte{}, data...), data[:bytes.IndexByte(data, '\n')+1]...),
		"a line out of place": append(append([]byte{}, data[bytes.IndexByte(data, '\n')+1:]...), data[:bytes.IndexByte(data, '\n')+1]...),
	}
	for name, b := range damaged {
		first, last := int64(5), int64(8)
		if name == "another range" {
			last = 9
		}
		if err := ReadPiece(bytes.NewReader(b), first, last, nothing); !errors.Is(err, ErrPieceDamaged) {
			t.Errorf("%s: %v, want ErrPieceDamaged", name, err)
		}
	}
}
