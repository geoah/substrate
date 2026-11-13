package changelogfile

// The line format's test vectors (docs/stored-format.md, "The line"). They
// are the format, written down: each entry, the exact line it encodes to and
// that line's checksum, and the canonical spelling of a set of numbers. A
// vector never changes once it is in testdata/vectors.json, because a
// directory written under it must read the same forever; a later format adds
// vectors beside them and raises LineFormat.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

type lineVectors struct {
	Lines []struct {
		Name  string `json:"name"`
		Entry struct {
			Seq       int64           `json:"seq"`
			TS        string          `json:"ts"`
			Actor     string          `json:"actor"`
			Principal string          `json:"principal"`
			Op        string          `json:"op"`
			RecordID  string          `json:"recordId"`
			Kind      string          `json:"kind"`
			CausedBy  *int64          `json:"causedBy"`
			Txn       int64           `json:"txn"`
			Payload   json.RawMessage `json:"payload"`
		} `json:"entry"`
		Line string `json:"line"`
		Sum  string `json:"sum"`
	} `json:"lines"`
	Numbers map[string]string `json:"numbers"`
}

func readVectors(t *testing.T) lineVectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v lineVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Lines) == 0 || len(v.Numbers) == 0 {
		t.Fatal("testdata/vectors.json holds no vectors")
	}
	return v
}

func TestTheLineVectors(t *testing.T) {
	t.Parallel()
	for _, v := range readVectors(t).Lines {
		t.Run(v.Name, func(t *testing.T) {
			ts, err := time.Parse(TSFormat, v.Entry.TS)
			if err != nil {
				t.Fatal(err)
			}
			e := Entry{
				Seq: v.Entry.Seq, TS: ts, Actor: v.Entry.Actor, Principal: v.Entry.Principal, Op: v.Entry.Op,
				RecordID: v.Entry.RecordID, Kind: v.Entry.Kind, Txn: v.Entry.Txn, Payload: v.Entry.Payload,
			}
			if v.Entry.CausedBy != nil {
				e.CausedBy, e.CausedByOK = *v.Entry.CausedBy, true
			}
			line, sum, err := Encode(e)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if string(line) != v.Line {
				t.Fatalf("the entry encodes to\n  %s\nand the format says\n  %s", line, v.Line)
			}
			if got := sumPrefix + hex.EncodeToString(sum[:]); got != v.Sum {
				t.Fatalf("checksum %s, the format says %s", got, v.Sum)
			}
			back, backSum, err := Decode([]byte(v.Line))
			if err != nil {
				t.Fatalf("decode the vector's line: %v", err)
			}
			if backSum != sum || back.Seq != e.Seq || !back.TS.Equal(e.TS) || back.Actor != e.Actor ||
				back.Principal != e.Principal || back.Op != e.Op || back.RecordID != e.RecordID || back.Kind != e.Kind ||
				back.Txn != e.Txn || back.CausedByOK != e.CausedByOK || back.CausedBy != e.CausedBy {
				t.Fatalf("the line decodes to %+v, want %+v", back, e)
			}
			canon, err := CanonicalJSON(v.Entry.Payload)
			if err != nil || !bytes.Equal(back.Payload, canon) {
				t.Fatalf("the payload decodes to %s, want the canonical %s (%v)", back.Payload, canon, err)
			}
		})
	}
}

func TestTheNumberVectors(t *testing.T) {
	t.Parallel()
	for in, want := range readVectors(t).Numbers {
		got, err := CanonicalJSON([]byte(in))
		if err != nil || string(got) != want {
			t.Errorf("%s spells %s (%v), the format says %s", in, got, err, want)
		}
	}
}

// A vector's line with a key the format lacks, or with any byte changed under
// its checksum, is refused.
func TestAVectorLineChangedIsRefused(t *testing.T) {
	t.Parallel()
	line := readVectors(t).Lines[0].Line
	extra := strings.Replace(line, `{"actor":`, `{"actorRef":"x","actor":`, 1)
	if _, _, err := Decode([]byte(extra)); err == nil || !strings.Contains(err.Error(), `unknown key "actorRef"`) {
		t.Fatalf("a line with a key the format lacks: %v", err)
	}
	edited := strings.Replace(line, `"Ship the fold"`, `"Ship the folds"`, 1)
	if _, _, err := Decode([]byte(edited)); !errors.Is(err, ErrBadSum) {
		t.Fatalf("a line edited under its checksum: %v, want ErrBadSum", err)
	}
}
