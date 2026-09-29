package changelogfile

// A line's checksum is SHA-256 over the canonical encoding of its entry with
// the `sum` key absent (Encode). Decode checks it the long way: it decodes
// the line, canonicalizes the payload and encodes the entry again, which is
// about a millisecond a line and most of what a walk of a long history costs
// (issue 761). A line the writer wrote IS that canonical encoding with the
// `sum` pair inserted, and `sum` sorts between `seq` and `ts`, so the bytes
// the checksum covers are the line with that pair cut out. lineChecker hashes
// those bytes directly and reads seq and txn from the line's tail; Decode
// stays the verdict for every line the cut does not fit or whose bytes do not
// hash to its sum, so a line is refused exactly when Decode refuses it.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"hash"
)

// The keys a canonical line ends with, in the order they sort: `seq`, `sum`,
// `ts` and `txn` are the last four, and their values are an integer, a
// prefixed hex digest, a timestamp with no quote in it and an integer, so a
// reader finds them from the end of the line without parsing what is before.
var (
	seqKey = []byte(`"seq":`)
	sumKey = []byte(`"sum":"` + sumPrefix)
	tsKey  = []byte(`"ts":`)
	txnKey = []byte(`,"txn":`)
)

// lineTail is what parseTail read off a line.
type lineTail struct {
	seq, txn int64
	sum      [32]byte
	// sumAt and tsAt bound the `"sum":"sha256:<hex>",` pair: line[sumAt:tsAt]
	// is what Encode inserted after hashing the rest.
	sumAt, tsAt int
}

// parseTail reads seq, sum and txn off the end of a line laid out as Encode
// lays one out: `…,"seq":N,"sum":"sha256:<hex>","ts":"<ts>","txn":M}`. It
// reports false for anything else, and the caller decodes the line instead.
func parseTail(line []byte) (lineTail, bool) {
	var t lineTail
	n := len(line)
	if n == 0 || line[n-1] != '}' {
		return t, false
	}
	// `,"txn":M}`
	i := digitsBefore(line, n-1)
	txn, ok := canonicalInt(line[i : n-1])
	if !ok || !bytes.HasSuffix(line[:i], txnKey) {
		return t, false
	}
	comma := i - len(txnKey)
	// `,"ts":"<ts>"` before that comma: a string with no quote in it.
	if comma < 1 || line[comma-1] != '"' {
		return t, false
	}
	open := bytes.LastIndexByte(line[:comma-1], '"')
	tsAt := open - len(tsKey)
	if open < 0 || tsAt < 1 || !bytes.Equal(line[tsAt:open], tsKey) || line[tsAt-1] != ',' {
		return t, false
	}
	// `,"sum":"sha256:<64 hex>"` before the comma that precedes `"ts"`.
	sumAt := tsAt - 2 - 2*sha256.Size - len(sumKey)
	if sumAt < 1 || line[tsAt-2] != '"' || line[sumAt-1] != ',' || !bytes.Equal(line[sumAt:sumAt+len(sumKey)], sumKey) {
		return t, false
	}
	if _, err := hex.Decode(t.sum[:], line[sumAt+len(sumKey):tsAt-2]); err != nil {
		return t, false
	}
	// `,"seq":N` before the comma that precedes `"sum"`.
	j := digitsBefore(line, sumAt-1)
	seq, ok := canonicalInt(line[j : sumAt-1])
	if !ok || j < len(seqKey)+1 || !bytes.Equal(line[j-len(seqKey):j], seqKey) || line[j-len(seqKey)-1] != ',' {
		return t, false
	}
	t.seq, t.txn, t.sumAt, t.tsAt = seq, txn, sumAt, tsAt
	return t, true
}

// digitsBefore is the index of the first of the decimal digits that end at
// line[end-1]; end itself when there are none.
func digitsBefore(line []byte, end int) int {
	i := end
	for i > 0 && line[i-1] >= '0' && line[i-1] <= '9' {
		i--
	}
	return i
}

// canonicalInt parses a positive integer spelled as strconv.FormatInt spells
// one: digits with no leading zero. Eighteen digits at most, so it cannot
// overflow; a longer seq is not one a line holds, and the caller decodes the
// line instead.
func canonicalInt(b []byte) (int64, bool) {
	if len(b) == 0 || len(b) > 18 || b[0] == '0' {
		return 0, false
	}
	var v int64
	for _, c := range b {
		v = v*10 + int64(c-'0')
	}
	return v, true
}

// lineChecker verifies lines' checksums, holding the hasher it reuses from
// one line to the next. One goroutine owns one.
type lineChecker struct {
	h   hash.Hash
	out []byte
}

func newLineChecker() *lineChecker {
	return &lineChecker{h: sha256.New(), out: make([]byte, 0, sha256.Size)}
}

// check verifies one line's checksum and returns its seq, txn and sum. A
// canonical line whose bytes, with the sum pair cut out, hash to its sum is
// verified without decoding it; every other line is decoded, and Decode's
// verdict and error are the answer.
func (c *lineChecker) check(line []byte) (seq, txn int64, sum [32]byte, err error) {
	if t, ok := parseTail(line); ok && checkTxn(t.seq, t.txn) == nil {
		c.h.Reset()
		c.h.Write(line[:t.sumAt])
		c.h.Write(line[t.tsAt:])
		c.out = c.h.Sum(c.out[:0])
		if bytes.Equal(c.out, t.sum[:]) {
			return t.seq, t.txn, t.sum, nil
		}
	}
	e, sum, err := Decode(line)
	if err != nil {
		return 0, 0, sum, err
	}
	return e.Seq, e.Txn, sum, nil
}

// lineSum reads a line's seq and sum without verifying either: from the tail
// when the line is laid out as Encode lays one out, else by decoding it. It
// is for a line a verified walk has already checked.
func lineSum(line []byte) (int64, [32]byte, error) {
	if t, ok := parseTail(line); ok {
		return t.seq, t.sum, nil
	}
	e, sum, err := Decode(line)
	if err != nil {
		return 0, sum, err
	}
	return e.Seq, sum, nil
}
