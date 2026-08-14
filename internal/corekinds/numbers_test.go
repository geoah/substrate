// This file is HAND-WRITTEN, like every other test in this package, and it is
// the one that runs INSIDE it: the number decoder's exactness lives in helpers
// no declaration reaches from outside, because no core kind declares a bound
// anywhere near the top of the int64 range. Testing them through a generated
// Decode would mean waiting for a declaration that may never be written, so they
// are tested where they are.
//
// What is being held: an integer that survives decoding must survive the
// COMPARISON too. Every step here rounds if it is allowed to, and a rounded
// comparison admits a value the declaration refuses — which then stores.
package corekinds

import (
	"encoding/json"
	"testing"
)

// TestIntegerBoundsCompareAsIntegers is the adversarial pair. Under a max of
// 9007199254740992, the value 9007199254740993 has no float64 of its own: it
// rounds DOWN to the bound and slips through a float comparison as if it were
// equal to it.
func TestIntegerBoundsCompareAsIntegers(t *testing.T) {
	const exact = 9007199254740992 // 2^53, the last integer float64 holds alone
	max := Bounds{Max: bound(exact)}
	if _, ok := (&decoder{}).integer("size", int64(exact+1), max); ok {
		t.Errorf("%d was admitted under a max of %d", int64(exact+1), int64(exact))
	}
	got, ok := (&decoder{}).integer("size", int64(exact), max)
	if !ok || got != exact {
		t.Errorf("the bound itself decoded as %d, %v", got, ok)
	}
	min := Bounds{Min: bound(exact)}
	if _, ok := (&decoder{}).integer("size", int64(exact-1), min); ok {
		t.Errorf("%d was admitted under a min of %d", int64(exact-1), int64(exact))
	}
	if got, ok := (&decoder{}).integer("size", int64(exact+1), min); !ok || got != exact+1 {
		t.Errorf("%d under a min of %d decoded as %d, %v", int64(exact+1), int64(exact), got, ok)
	}
}

// TestFractionalBoundsTightenToIntegers pins the rounding DIRECTION, which is the
// only one an integer value can have: nothing integral sits between 0.5 and 1, so
// `min: 0.5` refuses 0 and admits 1.
func TestFractionalBoundsTightenToIntegers(t *testing.T) {
	if !atLeast(1, 0.5) || atLeast(0, 0.5) {
		t.Error("a fractional lower bound does not round up")
	}
	if !atMost(1, 1.5) || atMost(2, 1.5) {
		t.Error("a fractional upper bound does not round down")
	}
	if !atLeast(-1, -1.5) || atLeast(-2, -1.5) {
		t.Error("a negative fractional lower bound does not round up")
	}
}

// TestBoundsOutsideInt64AnswerAboutTheWholeRange: a bound no int64 can reach is
// not a comparison. Converting it would overflow, and the answer is the same for
// every value.
func TestBoundsOutsideInt64AnswerAboutTheWholeRange(t *testing.T) {
	if atLeast(1<<62, 1e30) {
		t.Error("a lower bound above every int64 admitted one")
	}
	if !atMost(1<<62, 1e30) {
		t.Error("an upper bound above every int64 refused one")
	}
	if !atLeast(-(1 << 62), -1e30) {
		t.Error("a lower bound below every int64 refused one")
	}
	if atMost(-(1 << 62), -1e30) {
		t.Error("an upper bound below every int64 admitted one")
	}
}

// TestNumberSpellings holds the decoder to the spellings a json.Number arrives
// in. The write path reads one through a float (engine asFloat) and accepts an
// exponent form, so 1e3 is a legal way to have written 1000 and refusing it here
// would refuse a value the substrate admitted — while a large one must NOT go
// through that float, or the exactness above is undone at the parse instead of
// the comparison.
func TestNumberSpellings(t *testing.T) {
	cases := map[string]struct {
		want     int64
		admitted bool
	}{
		"1000":              {want: 1000, admitted: true},
		"1e3":               {want: 1000, admitted: true},
		"10e-1":             {want: 1, admitted: true},
		"9007199254740993":  {want: 9007199254740993, admitted: true},
		"-9007199254740993": {want: -9007199254740993, admitted: true},
		// Exact and integral, and still not an int64: refused rather than
		// truncated into one.
		"1e20": {admitted: false},
		// Not an integer in any spelling.
		"1.5":  {admitted: false},
		"1e-3": {admitted: false},
		"":     {admitted: false},
	}
	for spelling, want := range cases {
		t.Run(spelling, func(t *testing.T) {
			got, ok := (&decoder{}).integer("size", json.Number(spelling), Bounds{})
			if ok != want.admitted {
				t.Fatalf("%q admitted=%v, expected %v", spelling, ok, want.admitted)
			}
			if ok && got != want.want {
				t.Errorf("%q decoded as %d, expected %d", spelling, got, want.want)
			}
		})
	}
}
