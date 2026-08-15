package engine

import (
	"strings"
	"testing"
	"time"
)

func TestCanonicalNumberValueExact(t *testing.T) {
	cases := map[string]string{
		// One value, many lexemes: the normal form ignores display scale and
		// exponent shape, which is what survives a Postgres upgrade.
		"1.5":     "1.5E0",
		"1.50":    "1.5E0",
		"15e-1":   "1.5E0",
		"0.15E1":  "1.5E0",
		"1000":    "1E3",
		"1e3":     "1E3",
		"10.00e2": "1E3",
		"0":       "0",
		"0.000":   "0",
		"-0":      "0",
		"0e9":     "0",
		"-1.5":    "-1.5E0",
		"0.05":    "5E-2",
		"5e-2":    "5E-2",
		// Beyond float64: 2^53+1 and long fractions keep every digit.
		"9007199254740993":         "9.007199254740993E15",
		"9007199254740993.0":       "9.007199254740993E15",
		"1.23456789012345678901":   "1.23456789012345678901E0",
		"123456789012345678901234": "1.23456789012345678901234E23",
		"42":                       "4.2E1",
		"-273.15":                  "-2.7315E2",
	}
	for lex, want := range cases {
		got, err := canonicalNumber(lex)
		if err != nil {
			t.Fatalf("canonicalNumber(%q): %v", lex, err)
		}
		if got != want {
			t.Errorf("canonicalNumber(%q) = %q, want %q", lex, got, want)
		}
	}
	for _, bad := range []string{"", ".", "e5", "1e", "abc", "1.2.3", "0x10"} {
		if _, err := canonicalNumber(bad); err == nil {
			t.Errorf("canonicalNumber(%q) accepted a non-number", bad)
		}
	}
}

func TestCanonicalJSONNormalizes(t *testing.T) {
	cases := map[string]string{
		// Key order, whitespace and number lexemes all normalize.
		`{"b":1, "a":2}`:          `{"a":2E0,"b":1E0}`,
		`{ "a" : [ 1.50, true ]}`: `{"a":[1.5E0,true]}`,
		`null`:                    `null`,
		`"x"`:                     `"x"`,
		`[]`:                      `[]`,
		`{}`:                      `{}`,
		`{"n":9007199254740993}`:  `{"n":9.007199254740993E15}`,
		`{"z":-0.0}`:              `{"z":0}`,
		// Unicode: escape variants collapse to the value; distinct code
		// points stay distinct; the escaping policy is encoding/json's one
		// (HTML-significant characters escape, both ends alike).
		`{"s":"caf\u00e9"}`: `{"s":"café"}`,
		`{"s":"café"}`:      `{"s":"café"}`,
		`{"s":"<"}`:         `{"s":"\u003c"}`,
		// Postgres numeric's whole exponent domain fits; nothing rounds.
		`{"e":1e130}`:  `{"e":1E130}`,
		`{"e":1e-130}`: `{"e":1E-130}`,
	}
	for in, want := range cases {
		got, err := canonicalJSON([]byte(in))
		if err != nil {
			t.Fatalf("canonicalJSON(%q): %v", in, err)
		}
		if string(got) != want {
			t.Errorf("canonicalJSON(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := canonicalJSON([]byte(`{} trailing`)); err == nil {
		t.Error("canonicalJSON accepted trailing data")
	}
	if _, err := canonicalJSON([]byte(``)); err == nil {
		t.Error("canonicalJSON accepted an empty text")
	}
}

func TestEntryHashPreimageInjective(t *testing.T) {
	ts := time.Date(2026, 8, 15, 10, 0, 0, 123456000, time.UTC)
	base := chainEntry{
		Seq: 7, TS: ts, Actor: "api", Op: "put", RecordID: "r1", Kind: "task",
		PayloadText: []byte(`{"a":1}`),
	}
	h0, err := entryHash("repo1", base, zeroHash)
	if err != nil {
		t.Fatal(err)
	}
	// Every field moves the hash, including the repository and the NULL/zero
	// caused_by distinction, and the same content twice gives the same hash.
	same, _ := entryHash("repo1", base, zeroHash)
	if h0 != same {
		t.Fatal("entryHash is not deterministic")
	}
	mutations := []chainEntry{}
	for _, mut := range []func(e *chainEntry){
		func(e *chainEntry) { e.Seq = 8 },
		func(e *chainEntry) { e.TS = ts.Add(time.Microsecond) },
		func(e *chainEntry) { e.Actor = "console" },
		func(e *chainEntry) { e.Principal = ""; e.PrincipalOK = true }, // NULL vs stored empty
		func(e *chainEntry) { e.Principal = "tok1"; e.PrincipalOK = true },
		func(e *chainEntry) { e.Op = "patch" },
		func(e *chainEntry) { e.RecordID = "r2" },
		func(e *chainEntry) { e.Kind = "note" },
		func(e *chainEntry) { e.CausedBy = 0; e.CausedByOK = true }, // NULL vs stored zero
		func(e *chainEntry) { e.CausedBy = 3; e.CausedByOK = true },
		func(e *chainEntry) { e.PayloadText = []byte(`{"a":2}`) },
	} {
		e := base
		mut(&e)
		mutations = append(mutations, e)
	}
	seen := map[[32]byte]int{h0: -1}
	for i, e := range mutations {
		h, err := entryHash("repo1", e, zeroHash)
		if err != nil {
			t.Fatal(err)
		}
		if prev, dup := seen[h]; dup {
			t.Errorf("mutation %d collides with %d", i, prev)
		}
		seen[h] = i
	}
	if h, _ := entryHash("repo2", base, zeroHash); h == h0 {
		t.Error("a cross-repository splice would not move the hash")
	}
	if h, _ := entryHash("repo1", base, [32]byte{1}); h == h0 {
		t.Error("the previous hash does not chain")
	}
	// Value-exact payloads: a lexeme rewrite that keeps the value keeps the
	// hash, by design.
	e := base
	e.PayloadText = []byte(`{"a": 1.0}`)
	if h, _ := entryHash("repo1", e, zeroHash); h != h0 {
		t.Error("a value-preserving payload re-rendering moved the hash")
	}
}

func TestFieldBoundariesDoNotSlide(t *testing.T) {
	// Length framing means two adjacent fields cannot trade bytes: the classic
	// concatenation splice ("ab","c") == ("a","bc") must not hold.
	ts := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	a := chainEntry{Seq: 1, TS: ts, Actor: "ab", Op: "c", PayloadText: []byte(`{}`)}
	b := chainEntry{Seq: 1, TS: ts, Actor: "a", Op: "bc", PayloadText: []byte(`{}`)}
	ha, err := entryHash("r", a, zeroHash)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := entryHash("r", b, zeroHash)
	if err != nil {
		t.Fatal(err)
	}
	if ha == hb {
		t.Error("adjacent fields slide: the framing is broken")
	}
}

func TestEpochHashMovesWithEveryField(t *testing.T) {
	at := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	head := make([]byte, 32)
	base := chainEpoch{At: at, Reason: epochBackfill, FromSeq: 1, NewHead: head}
	h0 := epochHash("repo1", base)
	for name, mut := range map[string]func(*chainEpoch){
		"reason":     func(e *chainEpoch) { e.Reason = epochReseal },
		"fromSeq":    func(e *chainEpoch) { e.FromSeq = 2 },
		"oldHead":    func(e *chainEpoch) { e.OldHead = make([]byte, 32) },
		"newHead":    func(e *chainEpoch) { e.NewHead = append([]byte{1}, head[1:]...) },
		"publicKey":  func(e *chainEpoch) { e.PublicKey = make([]byte, 32) },
		"signedFrom": func(e *chainEpoch) { e.SignedFrom = 5 },
		"at":         func(e *chainEpoch) { e.At = at.Add(time.Microsecond) },
	} {
		e := base
		mut(&e)
		if epochHash("repo1", e) == h0 {
			t.Errorf("epoch field %s does not move the hash", name)
		}
	}
	if epochHash("repo2", base) == h0 {
		t.Error("epoch repository does not move the hash")
	}
}

func TestTimestampCanonicalFormIsFixedWidth(t *testing.T) {
	// The frame's timestamp form must not drop trailing zeros the way
	// RFC3339Nano does: two timestamps must never render at two widths.
	for _, ts := range []time.Time{
		time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		time.Date(2026, 1, 2, 3, 4, 5, 100000000, time.UTC),
		time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC),
	} {
		got := ts.Format(tsCanonical)
		if len(got) != len("2026-01-02T03:04:05.000000Z") || !strings.HasSuffix(got, "Z") {
			t.Errorf("timestamp %v renders %q: not the fixed-width UTC microsecond form", ts, got)
		}
	}
}
