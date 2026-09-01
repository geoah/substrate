package vocabulary_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

// The keys a reference gained when it absorbed the edge: `mustExist`, the
// `properties:` block that turns a value into an object, and `subject:`.

// refDocs is one authority holding a person kind and a membership kind whose
// `person` property is filled in per case.
func refDocs(person string) string {
	return `kind: core.substrate.reamde.dev/authority
metadata:
  id: ref.example.com
data:
  version: 1
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: ref.example.com/person
data:
  authority: ref.example.com
  names: {singular: person, plural: people}
  properties:
    label: {type: string}
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: ref.example.com/membership
data:
  authority: ref.example.com
  names: {singular: membership, plural: memberships}
  properties:
    person:
` + person
}

func loadRef(t *testing.T, person string) (*vocabulary.Registry, error) {
	t.Helper()
	return vocabulary.LoadFS(fstest.MapFS{
		"ref.yaml": {Data: []byte(refDocs(person))},
	})
}

// mustExist rides on any shape: the value carries the kind, so the write path
// can look the referent up with or without a pin.
func TestMustExistOnEveryShape(t *testing.T) {
	for name, person := range map[string]string{
		"single":   "      type: reference\n      kind: person\n      mustExist: true\n",
		"repeated": "      type: reference\n      kind: person\n      repeated: true\n      mustExist: true\n",
		"unpinned": "      type: reference\n      mustExist: true\n",
	} {
		t.Run(name, func(t *testing.T) {
			r, err := loadRef(t, person)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			m, _ := r.ByIdentity("ref.example.com/membership")
			p, _ := m.Prop("person")
			if !p.MustExist {
				t.Fatal("mustExist did not survive the parse")
			}
		})
	}
}

// Link data: `properties:` beside the pointer, parsed like a record's own
// property and held to the flat-value rule.
func TestReferenceLinkProperties(t *testing.T) {
	r, err := loadRef(t, `      type: reference
      kind: person
      repeated: true
      properties:
        role: {type: enum, values: [{value: owner}, {value: member}], required: true}
        since: {type: date}
`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	m, _ := r.ByIdentity("ref.example.com/membership")
	p, _ := m.Prop("person")
	if got := p.PropertyOrder; len(got) != 2 || got[0] != "role" || got[1] != "since" {
		t.Fatalf("link property order = %v", got)
	}
	role := p.Properties["role"]
	if role.Datatype != vocabulary.DatatypeEnum || !role.Required || len(role.Values) != 2 {
		t.Fatalf("role = %+v", role)
	}
	if p.Properties["since"].Datatype != vocabulary.DatatypeDate {
		t.Fatalf("since = %+v", p.Properties["since"])
	}
}

// The declaration door for link data, in one table. Every case here is a value
// the wire could not carry beside a pointer, or a site the refs index could not
// address the link data at.
func TestReferenceLinkPropertiesRefused(t *testing.T) {
	for name, tc := range map[string]struct{ person, want string }{
		"an object": {
			person: "      type: reference\n      kind: person\n      properties:\n        held: {type: object, fields: {a: string}}\n",
			want:   "a link property is a flat value",
		},
		"json": {
			person: "      type: reference\n      kind: person\n      properties:\n        held: {type: json}\n",
			want:   "`json` is a shape we do not own",
		},
		"a state": {
			person: "      type: reference\n      kind: person\n      properties:\n        held: {type: state, states: [a], initial: a}\n",
			want:   "a machine belongs to a record",
		},
		"a secret": {
			person: "      type: reference\n      kind: person\n      properties:\n        held: {type: secret}\n",
			want:   "a secret is a property of a record",
		},
		"a digest": {
			person: "      type: reference\n      kind: person\n      properties:\n        held: {type: digest}\n",
			want:   "a digest is minted onto a record",
		},
		"a blobref": {
			person: "      type: reference\n      kind: person\n      properties:\n        held: {type: blobref}\n",
			want:   "a blob-ref resolves on a record's read path",
		},
		"a second reference": {
			person: "      type: reference\n      kind: person\n      properties:\n        held: {type: reference, kind: person}\n",
			want:   "the reference IS the pointer",
		},
		"a container": {
			person: "      type: reference\n      kind: person\n      properties:\n        held: {type: string, repeated: true}\n",
			want:   `unknown key "repeated"`,
		},
		"the reserved ref key": {
			person: "      type: reference\n      kind: person\n      properties:\n        ref: {type: string}\n",
			want:   "the reserved key holding the referent's path",
		},
		// `target` is not a key of the stored value: it is the referent record
		// on the generated GraphQL object, written into the same field map as
		// the link properties, so a declared one would take its place.
		"the reserved target key": {
			person: "      type: reference\n      kind: person\n      properties:\n        target: {type: string}\n",
			want:   "the reserved key holding the referent record",
		},
		"a bare datatype": {
			person: "      type: reference\n      kind: person\n      properties:\n        held: string\n",
			want:   "a link property is a mapping",
		},
		"an empty block": {
			person: "      type: reference\n      kind: person\n      properties: {}\n",
			want:   "drop the key rather than declaring none",
		},
		"a keyed reference": {
			person: "      type: reference\n      kind: person\n      keyed: true\n      properties:\n        role: {type: string}\n",
			want:   "link data is declarable on a single or repeated reference",
		},
		"an object field": {
			person: "      type: object\n      fields:\n        held: {type: reference, kind: person, properties: {role: {type: string}}}\n",
			want:   "link data is a kind's own reference",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadRef(t, tc.person)
			if err == nil {
				t.Fatal("expected the declaration to be refused")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to name %q", err, tc.want)
			}
		})
	}
}

// `subject: true` marks the one reference a source record's mapping points
// through, and it is refused wherever a subject could not be one record.
func TestSubjectReferenceShape(t *testing.T) {
	r, err := loadRef(t, "      type: reference\n      kind: person\n      required: true\n      mustExist: true\n      subject: true\n")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	m, _ := r.ByIdentity("ref.example.com/membership")
	p, _ := m.Prop("person")
	if !p.Subject {
		t.Fatal("subject did not survive the parse")
	}

	for name, tc := range map[string]struct{ person, want string }{
		"repeated": {
			person: "      type: reference\n      kind: person\n      repeated: true\n      subject: true\n",
			want:   "a source record describes ONE subject",
		},
		"keyed": {
			person: "      type: reference\n      kind: person\n      keyed: true\n      subject: true\n",
			want:   "a source record describes ONE subject",
		},
		"cascading": {
			person: "      type: reference\n      kind: person\n      onDelete: cascade\n      subject: true\n",
			want:   "a subject is never `onDelete: cascade`",
		},
		"an object field": {
			person: "      type: object\n      fields:\n        held: {type: reference, kind: person, subject: true}\n",
			want:   "a subject is a kind's own property",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadRef(t, tc.person)
			if err == nil {
				t.Fatal("expected the declaration to be refused")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to name %q", err, tc.want)
			}
		})
	}
}
