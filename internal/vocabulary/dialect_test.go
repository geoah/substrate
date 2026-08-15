package vocabulary_test

// The property dialect's widenings: a repeated field, an object field nested to
// the declared depth, a keyed map with its key contract, and a reference field
// that resolves its `kind:` pin wherever it sits. Plus the marker that carries
// declared INTENT and changes no stored value, `managed`, and the derived
// template tokens.
//
// The refusals live in TestManifestValidationRejected's matrix
// (vocabulary_test.go); this file is what the loader must ACCEPT and what the
// parsed shape has to say afterwards.

import (
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

const dialectHead = `kind: core.substrate.reamde.dev/authority
metadata: {id: w.example.com}
data: {version: 1}
---
kind: core.substrate.reamde.dev/kind
metadata: {id: w.example.com/target}
data:
  authority: w.example.com
  names: {singular: target, plural: targets}
`

// dialectLoad loads one kind document's body against a authority that already
// declares a `target` kind for the references to point at.
func dialectLoad(t *testing.T, body string) (*vocabulary.Registry, error) {
	t.Helper()
	src := dialectHead + `---
kind: core.substrate.reamde.dev/kind
metadata: {id: w.example.com/widget}
data:
  authority: w.example.com
  names: {singular: widget, plural: widgets}
` + body
	return vocabulary.LoadFS(fstest.MapFS{
		"w.example.com/all.yaml": &fstest.MapFile{Data: []byte(src)},
	})
}

func dialectWidget(t *testing.T, body string) *vocabulary.Kind {
	t.Helper()
	r, err := dialectLoad(t, body)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	w, ok := r.ByIdentity("w.example.com/widget")
	if !ok {
		t.Fatal("w.example.com/widget missing")
	}
	return w
}

// A field may be a list: `repeated: true` on the field, coerced elementwise.
func TestRepeatedScalarFieldAccepted(t *testing.T) {
	w := dialectWidget(t, `  properties:
    grant:
      type: object
      fields:
        scopes: {type: string, repeated: true}
        subject: {type: string}
`)
	grant, _ := w.Prop("grant")
	scopes := grant.Fields["scopes"]
	if scopes == nil || !scopes.Repeated || scopes.Datatype != vocabulary.DatatypeString {
		t.Fatalf("grant.scopes = %+v", scopes)
	}
	if grant.Fields["subject"].Repeated {
		t.Fatal("a sibling field must not inherit the list")
	}
}

// Object fields nest to MaxFieldDepth: the property is level 1, so the deepest
// admitted field is level 4 and holds a scalar.
func TestObjectFieldsNestToTheDeclaredDepth(t *testing.T) {
	w := dialectWidget(t, `  properties:
    deep:
      type: object
      fields:
        l2:
          type: object
          repeated: true
          fields:
            l3:
              type: object
              keyed: true
              keyPattern: camel
              fields:
                l4: {type: string}
`)
	deep, _ := w.Prop("deep")
	l2 := deep.Fields["l2"]
	if l2 == nil || !l2.Repeated || l2.Datatype != vocabulary.DatatypeObject {
		t.Fatalf("deep.l2 = %+v", l2)
	}
	l3 := l2.Fields["l3"]
	if l3 == nil || !l3.Keyed || l3.KeyPattern != vocabulary.KeyPatternCamel {
		t.Fatalf("deep.l2.l3 = %+v", l3)
	}
	if l4 := l3.Fields["l4"]; l4 == nil || l4.Datatype != vocabulary.DatatypeString {
		t.Fatalf("deep.l2.l3.l4 = %+v", l4)
	}
	// A container at any level stays out of FTS and embed, exactly as a
	// top-level object does: a map or a list renders as no text at all.
	for _, p := range []*vocabulary.Property{deep, l2, l3} {
		if p.FTS || p.Embed {
			t.Fatalf("%s claims a search band: fts=%v embed=%v", p.Name, p.FTS, p.Embed)
		}
	}
}

// A keyed map is the twin of a list: values follow the declaration, the keys
// hold to the declared contract, and both key contracts load.
func TestKeyedMapsAccepted(t *testing.T) {
	w := dialectWidget(t, `  properties:
    effects: {type: int, keyed: true, keyPattern: camel}
    labels: {type: string, keyed: true}
    installs:
      type: object
      keyed: true
      keyPattern: kindRef
      fields:
        version: {type: string}
        pinned: {type: bool}
`)
	effects, _ := w.Prop("effects")
	if !effects.Keyed || effects.Repeated || effects.Datatype != vocabulary.DatatypeInt {
		t.Fatalf("effects = %+v", effects)
	}
	labels, _ := w.Prop("labels")
	// A keyed string is the case that must LEAVE the FTS band without the
	// author having to say so: the whole family is indexed by default.
	if !labels.Keyed || labels.FTS || labels.Embed || labels.KeyPattern != "" {
		t.Fatalf("labels = %+v", labels)
	}
	installs, _ := w.Prop("installs")
	if !installs.Keyed || installs.KeyPattern != vocabulary.KeyPatternKindRef ||
		installs.Fields["pinned"].Datatype != vocabulary.DatatypeBool {
		t.Fatalf("installs = %+v", installs)
	}

	// The key contract, as the write path applies it.
	for _, ok := range []string{"backfillDepth", "a"} {
		if err := effects.CheckKey(ok); err != nil {
			t.Errorf("camel key %q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "back_fill", "Backfill", "tasks.example.com/task"} {
		if err := effects.CheckKey(bad); err == nil {
			t.Errorf("camel key %q must be refused", bad)
		}
	}
	for _, ok := range []string{"task", "tasks.example.com/task"} {
		if err := installs.CheckKey(ok); err != nil {
			t.Errorf("kindRef key %q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "tasks.*", "a b"} {
		if err := installs.CheckKey(bad); err == nil {
			t.Errorf("kindRef key %q must be refused", bad)
		}
	}
	// No contract admits any non-empty key: the keys are data.
	if err := labels.CheckKey("Anything Goes/here"); err != nil {
		t.Errorf("an uncontracted key: %v", err)
	}
	if err := labels.CheckKey(""); err == nil {
		t.Error("an empty key must be refused whatever the contract")
	}
}

// A reference FIELD resolves its `to:` like a property does — at every admitted
// depth. Before this it parsed and was never resolved, so the stored value was
// compared against a bare name that could never match a full identity.
func TestNestedReferenceFieldsResolveTheirTarget(t *testing.T) {
	w := dialectWidget(t, `  properties:
    tools:
      type: object
      repeated: true
      fields:
        callable: {type: reference, kind: target}
        note: {type: string}
    inputs:
      type: object
      keyed: true
      keyPattern: camel
      fields:
        kind: {type: reference, kind: target}
    deep:
      type: object
      fields:
        l2:
          type: object
          fields:
            l3: {type: object, fields: {ref: {type: reference, kind: target}}}
`)
	for _, path := range []struct {
		label string
		prop  *vocabulary.Property
	}{
		{"tools[].callable", w.Props["tools"].Fields["callable"]},
		{"inputs{}.kind", w.Props["inputs"].Fields["kind"]},
		{"deep.l2.l3.ref", w.Props["deep"].Fields["l2"].Fields["l3"].Fields["ref"]},
	} {
		if path.prop == nil {
			t.Fatalf("%s: not parsed", path.label)
		}
		if path.prop.To != "w.example.com/target" {
			t.Errorf("%s: to = %q, want the resolved identity", path.label, path.prop.To)
		}
	}
}

// An unresolvable `to:` on a nested reference fails at LOAD, naming the path —
// the same refusal a top-level reference gets.
func TestNestedReferenceTargetMustExist(t *testing.T) {
	_, err := dialectLoad(t, `  properties:
    tools: {type: object, fields: {callable: {type: reference, kind: nosuchkind}}}
`)
	if err == nil {
		t.Fatal("expected a load error")
	}
	if !strings.Contains(err.Error(), "tools.fields.callable.to") {
		t.Fatalf("the refusal must name the field's path, got: %v", err)
	}
}

// A NESTED inverse is not claimed, and that is a migration constraint, not an
// oversight: every earlier binary admitted and stored one, so claiming it here
// would make a stored declaration inadmissible — fatal at open for core, a
// quarantine for a bundle — over a label nothing resolves by. The kind's OWN
// pointers still collide.
func TestNestedReferenceInverseIsNotClaimed(t *testing.T) {
	if _, err := dialectLoad(t, `  properties:
    target: {type: reference, kind: target, inverse: widgets}
    tools:
      type: object
      fields:
        callable: {type: reference, kind: target, inverse: widgets}
`); err != nil {
		t.Fatalf("a nested inverse must keep loading beside a top-level claim: %v", err)
	}
	// Two of the kind's own pointers claiming one word on one target still
	// refuse: that check predates this dialect and nothing about it moved.
	_, err := dialectLoad(t, `  properties:
    target: {type: reference, kind: target, inverse: widgets}
    alsoTarget: {type: reference, kind: target, inverse: widgets}
`)
	if err == nil {
		t.Fatal("expected a load error")
	}
	if !strings.Contains(err.Error(), "already claimed by") {
		t.Fatalf("the refusal must name the earlier claim, got: %v", err)
	}
}

// managed says the engine stamps the property. It changes no stored value, and
// it rides into the Definition map the read surfaces render.
func TestManagedIsParsedAndRendered(t *testing.T) {
	w := dialectWidget(t, `  properties:
    version: {type: string, managed: true}
    source: {type: string, managed: true}
`)
	if p, _ := w.Prop("version"); !p.Managed {
		t.Fatalf("version = %+v", p)
	}
	if p, _ := w.Prop("source"); !p.Managed {
		t.Fatalf("source = %+v", p)
	}
	// The declaration the console reads is the authored data map, so the key
	// reaches the API by riding it — the same route displayName travels.
	props := w.Definition["properties"].(map[string]any)
	if props["version"].(map[string]any)["managed"] != true {
		t.Fatalf("definition.version = %v", props["version"])
	}
}

// refersTo is DEAD: it was a picker hint beside a string, enforced nowhere. The
// property it marked is a `reference` with a `kind:` pin, which every write is
// held to, so the retired key is refused by name rather than ignored — a
// declaration still carrying one would otherwise look obeyed.
func TestRefersToIsRefusedByName(t *testing.T) {
	_, err := dialectLoad(t, `  properties:
    emit: {type: string, repeated: true, refersTo: kind}
`)
	if err == nil {
		t.Fatal("refersTo must be refused")
	}
	for _, want := range []string{"refersTo", "type: reference", "kind:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q, got: %v", want, err)
		}
	}
}

// {localName} and {id} are DERIVED tokens: they need no declaration, so a kind
// with neither property still loads with them in its template.
func TestDerivedTemplateTokensNeedNoDeclaration(t *testing.T) {
	for _, tmpl := range []string{"{localName}", "{id}", "{localName} ({id})", "{label|localName}"} {
		w := dialectWidget(t, `  displayTemplate: "`+tmpl+`"
  properties: {label: {type: string}}
`)
		if w.Template == nil {
			t.Fatalf("%s: no parsed template", tmpl)
		}
	}
	// A kind that DECLARES the token's name — as a property or as an edge — is
	// legal, and the declaration is what renders.
	for name, body := range map[string]string{
		"property": `  displayTemplate: "{localName}"
  properties: {localName: {type: string}}
`,
		"edge": `  displayTemplate: "{localName}"
  edges: {localName: {to: target}}
`,
	} {
		if _, err := dialectLoad(t, body); err != nil {
			t.Fatalf("a declared %s of the token's name must load: %v", name, err)
		}
	}
	// And a derived token is held to the bare token's rules where the kind DOES
	// declare the name: a sensitive value never renders into a title, whichever
	// spelling asked for it.
	_, err := dialectLoad(t, `  displayTemplate: "{localName}"
  properties: {localName: {type: secret}}
`)
	if err == nil {
		t.Fatal("expected a load error")
	}
	if !strings.Contains(err.Error(), "sensitive value never renders into a title") {
		t.Fatalf("the refusal must name the leak it prevents, got: %v", err)
	}
}

// A REAL property wins over the derived token of the same name, and it wins by
// being DECLARED rather than by holding a value: a kind that declares
// `localName` means its own value every time, so an empty one renders empty
// instead of quietly showing the id.
func TestDerivedTokenYieldsToADeclaredProperty(t *testing.T) {
	tmpl, err := vocabulary.ParseTemplate("{localName}/{id}")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	derived := map[string]string{
		vocabulary.DerivedLocalName: "person",
		vocabulary.DerivedID:        "people.example.com/person",
	}
	if got := tmpl.Render(testResolver{derived: derived}); got != "person/people.example.com/person" {
		t.Fatalf("derived render = %q", got)
	}
	got := tmpl.Render(testResolver{
		props:   map[string]string{vocabulary.DerivedLocalName: "declared"},
		derived: derived,
	})
	if got != "declared/people.example.com/person" {
		t.Fatalf("a declared property must win, got %q", got)
	}
	// Declared and EMPTY: the token still yields, so the segment renders
	// nothing. The alternative is a title that looks filled and is not.
	got = tmpl.Render(testResolver{
		declares: []string{vocabulary.DerivedLocalName},
		derived:  derived,
	})
	if got != "/people.example.com/person" {
		t.Fatalf("a declared-but-empty property must not fall back, got %q", got)
	}
	// An alternative list still moves on, which is how a template asks for the
	// property first and the derived value second.
	either, err := vocabulary.ParseTemplate("{localName|id}")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := either.Render(testResolver{
		declares: []string{vocabulary.DerivedLocalName},
		derived:  derived,
	}); got != "people.example.com/person" {
		t.Fatalf("an empty alternative must yield to the next, got %q", got)
	}
	// A declared EDGE of the token's name wins the same way a property does, and
	// resolves as the bare form: property first, then the edge's targets. One name
	// is one pointer, so a token cannot mean the id here and the edge there.
	localName, err := vocabulary.ParseTemplate("{localName}")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := localName.Render(testResolver{
		edges:   map[string]string{vocabulary.DerivedLocalName: "Ada"},
		derived: derived,
	}); got != "Ada" {
		t.Fatalf("a declared edge must win over the derived token, got %q", got)
	}
}

// The key contract has ONE grammar: CheckKey asks it in Go and the narrowing
// guard hands the same pattern to Postgres, so the two answers cannot drift.
func TestKeyPatternRegexpAgreesWithCheckKey(t *testing.T) {
	keys := []string{
		"backfillDepth", "a", "task", "tasks.example.com/task", "x9",
		"back_fill", "Backfill", "tasks.*", "a b", "9lives", "a/b/c",
		"tasks.example.com/Task", "-nope", "tasks.example.com/",
		// The spellings where the two sides could disagree, and one where they
		// did: ValidKindReference admits "/task" (SplitKindRef yields an empty
		// authority and it only validates a non-empty one), which is not a kind
		// reference. A key the write path admitted and the guard refused was a
		// stored map that could never be brought under its own declaration, so
		// CheckKey holds to the grammar (types.go says why the validator keeps its
		// quirk).
		"/task", "task/", "a//b", "/", "//", "tasks.example.com//task",
	}
	for _, contract := range []string{vocabulary.KeyPatternCamel, vocabulary.KeyPatternKindRef} {
		src := vocabulary.KeyPatternRegexp(contract)
		if src == "" {
			t.Fatalf("%s has no pattern", contract)
		}
		re, err := regexp.Compile(src)
		if err != nil {
			t.Fatalf("%s pattern %q does not compile: %v", contract, src, err)
		}
		p := &vocabulary.Property{Keyed: true, KeyPattern: contract}
		for _, key := range keys {
			if got, want := re.MatchString(key), p.CheckKey(key) == nil; got != want {
				t.Errorf("%s key %q: regexp says %v, CheckKey says %v", contract, key, got, want)
			}
		}
	}
	// No contract carries no pattern: every non-empty key is admitted, and the
	// guard has nothing to count.
	if src := vocabulary.KeyPatternRegexp(""); src != "" {
		t.Errorf("an uncontracted keyed map must have no pattern, got %q", src)
	}
}
