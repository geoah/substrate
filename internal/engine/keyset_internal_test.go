package engine

import (
	"strings"
	"testing"
)

// TestListSQLHasNoOffset pins the keyset shape: a LIMIT, an
// ORDER BY, the aliased key-capture columns, and NEVER an OFFSET — a deep page
// must not pay O(offset).
func TestListSQLHasNoOffset(t *testing.T) {
	terms := []orderTerm{{expr: "created_at", desc: true}, {expr: "id", desc: true}}
	order := renderOrder(terms)
	keyCols := []string{`(created_at)::text AS __k0`, `(id)::text AS __k1`}
	sql := listSQL("TRUE", keyCols, order, "$1")
	if strings.Contains(strings.ToUpper(sql), "OFFSET") {
		t.Fatalf("list SQL carries an OFFSET: %s", sql)
	}
	for _, want := range []string{"ORDER BY", "LIMIT $1", "__k0", "__k1"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("list SQL missing %q: %s", want, sql)
		}
	}
}

func TestRenderOrder(t *testing.T) {
	got := renderOrder([]orderTerm{{expr: "at", desc: true}, {expr: "id", desc: true}})
	want := "at DESC NULLS LAST, id DESC"
	if got != want {
		t.Fatalf("renderOrder = %q, want %q", got, want)
	}
	got = renderOrder([]orderTerm{{expr: "created_at", desc: false}, {expr: "id", desc: false}})
	want = "created_at ASC NULLS LAST, id ASC"
	if got != want {
		t.Fatalf("renderOrder asc = %q, want %q", got, want)
	}
}

// TestSeekPredicate exercises the lexicographic seek: the strictly-after cut
// for a two-key DESC order, and the NULLS LAST handling on a null cursor key.
func TestSeekPredicate(t *testing.T) {
	terms := []orderTerm{{expr: "at", desc: true}, {expr: "id", desc: true}}
	at := "2026-08-09T00:00:00Z"
	id := "p5"

	b := &builder{}
	pred := seekPredicate(b, terms, []*string{&at, &id})
	// First disjunct: strictly-past on `at` (DESC → `<`), admitting nulls last.
	if !strings.Contains(pred, "at < $1 OR at IS NULL") {
		t.Fatalf("seek missing at-cut: %s", pred)
	}
	// Second disjunct: equal on `at`, then strictly-past on the id tiebreak.
	if !strings.Contains(pred, "at = $2 AND id < $3") {
		t.Fatalf("seek missing id tiebreak: %s", pred)
	}

	// A NULL leading key: NULLS LAST means nothing is strictly after it on that
	// key, so the first disjunct is dropped and only the equal-prefix (`at IS
	// NULL`) + id cut remains.
	b = &builder{}
	pred = seekPredicate(b, terms, []*string{nil, &id})
	if strings.Contains(pred, "at <") {
		t.Fatalf("null leading key must not emit an at-cut: %s", pred)
	}
	if !strings.Contains(pred, "at IS NULL AND id < $1") {
		t.Fatalf("null leading key seek = %s", pred)
	}
}

// TestKeysetRoundTrip pins the opaque token: it round-trips the resolved order
// signature and the key values (nil = NULL), and a corrupt token is rejected.
func TestKeysetRoundTrip(t *testing.T) {
	v := "hello"
	tok := encodeKeyset("created_at DESC NULLS LAST, id DESC", []*string{&v, nil}, 42)
	got, err := decodeKeyset(tok)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.O != "created_at DESC NULLS LAST, id DESC" || len(got.K) != 2 {
		t.Fatalf("token = %+v", got)
	}
	if got.K[0] == nil || *got.K[0] != "hello" || got.K[1] != nil {
		t.Fatalf("token keys = %v", got.K)
	}
	// The first page's head rides the cursor (codex regress #3) so every page of
	// one walk reports the same head.
	if got.H != 42 {
		t.Fatalf("token head = %d, want 42", got.H)
	}
	if _, err := decodeKeyset("!!!not base64!!!"); err == nil {
		t.Fatalf("expected a bad-cursor error")
	}
}
