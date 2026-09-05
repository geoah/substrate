// Package changelogfile is the on-disk form of a repository's changelog: one
// JSON object per line, each line carrying its own checksum, in segment files
// under `<data root>/repositories/<id>/changelog/`. The Postgres `changelog`
// table is the live index of the same entries; this package is what a backup
// copies and what a boot reads back. docs/plans/filesystem-changelog.md is the
// spec.
package changelogfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TSFormat is the one timestamp form a line carries: UTC, microseconds, the
// precision timestamptz stores, so the value written and the value read back
// format identically.
const TSFormat = "2006-01-02T15:04:05.000000Z07:00"

// sumPrefix names the digest in the `sum` field, so a later digest is a new
// prefix and never a silent change of meaning.
const sumPrefix = "sha256:"

// Entry is one changelog entry as the file carries it. Payload is the JSON
// text Postgres stored (`payload::text`); Encode canonicalizes it, so the
// caller never has to.
type Entry struct {
	Seq       int64
	TS        time.Time
	Actor     string
	Principal string
	Op        string
	RecordID  string
	Kind      string
	// CausedBy is the seq of the entry that caused this one; CausedByOK is
	// false where there is none (a direct write). The line carries the key
	// only when set.
	CausedBy   int64
	CausedByOK bool
	Payload    json.RawMessage
}

// ErrBadSum is returned by Decode when a line's `sum` does not match its
// content: the line was damaged or edited.
var ErrBadSum = errors.New("changelogfile: line checksum does not match")

// Encode renders one entry as its canonical line, without the trailing
// newline, and returns the 32-byte checksum the line carries in `sum`. The
// checksum is SHA-256 over the canonical encoding of the same object with the
// `sum` key absent.
func Encode(e Entry) (line []byte, sum [32]byte, err error) {
	obj, err := e.object()
	if err != nil {
		return nil, sum, err
	}
	var buf bytes.Buffer
	if err := writeCanonical(&buf, obj); err != nil {
		return nil, sum, err
	}
	sum = sha256.Sum256(buf.Bytes())
	obj["sum"] = sumPrefix + hex.EncodeToString(sum[:])
	buf.Reset()
	if err := writeCanonical(&buf, obj); err != nil {
		return nil, sum, err
	}
	return buf.Bytes(), sum, nil
}

// Decode parses one line, verifies its checksum and returns the entry and the
// checksum. The returned Payload is the canonical text the line carried.
func Decode(line []byte) (Entry, [32]byte, error) {
	var sum [32]byte
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return Entry{}, sum, fmt.Errorf("changelogfile: decode line: %w", err)
	}
	if dec.More() {
		return Entry{}, sum, errors.New("changelogfile: trailing data after the line's object")
	}
	var e Entry
	var sumText string
	for k, v := range raw {
		var err error
		switch k {
		case "seq":
			err = json.Unmarshal(v, &e.Seq)
		case "ts":
			var s string
			if err = json.Unmarshal(v, &s); err == nil {
				e.TS, err = time.Parse(TSFormat, s)
			}
		case "actor":
			err = json.Unmarshal(v, &e.Actor)
		case "principal":
			err = json.Unmarshal(v, &e.Principal)
		case "op":
			err = json.Unmarshal(v, &e.Op)
		case "recordId":
			err = json.Unmarshal(v, &e.RecordID)
		case "kind":
			err = json.Unmarshal(v, &e.Kind)
		case "causedBy":
			err = json.Unmarshal(v, &e.CausedBy)
			e.CausedByOK = err == nil
		case "payload":
			e.Payload = append(json.RawMessage(nil), v...)
		case "sum":
			err = json.Unmarshal(v, &sumText)
		default:
			return Entry{}, sum, fmt.Errorf("changelogfile: line carries an unknown key %q", k)
		}
		if err != nil {
			return Entry{}, sum, fmt.Errorf("changelogfile: decode %s: %w", k, err)
		}
	}
	if e.Seq < 1 {
		return Entry{}, sum, errors.New("changelogfile: line has no positive seq")
	}
	if e.Payload == nil {
		return Entry{}, sum, fmt.Errorf("changelogfile: seq %d has no payload", e.Seq)
	}
	if !strings.HasPrefix(sumText, sumPrefix) {
		return Entry{}, sum, fmt.Errorf("changelogfile: seq %d has no %s sum", e.Seq, sumPrefix)
	}
	claimed, err := hex.DecodeString(strings.TrimPrefix(sumText, sumPrefix))
	if err != nil || len(claimed) != 32 {
		return Entry{}, sum, fmt.Errorf("changelogfile: seq %d has a malformed sum", e.Seq)
	}
	_, got, err := Encode(e)
	if err != nil {
		return Entry{}, sum, err
	}
	if !bytes.Equal(got[:], claimed) {
		return Entry{}, sum, fmt.Errorf("%w at seq %d", ErrBadSum, e.Seq)
	}
	canon, err := CanonicalJSON(e.Payload)
	if err != nil {
		return Entry{}, sum, err
	}
	e.Payload = canon
	return e, got, nil
}

// object is the entry as the decoded tree writeCanonical renders, with the
// payload parsed so its keys sort and its numbers normalize with the rest.
func (e Entry) object() (map[string]any, error) {
	if e.TS.IsZero() {
		return nil, fmt.Errorf("changelogfile: seq %d has no timestamp", e.Seq)
	}
	payload, err := decodeCanonical(e.Payload)
	if err != nil {
		return nil, fmt.Errorf("changelogfile: seq %d: payload: %w", e.Seq, err)
	}
	obj := map[string]any{
		"seq":       e.Seq,
		"ts":        e.TS.UTC().Format(TSFormat),
		"actor":     e.Actor,
		"principal": e.Principal,
		"op":        e.Op,
		"recordId":  e.RecordID,
		"kind":      e.Kind,
		"payload":   payload,
	}
	if e.CausedByOK {
		obj["causedBy"] = e.CausedBy
	}
	return obj, nil
}

// CanonicalJSON renders one JSON text in the canonical form every line uses:
// object keys sorted bytewise, strings re-escaped by encoding/json's one
// policy, numbers in the value-exact normal form of canonicalNumber. The
// write path runs it on the text Postgres returned and a reader runs it on
// the text the file holds, so the two sides can only agree.
func CanonicalJSON(raw []byte) ([]byte, error) {
	v, err := decodeCanonical(raw)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := writeCanonical(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeCanonical(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	// One value and nothing after it: trailing bytes would let two different
	// texts canonicalize to one.
	if dec.More() {
		return nil, errors.New("trailing data after the JSON value")
	}
	return v, nil
}

func writeCanonical(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case string:
		enc, err := json.Marshal(x)
		if err != nil {
			return err
		}
		buf.Write(enc)
	case int64:
		// The entry's own integers (seq, causedBy) are plain decimals; only
		// payload numbers take the value-exact form, because only they come
		// back from a jsonb column that may respell them.
		buf.WriteString(strconv.FormatInt(x, 10))
	case json.Number:
		n, err := canonicalNumber(string(x))
		if err != nil {
			return err
		}
		buf.WriteString(n)
	case []any:
		buf.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			enc, err := json.Marshal(k)
			if err != nil {
				return err
			}
			buf.Write(enc)
			buf.WriteByte(':')
			if err := writeCanonical(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("canonical JSON: unexpected decoded type %T", v)
	}
	return nil
}

// canonicalNumber renders one JSON number lexeme in a value-exact normal
// form: sign, the significant digits with one leading digit before the point,
// and an explicit decimal exponent, so `1.5`, `1.50` and `15e-1` all become
// "1.5E0" because they are the same value. Computed with integer string
// operations, never through a float, so nothing jsonb's arbitrary-precision
// numeric can hold is rounded. The form depends only on the VALUE: how a
// Postgres version prints the lexeme cannot move the checksum.
func canonicalNumber(lex string) (string, error) {
	s := lex
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	mant, expPart, hasExp := strings.Cut(s, "e")
	if !hasExp {
		mant, expPart, hasExp = strings.Cut(s, "E")
	}
	var exp int64
	if hasExp {
		// 32-bit on purpose: Postgres numeric tops out around 1e131072, so a
		// larger exponent cannot come out of the jsonb column this reads.
		v, err := strconv.ParseInt(strings.TrimPrefix(expPart, "+"), 10, 32)
		if err != nil {
			return "", fmt.Errorf("number %q: exponent: %w", lex, err)
		}
		exp = v
	}
	ip, fp, _ := strings.Cut(mant, ".")
	digits := ip + fp
	if digits == "" {
		return "", fmt.Errorf("number %q has no digits", lex)
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return "", fmt.Errorf("number %q is not a JSON number", lex)
		}
	}
	// value = digits * 10^exp10, as an exact integer times a power of ten.
	exp10 := exp - int64(len(fp))
	digits = strings.TrimLeft(digits, "0")
	for len(digits) > 0 && digits[len(digits)-1] == '0' {
		digits = digits[:len(digits)-1]
		exp10++
	}
	if digits == "" {
		// Every zero (0, 0.00, -0, 0e9) is the one value zero.
		return "0", nil
	}
	adjusted := exp10 + int64(len(digits)) - 1
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	b.WriteByte(digits[0])
	if len(digits) > 1 {
		b.WriteByte('.')
		b.WriteString(digits[1:])
	}
	b.WriteByte('E')
	b.WriteString(strconv.FormatInt(adjusted, 10))
	return b.String(), nil
}
