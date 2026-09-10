package vocabulary

// ONE RUNTIME ENUM, TWO SPELLINGS. This package carries the enum as Go: the
// values `FunctionRuntimes` names, the map the loader admits from, and
// `retiredFunctionRuntimes`. The `function` kind carries the same enum as a
// declaration, which is what every admission door reads off the stored row
// (decision record 0055). A value in one and not the other binds on one door
// and not the next: an admitted runtime the declaration refuses is a body the
// loader takes and the record write rejects, and a retirement one side forgot
// is a value refused here and admitted there. So this loads the shipped kind
// and holds both lists equal.

import (
	"reflect"
	"sort"
	"testing"

	"github.com/geoah/substrate/kinds"
)

func TestShippedFunctionRuntimesMatchThisPackage(t *testing.T) {
	r, err := LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed off the kinds embed: %v", err)
	}
	fn, ok := r.ByIdentity(PackageCore + "/function")
	if !ok {
		t.Fatalf("the seed declares no %s/function", PackageCore)
	}
	runtime := fn.Props["runtime"]
	if runtime == nil {
		t.Fatal("the shipped function kind declares no `runtime` property")
	}

	// The admitted values, in order: `FunctionRuntimes` is the order the
	// errors name them in and the declaration lists them the same way, so a
	// reader comparing the two reads one list.
	declared := make([]string, 0, len(runtime.Values))
	for _, v := range runtime.Values {
		declared = append(declared, v.Value)
	}
	if !reflect.DeepEqual(declared, FunctionRuntimes) {
		t.Errorf("the function kind admits %v, this package admits %v", declared, FunctionRuntimes)
	}
	// functionRuntimes is the door the loader checks; FunctionRuntimes is only
	// the error text. They cover the same set or one of them is decoration.
	if len(functionRuntimes) != len(FunctionRuntimes) {
		t.Errorf("functionRuntimes admits %d runtimes, FunctionRuntimes names %d",
			len(functionRuntimes), len(FunctionRuntimes))
	}
	for _, name := range FunctionRuntimes {
		if !functionRuntimes[name] {
			t.Errorf("FunctionRuntimes names %q, which functionRuntimes does not admit", name)
		}
	}

	// The spent values.
	refused := make([]string, 0, len(retiredFunctionRuntimes))
	for name := range retiredFunctionRuntimes {
		refused = append(refused, name)
	}
	sort.Strings(refused)
	retired := append([]string{}, fn.Retired.Values["runtime"]...)
	sort.Strings(retired)
	if len(retired) == 0 {
		t.Fatalf("the function kind retires no runtime value; the loader refuses %v", refused)
	}
	if !reflect.DeepEqual(retired, refused) {
		t.Fatalf("the function kind retires %v, the loader refuses %v", retired, refused)
	}
	// A retired value the enum still admits is the contradiction retired.go
	// refuses on any declaration; assert it here too, because this is the one
	// place both spellings are in hand.
	for _, name := range refused {
		if functionRuntimes[name] {
			t.Errorf("runtime %q is retired and still in the enum", name)
		}
	}
}
