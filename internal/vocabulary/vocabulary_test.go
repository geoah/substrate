package vocabulary_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Fixtures are inline: the loader's contract is the manifest format, not the
// shipped vocabulary, and pinning behavior to files that change for editorial
// reasons is what made the old suite brittle. The shipped tree gets two tests,
// below: the things the engine cannot boot without, and the block-style rule
// the owner pinned for the files' formatting.

// --- fixtures ------------------------------------------------------------

// coreDocs is a miniature core: the authority, its actors, the temporal
// capability every other authority binds across the boundary, and one type.
const coreDocs = `# the substrate's own machinery
kind: core.substrate.reamde.dev/authority
metadata:
  id: core.example.com
data:
  version: v1alpha1
---
kind: core.substrate.reamde.dev/actor
metadata:
  id: owner
data:
  authority: core.example.com
---
kind: core.substrate.reamde.dev/actor
metadata:
  id: engram
data:
  authority: core.example.com
---
# when a thing sits on the timeline — an instant, or a span
kind: core.substrate.reamde.dev/trait
metadata:
  id: core.example.com/temporal
data:
  authority: core.example.com
  # backed by the physical at/ends_at/due_at columns every record row carries
  oneOf:
    point: {at: datetime}
    range: {at: datetime, endsAt: datetime}
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: core.example.com/account
data:
  authority: core.example.com
  names: {singular: account, plural: accounts}
status:
  # server-set, ignored on input, so get -o yaml output is apply-able
  observedGeneration: 3
`

// vocabDocs is a vocabulary authority: refinements, cross-authority edges, a bound
// capability with a hot-property remap, a source record with its mapping,
// object properties and a machine.
const vocabDocs = `kind: core.substrate.reamde.dev/authority
metadata:
  id: vocab.example.com
data:
  version: v1alpha1
---
# the connector-style actor whose authority maps onto contact: the machine tier
kind: core.substrate.reamde.dev/actor
metadata:
  id: connector:google
data:
  authority: vocab.example.com
---
# Amazon's audiobook identifier: the one durable key a library row carries
kind: core.substrate.reamde.dev/propertytype
metadata:
  id: vocab.example.com/asin
data:
  authority: vocab.example.com
  base: string
  pattern: "^B0[A-Z0-9]{8}$"
---
# one person; nothing matches on their addresses, so two contacts holding one
# email stay two contacts until an owner merges them
kind: core.substrate.reamde.dev/kind
metadata:
  id: vocab.example.com/contact
data:
  authority: vocab.example.com
  names: {singular: contact, plural: contacts}
  properties:
    name: {type: string}
    company: {type: string}
    emails: {type: email, repeated: true}
---
# one source's record of a contact: an ordinary edge points at the contact,
# and the mapping beside it says how these properties reach it (§6.1)
kind: core.substrate.reamde.dev/kind
metadata:
  id: vocab.example.com/googlecontact
data:
  authority: vocab.example.com
  names: {singular: googlecontact, plural: googlecontacts}
  properties:
    # what the provider actually sends, declared (record 49) — never json
    name:
      type: object
      fields:
        displayName: {type: string}
        firstName: {type: string}
    emails:
      type: object
      repeated: true
      # a bare kind is shorthand for {type: kind}
      fields: {value: email, primary: bool}
  edges:
    contact: {to: contact, required: true}
---
# how googlecontact's properties reach the contact (record 50)
kind: core.substrate.reamde.dev/recordmapping
metadata:
  id: vocab.example.com/googlecontactcontact
data:
  authority: vocab.example.com
  from: vocab.example.com/googlecontact
  to: vocab.example.com/contact
  edge: contact
  match:
    - {from: "emails[].value", to: emails}
  map:
    name: name.displayName
    emails: {path: "emails[].value", merge: union}
---
# one book, in whatever formats you hold it
kind: core.substrate.reamde.dev/kind
metadata:
  id: vocab.example.com/book
data:
  authority: vocab.example.com
  names: {singular: book, plural: books}
  displayTemplate: "{title}"
  properties:
    asin: {type: asin}
    format: {type: enum, values: [print, ebook, audio]}
    blurb: {type: markdown, embed: true}
    secretKey: {type: secret}
  edges:
    author: {to: contact, many: true}
    account: {to: account, required: true, ownerRef: true}
---
# one task; temporal(point: dueAt) remaps the capability's hot property
kind: core.substrate.reamde.dev/kind
metadata:
  id: vocab.example.com/task
data:
  authority: vocab.example.com
  names: {singular: task, plural: tasks}
  traits: ["temporal(point: dueAt)"]
  indices: [[status, dueAt]]
  properties:
    # a machine IS a property: one namespace, one wire map
    status:
      type: state
      states: [open, doing, done]
      initial: open
      transitions:
        - from: open
          to: done
          stamps: {doneAt: now}
        - {from: open, to: doing}
        - {from: doing, to: done, onEnter: applyDiff}
`

func loadFixture(t *testing.T, files map[string]string) *vocabulary.Registry {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	r, err := vocabulary.LoadFS(fsys)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return r
}

func loadVocab(t *testing.T) *vocabulary.Registry {
	t.Helper()
	// Split across a directory tree, in the file layout FORMAT.md §5 pins:
	// the unit is the document, not the file, and the loader recurses.
	return loadFixture(t, map[string]string{
		"core.example.com/authority.yaml":  coreDocs,
		"vocab.example.com/authority.yaml": vocabDocs,
		"vocab.example.com/extra.yaml":     extraDocs,
		"vocab.example.com/notyaml.txt":    "ignored",
	})
}

// extraDocs proves an authority's manifests may live in any file under the tree.
const extraDocs = `kind: core.substrate.reamde.dev/kind
metadata:
  id: vocab.example.com/note
data:
  authority: vocab.example.com
  names: {singular: note, plural: notes}
  traits: [temporal(range)]
  properties:
    notes: {type: text}
`

// --- the loader ----------------------------------------------------------

func TestLoadManifestStream(t *testing.T) {
	r := loadVocab(t)

	book, ok := r.ByIdentity("vocab.example.com/book")
	if !ok {
		t.Fatal("book type missing")
	}
	if book.Plural != "books" || book.Name != "book" || book.Authority != "vocab.example.com" {
		t.Fatalf("book = %+v", book)
	}
	if book.Version != "v1alpha1" {
		t.Fatalf("version = %q (the authority's, unless the type overrides it)", book.Version)
	}
	// Edges: in-authority short name, and a cross-authority one.
	if got := book.Edges["author"].To; got != "vocab.example.com/contact" {
		t.Fatalf("author edge = %q", got)
	}
	acct := book.Edges["account"]
	if acct.To != "core.example.com/account" || !acct.Required || !acct.OwnerRef {
		t.Fatalf("account edge = %+v", acct)
	}
	// Refinements survive the property that names them.
	if p, _ := book.Prop("asin"); p.Refined != "asin" || p.Pattern == nil {
		t.Fatalf("asin refinement lost: %+v", p)
	}
	if p, _ := book.Prop("format"); p.Datatype != vocabulary.DatatypeEnum || len(p.Values) != 3 {
		t.Fatalf("book.format = %+v", p)
	}
	if p, _ := book.Prop("blurb"); !p.Embed {
		t.Fatal("book.blurb should be embed:true")
	}
	if p, _ := book.Prop("secretKey"); !p.Secret() || p.FTS || p.Embed {
		t.Fatalf("secret property = %+v", p)
	}
	if _, ok := r.ByPlural("vocab.example.com", "books"); !ok {
		t.Fatal("plural lookup failed")
	}

	// A list is `repeated: true` on the property, never a bracketed type.
	contact, _ := r.ByIdentity("vocab.example.com/contact")
	if p, _ := contact.Prop("emails"); !p.Repeated || p.Datatype != vocabulary.DatatypeEmail {
		t.Fatalf("contact.emails = %+v", p)
	}

	// A document in a second file joins the same authority.
	note, ok := r.ByIdentity("vocab.example.com/note")
	if !ok {
		t.Fatal("note type missing: documents, not files, are the unit")
	}
	if !note.UsesHot("at") || !note.UsesHot("endsAt") {
		t.Fatalf("note hot properties = %v", note.HotColumns)
	}

	// temporal lives in core, so this binding crosses an authority boundary, and
	// the hot-property remap survives the crossing.
	task, _ := r.ByIdentity("vocab.example.com/task")
	if !task.UsesHot("dueAt") || task.UsesHot("at") {
		t.Fatalf("task hot properties = %v", task.HotColumns)
	}
	if len(task.Traits) != 1 || task.Traits[0].Trait != "temporal" ||
		task.Traits[0].Variant != "point" {
		t.Fatalf("task capabilities = %+v", task.Traits)
	}
	if len(task.Indices) != 1 || task.Indices[0][0] != "status" {
		t.Fatalf("task indices = %v", task.Indices)
	}
	// Stamp targets are implicitly declared datetime properties.
	if p, ok := task.Prop("doneAt"); !ok || p.Datatype != vocabulary.DatatypeDatetime || !p.Implicit {
		t.Fatalf("stamp target doneAt = %+v", p)
	}

	// Actors are their own manifests now, one document each.
	if got := r.Actors(); len(got) != 3 || got[0] != "connector:google" ||
		got[1] != "engram" || got[2] != "owner" {
		t.Fatalf("actors = %v", got)
	}

	// Object properties: fields declared inline, one level deep,
	// a bare kind as shorthand — and never FTS, never embed.
	gc, _ := r.ByIdentity("vocab.example.com/googlecontact")
	name, _ := gc.Prop("name")
	if name.Datatype != vocabulary.DatatypeObject || name.Repeated || name.FTS || name.Embed {
		t.Fatalf("googlecontact.name = %+v", name)
	}
	if len(name.FieldOrder) != 2 || name.FieldOrder[0] != "displayName" ||
		name.Fields["firstName"].Datatype != vocabulary.DatatypeString {
		t.Fatalf("googlecontact.name fields = %v", name.FieldOrder)
	}
	emails, _ := gc.Prop("emails")
	if emails.Datatype != vocabulary.DatatypeObject || !emails.Repeated ||
		emails.Fields["value"].Datatype != vocabulary.DatatypeEmail ||
		emails.Fields["primary"].Datatype != vocabulary.DatatypeBool {
		t.Fatalf("googlecontact.emails = %+v", emails)
	}
	if g, ok := r.ActorAuthority("engram"); !ok || g != "core.example.com" {
		t.Fatalf("actor authority = %q", g)
	}
	if g, _ := r.AuthorityByName("core.example.com"); len(g.Actors) != 2 {
		t.Fatalf("authority actors = %v", g.Actors)
	}
}

// An enum's values are a first-class ordered {value, label} list.
// BOTH declared forms load into ONE type — a bare scalar is a value with no
// label, a mapping carries both — so a stored closure written before labels
// still admits. Validation reads Value alone (ValueStrings); the Definition map
// the console reads is canonicalized to the labeled wire form whichever way the
// manifest authored it.
func TestEnumValueLabels(t *testing.T) {
	const src = `kind: core.substrate.reamde.dev/authority
metadata: {id: vocab.example.com}
data: {version: v1}
---
kind: core.substrate.reamde.dev/kind
metadata: {id: vocab.example.com/account}
data:
  authority: vocab.example.com
  names: {singular: account, plural: accounts}
  properties:
    backfillDepth:
      type: enum
      values:
        - {value: none, label: "Don't backfill"}
        - {value: last30d, label: "Last 30 days"}
        - all
`
	r := loadFixture(t, map[string]string{"vocab.example.com/g.yaml": src})
	acct, ok := r.ByIdentity("vocab.example.com/account")
	if !ok {
		t.Fatal("account type missing")
	}
	p, _ := acct.Prop("backfillDepth")
	// Order is declaration order; the value carries whichever label was declared.
	want := []vocabulary.EnumValue{
		{Value: "none", Label: "Don't backfill"},
		{Value: "last30d", Label: "Last 30 days"},
		{Value: "all", Label: ""}, // bare scalar: no label, the UI humanizes
	}
	if len(p.Values) != len(want) {
		t.Fatalf("Values = %v", p.Values)
	}
	for i, w := range want {
		if p.Values[i] != w {
			t.Fatalf("Values[%d] = %+v, want %+v", i, p.Values[i], w)
		}
	}
	// ValueStrings is the validation set — every value, labels stripped.
	if got := p.ValueStrings(); len(got) != 3 || got[0] != "none" || got[2] != "all" {
		t.Fatalf("ValueStrings = %v", got)
	}
	// The read surfaces (the console) get one canonical shape off the Definition
	// map: [{value, label}], both keys present, even for the bare-scalar entry.
	props := acct.Definition["properties"].(map[string]any)
	def := props["backfillDepth"].(map[string]any)
	vals, ok := def["values"].([]any)
	if !ok || len(vals) != 3 {
		t.Fatalf("definition values = %v (want a [{value,label}] list)", def["values"])
	}
	bare := vals[2].(map[string]any)
	if bare["value"] != "all" || bare["label"] != "" {
		t.Fatalf("bare entry canonicalized to %v, want {value: all, label: \"\"}", bare)
	}
}

// The definition the projections and the GraphQL builder read is the
// manifest's data map, spelled exactly as it was authored.
func TestDefinitionIsTheData(t *testing.T) {
	r := loadVocab(t)
	task, _ := r.ByIdentity("vocab.example.com/task")
	if task.Definition["authority"] != "vocab.example.com" {
		t.Fatalf("definition = %v", task.Definition)
	}
	props, _ := task.Definition["properties"].(map[string]any)
	status, _ := props["status"].(map[string]any)
	if status["type"] != "state" {
		t.Fatal("definition.properties.status is the state machine the GraphQL builder reads")
	}
	if _, ok := task.Definition["traits"]; !ok {
		t.Fatal("definition.traits is what the GraphQL builder reads")
	}
	names, _ := task.Definition["names"].(map[string]any)
	if names["plural"] != "tasks" {
		t.Fatalf("definition.names = %v", names)
	}
}

// Descriptions: a declared one-sentence explanation on property
// and edge declarations, carried structurally so the kind mirrors — and
// the console's hover tooltips — serve it without parsing comments.
func TestDescriptions(t *testing.T) {
	mk := func(body string) map[string]string {
		return map[string]string{"d.example.com/authority.yaml": `kind: core.substrate.reamde.dev/authority
metadata:
  id: d.example.com
data:
  version: v1alpha1
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: d.example.com/widget
data:
  authority: d.example.com
  names: {singular: widget, plural: widgets}
  properties:
` + body}
	}

	r := loadFixture(t, mk(`    name: {type: string, description: "what the widget is called"}
    status:
      type: state
      description: where the widget is in its life
      states: [open, done]
      initial: open
      transitions:
        - {from: open, to: done}
    dims:
      type: object
      description: the measured size
      fields:
        width: {type: int, description: "millimeters across"}
        height: int
  edges:
    parent: {to: widget, description: "the widget this one hangs off"}
`))
	w, _ := r.ByIdentity("d.example.com/widget")
	if p, _ := w.Prop("name"); p.Description != "what the widget is called" {
		t.Errorf("name description = %q", p.Description)
	}
	if p, _ := w.Prop("status"); p.Description != "where the widget is in its life" {
		t.Errorf("state description = %q", p.Description)
	}
	dims, _ := w.Prop("dims")
	if dims.Description != "the measured size" {
		t.Errorf("object description = %q", dims.Description)
	}
	if f := dims.Fields["width"]; f.Description != "millimeters across" {
		t.Errorf("field description = %q", f.Description)
	}
	if f := dims.Fields["height"]; f.Description != "" {
		t.Errorf("bare-kind field grew a description: %q", f.Description)
	}
	if e, _ := w.Edge("parent"); e.Description != "the widget this one hangs off" {
		t.Errorf("edge description = %q", e.Description)
	}

	// The mirror shape: the definition IS the data map, so the console reads
	// definition.properties.<name>.description / definition.edges.<rel>.description.
	props, _ := w.Definition["properties"].(map[string]any)
	name, _ := props["name"].(map[string]any)
	if name["description"] != "what the widget is called" {
		t.Errorf("definition.properties.name.description = %v", name["description"])
	}
	edges, _ := w.Definition["edges"].(map[string]any)
	parent, _ := edges["parent"].(map[string]any)
	if parent["description"] != "the widget this one hangs off" {
		t.Errorf("definition.edges.parent.description = %v", parent["description"])
	}

	// One rule: a description is a single short sentence — a tooltip, not
	// documentation. Newlines and >200 chars are load errors.
	long := strings.Repeat("x", 201)
	for what, body := range map[string]string{
		"newline": "    name: {type: string, description: \"two\\nlines\"}\n",
		"long":    "    name: {type: string, description: \"" + long + "\"}\n",
		"edge": "    name: {type: string}\n  edges:\n" +
			"    parent: {to: widget, description: \"" + long + "\"}\n",
	} {
		fsys := fstest.MapFS{}
		for fname, fbody := range mk(body) {
			fsys[fname] = &fstest.MapFile{Data: []byte(fbody)}
		}
		if _, err := vocabulary.LoadFS(fsys); err == nil {
			t.Errorf("%s description loaded; a description is one short sentence", what)
		}
	}
}

// A KIND's description is the one that heads its page in the console, so it
// gets two sentences where a property's tooltip gets one — 400 chars, still
// on one line — and it is carried structurally, not left in the data map.
func TestKindDescription(t *testing.T) {
	mk := func(desc string) map[string]string {
		return map[string]string{"d.example.com/authority.yaml": `kind: core.substrate.reamde.dev/authority
metadata:
  id: d.example.com
data:
  version: v1alpha1
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: d.example.com/widget
data:
  authority: d.example.com
  description: "` + desc + `"
  names: {singular: widget, plural: widgets}
  properties:
    name: {type: string}
`}
	}

	// Two sentences, longer than a property's 200-char tooltip bound.
	two := "One widget, the thing on the shelf. " + strings.Repeat("x", 200)
	r := loadFixture(t, mk(two))
	w, _ := r.ByIdentity("d.example.com/widget")
	if w.Description != two {
		t.Errorf("kind description = %q", w.Description)
	}
	// The bound is CHARACTERS, not bytes: a description of em dashes is held
	// to the same length as one of ASCII, and 400 of them is admitted.
	dashes := strings.Repeat("—", 400)
	if got, _ := loadFixture(t, mk(dashes)).ByIdentity("d.example.com/widget"); got.Description != dashes {
		t.Errorf("400 non-ASCII chars refused; the bound counts bytes")
	}
	// The mirror shape: the definition IS the data map, so a console reading
	// the stored declaration sees the same text.
	if w.Definition["description"] != two {
		t.Errorf("definition.description = %v", w.Definition["description"])
	}

	for what, desc := range map[string]string{
		"newline":       "two\\nlines",
		"long":          strings.Repeat("x", 401),
		"over in chars": strings.Repeat("—", 401),
	} {
		fsys := fstest.MapFS{}
		for fname, fbody := range mk(desc) {
			fsys[fname] = &fstest.MapFile{Data: []byte(fbody)}
		}
		if _, err := vocabulary.LoadFS(fsys); err == nil {
			t.Errorf("%s kind description loaded; it is two sentences on one line", what)
		}
	}
}

// --- source capture ------------------------------------------------------

// The console shows a resource's schema by printing SourceYAML, so the loader
// has to hand back the document's own text: comments, key order, formatting.
// The document IS the block now, which makes the capture exact.
func TestSourceYAMLIsTheDocument(t *testing.T) {
	r := loadVocab(t)

	// A type's source is its whole manifest, envelope included, with the
	// comment above it that says what it is for — and nothing of its
	// neighbors, in either direction.
	book, _ := r.ByIdentity("vocab.example.com/book")
	wantHead := strings.Join([]string{
		`# one book, in whatever formats you hold it`,
		`kind: core.substrate.reamde.dev/kind`,
		`metadata:`,
		`  id: vocab.example.com/book`,
		`data:`,
		`  authority: vocab.example.com`,
		`  names: {singular: book, plural: books}`,
	}, "\n")
	if !strings.HasPrefix(book.SourceYAML, wantHead) {
		t.Fatalf("book source:\n%s\nwant prefix:\n%s", book.SourceYAML, wantHead)
	}
	if !strings.HasSuffix(book.SourceYAML, "    account: {to: account, required: true, ownerRef: true}") {
		t.Fatalf("book source ends:\n%s", book.SourceYAML)
	}
	for _, leak := range []string{"contact.vocab", "task.vocab", "---"} {
		if strings.Contains(book.SourceYAML, leak) {
			t.Fatalf("book source leaked %q:\n%s", leak, book.SourceYAML)
		}
	}

	// A authority's source is its authority manifest, carrying the file's own
	// opening comment.
	g, ok := r.AuthorityByName("core.example.com")
	if !ok {
		t.Fatal("core authority missing")
	}
	wantAuthority := strings.Join([]string{
		`# the substrate's own machinery`,
		`kind: core.substrate.reamde.dev/authority`,
		`metadata:`,
		`  id: core.example.com`,
		`data:`,
		`  version: v1alpha1`,
	}, "\n")
	if g.SourceYAML != wantAuthority {
		t.Fatalf("authority source:\n%s\nwant:\n%s", g.SourceYAML, wantAuthority)
	}

	// Traits and custom property types are manifests too, so their
	// text is exact rather than sliced.
	temporal := g.Traits["temporal"]
	if temporal.Identity() != "core.example.com/temporal" {
		t.Fatalf("capability identity = %q", temporal.Identity())
	}
	if !strings.HasPrefix(temporal.SourceYAML, "# when a thing sits on the timeline") ||
		!strings.Contains(temporal.SourceYAML, "  # backed by the physical at/ends_at/due_at columns") ||
		!strings.HasSuffix(temporal.SourceYAML, "    range: {at: datetime, endsAt: datetime}") {
		t.Fatalf("temporal source:\n%s", temporal.SourceYAML)
	}
	if temporal.Definition["oneOf"] == nil {
		t.Fatalf("capability definition = %v", temporal.Definition)
	}

	vocab, _ := r.AuthorityByName("vocab.example.com")
	asin := vocab.PropertyTypes["asin"]
	if asin.Base != vocabulary.DatatypeString || asin.Identity() != "vocab.example.com/asin" {
		t.Fatalf("asin datatype = %+v", asin)
	}
	if !strings.HasPrefix(asin.SourceYAML, "# Amazon's audiobook identifier") ||
		!strings.HasSuffix(asin.SourceYAML, `  pattern: "^B0[A-Z0-9]{8}$"`) {
		t.Fatalf("asin source:\n%s", asin.SourceYAML)
	}

	// Registry listings are by identity, across authorities.
	caps := r.Traits()
	if len(caps) != 1 || caps[0].Identity() != "core.example.com/temporal" {
		t.Fatalf("capabilities = %+v", caps)
	}
	dts := r.PropertyTypes()
	if len(dts) != 1 || dts[0].Identity() != "vocab.example.com/asin" {
		t.Fatalf("datatypes = %+v", dts)
	}
}

// Installed manifests arrive as maps, never as text: their source is derived,
// and stable enough that re-projecting it writes nothing.
func TestSourceYAMLForInstalledManifests(t *testing.T) {
	m := gmailManifest()
	g, err := vocabulary.ParseManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	got := g.Kinds["cursor"].SourceYAML
	want := strings.Join([]string{
		`data:`,
		`    authority: gmail.connectors.example.com`,
		`    names:`,
		`        plural: cursors`,
		`        singular: cursor`,
		`    properties:`,
		`        pageToken:`,
		`            type: string`,
		`kind: core.substrate.reamde.dev/kind`,
		`metadata:`,
		`    id: gmail.connectors.example.com/cursor`,
	}, "\n")
	if got != want {
		t.Fatalf("installed source:\n%s\nwant:\n%s", got, want)
	}
	again, err := vocabulary.ParseManifest(gmailManifest())
	if err != nil {
		t.Fatal(err)
	}
	if again.Kinds["cursor"].SourceYAML != got {
		t.Fatal("derived source must be deterministic, or projections churn")
	}
	if again.SourceYAML == "" || !strings.Contains(again.SourceYAML, "kind: core.substrate.reamde.dev/authority") {
		t.Fatalf("installed authority source:\n%s", again.SourceYAML)
	}
}

func gmailManifest() vocabulary.Manifest {
	const authority = "gmail.connectors.example.com"
	return vocabulary.Manifest{
		Name:      "google.gmail",
		Authority: authority,
		Manifests: []map[string]any{
			vocabulary.AuthorityManifest(authority, "v1alpha1"),
			vocabulary.ActorManifest(authority, "connector:gmail"),
			vocabulary.KindManifest(authority,
				map[string]any{"singular": "cursor", "plural": "cursors"},
				map[string]any{"properties": map[string]any{
					"pageToken": map[string]any{"type": "string"},
				}}),
		},
	}
}

// --- resolution rules ----------------------------------------------------

// Edge targets resolve in-authority first, then uniquely across authorities, and an
// ambiguous short name is a load error rather than an arbitrary pick.
func TestEdgeTargetResolutionRules(t *testing.T) {
	authority := func(name string) string {
		return `kind: core.substrate.reamde.dev/authority
metadata: {id: ` + name + `}
data: {version: v1}
`
	}
	typ := func(authority, name, data string) string {
		return `---
kind: core.substrate.reamde.dev/kind
metadata: {id: ` + authority + `/` + name + `}
data:
  authority: ` + authority + `
  names: {singular: ` + name + `, plural: ` + name + `s}
` + data
	}
	alpha := authority("a.example.com") + typ("a.example.com", "alpha", "")

	t.Run("across authorities", func(t *testing.T) {
		r := loadFixture(t, map[string]string{
			"a.yaml": alpha,
			"b.yaml": authority("b.example.com") +
				typ("b.example.com", "beta", "  edges:\n    target: {to: alpha}\n"),
		})
		beta, _ := r.ByIdentity("b.example.com/beta")
		if got := beta.Edges["target"].To; got != "a.example.com/alpha" {
			t.Fatalf("edge target = %q", got)
		}
	})

	// Two authorities may hold the same LOCAL name only when one of them is
	// installed: a shipped kind's GraphQL name is its bare singular, and the
	// declaration-time uniqueness check refuses a second claim on it. An
	// installed kind is authority-prefixed, so the pair is legal — and that is
	// exactly the case the in-authority-first rule exists for.
	t.Run("in authority first", func(t *testing.T) {
		install := func(t *testing.T, body string) *vocabulary.Registry {
			t.Helper()
			r := loadFixture(t, map[string]string{"a.yaml": alpha})
			gs, err := vocabulary.ParseYAML([]byte(body), vocabulary.SourceInstalled)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			for _, g := range gs {
				if err := r.Install(g); err != nil {
					t.Fatalf("install: %v", err)
				}
			}
			return r
		}
		r := install(t, authority("b.example.com")+typ("b.example.com", "alpha", "")+
			typ("b.example.com", "beta", "  edges:\n    target: {to: alpha}\n"))
		beta, _ := r.ByIdentity("b.example.com/beta")
		if got := beta.Edges["target"].To; got != "b.example.com/alpha" {
			t.Fatalf("edge target = %q, want the in-authority alpha", got)
		}
		// The full reference always addresses the other authority's kind.
		r2 := install(t, authority("b.example.com")+typ("b.example.com", "alpha", "")+
			typ("b.example.com", "beta", "  edges:\n    target: {to: a.example.com/alpha}\n"))
		beta2, _ := r2.ByIdentity("b.example.com/beta")
		if got := beta2.Edges["target"].To; got != "a.example.com/alpha" {
			t.Fatalf("qualified edge target = %q", got)
		}
	})

	t.Run("ambiguous is an error", func(t *testing.T) {
		_, err := vocabulary.LoadFS(fstest.MapFS{
			"a.yaml": {Data: []byte(alpha)},
			"b.yaml": {Data: []byte(authority("b.example.com") + typ("b.example.com", "alpha", ""))},
			"c.yaml": {Data: []byte(authority("c.example.com") +
				typ("c.example.com", "gamma", "  edges:\n    target: {to: alpha}\n"))},
		})
		if err == nil {
			t.Fatal("expected an ambiguity error")
		}
		if !strings.Contains(err.Error(), "ambiguous type") ||
			!strings.Contains(err.Error(), "a.example.com/alpha") {
			t.Fatalf("error = %v", err)
		}
		if !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("expected ErrValidation, got %v", err)
		}
	})

	t.Run("unknown target", func(t *testing.T) {
		_, err := vocabulary.LoadFS(fstest.MapFS{
			"a.yaml": {Data: []byte(alpha)},
			"c.yaml": {Data: []byte(authority("c.example.com") +
				typ("c.example.com", "gamma", "  edges:\n    target: {to: a.example.com/nosuch}\n"))},
		})
		if err == nil || !strings.Contains(err.Error(), "unknown target type") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("edges are a mapping", func(t *testing.T) {
		_, err := vocabulary.LoadFS(fstest.MapFS{
			"a.yaml": {Data: []byte(alpha)},
			"c.yaml": {Data: []byte(authority("c.example.com") +
				typ("c.example.com", "gamma", "  edges:\n    - {rel: target, to: alpha}\n"))},
		})
		if err == nil || !strings.Contains(err.Error(), "not a list") {
			t.Fatalf("error = %v", err)
		}
	})
}

// TestReferencePropertyType covers the reference property kind:
// `type: reference` is admitted, a `to:` constraint resolves like an edge
// target (bare name to full identity), `to: any` and absent leave it
// unconstrained, and an unknown `to:` is a load error.
func TestReferencePropertyType(t *testing.T) {
	authority := func(name string) string {
		return `kind: core.substrate.reamde.dev/authority
metadata: {id: ` + name + `}
data: {version: v1}
`
	}
	typ := func(g, name, data string) string {
		return `---
kind: core.substrate.reamde.dev/kind
metadata: {id: ` + g + `/` + name + `}
data:
  authority: ` + g + `
  names: {singular: ` + name + `, plural: ` + name + `s}
` + data
	}
	alpha := authority("a.example.com") + typ("a.example.com", "alpha", "")

	t.Run("to resolves to a full identity", func(t *testing.T) {
		r := loadFixture(t, map[string]string{
			"a.yaml": alpha,
			"b.yaml": authority("b.example.com") +
				typ("b.example.com", "beta", "  properties:\n    ptr: {type: reference, to: alpha}\n"),
		})
		beta, _ := r.ByIdentity("b.example.com/beta")
		p := beta.Props["ptr"]
		if p.Datatype != vocabulary.DatatypeReference {
			t.Fatalf("kind = %q", p.Datatype)
		}
		if p.To != "a.example.com/alpha" {
			t.Fatalf("to = %q", p.To)
		}
	})

	t.Run("to any is unconstrained", func(t *testing.T) {
		r := loadFixture(t, map[string]string{
			"b.yaml": authority("b.example.com") +
				typ("b.example.com", "beta", "  properties:\n    ptr: {type: reference, to: any}\n"),
		})
		beta, _ := r.ByIdentity("b.example.com/beta")
		if got := beta.Props["ptr"].To; got != vocabulary.ToAny {
			t.Fatalf("to = %q, want any", got)
		}
	})

	t.Run("absent to is unconstrained", func(t *testing.T) {
		r := loadFixture(t, map[string]string{
			"b.yaml": authority("b.example.com") +
				typ("b.example.com", "beta", "  properties:\n    ptr: {type: reference}\n"),
		})
		beta, _ := r.ByIdentity("b.example.com/beta")
		if got := beta.Props["ptr"].To; got != "" {
			t.Fatalf("to = %q, want empty", got)
		}
	})

	t.Run("repeated reference", func(t *testing.T) {
		r := loadFixture(t, map[string]string{
			"a.yaml": alpha,
			"b.yaml": authority("b.example.com") +
				typ("b.example.com", "beta", "  properties:\n    ptrs: {type: reference, to: alpha, repeated: true}\n"),
		})
		beta, _ := r.ByIdentity("b.example.com/beta")
		if !beta.Props["ptrs"].Repeated {
			t.Fatal("expected repeated")
		}
	})

	t.Run("unknown to is an error", func(t *testing.T) {
		_, err := vocabulary.LoadFS(fstest.MapFS{
			"b.yaml": {Data: []byte(authority("b.example.com") +
				typ("b.example.com", "beta", "  properties:\n    ptr: {type: reference, to: nosuch}\n"))},
		})
		if err == nil || !strings.Contains(err.Error(), "unknown type") {
			t.Fatalf("error = %v", err)
		}
	})
}

// The same rule governs capabilities, and both resolutions are deferred to
// Finalize/Install: documents may be loaded in any order and still see each
// other.
func TestCapabilityResolutionRules(t *testing.T) {
	capAuthority := func(authority string) string {
		return `kind: core.substrate.reamde.dev/authority
metadata: {id: ` + authority + `}
data: {version: v1}
---
kind: core.substrate.reamde.dev/trait
metadata: {id: ` + authority + `/ranked}
data:
  authority: ` + authority + `
  properties: {score: int, label: string}
`
	}
	binder := func(props string) string {
		return `kind: core.substrate.reamde.dev/authority
metadata: {id: a.example.com}
data: {version: v1}
---
kind: core.substrate.reamde.dev/kind
metadata: {id: a.example.com/thing}
data:
  authority: a.example.com
  names: {singular: thing, plural: things}
  traits: [ranked]
  properties:
` + props
	}

	t.Run("across authorities with a contract", func(t *testing.T) {
		mk := func(props string) fstest.MapFS {
			return fstest.MapFS{
				// Sorts before z.yaml, so the binding authority is parsed first.
				"a.yaml": {Data: []byte(binder(props))},
				"z.yaml": {Data: []byte(capAuthority("caps.example.com"))},
			}
		}
		if _, err := vocabulary.LoadFS(mk("    score: {type: int}\n")); err == nil {
			t.Fatal("a bound capability's properties are a contract")
		} else if !strings.Contains(err.Error(), "label") {
			t.Fatalf("error = %v", err)
		}
		r, err := vocabulary.LoadFS(mk("    score: {type: int}\n    label: {type: string}\n"))
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		thing, _ := r.ByIdentity("a.example.com/thing")
		if !thing.Implements("Ranked") {
			t.Fatalf("capabilities = %+v", thing.Traits)
		}
	})

	t.Run("ambiguous is an error", func(t *testing.T) {
		_, err := vocabulary.LoadFS(fstest.MapFS{
			"a.yaml": {Data: []byte(binder("    score: {type: int}\n    label: {type: string}\n"))},
			"y.yaml": {Data: []byte(capAuthority("caps.example.com"))},
			"z.yaml": {Data: []byte(capAuthority("caps2.example.com"))},
		})
		if err == nil || !strings.Contains(err.Error(), "ambiguous trait") {
			t.Fatalf("error = %v", err)
		}
	})
}

// --- install / uninstall -------------------------------------------------

// A capability no authority declares is a load error, not a silent no-op.
func TestUnknownCapabilityRejected(t *testing.T) {
	r := loadVocab(t)
	const authority = "x.connectors.example.com"
	g, err := vocabulary.ParseManifest(vocabulary.Manifest{
		Name: "x", Authority: authority,
		Manifests: []map[string]any{
			vocabulary.AuthorityManifest(authority, "v1"),
			vocabulary.KindManifest(authority,
				map[string]any{"singular": "thing", "plural": "things"},
				map[string]any{"traits": []any{"nosuchcapability"}}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Install(g); err == nil {
		t.Fatal("expected an unknown-capability error")
	} else if !strings.Contains(err.Error(), "unknown trait") {
		t.Fatalf("error = %v", err)
	}
	if _, ok := r.ByIdentity(authority + "/thing"); ok {
		t.Fatal("a failed install must not leave the authority behind")
	}
}

// An installed authority can bind a loaded capability across an authority boundary at
// install time, and its variant is validated the same way a file's is.
func TestInstalledGroupBindsLoadedCapability(t *testing.T) {
	r := loadVocab(t)
	const authority = "media.connectors.example.com"
	mk := func(binding string) *vocabulary.Authority {
		t.Helper()
		g, err := vocabulary.ParseManifest(vocabulary.Manifest{
			Name: "media", Authority: authority,
			Manifests: []map[string]any{
				vocabulary.AuthorityManifest(authority, "v1"),
				vocabulary.KindManifest(authority,
					map[string]any{"singular": "clip", "plural": "clips"},
					map[string]any{
						"traits":     []any{binding},
						"properties": map[string]any{"mediaRef": map[string]any{"type": "url"}},
					}),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	if err := r.Install(mk("temporal")); err == nil {
		t.Fatal("a variant capability must be bound with a variant")
	} else if !strings.Contains(err.Error(), "requires a variant") {
		t.Fatalf("error = %v", err)
	}
	if err := r.Install(mk("temporal(nosuchvariant)")); err == nil {
		t.Fatal("an undeclared variant must fail")
	}
	if err := r.Install(mk("temporal(range)")); err != nil {
		t.Fatalf("install: %v", err)
	}
	clip, _ := r.ByIdentity(authority + "/clip")
	if !clip.Implements("Temporal") {
		t.Fatalf("installed type capabilities = %+v", clip.Traits)
	}
	if !clip.UsesHot("at") || !clip.UsesHot("endsAt") {
		t.Fatalf("installed hot properties = %v", clip.HotColumns)
	}
}

func TestInstallBumpsVersion(t *testing.T) {
	r := loadVocab(t)
	before := r.Version()
	m := gmailManifest()
	// Its one type reaches into another authority for its owner.
	m.Manifests = append(m.Manifests, vocabulary.KindManifest(m.Authority,
		map[string]any{"singular": "label", "plural": "labels"},
		map[string]any{"edges": map[string]any{
			"account": map[string]any{"to": "account", "required": true, "ownerRef": true},
		}}))
	g, err := vocabulary.ParseManifest(m)
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if err := r.Install(g); err != nil {
		t.Fatalf("install: %v", err)
	}
	if r.Version() <= before {
		t.Fatal("install must bump the registry version")
	}
	ty, ok := r.ByIdentity("gmail.connectors.example.com/label")
	if !ok {
		t.Fatal("installed type missing")
	}
	if ty.Source != vocabulary.SourceInstalled {
		t.Fatalf("source = %q", ty.Source)
	}
	if got := ty.Edges["account"].To; got != "core.example.com/account" {
		t.Fatalf("cross-authority edge = %q", got)
	}
	if !contains(r.Actors(), "connector:gmail") {
		t.Fatal("installed actor missing")
	}
	if g, ok := r.ActorAuthority("connector:gmail"); !ok || g != "gmail.connectors.example.com" {
		t.Fatalf("actor authority = %q", g)
	}
}

// A connector's mapping installs with its authority: MappingManifest is the
// constructor registration calls, and Install runs the same validation the
// loader does — the registry-wide rules included.
func TestInstalledMapping(t *testing.T) {
	r := loadVocab(t)
	const authority = "slack.connectors.example.com"
	mk := func(mapping map[string]any) *vocabulary.Authority {
		t.Helper()
		g, err := vocabulary.ParseManifest(vocabulary.Manifest{
			Name: "slack", Authority: authority,
			Manifests: []map[string]any{
				vocabulary.AuthorityManifest(authority, "v1"),
				vocabulary.ActorManifest(authority, "connector:slack"),
				vocabulary.KindManifest(authority,
					map[string]any{"singular": "slackuser", "plural": "slackusers"},
					map[string]any{
						"properties": map[string]any{
							"realName": map[string]any{"type": "string"},
							"email":    map[string]any{"type": "email"},
						},
						"edges": map[string]any{
							"person": map[string]any{"to": "vocab.example.com/contact", "required": true},
						},
					}),
				mapping,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	good := vocabulary.MappingManifest(authority, "slackusercontact", map[string]any{
		"from": authority + "/slackuser",
		"to":   "vocab.example.com/contact",
		"edge": "person",
		"match": []any{
			map[string]any{"from": "email", "to": "emails"},
		},
		"map": map[string]any{
			"name":   "realName",
			"emails": map[string]any{"path": "email", "merge": "union"},
		},
	})
	// A scalar path onto a union target is a singleton contribution — legal.
	if err := r.Install(mk(good)); err != nil {
		t.Fatalf("install: %v", err)
	}
	m, ok := r.MappingFor(authority + "/slackuser")
	if !ok || m.To != "vocab.example.com/contact" {
		t.Fatalf("installed mapping = %+v", m)
	}
	// Two authorities now map onto contact: both declared actors are authority
	// actors (the machine tier's connector half, primitives §6), and
	// MappingsTo orders by identity.
	for _, a := range []string{"connector:google", "connector:slack"} {
		if _, ok := r.ActorAuthority(a); !ok {
			t.Fatalf("%s is not a declared authority actor", a)
		}
	}
	tos := r.MappingsTo("vocab.example.com/contact")
	// Ordered by identity, which is now "<authority>/<name>": the slack
	// authority sorts before vocab's.
	if len(tos) != 2 || tos[0].Name != "slackusercontact" || tos[1].Name != "googlecontactcontact" {
		names := make([]string, 0, len(tos))
		for _, m := range tos {
			names = append(names, m.Identity())
		}
		t.Fatalf("mappings to contact = %v", names)
	}

	// A mapping onto a source type violates the bipartite rule at INSTALL:
	// googlecontact is already a mapping's from.
	bad := vocabulary.MappingManifest("bad.connectors.example.com", "usergooglecontact", map[string]any{
		"from": "bad.connectors.example.com/user",
		"to":   "vocab.example.com/googlecontact",
		"edge": "record",
	})
	g, err := vocabulary.ParseManifest(vocabulary.Manifest{
		Name: "bad", Authority: "bad.connectors.example.com",
		Manifests: []map[string]any{
			vocabulary.AuthorityManifest("bad.connectors.example.com", "v1"),
			vocabulary.KindManifest("bad.connectors.example.com",
				map[string]any{"singular": "user", "plural": "users"},
				map[string]any{"edges": map[string]any{
					"record": map[string]any{"to": "vocab.example.com/googlecontact", "required": true},
				}}),
			bad,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Install(g); err == nil {
		t.Fatal("expected a bipartite violation")
	} else if !strings.Contains(err.Error(), "bipartite") {
		t.Fatalf("error = %v", err)
	}
	if _, ok := r.AuthorityByName("bad.connectors.example.com"); ok {
		t.Fatal("a failed install must not leave the authority behind")
	}
}

// A manifest is one authority's worth of documents; anything else is a mistake
// the loader must not paper over.
func TestManifestShapeRejected(t *testing.T) {
	const authority = "x.connectors.example.com"
	for name, m := range map[string]vocabulary.Manifest{
		"no authority manifest": {Name: "x", Authority: authority, Manifests: []map[string]any{
			vocabulary.KindManifest(authority, map[string]any{"singular": "thing", "plural": "things"}, nil),
		}},
		"two authorities": {Name: "x", Authority: authority, Manifests: []map[string]any{
			vocabulary.AuthorityManifest(authority, "v1"),
			vocabulary.AuthorityManifest("y.connectors.example.com", "v1"),
		}},
		"authority mismatch": {Name: "x", Authority: authority, Manifests: []map[string]any{
			vocabulary.AuthorityManifest("y.connectors.example.com", "v1"),
		}},
		"empty": {Name: "x", Authority: authority},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := vocabulary.ParseManifest(m); err == nil {
				t.Fatal("expected a validation error")
			} else if !errors.Is(err, substrate.ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
		})
	}
}

func TestCloneIsolatesInstalls(t *testing.T) {
	base := loadVocab(t)
	clone := base.Clone()
	g, err := vocabulary.ParseManifest(gmailManifest())
	if err != nil {
		t.Fatal(err)
	}
	if err := clone.Install(g); err != nil {
		t.Fatal(err)
	}
	if _, ok := base.ByIdentity("gmail.connectors.example.com/cursor"); ok {
		t.Fatal("install leaked into the base registry")
	}
	if _, ok := clone.ByIdentity("gmail.connectors.example.com/cursor"); !ok {
		t.Fatal("install missing from the clone")
	}
}

func TestResolveAmbiguity(t *testing.T) {
	r := loadVocab(t)
	ty, err := r.Resolve("contact")
	if err != nil || ty.Identity != "vocab.example.com/contact" {
		t.Fatalf("resolve short name: %v %v", ty, err)
	}
	if _, err := r.Resolve("nosuch"); err == nil {
		t.Fatal("expected unknown type error")
	}
	const authority = "dup.connectors.example.com"
	g, err := vocabulary.ParseManifest(vocabulary.Manifest{
		Name: "dup", Authority: authority,
		Manifests: []map[string]any{
			vocabulary.AuthorityManifest(authority, "v1"),
			vocabulary.KindManifest(authority, map[string]any{"singular": "contact", "plural": "contacts"}, nil),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Install(g); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resolve("contact"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
}

// --- behavior on the parsed model ----------------------------------------

func TestInterfacesAndMachines(t *testing.T) {
	r := loadVocab(t)
	temporal := map[string]bool{}
	for _, ty := range r.Implementing("Temporal") {
		temporal[ty.Identity] = true
	}
	if !temporal["vocab.example.com/task"] || !temporal["vocab.example.com/note"] {
		t.Fatalf("Temporal implementors = %v", temporal)
	}
	if temporal["vocab.example.com/contact"] {
		t.Fatal("contact should not implement Temporal")
	}
	status := r.Implementing("HasStatus")
	if len(status) != 1 || status[0].Identity != "vocab.example.com/task" {
		t.Fatalf("HasStatus = %v", status)
	}

	task, _ := r.ByIdentity("vocab.example.com/task")
	m := task.Machines["status"]
	// `initial` is ONE declared state: the per-actor map died with the guards.
	if m.Initial != "open" {
		t.Fatalf("initial = %q", m.Initial)
	}
	done := m.Transition("open", "done")
	if done == nil {
		t.Fatal("open→done missing")
	}
	if done.Stamps["doneAt"] != "now" {
		t.Fatalf("stamps = %v", done.Stamps)
	}
	if m.Transition("doing", "done").OnEnter != "applyDiff" {
		t.Fatal("onEnter lost")
	}
}

// A mapping is readable off the registry: MappingFor says a type is a source
// record, MappingsTo lists what manages a target, ActorAuthority answers the
// machine tier's connector half (§6.1, records 50–51, primitives §6).
func TestMappings(t *testing.T) {
	r := loadVocab(t)
	c, _ := r.ByIdentity("vocab.example.com/contact")
	g, _ := r.ByIdentity("vocab.example.com/googlecontact")

	m, ok := r.MappingFor(g.Identity)
	if !ok {
		t.Fatal("googlecontact carries a mapping")
	}
	if m.Identity() != "vocab.example.com/googlecontactcontact" ||
		m.From != g.Identity || m.To != c.Identity || m.Edge != "contact" {
		t.Fatalf("mapping = %+v", m)
	}
	if _, ok := r.MappingFor(c.Identity); ok {
		t.Fatal("a subject type is never a source record")
	}

	// Paths parse into their three forms and render back.
	if len(m.Match) != 1 {
		t.Fatalf("match = %+v", m.Match)
	}
	probe := m.Match[0]
	if probe.From != (vocabulary.Path{Prop: "emails", Field: "value", OverList: true}) ||
		probe.From.String() != "emails[].value" || probe.To != "emails" {
		t.Fatalf("probe = %+v", probe)
	}
	if len(m.MapOrder) != 2 || m.MapOrder[0] != "emails" || m.MapOrder[1] != "name" {
		t.Fatalf("map order = %v", m.MapOrder)
	}
	if rule := m.Map["name"]; rule.Merge != vocabulary.MergeAtomic ||
		rule.Path != (vocabulary.Path{Prop: "name", Field: "displayName"}) {
		t.Fatalf("map.name = %+v", rule)
	}
	if rule := m.Map["emails"]; rule.Merge != vocabulary.MergeUnion || !rule.Path.OverList {
		t.Fatalf("map.emails = %+v", rule)
	}
	if m.SourceYAML == "" || m.Definition["edge"] != "contact" {
		t.Fatalf("mapping definition/source lost: %+v", m)
	}

	// The target side.
	tos := r.MappingsTo(c.Identity)
	if len(tos) != 1 || tos[0] != m {
		t.Fatalf("mappings to contact = %v", tos)
	}
	if len(r.MappingsTo(g.Identity)) != 0 {
		t.Fatal("a source record is managed by nothing")
	}
	// The mapping authority's declared actor is an authority actor — the machine
	// tier's connector half (primitives §6); a stranger is not.
	if grp, ok := r.ActorAuthority("connector:google"); !ok || grp != "vocab.example.com" {
		t.Fatalf("ActorAuthority(google.connectors.substrate.reamde.dev) = %q, %v", grp, ok)
	}
	if _, ok := r.ActorAuthority("stranger.example.com"); ok {
		t.Fatal("an undeclared actor is nobody's authority actor")
	}

	// The path type-checker the engine reuses.
	if p, rep, err := vocabulary.PathProperty(g, probe.From); err != nil ||
		p.Datatype != vocabulary.DatatypeEmail || !rep {
		t.Fatalf("PathProperty(emails[].value) = %+v %v %v", p, rep, err)
	}
	if p, rep, err := vocabulary.PathProperty(g, m.Map["name"].Path); err != nil ||
		p.Datatype != vocabulary.DatatypeString || rep {
		t.Fatalf("PathProperty(name.displayName) = %+v %v %v", p, rep, err)
	}
	// A bare target name is Path{Prop: name}; title is column-backed but legal.
	if p, _, err := vocabulary.PathProperty(c, vocabulary.Path{Prop: "title"}); err != nil ||
		p.Datatype != vocabulary.DatatypeString {
		t.Fatalf("PathProperty(title) = %+v %v", p, err)
	}
	for _, bad := range []vocabulary.Path{
		{Prop: "nosuch"},
		{Prop: "name", Field: "nosuch"},
		{Prop: "emails", Field: "value"},                     // repeated: needs []
		{Prop: "name", Field: "displayName", OverList: true}, // not repeated
	} {
		if _, _, err := vocabulary.PathProperty(g, bad); err == nil {
			t.Errorf("PathProperty(%v) should fail", bad)
		}
	}
}

// The three path forms, and nothing else.
func TestParsePath(t *testing.T) {
	for s, want := range map[string]vocabulary.Path{
		"name":           {Prop: "name"},
		"name.first":     {Prop: "name", Field: "first"},
		"emails[].value": {Prop: "emails", Field: "value", OverList: true},
	} {
		p, err := vocabulary.ParsePath(s)
		if err != nil || p != want {
			t.Errorf("ParsePath(%q) = %+v, %v", s, p, err)
		}
		if p.String() != s {
			t.Errorf("String() = %q, want %q", p.String(), s)
		}
	}
	for _, bad := range []string{"", "a[]", "a.b.c", "a[].b.c", "a[]b", "A.b", "a.B", "a_b", ".b", "a."} {
		if _, err := vocabulary.ParsePath(bad); err == nil {
			t.Errorf("ParsePath(%q) should fail", bad)
		}
	}
}

// A mapping's rules are loader-enforced: the edge's shape, every
// path against both declared types, one mapping per source type, and the
// registry-wide bipartite rule.
func TestMappingRules(t *testing.T) {
	head := `kind: core.substrate.reamde.dev/authority
metadata: {id: x.example.com}
data: {version: v1alpha1}
---
kind: core.substrate.reamde.dev/kind
metadata: {id: x.example.com/person}
data:
  authority: x.example.com
  names: {singular: person, plural: people}
  properties:
    name: {type: string}
    primaryEmail: {type: email}
    emails: {type: email, repeated: true}
    score: {type: int}
    prominence:
      type: state
      states: [utility, known]
      initial: utility
      transitions: [{from: utility, to: known}]
---
kind: core.substrate.reamde.dev/kind
metadata: {id: x.example.com/rec}
data:
  authority: x.example.com
  names: {singular: rec, plural: recs}
  properties:
    name:
      type: object
      fields: {displayName: {type: string}}
    emails:
      type: object
      repeated: true
      fields: {value: email}
    count: {type: int}
  edges:
    person: {to: person, required: true}
    other: {to: person}
`
	mapping := func(name, body string) string {
		return head + `---
kind: core.substrate.reamde.dev/recordmapping
metadata: {id: x.example.com/` + name + `}
data:
  authority: x.example.com
` + body
	}
	recperson := func(rules string) string {
		return mapping("recperson", `  from: x.example.com/rec
  to: x.example.com/person
  edge: person
`+rules)
	}
	bad := map[string]string{
		"missing edge": mapping("recperson", `  from: x.example.com/rec
  to: x.example.com/person
  edge: nosuch
`),
		// `other` points at person, but the mapping's edge must be the one
		// the data.to names AND carry the subject shape.
		"optional edge": mapping("recperson", `  from: x.example.com/rec
  to: x.example.com/person
  edge: other
`),
		"wrong edge target": mapping("recperson", `  from: x.example.com/rec
  to: x.example.com/rec
  edge: person
`),
		"from not in authority": mapping("recperson", `  from: x.example.com/nosuch
  to: x.example.com/person
  edge: person
`),
		"from must be full": mapping("recperson", `  from: rec
  to: x.example.com/person
  edge: person
`),
		"to must be full": mapping("recperson", `  from: x.example.com/rec
  to: person
  edge: person
`),
		"unknown to type": mapping("recperson", `  from: x.example.com/rec
  to: y.example.com/nosuch
  edge: person
`),
		"union onto scalar": recperson(`  map:
    name: {path: name.displayName, merge: union}
`),
		"repeated source onto scalar": recperson(`  map:
    primaryEmail: {path: "emails[].value"}
`),
		"path into undeclared field": recperson(`  map:
    name: name.nosuch
`),
		"path into undeclared property": recperson(`  map:
    name: nosuch.displayName
`),
		"missing [] on a repeated object": recperson(`  map:
    emails: {path: "emails.value", merge: union}
`),
		"kinds disagree": recperson(`  map:
    score: name.displayName
`),
		"a state is never a map target": recperson(`  map:
    prominence: name.displayName
`),
		"unknown merge": recperson(`  map:
    name: {path: name.displayName, merge: fuse}
`),
		"bare [] path": recperson(`  map:
    emails: {path: "emails[]", merge: union}
`),
		"match is a list": recperson(`  match: {emails: value}
`),
		"match to must be short-string": recperson(`  match:
    - {from: "emails[].value", to: score}
`),
		"match from must be short-string": recperson(`  match:
    - {from: count, to: emails}
`),
		"two mappings for one from-type": recperson("") + `---
kind: core.substrate.reamde.dev/recordmapping
metadata: {id: x.example.com/recperson2}
data:
  authority: x.example.com
  from: x.example.com/rec
  to: x.example.com/person
  edge: person
`,
		// person is recperson's `to` AND personorg's `from`: the
		// source→subject graph stays bipartite.
		"bipartite violation": recperson("") + `---
kind: core.substrate.reamde.dev/kind
metadata: {id: x.example.com/org}
data:
  authority: x.example.com
  names: {singular: org, plural: orgs}
  properties: {name: {type: string}}
---
kind: core.substrate.reamde.dev/recordmapping
metadata: {id: x.example.com/personorg}
data:
  authority: x.example.com
  from: x.example.com/person
  to: x.example.com/org
  edge: employer
`,
		// No edge anywhere may land on a mapped source type: resolution
		// stays one hop deep (§6.2).
		"edge onto a source record": recperson("") + `---
kind: core.substrate.reamde.dev/kind
metadata: {id: x.example.com/note}
data:
  authority: x.example.com
  names: {singular: note, plural: notes}
  edges: {about: {to: rec}}
`,
		"unknown mapping key": recperson(`  fuse: true
`),
	}
	for name, src := range bad {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{"x.example.com/all.yaml": &fstest.MapFile{Data: []byte(src)}}
			if _, err := vocabulary.LoadFS(fsys); err == nil {
				t.Fatal("expected a load error")
			} else if !errors.Is(err, substrate.ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
		})
	}

	// `projects:` is deleted from the DSL, and the error names its
	// replacement rather than treating the key as unknown.
	t.Run("projects is deleted", func(t *testing.T) {
		src := head + `---
kind: core.substrate.reamde.dev/kind
metadata: {id: x.example.com/two}
data:
  authority: x.example.com
  names: {singular: two, plural: twos}
  edges: {person: {to: person, required: true, projects: true}}
`
		fsys := fstest.MapFS{"x.example.com/all.yaml": &fstest.MapFile{Data: []byte(src)}}
		_, err := vocabulary.LoadFS(fsys)
		if err == nil {
			t.Fatal("expected a load error")
		}
		if !strings.Contains(err.Error(), "recordmapping") ||
			!strings.Contains(err.Error(), `"projects" is deleted`) {
			t.Fatalf("error = %v", err)
		}
	})

	// Empty match and map are legal: a link-only mapping (bookedition→book)
	// still makes the from a source record and pins the subject edge.
	t.Run("link-only mapping", func(t *testing.T) {
		fsys := fstest.MapFS{"x.example.com/all.yaml": &fstest.MapFile{Data: []byte(recperson(""))}}
		r, err := vocabulary.LoadFS(fsys)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		m, ok := r.MappingFor("x.example.com/rec")
		if !ok || len(m.Match) != 0 || len(m.Map) != 0 {
			t.Fatalf("link-only mapping = %+v", m)
		}
	})
}

func TestTemplates(t *testing.T) {
	tpl, err := vocabulary.ParseTemplate("{author.name}: {snippet}")
	if err != nil {
		t.Fatal(err)
	}
	got := tpl.Render(testResolver{
		edges:   map[string]string{"author.name": "Alex"},
		snippet: "hello there",
	})
	if got != "Alex: hello there" {
		t.Fatalf("render = %q", got)
	}
	if refs := tpl.EdgeRefs(); len(refs) != 1 || refs[0] != "author" {
		t.Fatalf("edge refs = %v", refs)
	}

	fallback, err := vocabulary.ParseTemplate("{name|participants}")
	if err != nil {
		t.Fatal(err)
	}
	if got := fallback.Render(testResolver{props: map[string]string{"name": "#general"}}); got != "#general" {
		t.Fatalf("render = %q", got)
	}
	if got := fallback.Render(testResolver{edges: map[string]string{"participants": "Alex, Nina"}}); got != "Alex, Nina" {
		t.Fatalf("fallback render = %q", got)
	}
	if got := fallback.Render(testResolver{}); got != "" {
		t.Fatalf("empty render = %q", got)
	}

	// A camelCase name is legal now; a snake one is not a property name.
	if _, err := vocabulary.ParseTemplate("{displayName}"); err != nil {
		t.Fatalf("camelCase template: %v", err)
	}
	for _, bad := range []string{"{", "}", "{}", "{Name}", "{a.b.c}", "{a|}", "{display_name}"} {
		if _, err := vocabulary.ParseTemplate(bad); err == nil {
			t.Fatalf("expected %q to fail", bad)
		}
	}
}

// Template tokens validate against the type's own declarations at load: a
// bare token is a property, an edge or a column-backed property; a dotted one
// is an edge's property or ONE LEVEL into an object property's declared
// fields — so `{name.displayName}` works and a typo fails on the
// manifest, not as an empty title.
func TestTemplateTokensValidate(t *testing.T) {
	typ := func(template string) string {
		return `kind: core.substrate.reamde.dev/authority
metadata: {id: x.example.com}
data: {version: v1alpha1}
---
kind: core.substrate.reamde.dev/kind
metadata: {id: x.example.com/card}
data:
  authority: x.example.com
  names: {singular: card, plural: cards}
  displayTemplate: "` + template + `"
  properties:
    label: {type: string}
    name:
      type: object
      fields: {displayName: {type: string}}
  edges:
    owner: {to: card}
`
	}
	load := func(src string) error {
		fsys := fstest.MapFS{"x.example.com/all.yaml": &fstest.MapFile{Data: []byte(src)}}
		_, err := vocabulary.LoadFS(fsys)
		return err
	}
	for name, good := range map[string]string{
		"declared property":   "{label}",
		"object field":        "{name.displayName}",
		"edge property":       "{owner.label}",
		"column-backed":       "{title|body}",
		"edge titles":         "{owner}",
		"snippet":             "{snippet}",
		"fallback into field": "{label|name.displayName}",
	} {
		if err := load(typ(good)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for name, bad := range map[string]string{
		"undeclared property": "{nope}",
		"undeclared field":    "{name.nope}",
		"undeclared edge":     "{friend.label}",
	} {
		err := load(typ(bad))
		if err == nil {
			t.Fatalf("%s: expected a load error", name)
		}
		if !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("%s: expected ErrValidation, got %v", name, err)
		}
	}
}

type testResolver struct {
	props   map[string]string
	edges   map[string]string
	snippet string
}

func (r testResolver) Prop(n string) string { return r.props[n] }
func (r testResolver) Snippet() string      { return r.snippet }
func (r testResolver) Edge(rel, prop string) string {
	if prop == "" {
		return r.edges[rel]
	}
	return r.edges[rel+"."+prop]
}

// --- strictness ----------------------------------------------------------

// Unknown keys, unknown property types and mis-spelled identities are hard
// errors: a manifest the loader half-understands is a schema nobody can trust.
func TestManifestValidationRejected(t *testing.T) {
	const head = `kind: core.substrate.reamde.dev/authority
metadata: {id: x.example.com}
data: {version: v1}
---
`
	typ := func(data string) string {
		return head + `kind: core.substrate.reamde.dev/kind
metadata: {id: x.example.com/contact}
data:
  authority: x.example.com
` + data
	}
	cases := map[string]string{
		// the envelope
		"unknown type": head + `kind: core.substrate.reamde.dev/schemawidget
metadata: {id: x.example.com/w}
data: {authority: x.example.com}
`,
		"wrong authority": head + `kind: x.example.com/kind
metadata: {id: x.example.com/contact}
data:
  authority: x.example.com
  names: {singular: contact, plural: contacts}
`,
		"unknown envelope key": head + `kind: core.substrate.reamde.dev/kind
metadata: {id: x.example.com/contact}
extra: nope
data:
  authority: x.example.com
  names: {singular: contact, plural: contacts}
`,
		// The Kubernetes envelope is DELETED, and each key names what took
		// its job rather than reading as an unknown key.
		"apiVersion is deleted": head + `apiVersion: core.substrate.reamde.dev/v1alpha1
type: kind
metadata: {id: x.example.com/contact}
data:
  authority: x.example.com
  names: {singular: contact, plural: contacts}
`,
		"group is deleted": head + `group: core.substrate.reamde.dev
kind: core.substrate.reamde.dev/kind
metadata: {id: x.example.com/contact}
data:
  authority: x.example.com
  names: {singular: contact, plural: contacts}
`,
		"type is deleted": head + `type: kind
metadata: {id: x.example.com/contact}
data:
  authority: x.example.com
  names: {singular: contact, plural: contacts}
`,
		"spec is deleted": head + `kind: core.substrate.reamde.dev/kind
metadata: {id: x.example.com/contact}
spec:
  authority: x.example.com
  names: {singular: contact, plural: contacts}
`,
		"metadata.name is deleted": head + `kind: core.substrate.reamde.dev/kind
metadata: {name: x.example.com/contact}
data:
  authority: x.example.com
  names: {singular: contact, plural: contacts}
`,
		"no metadata.id": head + `kind: core.substrate.reamde.dev/kind
data:
  authority: x.example.com
  names: {singular: contact, plural: contacts}
`,
		"unnamespaced label": head + `kind: core.substrate.reamde.dev/kind
metadata:
  id: x.example.com/contact
  labels: {owner: me}
data:
  authority: x.example.com
  names: {singular: contact, plural: contacts}
`,
		"orphan document": `kind: core.substrate.reamde.dev/kind
metadata: {id: y.example.com/contact}
data:
  authority: y.example.com
  names: {singular: contact, plural: contacts}
`,
		"identity mismatch": head + `kind: core.substrate.reamde.dev/kind
metadata: {id: x.example.com/contacts}
data:
  authority: x.example.com
  names: {singular: contact, plural: contacts}
`,
		"duplicate type": head + `kind: core.substrate.reamde.dev/kind
metadata: {id: x.example.com/contact}
data:
  authority: x.example.com
  names: {singular: contact, plural: contacts}
---
kind: core.substrate.reamde.dev/kind
metadata: {id: x.example.com/contact}
data:
  authority: x.example.com
  names: {singular: contact, plural: contacts}
`,
		"duplicate authority": head + `kind: core.substrate.reamde.dev/authority
metadata: {id: x.example.com}
data: {version: v1}
`,
		"duplicate actor": head + `kind: core.substrate.reamde.dev/actor
metadata: {id: owner}
data: {authority: x.example.com}
---
kind: core.substrate.reamde.dev/actor
metadata: {id: owner}
data: {authority: x.example.com}
`,
		"bad authority name": `kind: core.substrate.reamde.dev/authority
metadata: {id: Vocab}
data: {version: v1}
`,
		"snake actor": head + `kind: core.substrate.reamde.dev/actor
metadata: {id: my_actor}
data: {authority: x.example.com}
`,
		// the body DSL
		"missing names":     typ("  properties: {a: {type: string}}\n"),
		"missing plural":    typ("  names: {singular: contact}\n"),
		"bad type name":     typ("  names: {singular: my_contact, plural: mycontacts}\n"),
		"bad plural":        typ("  names: {singular: contact, plural: my_contacts}\n"),
		"unknown names key": typ("  names: {singular: contact, plural: contacts, short: c}\n"),
		// One casing rule: every declared name is camelCase.
		"capitalised property": typ(`  names: {singular: contact, plural: contacts}
  properties: {FirstName: {type: string}}
`),
		"snake property": typ(`  names: {singular: contact, plural: contacts}
  properties: {first_name: {type: string}}
`),
		"snake edge": typ(`  names: {singular: contact, plural: contacts}
  edges: {work_place: {to: contact}}
`),
		"snake stamp": typ(`  names: {singular: contact, plural: contacts}
  properties: {m: {type: state, states: [a, b], transitions: [{from: a, to: b, stamps: {done_at: now}}]}}
`),
		"snake capability property": head + `kind: core.substrate.reamde.dev/trait
metadata: {id: x.example.com/ranked}
data:
  authority: x.example.com
  properties: {top_score: int}
`,
		// Enum and state VALUES stay lowercase words — they are data.
		"capitalised state value": typ(`  names: {singular: contact, plural: contacts}
  properties: {m: {type: state, states: [Open], transitions: []}}
`),
		"snake enum value": typ(`  names: {singular: contact, plural: contacts}
  properties: {a: {type: enum, values: [in_progress]}}
`),
		"unknown property type": typ(`  names: {singular: contact, plural: contacts}
  properties: {a: {type: blob}}
`),
		"missing transitions": typ(`  names: {singular: contact, plural: contacts}
  properties: {m: {type: state, states: [open]}}
`),
		"initial is not a state": typ(`  names: {singular: contact, plural: contacts}
  properties: {m: {type: state, states: [open], initial: shut, transitions: []}}
`),
		// `initial` is one declared state; the per-actor map died with the
		// guards.
		"initial map is deleted": typ(`  names: {singular: contact, plural: contacts}
  properties: {m: {type: state, states: [open], initial: {default: open}, transitions: []}}
`),
		// A transition carries no guard: anyone may perform any of them.
		"transition actor is deleted": typ(`  names: {singular: contact, plural: contacts}
  properties: {m: {type: state, states: [a, b], transitions: [{from: a, to: b, actor: owner}]}}
`),
		"bad stamp": typ(`  names: {singular: contact, plural: contacts}
  properties: {m: {type: state, states: [a, b], transitions: [{from: a, to: b, stamps: {x: yesterday}}]}}
`),
		"stamp collides with a property": typ(`  names: {singular: contact, plural: contacts}
  properties:
    doneAt: {type: string}
    m: {type: state, states: [a, b], transitions: [{from: a, to: b, stamps: {doneAt: now}}]}
`),
		"unknown machine key": typ(`  names: {singular: contact, plural: contacts}
  properties: {m: {type: state, states: [a], guards: []}}
`),
		// `machines:` is DELETED from the DSL (MODEL §11.4), not renamed.
		"machines is gone": typ(`  names: {singular: contact, plural: contacts}
  machines: {status: {states: [open, done]}}
`),
		"snake onEnter": typ(`  names: {singular: contact, plural: contacts}
  properties: {m: {type: state, states: [a, b], transitions: [{from: a, to: b, on_enter: applyDiff}]}}
`),
		"snake onEnter value": typ(`  names: {singular: contact, plural: contacts}
  properties: {m: {type: state, states: [a, b], transitions: [{from: a, to: b, onEnter: apply_diff}]}}
`),
		"snake ownerRef": typ(`  names: {singular: contact, plural: contacts}
  edges: {org: {to: contact, owner_ref: true}}
`),
		// A list is `repeated: true`; the bracketed spelling is deleted.
		"bracketed list type": typ(`  names: {singular: contact, plural: contacts}
  properties: {emails: {type: '[email]'}}
`),
		"a state property is not a list": typ(`  names: {singular: contact, plural: contacts}
  properties: {m: {type: state, repeated: true, states: [a, b], transitions: []}}
`),
		// Identity is the id and nothing else: everything that
		// matched by value is deleted, each naming what replaced it.
		"identifying is gone": typ(`  names: {singular: contact, plural: contacts}
  properties: {email: {type: email, identifying: true}}
`),
		"aliasNamespaces is gone": typ(`  names: {singular: contact, plural: contacts}
  aliasNamespaces: [google.contact]
`),
		"id strategy is gone": typ(`  names: {singular: contact, plural: contacts}
  id: from_alias
`),
		"merge is gone": typ(`  names: {singular: contact, plural: contacts}
  merge: auto
`),
		// `ref` is gone with the kind it constrained (MODEL §11.5).
		"ref is gone": typ(`  names: {singular: contact, plural: contacts}
  properties: {target: {type: ref}}
`),
		"unknown data key": typ(`  names: {singular: contact, plural: contacts}
  fields: {a: {type: string}}
`),
		// Object properties: fields are one level deep, declared
		// scalars only, camelCase, one value each — and never a refinement base.
		"nested object": typ(`  names: {singular: contact, plural: contacts}
  properties: {name: {type: object, fields: {inner: {type: object, fields: {a: {type: string}}}}}}
`),
		"json field": typ(`  names: {singular: contact, plural: contacts}
  properties: {name: {type: object, fields: {raw: {type: json}}}}
`),
		"secret field": typ(`  names: {singular: contact, plural: contacts}
  properties: {name: {type: object, fields: {key: {type: secret}}}}
`),
		"state field": typ(`  names: {singular: contact, plural: contacts}
  properties: {name: {type: object, fields: {m: {type: state, states: [a], transitions: []}}}}
`),
		"snake field": typ(`  names: {singular: contact, plural: contacts}
  properties: {name: {type: object, fields: {display_name: {type: string}}}}
`),
		"reserved field": typ(`  names: {singular: contact, plural: contacts}
  properties: {name: {type: object, fields: {title: {type: string}}}}
`),
		"repeated field": typ(`  names: {singular: contact, plural: contacts}
  properties: {name: {type: object, fields: {alias: {type: string, repeated: true}}}}
`),
		"object without fields": typ(`  names: {singular: contact, plural: contacts}
  properties: {name: {type: object}}
`),
		"fields on a scalar": typ(`  names: {singular: contact, plural: contacts}
  properties: {name: {type: string, fields: {a: {type: string}}}}
`),
		"embed on an object": typ(`  names: {singular: contact, plural: contacts}
  properties: {name: {type: object, embed: true, fields: {a: {type: string}}}}
`),
		"fts on an object": typ(`  names: {singular: contact, plural: contacts}
  properties: {name: {type: object, fts: true, fields: {a: {type: string}}}}
`),
		"object refinement base": head + `kind: core.substrate.reamde.dev/propertytype
metadata: {id: x.example.com/shape}
data:
  authority: x.example.com
  base: object
`,
		// The five column-backed properties every record already carries may
		// not be redeclared: two declarations of one name means
		// the write path validates against the wrong one.
		"title is reserved": typ(`  names: {singular: contact, plural: contacts}
  properties: {title: {type: string}}
`),
		"body is reserved": typ(`  names: {singular: contact, plural: contacts}
  properties: {body: {type: text}}
`),
		"at is reserved": typ(`  names: {singular: contact, plural: contacts}
  properties: {at: {type: datetime}}
`),
		"endsAt is reserved": typ(`  names: {singular: contact, plural: contacts}
  properties: {endsAt: {type: datetime}}
`),
		"dueAt is reserved": typ(`  names: {singular: contact, plural: contacts}
  properties: {dueAt: {type: datetime}}
`),
		// the old authority-file spellings are gone, not tolerated
		"old plural key": typ(`  names: {singular: contact, plural: contacts}
  plural: contacts
`),
		"old display_template": typ(`  names: {singular: contact, plural: contacts}
  display_template: "{name}"
`),
		"old property_precedence": typ(`  names: {singular: contact, plural: contacts}
  property_precedence: [owner]
`),
		"propertyPrecedence is gone": typ(`  names: {singular: contact, plural: contacts}
  propertyPrecedence: [api, connector:slack]
`),
		"props is gone": typ(`  names: {singular: contact, plural: contacts}
  props: {a: {type: string}}
`),
		"old one_of": `kind: core.substrate.reamde.dev/authority
metadata: {id: x.example.com}
data: {version: v1}
---
kind: core.substrate.reamde.dev/trait
metadata: {id: x.example.com/ranked}
data:
  authority: x.example.com
  one_of: {point: {at: datetime}}
`,
		"enum values": typ(`  names: {singular: contact, plural: contacts}
  properties: {a: {type: enum}}
`),
		"valueLabels unknown key": typ(`  names: {singular: contact, plural: contacts}
  properties: {a: {type: enum, values: [off, on], valueLabels: {off: "Off", nope: "No"}}}
`),
		"bad template": typ(`  names: {singular: contact, plural: contacts}
  displayTemplate: "{Name}"
`),
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := vocabulary.ParseYAML([]byte(src), vocabulary.SourceBuiltin); err == nil {
				t.Fatal("expected a validation error")
			} else if !errors.Is(err, substrate.ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
		})
	}
}

// The id alphabet: a writer's own key travels in a URL path segment, so
// nothing that needs percent-encoding beyond the one "/" a kind reference
// carries (a DECLARATION's id IS a kind reference).
func TestValidID(t *testing.T) {
	for _, ok := range []string{
		"9f2k", "calendar.substrate.reamde.dev/calendarevent", "people-c123", "people/c123",
		"gcal-abc_work", "T01:C01", "ada@example.com", "A~b",
	} {
		if !vocabulary.ValidID(ok) {
			t.Errorf("%q should be a legal id", ok)
		}
	}
	for _, bad := range []string{
		"", "-leading", ".leading", "has space", "a%2Fb",
		"a#b", "a?b", "a&b", strings.Repeat("a", vocabulary.MaxIDLen+1),
	} {
		if vocabulary.ValidID(bad) {
			t.Errorf("%q should not be a legal id", bad)
		}
	}
}

// --- the shipped tree ----------------------------------------------------

// The one test against the real files: what the engine cannot boot without.
// Everything else about the vocabulary is editorial and belongs in the files'
// own review, not in the loader's suite.
func TestShippedSchemaLoads(t *testing.T) {
	r, err := vocabulary.LoadDir("../../kinds/core.substrate.reamde.dev")
	if err != nil {
		t.Fatalf("load shipped schema: %v", err)
	}
	for _, ident := range []string{
		// The meta-model the projections write, and the machinery the engine
		// addresses by name.
		"core.substrate.reamde.dev/kind", "core.substrate.reamde.dev/authority",
		"core.substrate.reamde.dev/trait", "core.substrate.reamde.dev/propertytype",
		"core.substrate.reamde.dev/recordmapping",
		"core.substrate.reamde.dev/actor", "core.substrate.reamde.dev/repository", "core.substrate.reamde.dev/token",
		"core.substrate.reamde.dev/recordmerge", "core.substrate.reamde.dev/recordsplit",
	} {
		if _, ok := r.ByIdentity(ident); !ok {
			t.Errorf("%s missing", ident)
		}
	}
	// `projectionpolicy` does not exist in this build:
	// projection is a same-named copy with latest-write-wins, and nothing
	// ranks sources.
	if _, ok := r.ByIdentity("core.substrate.reamde.dev/projectionpolicy"); ok {
		t.Error("core.substrate.reamde.dev/projectionpolicy is not part of this build")
	}
	if _, err := r.ResolveTrait("core.substrate.reamde.dev", "temporal"); err != nil {
		t.Errorf("temporal capability: %v", err)
	}
	// The runtime the substrate maintains is core's too (2026-08-12): the
	// delivery plumbing and the agent loop's data, folded out of the former
	// automation.substrate.reamde.dev / ai.substrate.reamde.dev authorities.
	for _, ident := range []string{
		"core.substrate.reamde.dev/trigger", "core.substrate.reamde.dev/run",
		"core.substrate.reamde.dev/llmprovider", "core.substrate.reamde.dev/llmthread", "core.substrate.reamde.dev/llmmessage",
		"core.substrate.reamde.dev/agent", "core.substrate.reamde.dev/function", "core.substrate.reamde.dev/bundle",
	} {
		if _, ok := r.ByIdentity(ident); !ok {
			t.Errorf("%s missing", ident)
		}
	}
	// THE SHIPPED TREE IS CORE ALONE. Every other authority is a registry
	// bundle a repository imports; a domain authority reappearing here would
	// silently go back to being seeded into every new repository.
	for _, g := range r.AuthorityList() {
		if g.Name != vocabulary.AuthorityCore {
			t.Errorf("the shipped tree declares %s — only %s is seeded; vocabulary ships as an importable bundle under kinds/",
				g.Name, vocabulary.AuthorityCore)
		}
	}
	// The actor domain is closed and flat: the three doors
	// and the substrate's own hand.
	for _, a := range []string{"api", "console", "substratectl", "substrate"} {
		if !contains(r.Actors(), a) {
			t.Errorf("actor %q missing", a)
		}
	}
	// Identity is the kind reference <authority>/<name>, everywhere.
	for _, ty := range r.Kinds() {
		if ty.Identity != vocabulary.KindRef(ty.Authority, ty.Name) {
			t.Errorf("identity %q is not %s/%s", ty.Identity, ty.Authority, ty.Name)
		}
		if ty.SourceYAML == "" {
			t.Errorf("%s carries no source", ty.Identity)
		}
		// Every shipped property and edge carries a description:
		// the console's hover tooltip, one short sentence. Implicit properties
		// (machine stamps) are declared by a transition, not an author.
		for _, pn := range ty.PropOrder {
			if p := ty.Props[pn]; !p.Implicit && p.Description == "" {
				t.Errorf("%s.%s carries no description", ty.Identity, pn)
			}
		}
		for _, en := range ty.EdgeOrder {
			if ty.Edges[en].Description == "" {
				t.Errorf("%s.%s (edge) carries no description", ty.Identity, en)
			}
		}
	}
	for _, g := range r.AuthorityList() {
		if g.SourceYAML == "" {
			t.Errorf("authority %s carries no manifest text", g.Name)
		}
	}
}

// The shipped tree is block-style YAML all the way down (owner ruling: "why is
// this attribute shown with {} instead of proper yaml? … let's stop inlining
// ANY values"). No mapping or sequence anywhere below a document's `data:` may
// carry flow style — not even an empty `{}`; none exist, so none are allowed.
func TestShippedSchemaUsesBlockStyle(t *testing.T) {
	var flow func(path string, n *yaml.Node)
	flow = func(path string, n *yaml.Node) {
		if (n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode) &&
			n.Style&yaml.FlowStyle != 0 {
			t.Errorf("%s:%d: flow-style value below data:", path, n.Line)
		}
		for _, c := range n.Content {
			flow(path, c)
		}
	}
	docs := 0
	walk := func(root string) error {
		return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".yaml" {
				return err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			dec := yaml.NewDecoder(bytes.NewReader(body))
			for {
				var doc yaml.Node
				if err := dec.Decode(&doc); err != nil {
					if errors.Is(err, io.EOF) {
						return nil
					}
					return err
				}
				docs++
				root := doc.Content[0]
				for i := 0; i+1 < len(root.Content); i += 2 {
					if root.Content[i].Value == "data" {
						flow(path, root.Content[i+1])
					}
				}
			}
		})
	}
	// The seeded tree and the shipped VOCABULARY bundles — the same manifests
	// the tree used to hold, moved to the catalog and held to the same rule.
	roots := append([]string{"../../kinds/core.substrate.reamde.dev"}, shippedVocabularyDirs...)
	for _, root := range roots {
		if err := walk(root); err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if docs == 0 {
		t.Fatal("no shipped documents found; the walk is looking in the wrong place")
	}
}

// shippedVocabularyDirs are the five VOCABULARY bundles the binary ships and a
// repository IMPORTS — the authorities the creation seed used to write and no
// longer does.
var shippedVocabularyDirs = []string{
	"../../kinds/calendar.substrate.reamde.dev",
	"../../kinds/media.substrate.reamde.dev",
	"../../kinds/messaging.substrate.reamde.dev",
	"../../kinds/people.substrate.reamde.dev",
	"../../kinds/tasks.substrate.reamde.dev",
}

// The shipped vocabulary is no longer seeded, so its manifests are held to the
// same bar where they now live: they admit TOGETHER (messaging, calendar and
// media all point at people), they carry the shape a vocabulary bundle has —
// a bare authority, no config type, no callables — and they stay SHIPPED
// (`source: builtin`) even when built on an install path, which is what keeps
// `Person` from becoming `People_Person` in GraphQL just because the delivery
// changed.
func TestShippedVocabularyBundles(t *testing.T) {
	var docs []vocabulary.Document
	for _, dir := range shippedVocabularyDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			parsed, err := vocabulary.ParseStream(body)
			if err != nil {
				t.Fatalf("%s: %v", e.Name(), err)
			}
			docs = append(docs, parsed...)
		}
	}
	// SourceInstalled is the install path's answer; a vocabulary bundle
	// overrides it, because the binary publishes this vocabulary however it
	// was delivered.
	authorities, err := vocabulary.BuildAuthorities(docs, vocabulary.SourceInstalled)
	if err != nil {
		t.Fatalf("build the shipped vocabulary: %v", err)
	}
	// Into a repository that holds core and nothing else, all five at once —
	// the import order a fresh repository actually faces.
	r, err := vocabulary.LoadDir("../../kinds/core.substrate.reamde.dev")
	if err != nil {
		t.Fatalf("load the seeded tree: %v", err)
	}
	if err := r.InstallAll(authorities); err != nil {
		t.Fatalf("import the shipped vocabulary: %v", err)
	}
	want := map[string]string{
		"calendar.substrate.reamde.dev":  "calendar.substrate.reamde.dev/calendar",
		"media.substrate.reamde.dev":     "media.substrate.reamde.dev/media",
		"messaging.substrate.reamde.dev": "messaging.substrate.reamde.dev/messaging",
		"people.substrate.reamde.dev":    "people.substrate.reamde.dev/people",
		"tasks.substrate.reamde.dev":     "tasks.substrate.reamde.dev/tasks",
	}
	for _, g := range authorities {
		id, ok := want[g.Name]
		if !ok {
			t.Errorf("unexpected authority %s", g.Name)
			continue
		}
		if g.Bundle == nil {
			t.Errorf("%s ships no bundle document — it would not be importable", g.Name)
			continue
		}
		if !g.Bundle.Vocabulary || g.Bundle.ConfigType != "" {
			t.Errorf("%s: vocabulary=%v configType=%q", g.Name, g.Bundle.Vocabulary, g.Bundle.ConfigType)
		}
		if g.Bundle.Identity() != id {
			t.Errorf("%s bundle id = %q, want %q", g.Name, g.Bundle.Identity(), id)
		}
		if g.Source != vocabulary.SourceBuiltin {
			t.Errorf("%s source = %q — shipped vocabulary stays builtin however it is delivered", g.Name, g.Source)
		}
		if len(g.Functions) != 0 || len(g.Agents) != 0 {
			t.Errorf("%s ships callables — a vocabulary bundle is kinds and nothing else", g.Name)
		}
	}
	// Everything that maps onto people says so, so an import into a
	// core-only repository is refused with a legible reason.
	for _, name := range []string{"calendar.substrate.reamde.dev", "media.substrate.reamde.dev", "messaging.substrate.reamde.dev"} {
		g, ok := r.AuthorityByName(name)
		if !ok || !contains(g.Bundle.Requires, "people.substrate.reamde.dev") {
			t.Errorf("%s does not require people.substrate.reamde.dev", name)
		}
	}
	// The one shipped mapping: bookedition→book, link-only.
	if m, ok := r.MappingFor("media.substrate.reamde.dev/bookedition"); !ok ||
		m.To != "media.substrate.reamde.dev/book" || m.Edge != "work" ||
		len(m.Match) != 0 || len(m.Map) != 0 {
		t.Errorf("bookedition mapping = %+v", m)
	}
	// Person carries no structured name parts and no pronouns (owner ruling):
	// the full name and the friendly one, nothing else name-shaped.
	person, ok := r.ByIdentity("people.substrate.reamde.dev/person")
	if !ok {
		t.Fatal("people.substrate.reamde.dev/person missing")
	}
	for _, p := range []string{"name", "displayName"} {
		if prop, ok := person.Prop(p); !ok || prop.Datatype != vocabulary.DatatypeString {
			t.Errorf("person.%s missing", p)
		}
	}
	for _, p := range []string{"firstName", "middleName", "lastName", "pronouns"} {
		if _, ok := person.Prop(p); ok {
			t.Errorf("person.%s must not exist", p)
		}
	}
	// GraphQL names stay bare: shipped vocabulary is shipped whichever door it
	// came through.
	if got := vocabulary.GraphQLName("people.substrate.reamde.dev/person", person.Source); got != "Person" {
		t.Errorf("GraphQL name = %q, want Person", got)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// --- renamedFrom (reserved, ticket 003) ------------------------------------

// renamedFrom is admitted and stored — reserved manifest room for declared
// evolution — but shape-checked at load: camelCase, never the property
// itself, never a still-declared sibling, never a built-in, never a field.
func TestRenamedFromReserved(t *testing.T) {
	mk := func(props string) fstest.MapFS {
		return fstest.MapFS{"g.yaml": &fstest.MapFile{Data: []byte(`kind: core.substrate.reamde.dev/authority
metadata: {id: g.example.com}
data: {version: v1}
---
kind: core.substrate.reamde.dev/kind
metadata: {id: g.example.com/thing}
data:
  authority: g.example.com
  names: {singular: thing, plural: things}
  properties:
` + props)}}
	}

	t.Run("admitted and parsed", func(t *testing.T) {
		r, err := vocabulary.LoadFS(mk("    label: {type: string, renamedFrom: caption}\n"))
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		thing, _ := r.ByIdentity("g.example.com/thing")
		if got := thing.Props["label"].RenamedFrom; got != "caption" {
			t.Fatalf("RenamedFrom = %q", got)
		}
		// Stored: the definition map (what the projection persists) keeps it.
		props, _ := thing.Definition["properties"].(map[string]any)
		label, _ := props["label"].(map[string]any)
		if got, _ := label["renamedFrom"].(string); got != "caption" {
			t.Fatalf("definition renamedFrom = %q", got)
		}
	})

	t.Run("self is an error", func(t *testing.T) {
		_, err := vocabulary.LoadFS(mk("    label: {type: string, renamedFrom: label}\n"))
		if err == nil || !strings.Contains(err.Error(), "names the property itself") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("declared sibling is an error", func(t *testing.T) {
		_, err := vocabulary.LoadFS(mk("    caption: {type: string}\n    label: {type: string, renamedFrom: caption}\n"))
		if err == nil || !strings.Contains(err.Error(), "still declared") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("built-in is an error", func(t *testing.T) {
		_, err := vocabulary.LoadFS(mk("    label: {type: string, renamedFrom: title}\n"))
		if err == nil || !strings.Contains(err.Error(), "built-in") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("field is an error", func(t *testing.T) {
		_, err := vocabulary.LoadFS(mk("    spec: {type: object, fields: {label: {type: string, renamedFrom: caption}}}\n"))
		if err == nil || !strings.Contains(err.Error(), "not a field") {
			t.Fatalf("error = %v", err)
		}
	})
}
