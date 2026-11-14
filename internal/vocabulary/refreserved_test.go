package vocabulary_test

import (
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// A reference value's keys beside its link properties are reserved: `ref`
// holds the referent's path, `target` is held for a referent a read may
// inline, and `repository` is held for the repository a referent lives in,
// once a reference can point into another one (the 1.0 review, #1313, F4).
// A link property may take none of the three names, and the refusal says
// why. A kind's own property may be called `repository`: the reservation is
// a key of the reference value, not a property name.
func TestALinkPropertyMayNotTakeAReservedName(t *testing.T) {
	for name, says := range map[string]string{
		vocabulary.ReferenceValueKey:        "the reserved key holding the referent's path",
		vocabulary.ReferenceTargetField:     "the reserved key holding the referent record",
		vocabulary.ReferenceRepositoryField: "the reserved key naming the repository the referent lives in",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadRef(t, "      type: reference\n      kind: ref.example.com/ref/person\n"+
				"      properties:\n        "+name+":\n          type: string\n")
			if err == nil {
				t.Fatalf("a link property named %q loaded", name)
			}
			want := `properties.person.properties.` + name + `: "` + name + `" is ` + says
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("the refusal = %v\nwant it to contain %q", err, want)
			}
		})
	}

	r, err := loadRef(t, "      type: reference\n      kind: ref.example.com/ref/person\n"+
		"      properties:\n        source:\n          type: string\n"+
		"    repository:\n      type: string\n")
	if err != nil {
		t.Fatalf("a kind property named repository, beside a link property of another name, is refused: %v", err)
	}
	m, _ := r.ByIdentity("ref.example.com/ref/membership")
	if _, ok := m.Prop("repository"); !ok {
		t.Fatal("the kind's own `repository` property did not load")
	}
}
