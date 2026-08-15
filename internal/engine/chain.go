package engine

// THE CHAIN.
//
// Every changelog entry carries a SHA-256 hash over its own content and the
// previous entry's hash, so an in-place edit, a reorder, an insert or a
// cross-repository splice breaks the chain at the first touched seq and a
// verifier names it. The chain is per repository; genesis links to 32 zero
// bytes. What the chain is NOT: a defense against a writer who holds full
// database access AND recomputes downstream hashes — without a signature the
// chain needs no secret. Signatures (signing.go) raise that bar to "database
// access AND the host credential key".
//
// The preimage hashes WHAT POSTGRES STORED, never what Go marshaled: payload
// is a jsonb column and Postgres re-renders it (key order, whitespace, number
// lexemes), so the write path reads the stored text back (RETURNING
// payload::text) and the verifier reads the same text later. Both sides pass
// that text through canonicalJSON, whose number form depends only on the
// numeric VALUE — the one thing Postgres numeric guarantees — never on how
// numeric_out happens to print it (trailing zeros, exponent shape), so a
// Postgres major upgrade cannot strand historical hashes.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// chainDomain versions the preimage: a future change to the frame is a new
// domain string, an explicit fork, never a silent one.
const chainDomain = "substrate/changelog/v1"

// epochDomain versions the chain-epoch preimage (chain_epochs rows).
const epochDomain = "substrate/chainepoch/v1"

// tsCanonical is the one timestamp form the frame carries: UTC, microseconds
// — the precision timestamptz stores and nowUTC() truncates to — so the
// value written and the value read back format identically.
const tsCanonical = "2006-01-02T15:04:05.000000Z07:00"

// chainEntry is one changelog row as the preimage sees it: every hashed
// column, with the payload as the STORED text.
type chainEntry struct {
	Seq        int64
	TS         time.Time
	Actor      string
	Op         string
	RecordID   string
	Kind       string
	CausedBy   int64
	CausedByOK bool
	// PayloadText is the jsonb column's own rendering (payload::text), never
	// the bytes Go sent.
	PayloadText []byte
}

// entryHash computes one entry's chain hash from the previous entry's.
func entryHash(repository string, e chainEntry, prev [32]byte) ([32]byte, error) {
	canon, err := canonicalJSON(e.PayloadText)
	if err != nil {
		return [32]byte{}, fmt.Errorf("substrate/engine: chain seq %d: canonicalize payload: %w", e.Seq, err)
	}
	var buf bytes.Buffer
	buf.WriteString(chainDomain)
	frameBytes(&buf, []byte(repository))
	frameInt64(&buf, e.Seq)
	frameBytes(&buf, []byte(e.TS.UTC().Format(tsCanonical)))
	frameBytes(&buf, []byte(e.Actor))
	frameBytes(&buf, []byte(e.Op))
	frameBytes(&buf, []byte(e.RecordID))
	frameBytes(&buf, []byte(e.Kind))
	// A presence byte keeps NULL and 0 distinct in the preimage: encoding
	// NULL as a bare zero would let an UPDATE flip one to the other without
	// moving the hash.
	frameOptionalInt64(&buf, e.CausedBy, e.CausedByOK)
	frameBytes(&buf, canon)
	buf.Write(prev[:])
	return sha256.Sum256(buf.Bytes()), nil
}

// chainEpoch is one recorded transition of a repository's chain: the backfill
// that started attested history, a reseal's sanctioned rewrite, or signing
// activation. It exists because a re-chained history is byte-indistinguishable
// from a tampered one; the epoch is what lets a verifier explain a remembered
// head that no longer matches.
type chainEpoch struct {
	At         time.Time
	Reason     string
	FromSeq    int64
	OldHead    []byte
	NewHead    []byte
	PublicKey  []byte
	SignedFrom int64
}

const (
	epochBackfill = "backfill"
	epochReseal   = "reseal"
	epochActivate = "activate"
)

// epochHash is the epoch's signing preimage.
func epochHash(repository string, ep chainEpoch) [32]byte {
	var buf bytes.Buffer
	buf.WriteString(epochDomain)
	frameBytes(&buf, []byte(repository))
	frameBytes(&buf, []byte(ep.At.UTC().Format(tsCanonical)))
	frameBytes(&buf, []byte(ep.Reason))
	frameInt64(&buf, ep.FromSeq)
	frameOptionalBytes(&buf, ep.OldHead)
	frameOptionalBytes(&buf, ep.NewHead)
	frameOptionalBytes(&buf, ep.PublicKey)
	frameOptionalInt64(&buf, ep.SignedFrom, ep.SignedFrom > 0)
	return sha256.Sum256(buf.Bytes())
}

// frameBytes writes one length-delimited field: a fixed big-endian uint32
// length, then the bytes. The widths are part of the format.
func frameBytes(buf *bytes.Buffer, b []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(b)))
	buf.Write(n[:])
	buf.Write(b)
}

func frameInt64(buf *bytes.Buffer, v int64) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(v))
	buf.Write(n[:])
}

func frameOptionalInt64(buf *bytes.Buffer, v int64, ok bool) {
	if !ok {
		buf.WriteByte(0)
		return
	}
	buf.WriteByte(1)
	frameInt64(buf, v)
}

func frameOptionalBytes(buf *bytes.Buffer, b []byte) {
	if b == nil {
		buf.WriteByte(0)
		return
	}
	buf.WriteByte(1)
	frameBytes(buf, b)
}

// --- the canonical form ----------------------------------------------------

// canonicalJSON renders one JSON text in the chain's canonical form: object
// keys sorted bytewise, strings re-escaped by encoding/json's one policy,
// numbers in the value-exact normal form below. It runs at write time on the
// text Postgres returned and at verify time on the text Postgres returns, so
// the two sides can only agree.
func canonicalJSON(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	// One value and nothing after it: trailing bytes would let two different
	// texts canonicalize to one.
	if dec.More() {
		return nil, fmt.Errorf("trailing data after the JSON value")
	}
	var buf bytes.Buffer
	if err := writeCanonical(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
		return fmt.Errorf("canonicalJSON: unexpected decoded type %T", v)
	}
	return nil
}

// canonicalNumber renders one JSON number lexeme in a value-exact normal
// form: sign, the significant digits with one leading digit before the point,
// and an explicit decimal exponent — `1.5`, `1.50` and `15e-1` all become
// "1.5E0" because they are the same value. Computed with integer string
// operations, never through a float, so nothing jsonb's arbitrary-precision
// numeric can hold is rounded. The form depends only on the VALUE: how a
// Postgres version prints the lexeme (display scale, exponent shape) cannot
// move the hash.
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
		// larger exponent cannot come out of the jsonb column this reads —
		// refusing it is a finding, never a rounding.
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
		// Every zero — 0, 0.00, -0, 0e9 — is the one value zero.
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

// zeroHash is the chain's genesis link.
var zeroHash [32]byte

// settleChain stamps the hash — and, when signing is active, the signature —
// of every entry this transaction appended, in seq order, chaining from the
// last committed entry's hash. It runs at commit (inTx), after settleFold
// has made the last payload final, under the changelog advisory lock the
// first append took, so the head it chains from cannot move underneath.
func (t *txn) settleChain() error {
	if len(t.pending) == 0 {
		return nil
	}
	prev := zeroHash
	if first := t.pending[0].Seq; first > 1 {
		var stored []byte
		if err := t.row(`SELECT hash FROM changelog WHERE seq = $1`, first-1).Scan(&stored); err != nil {
			return fmt.Errorf("substrate/engine: chain: read the head hash at seq %d: %w", first-1, err)
		}
		if len(stored) != 32 {
			return fmt.Errorf("substrate/engine: seq %d has no hash to chain from — the backfill has not run for this repository", first-1)
		}
		copy(prev[:], stored)
	}
	// A dataset that believes signing is off re-checks the durable mark here,
	// under the changelog lock: another process may have activated since this
	// dataset opened, and the guarantee is "no unsigned entry at or after the
	// mark", not "no unsigned entry this process knew to sign".
	signing := t.ds.signing()
	if signing.signedFrom == 0 {
		var err error
		if signing, err = t.ds.refreshSigning(t.ctx); err != nil {
			return fmt.Errorf("substrate/engine: chain: re-read the signing state: %w", err)
		}
	}
	repo := t.ds.scope.Repository
	for i := range t.pending {
		e := t.pending[i]
		h, err := entryHash(repo, e, prev)
		if err != nil {
			return err
		}
		var sig []byte
		if signing.signedFrom > 0 && e.Seq >= signing.signedFrom {
			if signing.key == nil {
				// Activation is one-way: a host that cannot sign refuses the
				// write rather than quietly appending an entry the guarantee
				// no longer covers.
				return fmt.Errorf("substrate/engine: changelog signing is active from seq %d but the signing key is unavailable (is SUBSTRATE_CREDENTIAL_KEY set?); refusing to append unsigned", signing.signedFrom)
			}
			sig = ed25519.Sign(signing.key, h[:])
		}
		res, err := t.exec(`UPDATE changelog SET hash = $2, sig = $3 WHERE seq = $1`, e.Seq, h[:], sig)
		if err != nil {
			return fmt.Errorf("substrate/engine: chain: stamp seq %d: %w", e.Seq, err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return fmt.Errorf("substrate/engine: chain: stamping seq %d touched %d rows", e.Seq, n)
		}
		prev = h
	}
	t.pending = nil
	return nil
}
