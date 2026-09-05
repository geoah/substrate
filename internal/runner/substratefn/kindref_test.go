package substratefn

// The SDK's kind grammar is a MIRROR of the engine's (vocabulary/naming.go and
// host.py's `_RE_KIND`, byte for byte), so a body learns its mistake where it
// made it instead of at admission. A reference is
// `{authority}/{package}/{name}` (decision record 0047), or the bare name of a
// kind in the caller's own package; the two-segment form every reference used
// to wear is refused, because a package cannot be inferred from it.

import "testing"

func TestValidKindTakesThePackageGrammar(t *testing.T) {
	for _, ok := range []string{
		"widget",
		"samples.substrate.reamde.dev/tasks/task",
		"acme.example.com/tools/widget2",
		"a.b/c/d",
	} {
		if err := validKind("put", ok); err != nil {
			t.Errorf("validKind(%q) = %v, want admitted", ok, err)
		}
	}
	for _, bad := range []string{
		"",
		// The retired two-segment form: an authority and a name, no package.
		"samples.substrate.reamde.dev/task",
		"acme.example.com/widget",
		// Four segments is a record path, not a kind.
		"acme.example.com/tools/widget/w1",
		// The package and the name are lowercase words, and the authority
		// carries a dot.
		"acme.example.com/Tools/widget",
		"acme.example.com/tools/Widget",
		"acme/tools/widget",
	} {
		if err := validKind("put", bad); err == nil {
			t.Errorf("validKind(%q) admitted a reference the engine refuses", bad)
		}
	}
}
