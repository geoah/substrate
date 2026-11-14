package vocabulary

import (
	"fmt"
	"slices"
	"strings"
)

// A PACKAGE NAMES THE FUNCTION INTERFACE ITS CODE IS WRITTEN AGAINST
// (docs/functions.md#the-interface, decision record 0265). The interface is
// what a body is handed and what it calls back: the `input` (pinned by
// internal/runner/testdata/input.golden.json) and the `host` object. The
// header's `interface:` names the version, and a server that does not run
// that version refuses the package when it loads, on every door, rather than
// installing code that would fail at its first delivery.
//
// An absent key stores nothing and reads as 1, the interface every package
// written before the key was written against. The key is reserved by name
// (record 0020): it is in packageDataKeys alone, so an authority row and a
// bundle document refuse it.

// FunctionInterface is the newest function interface this server runs.
const FunctionInterface = 1

// FunctionInterfaces are the interface versions this server runs a body
// against. A version leaves this list only in a release that says so.
var FunctionInterfaces = []int64{FunctionInterface}

// parsePackageInterface reads the header's `interface:`. A version this
// server does not run is refused naming the ones it does, because the likely
// cause is a package written for a newer server than this one.
func (l *loader) parsePackageInterface(where string, d map[string]any, g *Package) {
	raw, present := d["interface"]
	if !present {
		return
	}
	v, ok := VersionValue(raw)
	if !ok || v < 1 {
		l.errf("%s: data.interface: must be a whole number of at least 1, the function interface the package's code is written against, not %v — or drop the key and the package reads as interface 1",
			where, raw)
		return
	}
	if !slices.Contains(FunctionInterfaces, v) {
		l.errf("%s: data.interface: the package's code is written against function interface %d, and this server runs %s — run a server that knows interface %d, or install a version of the package written against one this server runs",
			where, v, interfaceWords(), v)
		return
	}
	g.Interface = v
}

// interfaceWords spells the versions this server runs as a refusal names them.
func interfaceWords() string {
	words := make([]string, len(FunctionInterfaces))
	for i, v := range FunctionInterfaces {
		words[i] = fmt.Sprint(v)
	}
	if len(words) == 1 {
		return "interface " + words[0] + " alone"
	}
	last := len(words) - 1
	return "interfaces " + strings.Join(words[:last], ", ") + " and " + words[last]
}
