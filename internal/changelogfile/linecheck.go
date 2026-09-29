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
	"time"
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

// The keys a canonical line opens with, in the order they sort, up to the
// tail; `causedBy` is there only when the entry has a cause.
var (
	actorKey     = []byte(`{"actor":`)
	causedByKey  = []byte(`,"causedBy":`)
	kindKey      = []byte(`,"kind":`)
	opKey        = []byte(`,"op":`)
	payloadKey   = []byte(`,"payload":`)
	principalKey = []byte(`,"principal":`)
	recordIDKey  = []byte(`,"recordId":`)
)

// lineTail is what parseTail read off a line.
type lineTail struct {
	seq, txn int64
	sum      [32]byte
	// seqAt is the comma before `"seq"`: line[:seqAt] is every key before
	// the tail (parseHead).
	seqAt int
	// sumAt and tsAt bound the `"sum":"sha256:<hex>",` pair: line[sumAt:tsAt]
	// is what Encode inserted after hashing the rest.
	sumAt, tsAt int
	// ts is the timestamp's text, between its quotes.
	ts []byte
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
	t.seq, t.txn, t.seqAt, t.sumAt, t.tsAt = seq, txn, j-len(seqKey)-1, sumAt, tsAt
	t.ts = line[open+1 : comma-1]
	return t, true
}

// parseHead holds line[:end], everything before the tail, to the keys Encode
// writes there and in its order: `actor`, `causedBy` when set, `kind`, `op`,
// `payload`, `principal` and `recordId`, with a string, an integer, two
// strings, one JSON value and two strings. A key Decode does not know, a
// repeated one or a missing one fails it, and the caller decodes the line:
// a line a newer writer added a key to is refused as Decode refuses it, even
// though its bytes hash to its sum.
func parseHead(line []byte, end int) bool {
	i, ok := expect(line, 0, actorKey, true)
	if i, ok = skipString(line, i, ok); !ok {
		return false
	}
	if bytes.HasPrefix(line[i:], causedByKey) {
		if i = skipInt(line, i+len(causedByKey)); i < 0 {
			return false
		}
	}
	i, ok = expect(line, i, kindKey, ok)
	i, ok = skipString(line, i, ok)
	i, ok = expect(line, i, opKey, ok)
	i, ok = skipString(line, i, ok)
	i, ok = expect(line, i, payloadKey, ok)
	i, ok = skipValue(line, i, ok)
	i, ok = expect(line, i, principalKey, ok)
	i, ok = skipString(line, i, ok)
	i, ok = expect(line, i, recordIDKey, ok)
	i, ok = skipString(line, i, ok)
	return ok && i == end
}

// expect steps over key at line[i:], when ok and the key is there.
func expect(line []byte, i int, key []byte, ok bool) (int, bool) {
	if !ok || i < 0 || !bytes.HasPrefix(line[i:], key) {
		return i, false
	}
	return i + len(key), true
}

// skipString steps over the JSON string at line[i:], escapes included; a
// control byte inside it, or no closing quote, fails it.
func skipString(line []byte, i int, ok bool) (int, bool) {
	if !ok || i >= len(line) || line[i] != '"' {
		return i, false
	}
	for j := i + 1; j < len(line); j++ {
		switch c := line[j]; {
		case c == '\\':
			j++
		case c == '"':
			return j + 1, true
		case c < 0x20:
			return i, false
		}
	}
	return i, false
}

// skipInt steps over the integer at line[i:], -1 when there is none.
func skipInt(line []byte, i int) int {
	if i < len(line) && line[i] == '-' {
		i++
	}
	j := i
	for j < len(line) && line[j] >= '0' && line[j] <= '9' {
		j++
	}
	if j == i {
		return -1
	}
	return j
}

// skipValue steps over the JSON value at line[i:]: a string, an object or an
// array to its matching close, or a number or literal to the byte that ends
// it. It finds where the value ends and nothing more; whether the value is
// canonical is what the checksum holds.
func skipValue(line []byte, i int, ok bool) (int, bool) {
	if !ok || i >= len(line) {
		return i, false
	}
	switch line[i] {
	case '"':
		return skipString(line, i, true)
	case '{', '[':
		depth := 0
		for j := i; j < len(line); j++ {
			switch line[j] {
			case '"':
				end, ok := skipString(line, j, true)
				if !ok {
					return i, false
				}
				j = end - 1
			case '{', '[':
				depth++
			case '}', ']':
				if depth--; depth == 0 {
					return j + 1, true
				}
			}
		}
		return i, false
	default:
		j := i
		for j < len(line) && !endsScalar(line[j]) {
			j++
		}
		return j, j > i
	}
}

// endsScalar reports whether c ends a number or a literal.
func endsScalar(c byte) bool {
	switch c {
	case ',', '}', ']', ' ', '\t', '\r', '\n':
		return true
	}
	return false
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
// line laid out as Encode lays one out, key for key, with a timestamp in
// TSFormat, whose bytes with the sum pair cut out hash to its sum, is
// verified without decoding it; every other line is decoded, and Decode's
// verdict and error are the answer.
//
// The cut holds the bytes to the sum and does not decode the values: a line
// rewritten with a payload that is not canonical JSON, or a string with an
// escape Go would not write, and its sum recomputed over those bytes, passes
// here and is refused by Decode. Only a writer that rewrites a line and its
// sum together makes one: this binary's writer hashes what it encoded, and
// the checksum is not held against a writer that rewrites both: nothing
// signs it (docs/changelog.md), so it catches corruption, not tampering.
func (c *lineChecker) check(line []byte) (seq, txn int64, sum [32]byte, err error) {
	if t, ok := parseTail(line); ok && parseHead(line, t.seqAt) && checkTxn(t.seq, t.txn) == nil && validTS(t.ts) {
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

// validTS reports whether ts, a timestamp's text with no escape in it,
// parses as Decode parses it.
func validTS(ts []byte) bool {
	if bytes.IndexByte(ts, '\\') >= 0 {
		return false
	}
	_, err := time.Parse(TSFormat, string(ts))
	return err == nil
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
