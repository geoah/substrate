package changelogfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// linecheckEntries are entries whose lines stress the cut: a cause and none,
// a payload that itself carries `seq`, `sum`, `ts` and `txn` keys and a
// string that looks like a sum, escapes, numbers the canonical form
// respells, and a transaction of several lines.
func linecheckEntries() []Entry {
	caused := entryAt(40)
	caused.CausedBy, caused.CausedByOK = 12, true
	nested := entryAt(41)
	nested.Payload = json.RawMessage(`{"txn":9,"ts":"x","sum":"sha256:` + fmt.Sprintf("%064x", 7) + `","seq":3,"z":[1.50,"}"]}`)
	escaped := entryAt(42)
	escaped.Payload = json.RawMessage(`{"quote":"a \"b\" c","tab":"\t","html":"<&>","greek":"γάμμα","n":-0.0e0}`)
	escaped.Actor, escaped.RecordID = `agent:"odd"`, "rec,42"
	inTxn := entryAt(43)
	inTxn.Txn = 45
	big := entryAt(123456789012345)
	big.Txn = big.Seq
	return []Entry{entryAt(1), caused, nested, escaped, inTxn, big}
}

// A line Encode wrote is checked by the cut: parseTail finds its seq, txn
// and sum, and the line with the sum pair cut out is what the sum hashes.
func TestParseTailReadsWhatEncodeWrote(t *testing.T) {
	for _, e := range linecheckEntries() {
		line, sum, err := Encode(e)
		if err != nil {
			t.Fatal(err)
		}
		tail, ok := parseTail(line)
		if !ok {
			t.Fatalf("seq %d: the tail of an encoded line does not parse: %s", e.Seq, line)
		}
		if tail.seq != e.Seq || tail.txn != e.Txn || tail.sum != sum {
			t.Fatalf("seq %d: tail = %+v, want seq %d txn %d", e.Seq, tail, e.Seq, e.Txn)
		}
		cut := append(append([]byte(nil), line[:tail.sumAt]...), line[tail.tsAt:]...)
		if sha256.Sum256(cut) != sum {
			t.Fatalf("seq %d: the line without its sum pair is not what the sum hashes: %s", e.Seq, cut)
		}
		seq, txn, got, err := newLineChecker().check(line)
		if err != nil || seq != e.Seq || txn != e.Txn || got != sum {
			t.Fatalf("seq %d: check = %d %d %x %v", e.Seq, seq, txn, got, err)
		}
	}
}

// The cut and Decode agree on every line with one byte changed anywhere in
// it: both refuse it, or both accept it with the same seq, txn and sum. A
// change the cut cannot see (the case of a hex digit in the sum) is one
// Decode accepts too.
func TestLineCheckerAgreesWithDecodeOnEveryByteFlip(t *testing.T) {
	lc := newLineChecker()
	for _, e := range linecheckEntries() {
		line, _, err := Encode(e)
		if err != nil {
			t.Fatal(err)
		}
		for i := range line {
			for _, bit := range []byte{0x01, 0x20} {
				damaged := bytes.Clone(line)
				damaged[i] ^= bit
				seq, txn, sum, err := lc.check(damaged)
				want, wantSum, wantErr := Decode(damaged)
				if (err == nil) != (wantErr == nil) {
					t.Fatalf("seq %d, byte %d ^ %#x: check err = %v, Decode err = %v\n%s", e.Seq, i, bit, err, wantErr, damaged)
				}
				if err == nil && (seq != want.Seq || txn != want.Txn || sum != wantSum) {
					t.Fatalf("seq %d, byte %d ^ %#x: check = %d %d %x, Decode = %d %d %x", e.Seq, i, bit, seq, txn, sum, want.Seq, want.Txn, wantSum)
				}
				if err != nil && err.Error() != wantErr.Error() {
					t.Fatalf("seq %d, byte %d ^ %#x: check err = %v, Decode err = %v", e.Seq, i, bit, err, wantErr)
				}
			}
		}
	}
}

// A line that is not laid out as Encode lays one out, but whose content is
// intact, is what the cut cannot check: Decode does, and accepts it.
func TestLineCheckerFallsBackToDecode(t *testing.T) {
	e := entryAt(7)
	line, sum, err := Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil {
		t.Fatal(err)
	}
	// The same object, pretty-printed: every key and value intact, no byte
	// where Encode puts it.
	pretty, err := json.MarshalIndent(obj, "", "")
	if err != nil {
		t.Fatal(err)
	}
	pretty = bytes.ReplaceAll(pretty, []byte("\n"), []byte(" "))
	if _, ok := parseTail(pretty); ok {
		t.Fatalf("the tail of a respelled line parsed: %s", pretty)
	}
	seq, txn, got, err := newLineChecker().check(pretty)
	if err != nil || seq != 7 || txn != 7 || got != sum {
		t.Fatalf("check = %d %d %x %v", seq, txn, got, err)
	}
	// And a respelled line whose content changed is refused, as Decode
	// refuses it.
	changed := bytes.Replace(pretty, []byte(`"api"`), []byte(`"apj"`), 1)
	if _, _, _, err := newLineChecker().check(changed); !errors.Is(err, ErrBadSum) {
		t.Fatalf("a changed respelled line: err = %v, want ErrBadSum", err)
	}
}

// parseTail refuses what is not the end of an encoded line, so the caller
// decodes it.
func TestParseTailRefusesOtherShapes(t *testing.T) {
	line := encodeLine(t, entryAt(5))
	for name, l := range map[string][]byte{
		"empty":            nil,
		"no brace":         line[:len(line)-1],
		"leading zero seq": bytes.Replace(line, []byte(`"seq":5,`), []byte(`"seq":05,`), 1),
		"negative txn":     bytes.Replace(line, []byte(`"txn":5}`), []byte(`"txn":-5}`), 1),
		"short sum":        bytes.Replace(line, []byte(`"sum":"sha256:`), []byte(`"sum":"sha256:0`), 1),
		"seq not a number": bytes.Replace(line, []byte(`"seq":5,`), []byte(`"seq":"5",`), 1),
		"object only":      []byte(`{}`),
		"txn nineteen digits": bytes.Replace(line, []byte(`"txn":5}`),
			[]byte(`"txn":1234567890123456789}`), 1),
	} {
		if _, ok := parseTail(l); ok {
			t.Errorf("%s: parsed %s", name, l)
		}
	}
}
