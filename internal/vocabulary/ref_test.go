package vocabulary

import "testing"

func TestATraitBindingNamesItsTraitWhateverTheVariant(t *testing.T) {
	for entry, want := range map[string]string{
		"substrate.reamde.dev/core/temporal":               "substrate.reamde.dev/core/temporal",
		"substrate.reamde.dev/core/temporal(range)":        "substrate.reamde.dev/core/temporal",
		"substrate.reamde.dev/core/temporal(point: dueAt)": "substrate.reamde.dev/core/temporal",
		// How the arguments are spelled is the loader's to hold, not this.
		"substrate.reamde.dev/core/temporal(point: due_at)": "substrate.reamde.dev/core/temporal",
		"  pantry.example/kitchen/perishable  ":             "pantry.example/kitchen/perishable",
	} {
		if got, ok := TraitBindingRef(entry); !ok || got != want {
			t.Errorf("TraitBindingRef(%q) = %q, %v; want %q", entry, got, ok, want)
		}
	}
	// A bare word names no trait a loaded declaration binds (decision 0098),
	// and a malformed entry names none at all.
	for _, entry := range []string{"temporal", "temporal(range)", "", "(range)", "substrate.reamde.dev/core/*", "not a binding"} {
		if got, ok := TraitBindingRef(entry); ok {
			t.Errorf("TraitBindingRef(%q) = %q, want no trait", entry, got)
		}
	}
}
