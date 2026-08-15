package strictjson

import (
	"strings"
	"testing"
)

type embedded struct {
	ID string `json:"id"`
}

type sample struct {
	embedded
	Kinds  []string `json:"kinds,omitempty"`
	Filter string   `json:"filter"`
	hidden string   //nolint:unused // an unexported field is not a wire key
	Skip   string   `json:"-"`
}

// Keys is what a refusal quotes back at a caller, so it has to be the SAME set
// the decoder holds the input to: an embedded struct's keys flatten in, `-`
// and unexported fields stay out, and the tag name wins over the field name.
func TestKeysAreTheAcceptedWireKeys(t *testing.T) {
	got := strings.Join(Keys(sample{}), ",")
	if got != "filter,id,kinds" {
		t.Fatalf("Keys = %s", got)
	}
	if err := DecodeBytes([]byte(`{"skip":"x"}`), &sample{}, false); err == nil {
		t.Fatal("a key Keys does not list must not decode")
	}
	for _, k := range Keys(sample{}) {
		if err := DecodeBytes([]byte(`{"`+k+`":null}`), &sample{}, false); err != nil {
			t.Fatalf("Keys lists %q but the decoder refuses it: %v", k, err)
		}
	}
}

// A pointer, a slice and a slice of pointers all describe the same element
// shape: orderBy arrives as a []Order, and its refusal names Order's keys.
func TestKeysUnwrapsPointersAndSlices(t *testing.T) {
	want := "filter,id,kinds"
	for _, v := range []any{&sample{}, []sample{}, []*sample{}, &[]sample{}} {
		if got := strings.Join(Keys(v), ","); got != want {
			t.Fatalf("Keys(%T) = %s", v, got)
		}
	}
	if Keys("not a struct") != nil {
		t.Fatal("a non-struct has no wire keys")
	}
}
