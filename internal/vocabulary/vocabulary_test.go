package vocabulary_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
kind: substrate.reamde.dev/core/package
metadata:
  id: core.example.com/core
data:
  authority: core.example.com
  package: core
  version: 1
---
kind: substrate.reamde.dev/core/actor
metadata:
  id: owner
data:
  authority: core.example.com
  package: core
---
kind: substrate.reamde.dev/core/actor
metadata:
  id: engram
data:
  authority: core.example.com
  package: core
---
# when a thing sits on the timeline — an instant, or a span
kind: substrate.reamde.dev/core/trait
metadata:
  id: core.example.com/core/temporal
data:
  authority: core.example.com
  package: core
  # backed by the physical at/ends_at/due_at columns every record row carries
  oneOf:
    - {name: point, properties: {at: datetime}}
    - {name: range, properties: {at: datetime, endsAt: datetime}}
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: core.example.com/core/account
data:
  authority: core.example.com
  package: core
  names: {singular: account, plural: accounts}
status:
  # server-set, ignored on input, so get -o yaml output is apply-able
  observedGeneration: 3
`

// vocabDocs is a vocabulary authority: refinements, cross-authority references, a bound
// capability with a hot-property remap, a source record with its mapping,
// object properties and a machine.
const vocabDocs = `kind: substrate.reamde.dev/core/package
metadata:
  id: vocab.example.com/vocab
data:
  authority: vocab.example.com
  package: vocab
  version: 1
---
# the connector-style actor whose authority maps onto contact: the machine tier
kind: substrate.reamde.dev/core/actor
metadata:
  id: connector:google
data:
  authority: vocab.example.com
  package: vocab
---
# Amazon's audiobook identifier: the one durable key a library row carries
kind: substrate.reamde.dev/core/propertytype
metadata:
  id: vocab.example.com/vocab/asin
data:
  authority: vocab.example.com
  package: vocab
  base: string
  pattern: "^B0[A-Z0-9]{8}$"
---
# one person; nothing matches on their addresses, so two contacts holding one
# email stay two contacts until an owner merges them
kind: substrate.reamde.dev/core/kind
metadata:
  id: vocab.example.com/vocab/contact
data:
  authority: vocab.example.com
  package: vocab
  names: {singular: contact, plural: contacts}
  properties:
    name: {type: string}
    company: {type: string}
    emails: {type: email, repeated: true}
---
# one source's record of a contact: a subject reference points at the contact,
# and the mapping beside it says how these properties reach it (§6.1)
kind: substrate.reamde.dev/core/kind
metadata:
  id: vocab.example.com/vocab/googlecontact
data:
  authority: vocab.example.com
  package: vocab
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
    contact:
      type: reference
      kind: contact
      required: true
      mustExist: true
      subject: true
---
# how googlecontact's properties reach the contact (record 50)
kind: substrate.reamde.dev/core/recordmapping
metadata:
  id: vocab.example.com/vocab/googlecontactcontact
data:
  authority: vocab.example.com
  package: vocab
  from: vocab.example.com/vocab/googlecontact
  to: vocab.example.com/vocab/contact
  property: contact
  match:
    - {from: "emails[].value", to: emails}
  map:
    name: {path: name.displayName}
    emails: {path: "emails[].value", merge: union}
---
# one book, in whatever formats you hold it
kind: substrate.reamde.dev/core/kind
metadata:
  id: vocab.example.com/vocab/book
data:
  authority: vocab.example.com
  package: vocab
  names: {singular: book, plural: books}
  displayTemplate: "{title}"
  properties:
    asin: {type: asin}
    format: {type: enum, values: [print, ebook, audio]}
    blurb: {type: markdown, embed: true}
    secretKey: {type: secret}
    author: {type: reference, kind: contact, repeated: true, mustExist: true}
    account:
      type: reference
      kind: account
      required: true
      mustExist: true
      onDelete: cascade
---
# one task; temporal(point: dueAt) remaps the capability's hot property
kind: substrate.reamde.dev/core/kind
metadata:
  id: vocab.example.com/vocab/task
data:
  authority: vocab.example.com
  package: vocab
  names: {singular: task, plural: tasks}
  traits: ["temporal(point: dueAt)"]
  indices: [{properties: [status, dueAt]}]
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
		"core.example.com/core/authority.yaml":   coreDocs,
		"vocab.example.com/vocab/authority.yaml": vocabDocs,
		"vocab.example.com/vocab/extra.yaml":     extraDocs,
		"vocab.example.com/vocab/notyaml.txt":    "ignored",
	})
}

// extraDocs proves an authority's manifests may live in any file under the tree.
const extraDocs = `kind: substrate.reamde.dev/core/kind
metadata:
  id: vocab.example.com/vocab/note
data:
  authority: vocab.example.com
  package: vocab
  names: {singular: note, plural: notes}
  traits: [temporal(range)]
  properties:
    notes: {type: text}
---
# a DECLARED stamp target: the author spells the property the transition writes
kind: substrate.reamde.dev/core/kind
metadata:
  id: vocab.example.com/vocab/shipment
data:
  authority: vocab.example.com
  package: vocab
  names: {singular: shipment, plural: shipments}
  properties:
    sentAt: {type: datetime, managed: true}
    dispatch:
      type: state
      states: [packed, sent]
      initial: packed
      transitions:
        - {from: packed, to: sent, stamps: {sentAt: now}}
`

// --- the loader ----------------------------------------------------------

func TestLoadManifestStream(t *testing.T) {
	r := loadVocab(t)

	book, ok := r.ByIdentity("vocab.example.com/vocab/book")
	if !ok {
		t.Fatal("book type missing")
	}
	if book.Name != "book" || book.Package != "vocab.example.com/vocab" {
		t.Fatalf("book = %+v", book)
	}
	if book.Version != 1 {
		t.Fatalf("version = %d (the authority's, unless the type overrides it)", book.Version)
	}
	// Reference pins: an in-authority short name, and a cross-authority one.
	author, _ := book.Prop("author")
	if author.To != "vocab.example.com/vocab/contact" || !author.Repeated || !author.MustExist {
		t.Fatalf("book.author = %+v", author)
	}
	acct, _ := book.Prop("account")
	if acct.To != "core.example.com/core/account" || !acct.Required || !acct.Cascades() {
		t.Fatalf("book.account = %+v", acct)
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
	// A list is `repeated: true` on the property, never a bracketed type.
	contact, _ := r.ByIdentity("vocab.example.com/vocab/contact")
	if p, _ := contact.Prop("emails"); !p.Repeated || p.Datatype != vocabulary.DatatypeEmail {
		t.Fatalf("contact.emails = %+v", p)
	}

	// A document in a second file joins the same authority.
	note, ok := r.ByIdentity("vocab.example.com/vocab/note")
	if !ok {
		t.Fatal("note type missing: documents, not files, are the unit")
	}
	if !note.UsesHot("at") || !note.UsesHot("endsAt") {
		t.Fatalf("note hot properties = %v", note.HotColumns)
	}

	// temporal lives in core, so this binding crosses an authority boundary, and
	// the hot-property remap survives the crossing.
	task, _ := r.ByIdentity("vocab.example.com/vocab/task")
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
	// An UNDECLARED stamp target is synthesized as an implicit datetime
	// property: stored declarations that predate declarable targets must keep
	// parsing at open.
	if p, ok := task.Prop("doneAt"); !ok || p.Datatype != vocabulary.DatatypeDatetime || !p.Implicit {
		t.Fatalf("stamp target doneAt = %+v", p)
	}
	// A DECLARED stamp target keeps its declaration: the authored property,
	// not a synthesized one, so `managed` and the description survive.
	shipment, _ := r.ByIdentity("vocab.example.com/vocab/shipment")
	if p, ok := shipment.Prop("sentAt"); !ok || p.Datatype != vocabulary.DatatypeDatetime ||
		p.Implicit || !p.Managed {
		t.Fatalf("declared stamp target sentAt = %+v", p)
	}

	// Actors are their own manifests now, one document each.
	if got := r.Actors(); len(got) != 3 || got[0] != "connector:google" ||
		got[1] != "engram" || got[2] != "owner" {
		t.Fatalf("actors = %v", got)
	}

	// Object properties: fields declared inline, one level deep,
	// a bare kind as shorthand — and never FTS, never embed.
	gc, _ := r.ByIdentity("vocab.example.com/vocab/googlecontact")
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
	if g, ok := r.ActorPackage("engram"); !ok || g != "core.example.com/core" {
		t.Fatalf("actor package = %q", g)
	}
	if g, _ := r.PackageByName("core.example.com/core"); len(g.Actors) != 2 {
		t.Fatalf("package actors = %v", g.Actors)
	}
}

// An enum's values are a first-class ordered {value, label} list.
// BOTH declared forms load into ONE type — a bare scalar is a value with no
// label, a mapping carries both — so a stored closure written before labels
// still admits. Validation reads Value alone (ValueStrings); the Definition map
// the console reads is canonicalized to the labeled wire form whichever way the
// manifest authored it.
func TestEnumValueLabels(t *testing.T) {
	const src = `kind: substrate.reamde.dev/core/package
metadata: {id: vocab.example.com/vocab}
data: {authority: vocab.example.com, package: vocab, version: 1}
---
kind: substrate.reamde.dev/core/kind
metadata: {id: vocab.example.com/vocab/account}
data:
  authority: vocab.example.com
  package: vocab
  names: {singular: account, plural: accounts}
  properties:
    backfillDepth:
      type: enum
      values:
        - {value: none, label: "Don't backfill"}
        - {value: last30d, label: "Last 30 days"}
        - all
`
	r := loadFixture(t, map[string]string{"vocab.example.com/vocab/g.yaml": src})
	acct, ok := r.ByIdentity("vocab.example.com/vocab/account")
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
	// THE DECLARATION IS UNTOUCHED. The Definition map is what a row stores, so
	// the values stay spelled the way they were authored — two mappings and one
	// bare scalar — and every reader of the stored form takes both spellings
	// (EnumValue.UnmarshalYAML here, parseEnumValues in the console).
	props := acct.Definition["properties"].(map[string]any)
	def := props["backfillDepth"].(map[string]any)
	vals, ok := def["values"].([]any)
	if !ok || len(vals) != 3 {
		t.Fatalf("definition values = %v", def["values"])
	}
	if first, isMap := vals[0].(map[string]any); !isMap || first["label"] != "Don't backfill" {
		t.Fatalf("the labeled entry was rewritten: %#v", vals[0])
	}
	if vals[2] != "all" {
		t.Fatalf("the bare entry was rewritten to %#v — the declaration is what the author wrote", vals[2])
	}
}

// The definition the projections and the GraphQL builder read is the
// manifest's data map, spelled exactly as it was authored.
func TestDefinitionIsTheData(t *testing.T) {
	r := loadVocab(t)
	task, _ := r.ByIdentity("vocab.example.com/vocab/task")
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

// Descriptions: a declared one-sentence explanation on every property
// declaration, carried structurally so the kind mirrors — and the console's
// hover tooltips — serve it without parsing comments.
func TestDescriptions(t *testing.T) {
	mk := func(body string) map[string]string {
		return map[string]string{"d.example.com/d/authority.yaml": `kind: substrate.reamde.dev/core/package
metadata:
  id: d.example.com/d
data:
  authority: d.example.com
  package: d
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: d.example.com/d/widget
data:
  authority: d.example.com
  package: d
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
    parent: {type: reference, kind: widget, description: "the widget this one hangs off"}
`))
	w, _ := r.ByIdentity("d.example.com/d/widget")
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
	if p, _ := w.Prop("parent"); p.Description != "the widget this one hangs off" {
		t.Errorf("reference description = %q", p.Description)
	}

	// The mirror shape: the definition IS the data map, so the console reads
	// definition.properties.<name>.description.
	props, _ := w.Definition["properties"].(map[string]any)
	name, _ := props["name"].(map[string]any)
	if name["description"] != "what the widget is called" {
		t.Errorf("definition.properties.name.description = %v", name["description"])
	}
	parent, _ := props["parent"].(map[string]any)
	if parent["description"] != "the widget this one hangs off" {
		t.Errorf("definition.properties.parent.description = %v", parent["description"])
	}

	// One rule: a description is a single short sentence — a tooltip, not
	// documentation. Newlines and >200 chars are load errors.
	long := strings.Repeat("x", 201)
	for what, body := range map[string]string{
		"newline": "    name: {type: string, description: \"two\\nlines\"}\n",
		"long":    "    name: {type: string, description: \"" + long + "\"}\n",
		"reference": "    name: {type: string}\n" +
			"    parent: {type: reference, kind: widget, description: \"" + long + "\"}\n",
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
		return map[string]string{"d.example.com/d/authority.yaml": `kind: substrate.reamde.dev/core/package
metadata:
  id: d.example.com/d
data:
  authority: d.example.com
  package: d
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: d.example.com/d/widget
data:
  authority: d.example.com
  package: d
  description: "` + desc + `"
  names: {singular: widget, plural: widgets}
  properties:
    name: {type: string}
`}
	}

	// Two sentences, longer than a property's 200-char tooltip bound.
	two := "One widget, the thing on the shelf. " + strings.Repeat("x", 200)
	r := loadFixture(t, mk(two))
	w, _ := r.ByIdentity("d.example.com/d/widget")
	if w.Description != two {
		t.Errorf("kind description = %q", w.Description)
	}
	// The bound is CHARACTERS, not bytes: a description of em dashes is held
	// to the same length as one of ASCII, and 400 of them is admitted.
	dashes := strings.Repeat("—", 400)
	if got, _ := loadFixture(t, mk(dashes)).ByIdentity("d.example.com/d/widget"); got.Description != dashes {
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
	book, _ := r.ByIdentity("vocab.example.com/vocab/book")
	wantHead := strings.Join([]string{
		`# one book, in whatever formats you hold it`,
		`kind: substrate.reamde.dev/core/kind`,
		`metadata:`,
		`  id: vocab.example.com/vocab/book`,
		`data:`,
		`  authority: vocab.example.com`,
		`  package: vocab`,
		`  names: {singular: book, plural: books}`,
	}, "\n")
	if !strings.HasPrefix(book.SourceYAML, wantHead) {
		t.Fatalf("book source:\n%s\nwant prefix:\n%s", book.SourceYAML, wantHead)
	}
	if !strings.HasSuffix(book.SourceYAML, "      onDelete: cascade") {
		t.Fatalf("book source ends:\n%s", book.SourceYAML)
	}
	for _, leak := range []string{"contact.vocab", "task.vocab", "---"} {
		if strings.Contains(book.SourceYAML, leak) {
			t.Fatalf("book source leaked %q:\n%s", leak, book.SourceYAML)
		}
	}

	// A package's source is its package manifest, carrying the file's own
	// opening comment.
	g, ok := r.PackageByName("core.example.com/core")
	if !ok {
		t.Fatal("core package missing")
	}
	wantAuthority := strings.Join([]string{
		`# the substrate's own machinery`,
		`kind: substrate.reamde.dev/core/package`,
		`metadata:`,
		`  id: core.example.com/core`,
		`data:`,
		`  authority: core.example.com`,
		`  package: core`,
		`  version: 1`,
	}, "\n")
	if g.SourceYAML != wantAuthority {
		t.Fatalf("package source:\n%s\nwant:\n%s", g.SourceYAML, wantAuthority)
	}

	// Traits and custom property types are manifests too, so their
	// text is exact rather than sliced.
	temporal := g.Traits["temporal"]
	if temporal.Identity() != "core.example.com/core/temporal" {
		t.Fatalf("capability identity = %q", temporal.Identity())
	}
	if !strings.HasPrefix(temporal.SourceYAML, "# when a thing sits on the timeline") ||
		!strings.Contains(temporal.SourceYAML, "  # backed by the physical at/ends_at/due_at columns") ||
		!strings.HasSuffix(temporal.SourceYAML, "    - {name: range, properties: {at: datetime, endsAt: datetime}}") {
		t.Fatalf("temporal source:\n%s", temporal.SourceYAML)
	}
	if temporal.Definition["oneOf"] == nil {
		t.Fatalf("capability definition = %v", temporal.Definition)
	}

	vocab, _ := r.PackageByName("vocab.example.com/vocab")
	asin := vocab.PropertyTypes["asin"]
	if asin.Base != vocabulary.DatatypeString || asin.Identity() != "vocab.example.com/vocab/asin" {
		t.Fatalf("asin datatype = %+v", asin)
	}
	if !strings.HasPrefix(asin.SourceYAML, "# Amazon's audiobook identifier") ||
		!strings.HasSuffix(asin.SourceYAML, `  pattern: "^B0[A-Z0-9]{8}$"`) {
		t.Fatalf("asin source:\n%s", asin.SourceYAML)
	}

	// Registry listings are by identity, across authorities.
	caps := r.Traits()
	if len(caps) != 1 || caps[0].Identity() != "core.example.com/core/temporal" {
		t.Fatalf("capabilities = %+v", caps)
	}
	dts := r.PropertyTypes()
	if len(dts) != 1 || dts[0].Identity() != "vocab.example.com/vocab/asin" {
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
		`    package: gmail`,
		`    properties:`,
		`        pageToken:`,
		`            type: string`,
		`kind: substrate.reamde.dev/core/kind`,
		`metadata:`,
		`    id: gmail.connectors.example.com/gmail/cursor`,
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
	if again.SourceYAML == "" || !strings.Contains(again.SourceYAML, "kind: substrate.reamde.dev/core/package") {
		t.Fatalf("installed package source:\n%s", again.SourceYAML)
	}
}

func gmailManifest() vocabulary.Manifest {
	const authority = "gmail.connectors.example.com/gmail"
	return vocabulary.Manifest{
		Name:      "google.gmail",
		Authority: authority,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(authority, 1),
			vocabulary.ActorManifest(authority, "connector:gmail"),
			vocabulary.KindManifest(authority,
				map[string]any{"singular": "cursor", "plural": "cursors"},
				map[string]any{"properties": map[string]any{
					"pageToken": map[string]any{"type": "string"},
				}}),
		},
	}
}

// packageHeader renders the package document a fixture authority publishes:
// one package per authority, named for the authority's first label.
func packageHeader(authority string) string {
	pkg := packageOf(authority)
	return `kind: substrate.reamde.dev/core/package
metadata: {id: ` + authority + `/` + pkg + `}
data: {authority: ` + authority + `, package: ` + pkg + `, version: 1}
`
}

// --- resolution rules ----------------------------------------------------

// A reference's `kind:` pin resolves in-authority first, then uniquely across
// authorities, and an ambiguous short name is a load error rather than an
// arbitrary pick.
func TestReferencePinResolutionRules(t *testing.T) {
	authority := func(name string) string {
		return packageHeader(name)
	}
	typ := func(authority, name, data string) string {
		pkg := packageOf(authority)
		return `---
kind: substrate.reamde.dev/core/kind
metadata: {id: ` + authority + `/` + pkg + `/` + name + `}
data:
  authority: ` + authority + `
  package: ` + pkg + `
  names: {singular: ` + name + `, plural: ` + name + `s}
` + data
	}
	alpha := authority("a.example.com") + typ("a.example.com", "alpha", "")

	t.Run("across authorities", func(t *testing.T) {
		r := loadFixture(t, map[string]string{
			"a.yaml": alpha,
			"b.yaml": authority("b.example.com") +
				typ("b.example.com", "beta", "  properties:\n    target: {type: reference, kind: alpha}\n"),
		})
		beta, _ := r.ByIdentity("b.example.com/b/beta")
		target, _ := beta.Prop("target")
		if target.To != "a.example.com/a/alpha" {
			t.Fatalf("reference pin = %q", target.To)
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
			typ("b.example.com", "beta", "  properties:\n    target: {type: reference, kind: alpha}\n"))
		beta, _ := r.ByIdentity("b.example.com/b/beta")
		target, _ := beta.Prop("target")
		if target.To != "b.example.com/b/alpha" {
			t.Fatalf("reference pin = %q, want the in-authority alpha", target.To)
		}
		// The full reference always addresses the other authority's kind.
		r2 := install(t, authority("b.example.com")+typ("b.example.com", "alpha", "")+
			typ("b.example.com", "beta", "  properties:\n    target: {type: reference, kind: a.example.com/a/alpha}\n"))
		beta2, _ := r2.ByIdentity("b.example.com/b/beta")
		target2, _ := beta2.Prop("target")
		if target2.To != "a.example.com/a/alpha" {
			t.Fatalf("qualified reference pin = %q", target2.To)
		}
	})

	t.Run("ambiguous is an error", func(t *testing.T) {
		_, err := vocabulary.LoadFS(fstest.MapFS{
			"a.yaml": {Data: []byte(alpha)},
			"b.yaml": {Data: []byte(authority("b.example.com") + typ("b.example.com", "alpha", ""))},
			"c.yaml": {Data: []byte(authority("c.example.com") +
				typ("c.example.com", "gamma", "  properties:\n    target: {type: reference, kind: alpha}\n"))},
		})
		if err == nil {
			t.Fatal("expected an ambiguity error")
		}
		if !strings.Contains(err.Error(), "ambiguous type") ||
			!strings.Contains(err.Error(), "a.example.com/a/alpha") {
			t.Fatalf("error = %v", err)
		}
		if !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("expected ErrValidation, got %v", err)
		}
	})

	t.Run("unknown referent kind", func(t *testing.T) {
		_, err := vocabulary.LoadFS(fstest.MapFS{
			"a.yaml": {Data: []byte(alpha)},
			"c.yaml": {Data: []byte(authority("c.example.com") +
				typ("c.example.com", "gamma", "  properties:\n    target: {type: reference, kind: a.example.com/a/nosuch}\n"))},
		})
		if err == nil || !strings.Contains(err.Error(), "unknown referent kind") {
			t.Fatalf("error = %v", err)
		}
	})

	// `data.edges` is gone with the edge, and the refusal names what replaced
	// it: a declaration still carrying the key would otherwise read as an
	// unknown key and leave the author guessing.
	t.Run("data.edges names its replacement", func(t *testing.T) {
		_, err := vocabulary.LoadFS(fstest.MapFS{
			"a.yaml": {Data: []byte(alpha)},
			"c.yaml": {Data: []byte(authority("c.example.com") +
				typ("c.example.com", "gamma", "  edges:\n    target: {to: alpha}\n"))},
		})
		if err == nil {
			t.Fatal("expected data.edges to be refused")
		}
		for _, want := range []string{`key "edges" is deleted`, "`properties` with `type: reference`"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("the refusal must carry %q, got: %v", want, err)
			}
		}
	})
}

// TestReferencePropertyType covers the reference property kind:
// `type: reference` is admitted, a `kind:` pin resolves from a bare name to a
// full identity, `kind: any` and absent leave it unconstrained, and an unknown
// pin is a load error.
func TestReferencePropertyType(t *testing.T) {
	authority := packageHeader
	typ := func(g, name, data string) string {
		pkg := packageOf(g)
		return `---
kind: substrate.reamde.dev/core/kind
metadata: {id: ` + g + `/` + pkg + `/` + name + `}
data:
  authority: ` + g + `
  package: ` + pkg + `
  names: {singular: ` + name + `, plural: ` + name + `s}
` + data
	}
	alpha := authority("a.example.com") + typ("a.example.com", "alpha", "")

	t.Run("to resolves to a full identity", func(t *testing.T) {
		r := loadFixture(t, map[string]string{
			"a.yaml": alpha,
			"b.yaml": authority("b.example.com") +
				typ("b.example.com", "beta", "  properties:\n    ptr: {type: reference, kind: alpha}\n"),
		})
		beta, _ := r.ByIdentity("b.example.com/b/beta")
		p := beta.Props["ptr"]
		if p.Datatype != vocabulary.DatatypeReference {
			t.Fatalf("kind = %q", p.Datatype)
		}
		if p.To != "a.example.com/a/alpha" {
			t.Fatalf("to = %q", p.To)
		}
	})

	t.Run("to any is unconstrained", func(t *testing.T) {
		r := loadFixture(t, map[string]string{
			"b.yaml": authority("b.example.com") +
				typ("b.example.com", "beta", "  properties:\n    ptr: {type: reference, kind: any}\n"),
		})
		beta, _ := r.ByIdentity("b.example.com/b/beta")
		if got := beta.Props["ptr"].To; got != vocabulary.ToAny {
			t.Fatalf("to = %q, want any", got)
		}
	})

	t.Run("absent to is unconstrained", func(t *testing.T) {
		r := loadFixture(t, map[string]string{
			"b.yaml": authority("b.example.com") +
				typ("b.example.com", "beta", "  properties:\n    ptr: {type: reference}\n"),
		})
		beta, _ := r.ByIdentity("b.example.com/b/beta")
		if got := beta.Props["ptr"].To; got != "" {
			t.Fatalf("to = %q, want empty", got)
		}
	})

	t.Run("repeated reference", func(t *testing.T) {
		r := loadFixture(t, map[string]string{
			"a.yaml": alpha,
			"b.yaml": authority("b.example.com") +
				typ("b.example.com", "beta", "  properties:\n    ptrs: {type: reference, kind: alpha, repeated: true}\n"),
		})
		beta, _ := r.ByIdentity("b.example.com/b/beta")
		if !beta.Props["ptrs"].Repeated {
			t.Fatal("expected repeated")
		}
	})

	t.Run("unknown to is an error", func(t *testing.T) {
		_, err := vocabulary.LoadFS(fstest.MapFS{
			"b.yaml": {Data: []byte(authority("b.example.com") +
				typ("b.example.com", "beta", "  properties:\n    ptr: {type: reference, kind: nosuch}\n"))},
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
		pkg := packageOf(authority)
		return packageHeader(authority) + `---
kind: substrate.reamde.dev/core/trait
metadata: {id: ` + authority + `/` + pkg + `/ranked}
data:
  authority: ` + authority + `
  package: ` + pkg + `
  properties: {score: int, label: string}
`
	}
	binder := func(props string) string {
		return `kind: substrate.reamde.dev/core/package
metadata: {id: a.example.com/a}
data: {authority: a.example.com, package: a, version: 1}
---
kind: substrate.reamde.dev/core/kind
metadata: {id: a.example.com/a/thing}
data:
  authority: a.example.com
  package: a
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
		thing, _ := r.ByIdentity("a.example.com/a/thing")
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
	const authority = "x.connectors.example.com/x"
	g, err := vocabulary.ParseManifest(vocabulary.Manifest{
		Name: "x", Authority: authority,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(authority, 1),
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
		t.Fatal("a failed install must not leave the package behind")
	}
}

// An installed authority can bind a loaded capability across an authority boundary at
// install time, and its variant is validated the same way a file's is.
func TestInstalledGroupBindsLoadedCapability(t *testing.T) {
	r := loadVocab(t)
	const authority = "media.connectors.example.com/media"
	mk := func(binding string) *vocabulary.Package {
		t.Helper()
		g, err := vocabulary.ParseManifest(vocabulary.Manifest{
			Name: "media", Authority: authority,
			Manifests: []map[string]any{
				vocabulary.PackageManifest(authority, 1),
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
	// Its one type reaches into another package for its owner.
	m.Manifests = append(m.Manifests, vocabulary.KindManifest(m.Authority,
		map[string]any{"singular": "label", "plural": "labels"},
		map[string]any{"properties": map[string]any{
			"account": map[string]any{
				"type": "reference", "kind": "account",
				"required": true, "mustExist": true, "onDelete": "cascade",
			},
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
	ty, ok := r.ByIdentity("gmail.connectors.example.com/gmail/label")
	if !ok {
		t.Fatal("installed type missing")
	}
	if ty.Source != vocabulary.SourceInstalled {
		t.Fatalf("source = %q", ty.Source)
	}
	account, _ := ty.Prop("account")
	if account.To != "core.example.com/core/account" || !account.Cascades() {
		t.Fatalf("cross-authority reference = %+v", account)
	}
	if !contains(r.Actors(), "connector:gmail") {
		t.Fatal("installed actor missing")
	}
	if g, ok := r.ActorPackage("connector:gmail"); !ok || g != "gmail.connectors.example.com/gmail" {
		t.Fatalf("actor authority = %q", g)
	}
}

// A mapping installs with the package that owns its TARGET (record 49):
// MappingManifest is the constructor an install calls, and Install runs the
// same validation the loader does, the registry-wide rules included.
func TestInstalledMapping(t *testing.T) {
	r := loadVocab(t)
	const provider = "slack.connectors.example.com/slack"
	const home = "home.example.com/home"

	// The PROVIDER installs a mirror kind whose subject slot is unpinned and
	// optional, and no mapping: the kind a slack user describes belongs to
	// whoever installed this, and this package owns none (record 49).
	prov, err := vocabulary.ParseManifest(vocabulary.Manifest{
		Name: "slack", Authority: provider,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(provider, 1),
			vocabulary.ActorManifest(provider, "connector:slack"),
			vocabulary.KindManifest(provider,
				map[string]any{"singular": "slackuser", "plural": "slackusers"},
				map[string]any{
					"properties": map[string]any{
						"realName": map[string]any{"type": "string"},
						"email":    map[string]any{"type": "email"},
						"person": map[string]any{
							"type": "reference", "mustExist": true, "subject": true,
						},
					},
				}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Install(prov); err != nil {
		t.Fatalf("install the provider: %v", err)
	}
	if ms := r.MappingsFrom(provider + "/slackuser"); len(ms) != 0 {
		t.Fatalf("the provider installed %d mappings; it declares none", len(ms))
	}

	// The REPOSITORY's own package owns the target kind, so it is the one that
	// may say what projects onto it. A scalar path onto a union target is a
	// singleton contribution, which is legal.
	mk := func(mapping map[string]any) *vocabulary.Package {
		t.Helper()
		g, err := vocabulary.ParseManifest(vocabulary.Manifest{
			Name: "home", Authority: home,
			Manifests: []map[string]any{
				vocabulary.PackageManifest(home, 1),
				vocabulary.KindManifest(home,
					map[string]any{"singular": "contactcard", "plural": "contactcards"},
					map[string]any{
						"properties": map[string]any{
							"name":   map[string]any{"type": "string"},
							"emails": map[string]any{"type": "email", "repeated": true},
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
	good := vocabulary.MappingManifest(home, "slackusercard", map[string]any{
		"from":     provider + "/slackuser",
		"to":       home + "/contactcard",
		"property": "person",
		"match": []any{
			map[string]any{"from": "email", "to": "emails"},
		},
		"map": map[string]any{
			"name":   map[string]any{"path": "realName"},
			"emails": map[string]any{"path": "email", "merge": "union"},
		},
	})
	if err := r.Install(mk(good)); err != nil {
		t.Fatalf("install: %v", err)
	}
	m, ok := r.MappingFor(provider+"/slackuser", "person")
	if !ok || m.To != home+"/contactcard" || m.Package != home {
		t.Fatalf("installed mapping = %+v", m)
	}
	if tos := r.MappingsTo(home + "/contactcard"); len(tos) != 1 || tos[0] != m {
		t.Fatalf("mappings to contactcard = %v", tos)
	}
	// The provider's declared actor is still a package actor (the machine
	// tier's sync half, primitives §6), even though it declares no mapping.
	if _, ok := r.ActorPackage("connector:slack"); !ok {
		t.Fatal("connector:slack is not a declared package actor")
	}

	// The registry-wide rules run at INSTALL, not only at load: a package
	// installing two mappings of its own where one's `to` is the other's
	// `from` breaks the bipartite rule, and nothing of it stays installed.
	const chain = "chain.example.com/chain"
	g, err := vocabulary.ParseManifest(vocabulary.Manifest{
		Name: "chain", Authority: chain,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(chain, 1),
			vocabulary.KindManifest(chain,
				map[string]any{"singular": "leaf", "plural": "leaves"},
				map[string]any{"properties": map[string]any{
					"middle": map[string]any{
						"type": "reference", "kind": chain + "/middle",
						"required": true, "mustExist": true, "subject": true,
					},
				}}),
			vocabulary.KindManifest(chain,
				map[string]any{"singular": "middle", "plural": "middles"},
				map[string]any{"properties": map[string]any{
					"root": map[string]any{
						"type": "reference", "kind": chain + "/root",
						"required": true, "mustExist": true, "subject": true,
					},
				}}),
			vocabulary.KindManifest(chain,
				map[string]any{"singular": "root", "plural": "roots"},
				map[string]any{"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				}}),
			vocabulary.MappingManifest(chain, "leafmiddle", map[string]any{
				"from": chain + "/leaf", "to": chain + "/middle", "property": "middle",
			}),
			vocabulary.MappingManifest(chain, "middleroot", map[string]any{
				"from": chain + "/middle", "to": chain + "/root", "property": "root",
			}),
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
	if _, ok := r.PackageByName(chain); ok {
		t.Fatal("a failed install must not leave the package behind")
	}
}

// A manifest is one authority's worth of documents; anything else is a mistake
// the loader must not paper over.
func TestManifestShapeRejected(t *testing.T) {
	const authority = "x.connectors.example.com/x"
	for name, m := range map[string]vocabulary.Manifest{
		"no authority manifest": {Name: "x", Authority: authority, Manifests: []map[string]any{
			vocabulary.KindManifest(authority, map[string]any{"singular": "thing", "plural": "things"}, nil),
		}},
		"two authorities": {Name: "x", Authority: authority, Manifests: []map[string]any{
			vocabulary.PackageManifest(authority, 1),
			vocabulary.PackageManifest("y.connectors.example.com/y", 1),
		}},
		"authority mismatch": {Name: "x", Authority: authority, Manifests: []map[string]any{
			vocabulary.PackageManifest("y.connectors.example.com/y", 1),
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
	if _, ok := base.ByIdentity("gmail.connectors.example.com/gmail/cursor"); ok {
		t.Fatal("install leaked into the base registry")
	}
	if _, ok := clone.ByIdentity("gmail.connectors.example.com/gmail/cursor"); !ok {
		t.Fatal("install missing from the clone")
	}
}

func TestResolveAmbiguity(t *testing.T) {
	r := loadVocab(t)
	ty, err := r.Resolve("contact")
	if err != nil || ty.Identity != "vocab.example.com/vocab/contact" {
		t.Fatalf("resolve short name: %v %v", ty, err)
	}
	if _, err := r.Resolve("nosuch"); err == nil {
		t.Fatal("expected unknown type error")
	}
	const authority = "dup.connectors.example.com/dup"
	g, err := vocabulary.ParseManifest(vocabulary.Manifest{
		Name: "dup", Authority: authority,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(authority, 1),
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
	if !temporal["vocab.example.com/vocab/task"] || !temporal["vocab.example.com/vocab/note"] {
		t.Fatalf("Temporal implementors = %v", temporal)
	}
	if temporal["vocab.example.com/vocab/contact"] {
		t.Fatal("contact should not implement Temporal")
	}
	status := r.Implementing("HasStatus")
	if len(status) != 1 || status[0].Identity != "vocab.example.com/vocab/task" {
		t.Fatalf("HasStatus = %v", status)
	}

	task, _ := r.ByIdentity("vocab.example.com/vocab/task")
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
	c, _ := r.ByIdentity("vocab.example.com/vocab/contact")
	g, _ := r.ByIdentity("vocab.example.com/vocab/googlecontact")

	m, ok := r.MappingFor(g.Identity, "contact")
	if !ok {
		t.Fatal("googlecontact carries a mapping on `contact`")
	}
	if m.Identity() != "vocab.example.com/vocab/googlecontactcontact" ||
		m.From != g.Identity || m.To != c.Identity || m.Property != "contact" {
		t.Fatalf("mapping = %+v", m)
	}
	// The key is the pair: the same source kind on another property is
	// another slot, and an empty one.
	if _, ok := r.MappingFor(g.Identity, "nosuch"); ok {
		t.Fatal("a mapping answered for a property it does not fill")
	}
	if ms := r.MappingsFrom(g.Identity); len(ms) != 1 || ms[0] != m {
		t.Fatalf("mappings from googlecontact = %v", ms)
	}
	if ms := r.MappingsFrom(c.Identity); len(ms) != 0 {
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
	if m.Definition["property"] != "contact" {
		t.Fatalf("the mapping's own data map lost its subject property: %+v", m)
	}

	// The target side.
	tos := r.MappingsTo(c.Identity)
	if len(tos) != 1 || tos[0] != m {
		t.Fatalf("mappings to contact = %v", tos)
	}
	if len(r.MappingsTo(g.Identity)) != 0 {
		t.Fatal("a source record is managed by nothing")
	}
	// The mapping package's declared actor is a package actor — the machine
	// tier's sync half; a stranger is not.
	if grp, ok := r.ActorPackage("connector:google"); !ok || grp != "vocab.example.com/vocab" {
		t.Fatalf("ActorPackage(connector:google) = %q, %v", grp, ok)
	}
	if _, ok := r.ActorPackage("stranger.example.com"); ok {
		t.Fatal("an undeclared actor is nobody's package actor")
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

// A mapping's rules are loader-enforced: the subject reference's shape, every
// path against both declared kinds, one mapping per source kind, and the
// registry-wide bipartite rule.
func TestMappingRules(t *testing.T) {
	head := `kind: substrate.reamde.dev/core/package
metadata: {id: x.example.com/x}
data: {authority: x.example.com, package: x, version: 1}
---
kind: substrate.reamde.dev/core/kind
metadata: {id: x.example.com/x/person}
data:
  authority: x.example.com
  package: x
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
kind: substrate.reamde.dev/core/kind
metadata: {id: x.example.com/x/rec}
data:
  authority: x.example.com
  package: x
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
    person:
      type: reference
      kind: person
      required: true
      mustExist: true
      subject: true
    other: {type: reference, kind: person}
    unmarked: {type: reference, kind: person, required: true, mustExist: true}
`
	mapping := func(name, body string) string {
		return head + `---
kind: substrate.reamde.dev/core/recordmapping
metadata: {id: x.example.com/x/` + name + `}
data:
  authority: x.example.com
  package: x
` + body
	}
	recperson := func(rules string) string {
		return mapping("recperson", `  from: x.example.com/x/rec
  to: x.example.com/x/person
  property: person
`+rules)
	}
	bad := map[string]string{
		"missing property": mapping("recperson", `  from: x.example.com/x/rec
  to: x.example.com/x/person
  property: nosuch
`),
		// `other` points at person, but the mapping's property must be the one
		// the data.to names AND carry the subject shape.
		"optional property": mapping("recperson", `  from: x.example.com/x/rec
  to: x.example.com/x/person
  property: other
`),
		// The marker is what the write path reads, so a subject-shaped
		// reference that does not declare it is still refused.
		"subject marker missing": mapping("recperson", `  from: x.example.com/x/rec
  to: x.example.com/x/person
  property: unmarked
`),
		"wrong referent kind": mapping("recperson", `  from: x.example.com/x/rec
  to: x.example.com/x/rec
  property: person
`),
		"from not in authority": mapping("recperson", `  from: x.example.com/x/nosuch
  to: x.example.com/x/person
  property: person
`),
		// The source's package resolves at Finalize, so a `from` naming a
		// package this repository does not have is refused there, naming what
		// to import.
		"from must be full": mapping("recperson", `  from: rec
  to: x.example.com/x/person
  property: person
`),
		"to must be full": mapping("recperson", `  from: x.example.com/x/rec
  to: person
  property: person
`),
		"unknown to type": mapping("recperson", `  from: x.example.com/x/rec
  to: y.example.com/y/nosuch
  property: person
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
		// Two mappings through ONE subject reference: the key is the (source
		// kind, subject property) pair, and `person` is filled twice here.
		"two mappings on one property": recperson("") + `---
kind: substrate.reamde.dev/core/recordmapping
metadata: {id: x.example.com/x/recperson2}
data:
  authority: x.example.com
  package: x
  from: x.example.com/x/rec
  to: x.example.com/x/person
  property: person
`,
		// person is recperson's `to` AND personorg's `from`: the
		// source→subject graph stays bipartite.
		"bipartite violation": recperson("") + `---
kind: substrate.reamde.dev/core/kind
metadata: {id: x.example.com/x/org}
data:
  authority: x.example.com
  package: x
  names: {singular: org, plural: orgs}
  properties: {name: {type: string}}
---
kind: substrate.reamde.dev/core/recordmapping
metadata: {id: x.example.com/x/personorg}
data:
  authority: x.example.com
  package: x
  from: x.example.com/x/person
  to: x.example.com/x/org
  property: employer
`,
		// No reference anywhere may name a mapped source kind: resolution
		// stays one hop deep (§6.2).
		"reference onto a source record": recperson("") + `---
kind: substrate.reamde.dev/core/kind
metadata: {id: x.example.com/x/note}
data:
  authority: x.example.com
  package: x
  names: {singular: note, plural: notes}
  properties: {about: {type: reference, kind: rec}}
`,
		"unknown mapping key": recperson(`  fuse: true
`),
	}
	for name, src := range bad {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{"x.example.com/x/all.yaml": &fstest.MapFile{Data: []byte(src)}}
			if _, err := vocabulary.LoadFS(fsys); err == nil {
				t.Fatal("expected a load error")
			} else if !errors.Is(err, substrate.ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
		})
	}

	// A `from` in another package resolves at Finalize, not at parse, so its
	// absence is reported THERE, naming what to import. The message is the
	// assertion: against the old rule this document failed at parse, with
	// "data.from must be x.example.com/x/<name>".
	t.Run("foreign from is absent", func(t *testing.T) {
		src := mapping("recperson", `  from: y.example.com/y/thing
  to: x.example.com/x/person
  property: person
`)
		fsys := fstest.MapFS{"x.example.com/x/all.yaml": &fstest.MapFile{Data: []byte(src)}}
		_, err := vocabulary.LoadFS(fsys)
		if err == nil {
			t.Fatal("expected a load error")
		}
		if !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("expected ErrValidation, got %v", err)
		}
		want := "data.from names y.example.com/y/thing, which this repository does not have"
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to carry %q", err, want)
		}
	})

	// `projects:` is deleted from the DSL, and the error names its
	// replacement rather than treating the key as unknown.
	t.Run("projects is deleted", func(t *testing.T) {
		src := head + `---
kind: substrate.reamde.dev/core/kind
metadata: {id: x.example.com/x/two}
data:
  authority: x.example.com
  package: x
  names: {singular: two, plural: twos}
  properties: {person: {type: reference, kind: person, projects: true}}
`
		fsys := fstest.MapFS{"x.example.com/x/all.yaml": &fstest.MapFile{Data: []byte(src)}}
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
	// still makes the from a source record and pins the subject reference.
	t.Run("link-only mapping", func(t *testing.T) {
		fsys := fstest.MapFS{"x.example.com/x/all.yaml": &fstest.MapFile{Data: []byte(recperson(""))}}
		r, err := vocabulary.LoadFS(fsys)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		m, ok := r.MappingFor("x.example.com/x/rec", "person")
		if !ok || len(m.Match) != 0 || len(m.Map) != 0 {
			t.Fatalf("link-only mapping = %+v", m)
		}
	})

	// Record 49, the accepted half: the owner of `to` declares the mapping,
	// its `from` is a mirror kind in a package it does not own, and that
	// mirror's subject reference is unpinned and optional. The mapping's `to`
	// is what says which kind the slot holds.
	t.Run("foreign from onto an owned to", func(t *testing.T) {
		r, err := vocabulary.LoadFS(fstest.MapFS{
			"p.example.com/p/all.yaml": &fstest.MapFile{Data: []byte(mirrorPackage)},
			"u.example.com/u/all.yaml": &fstest.MapFile{Data: []byte(userPackage)},
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		m, ok := r.MappingFor("p.example.com/p/issue", "task")
		if !ok {
			t.Fatal("the user's package declares the mapping from the mirror")
		}
		if m.Package != "u.example.com/u" || m.To != "u.example.com/u/task" {
			t.Fatalf("mapping = %+v", m)
		}
		mirror, _ := r.ByIdentity("p.example.com/p/issue")
		if slot := mirror.Props["task"]; slot.To != "" || slot.Required {
			t.Fatalf("the mirror's subject slot is unpinned and optional: %+v", slot)
		}
	})

	// The refused half, twice over: OWNING `to` is what licenses a mapping, so
	// a third package that owns neither end declares nothing, and neither does
	// the package that owns the SOURCE. Both kinds are installed in each case
	// and both mappings would type-check; who wrote them is the whole problem.
	for name, closure := range map[string]string{
		"neither end owned":        thirdPartyPackage,
		"source owned, not target": sourceOwnerMapping,
	} {
		t.Run(name, func(t *testing.T) {
			files := fstest.MapFS{
				"p.example.com/p/all.yaml": &fstest.MapFile{Data: []byte(mirrorPackage)},
				"u.example.com/u/all.yaml": &fstest.MapFile{Data: []byte(userPackage)},
			}
			if closure == sourceOwnerMapping {
				// The source's own package declares it, in its own file.
				files["p.example.com/p/all.yaml"] = &fstest.MapFile{
					Data: []byte(mirrorPackage + sourceOwnerMapping),
				}
			} else {
				files["z.example.com/z/all.yaml"] = &fstest.MapFile{Data: []byte(closure)}
			}
			_, err := vocabulary.LoadFS(files)
			if err == nil {
				t.Fatal("expected a load error")
			}
			if !errors.Is(err, substrate.ErrValidation) ||
				!strings.Contains(err.Error(), "declared by the package that owns that kind") {
				t.Fatalf("error = %v", err)
			}
		})
	}

	// One source kind reaches a given TARGET through one property. Two slots
	// onto the same kind would give the subject hop two answers for one pin,
	// so the pair is refused at declaration.
	t.Run("two mappings onto one target", func(t *testing.T) {
		_, err := vocabulary.LoadFS(fstest.MapFS{
			"p.example.com/p/all.yaml": &fstest.MapFile{Data: []byte(mirrorPackage)},
			"u.example.com/u/all.yaml": &fstest.MapFile{Data: []byte(userPackage + overlappingMapping)},
		})
		if err == nil {
			t.Fatal("expected a load error")
		}
		if !errors.Is(err, substrate.ErrValidation) ||
			!strings.Contains(err.Error(), "already reaches") {
			t.Fatalf("error = %v", err)
		}
	})

	// A subject slot may pin a TRAIT instead of a kind (record 0034), and the
	// mapping's `to` then has to implement it: a mapping that would fill the
	// slot with a kind the declaration refuses is caught on the manifest.
	t.Run("trait pin the target does not implement", func(t *testing.T) {
		_, err := vocabulary.LoadFS(fstest.MapFS{
			"p.example.com/p/all.yaml": &fstest.MapFile{Data: []byte(traitPinnedMirror)},
			"u.example.com/u/all.yaml": &fstest.MapFile{Data: []byte(userPackage + traitPinnedMapping)},
		})
		if err == nil {
			t.Fatal("expected a load error")
		}
		if !errors.Is(err, substrate.ErrValidation) ||
			!strings.Contains(err.Error(), "does not implement") {
			t.Fatalf("error = %v", err)
		}
	})

	// One mirror kind reaches TWO subject kinds through two subject
	// references: two slots, two mappings, one source kind.
	t.Run("two mappings onto two properties", func(t *testing.T) {
		r, err := vocabulary.LoadFS(fstest.MapFS{
			"p.example.com/p/all.yaml": &fstest.MapFile{Data: []byte(mirrorPackage)},
			"u.example.com/u/all.yaml": &fstest.MapFile{Data: []byte(userPackage + userNotePackage)},
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		ms := r.MappingsFrom("p.example.com/p/issue")
		if len(ms) != 2 {
			t.Fatalf("mappings from the mirror = %d", len(ms))
		}
		task, okTask := r.MappingFor("p.example.com/p/issue", "task")
		note, okNote := r.MappingFor("p.example.com/p/issue", "note")
		if !okTask || !okNote || task.To != "u.example.com/u/task" || note.To != "u.example.com/u/note" {
			t.Fatalf("the two slots resolve to %+v and %+v", task, note)
		}
	})
}

// A mapping's paths are type-checked against BOTH declared kinds, and the
// source kind lives in another package now, so a change to that package has to
// re-check the mappings other packages declare against it. Install is where
// that happens: resolvePackage only runs for the packages a batch rebuilds, so
// without the cross-package pass a narrowed mirror would strand a mapping and
// nothing would say so until the next repository open.
func TestInstallRechecksMappingsAgainstAChangedSourceKind(t *testing.T) {
	r, err := vocabulary.LoadFS(fstest.MapFS{
		"p.example.com/p/all.yaml": &fstest.MapFile{Data: []byte(mirrorPackage)},
		"u.example.com/u/all.yaml": &fstest.MapFile{Data: []byte(userPackage)},
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// The provider's next release retypes `headline`, which the user's
	// mapping reads onto a string property.
	next, err := vocabulary.ParseManifest(vocabulary.Manifest{
		Name: "p", Authority: "p.example.com/p",
		Manifests: []map[string]any{
			vocabulary.PackageManifest("p.example.com/p", 2),
			vocabulary.KindManifest("p.example.com/p",
				map[string]any{"singular": "issue", "plural": "issues"},
				map[string]any{"properties": map[string]any{
					"headline": map[string]any{"type": "int"},
					"task":     map[string]any{"type": "reference", "mustExist": true, "subject": true},
					"note":     map[string]any{"type": "reference", "mustExist": true, "subject": true},
				}}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.Remove("p.example.com/p")
	err = r.InstallAll([]*vocabulary.Package{next})
	if err == nil {
		t.Fatal("a retype under another package's mapping installed silently")
	}
	if !strings.Contains(err.Error(), "u.example.com/u/issuetask") ||
		!strings.Contains(err.Error(), "type-checks against both ends") {
		t.Fatalf("error = %v, want the stranded mapping named", err)
	}
}

// mirrorPackage is a provider's package as record 49 leaves it: a mirror kind
// with two unpinned, optional subject slots and no mapping of its own.
const mirrorPackage = `kind: substrate.reamde.dev/core/package
metadata: {id: p.example.com/p}
data: {authority: p.example.com, package: p, version: 1}
---
kind: substrate.reamde.dev/core/kind
metadata: {id: p.example.com/p/issue}
data:
  authority: p.example.com
  package: p
  names: {singular: issue, plural: issues}
  properties:
    headline: {type: string}
    task: {type: reference, mustExist: true, subject: true}
    note: {type: reference, mustExist: true, subject: true}
`

// userPackage is the repository's own: it owns `task` and declares the mapping
// onto it from the mirror it does not own.
const userPackage = `kind: substrate.reamde.dev/core/package
metadata: {id: u.example.com/u}
data: {authority: u.example.com, package: u, version: 1}
---
kind: substrate.reamde.dev/core/kind
metadata: {id: u.example.com/u/task}
data:
  authority: u.example.com
  package: u
  names: {singular: task, plural: tasks}
  properties:
    name: {type: string}
---
kind: substrate.reamde.dev/core/recordmapping
metadata: {id: u.example.com/u/issuetask}
data:
  authority: u.example.com
  package: u
  from: p.example.com/p/issue
  to: u.example.com/u/task
  property: task
  map:
    name: {path: headline}
`

// overlappingMapping is a second slot onto the SAME target kind: legal by the
// (source, property) key, refused by the (source, target) one.
const overlappingMapping = `---
kind: substrate.reamde.dev/core/recordmapping
metadata: {id: u.example.com/u/issuetaskagain}
data:
  authority: u.example.com
  package: u
  from: p.example.com/p/issue
  to: u.example.com/u/task
  property: note
`

// traitPinnedMirror pins its subject slot at a TRAIT, and `task` (the mapping
// target in userPackage) does not implement it.
const traitPinnedMirror = `kind: substrate.reamde.dev/core/package
metadata: {id: p.example.com/p}
data: {authority: p.example.com, package: p, version: 1}
---
kind: substrate.reamde.dev/core/trait
metadata: {id: p.example.com/p/ranked}
data:
  authority: p.example.com
  package: p
  properties: {score: int}
---
kind: substrate.reamde.dev/core/kind
metadata: {id: p.example.com/p/issue}
data:
  authority: p.example.com
  package: p
  names: {singular: issue, plural: issues}
  properties:
    headline: {type: string}
    task: {type: reference, trait: ranked, mustExist: true, subject: true}
`

// traitPinnedMapping fills that trait-pinned slot with a kind that implements
// nothing.
const traitPinnedMapping = `---
kind: substrate.reamde.dev/core/recordmapping
metadata: {id: u.example.com/u/issuetasktrait}
data:
  authority: u.example.com
  package: u
  from: p.example.com/p/issue
  to: u.example.com/u/task
  property: task
`

// sourceOwnerMapping is the mirror package declaring a mapping onto a kind it
// does not own: today's provider mappings, and refused since record 49.
const sourceOwnerMapping = `---
kind: substrate.reamde.dev/core/recordmapping
metadata: {id: p.example.com/p/issuetask}
data:
  authority: p.example.com
  package: p
  from: p.example.com/p/issue
  to: u.example.com/u/task
  property: task
`

// thirdPartyPackage owns neither end and declares the mapping anyway: both
// kinds are installed, so the only thing wrong with it is who wrote it.
const thirdPartyPackage = `kind: substrate.reamde.dev/core/package
metadata: {id: z.example.com/z}
data: {authority: z.example.com, package: z, version: 1}
---
kind: substrate.reamde.dev/core/recordmapping
metadata: {id: z.example.com/z/issuenote}
data:
  authority: z.example.com
  package: z
  from: p.example.com/p/issue
  to: u.example.com/u/task
  property: note
`

// userNotePackage is the second slot: the same mirror onto a second kind the
// same package owns.
const userNotePackage = `---
kind: substrate.reamde.dev/core/kind
metadata: {id: u.example.com/u/note}
data:
  authority: u.example.com
  package: u
  names: {singular: note, plural: notes}
  properties:
    name: {type: string}
---
kind: substrate.reamde.dev/core/recordmapping
metadata: {id: u.example.com/u/issuenote}
data:
  authority: u.example.com
  package: u
  from: p.example.com/p/issue
  to: u.example.com/u/note
  property: note
`

func TestTemplates(t *testing.T) {
	tpl, err := vocabulary.ParseTemplate("{author.name}: {snippet}")
	if err != nil {
		t.Fatal(err)
	}
	got := tpl.Render(testResolver{
		refs:    map[string]string{"author.name": "Alex"},
		snippet: "hello there",
	})
	if got != "Alex: hello there" {
		t.Fatalf("render = %q", got)
	}
	if heads := tpl.RefHeads(); len(heads) != 1 || heads[0] != "author" {
		t.Fatalf("ref heads = %v", heads)
	}

	fallback, err := vocabulary.ParseTemplate("{name|participants}")
	if err != nil {
		t.Fatal(err)
	}
	if got := fallback.Render(testResolver{props: map[string]string{"name": "#general"}}); got != "#general" {
		t.Fatalf("render = %q", got)
	}
	if got := fallback.Render(testResolver{refs: map[string]string{"participants": "Alex, Nina"}}); got != "Alex, Nina" {
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
// bare token is a property or a column-backed property; a dotted one reads a
// property of the record a reference names, or ONE LEVEL into an object
// property's declared fields — so `{name.displayName}` works and a typo fails
// on the manifest, not as an empty title.
func TestTemplateTokensValidate(t *testing.T) {
	typ := func(template string) string {
		return `kind: substrate.reamde.dev/core/package
metadata: {id: x.example.com/x}
data: {authority: x.example.com, package: x, version: 1}
---
kind: substrate.reamde.dev/core/kind
metadata: {id: x.example.com/x/card}
data:
  authority: x.example.com
  package: x
  names: {singular: card, plural: cards}
  displayTemplate: "` + template + `"
  properties:
    label: {type: string}
    body: {type: text}
    name:
      type: object
      fields: {displayName: {type: string}}
    owner: {type: reference, kind: card}
`
	}
	load := func(src string) error {
		fsys := fstest.MapFS{"x.example.com/x/all.yaml": &fstest.MapFile{Data: []byte(src)}}
		_, err := vocabulary.LoadFS(fsys)
		return err
	}
	for name, good := range map[string]string{
		"declared property":   "{label}",
		"object field":        "{name.displayName}",
		"referent property":   "{owner.label}",
		"column-backed":       "{title|body}",
		"referent titles":     "{owner}",
		"snippet":             "{snippet}",
		"fallback into field": "{label|name.displayName}",
	} {
		if err := load(typ(good)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for name, bad := range map[string]string{
		"undeclared property":  "{nope}",
		"undeclared field":     "{name.nope}",
		"undeclared reference": "{friend.label}",
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
	refs    map[string]string
	snippet string
	derived map[string]string
	// declares is the kind's declared property set where it differs from the
	// props map — a property declared but EMPTY on the row.
	declares []string
}

func (r testResolver) Prop(n string) string { return r.props[n] }

func (r testResolver) Declares(n string) bool {
	if _, ok := r.props[n]; ok {
		return true
	}
	if _, ok := r.refs[n]; ok {
		return true
	}
	return slices.Contains(r.declares, n)
}

func (r testResolver) Derived(token string) string {
	if token == vocabulary.DerivedSnippet {
		return r.snippet
	}
	return r.derived[token]
}

func (r testResolver) Reference(name, prop string) string {
	if prop == "" {
		return r.refs[name]
	}
	return r.refs[name+"."+prop]
}

// --- strictness ----------------------------------------------------------

// Unknown keys, unknown property types and mis-spelled identities are hard
// errors: a manifest the loader half-understands is a schema nobody can trust.
func TestManifestValidationRejected(t *testing.T) {
	const head = `kind: substrate.reamde.dev/core/package
metadata: {id: x.example.com/x}
data: {authority: x.example.com, package: x, version: 1}
---
`
	typ := func(data string) string {
		return head + `kind: substrate.reamde.dev/core/kind
metadata: {id: x.example.com/x/contact}
data:
  authority: x.example.com
  package: x
` + data
	}
	cases := map[string]string{
		// the envelope
		"unknown type": head + `kind: substrate.reamde.dev/core/schemawidget
metadata: {id: x.example.com/x/w}
data: {authority: x.example.com}
`,
		"wrong authority": head + `kind: x.example.com/x/kind
metadata: {id: x.example.com/x/contact}
data:
  authority: x.example.com
  package: x
  names: {singular: contact, plural: contacts}
`,
		"unknown envelope key": head + `kind: substrate.reamde.dev/core/kind
metadata: {id: x.example.com/x/contact}
extra: nope
data:
  authority: x.example.com
  package: x
  names: {singular: contact, plural: contacts}
`,
		// The Kubernetes envelope is DELETED, and each key names what took
		// its job rather than reading as an unknown key.
		"apiVersion is deleted": head + `apiVersion: substrate.reamde.dev/core/v1alpha1
type: kind
metadata: {id: x.example.com/x/contact}
data:
  authority: x.example.com
  package: x
  names: {singular: contact, plural: contacts}
`,
		"group is deleted": head + `group: substrate.reamde.dev/core
kind: substrate.reamde.dev/core/kind
metadata: {id: x.example.com/x/contact}
data:
  authority: x.example.com
  package: x
  names: {singular: contact, plural: contacts}
`,
		"type is deleted": head + `type: kind
metadata: {id: x.example.com/x/contact}
data:
  authority: x.example.com
  package: x
  names: {singular: contact, plural: contacts}
`,
		"spec is deleted": head + `kind: substrate.reamde.dev/core/kind
metadata: {id: x.example.com/x/contact}
spec:
  authority: x.example.com
  package: x
  names: {singular: contact, plural: contacts}
`,
		"metadata.name is deleted": head + `kind: substrate.reamde.dev/core/kind
metadata: {name: x.example.com/x/contact}
data:
  authority: x.example.com
  package: x
  names: {singular: contact, plural: contacts}
`,
		"no metadata.id": head + `kind: substrate.reamde.dev/core/kind
data:
  authority: x.example.com
  package: x
  names: {singular: contact, plural: contacts}
`,
		"unnamespaced label": head + `kind: substrate.reamde.dev/core/kind
metadata:
  id: x.example.com/x/contact
  labels: {owner: me}
data:
  authority: x.example.com
  package: x
  names: {singular: contact, plural: contacts}
`,
		"orphan document": `kind: substrate.reamde.dev/core/kind
metadata: {id: y.example.com/y/contact}
data:
  authority: y.example.com
  package: y
  names: {singular: contact, plural: contacts}
`,
		"identity mismatch": head + `kind: substrate.reamde.dev/core/kind
metadata: {id: x.example.com/x/contacts}
data:
  authority: x.example.com
  package: x
  names: {singular: contact, plural: contacts}
`,
		"duplicate type": head + `kind: substrate.reamde.dev/core/kind
metadata: {id: x.example.com/x/contact}
data:
  authority: x.example.com
  package: x
  names: {singular: contact, plural: contacts}
---
kind: substrate.reamde.dev/core/kind
metadata: {id: x.example.com/x/contact}
data:
  authority: x.example.com
  package: x
  names: {singular: contact, plural: contacts}
`,
		"duplicate authority": head + `kind: substrate.reamde.dev/core/package
metadata: {id: x.example.com/x}
data: {authority: x.example.com, package: x, version: 1}
`,
		"duplicate actor": head + `kind: substrate.reamde.dev/core/actor
metadata: {id: owner}
data: {authority: x.example.com}
---
kind: substrate.reamde.dev/core/actor
metadata: {id: owner}
data: {authority: x.example.com}
`,
		"bad authority name": `kind: substrate.reamde.dev/core/authority
metadata: {id: Vocab}
data: {version: 1}
`,
		"snake actor": head + `kind: substrate.reamde.dev/core/actor
metadata: {id: my_actor}
data: {authority: x.example.com}
`,
		// the body DSL
		"missing names":     typ("  properties: {a: {type: string}}\n"),
		"bad type name":     typ("  names: {singular: my_contact, plural: mycontacts}\n"),
		"unknown names key": typ("  names: {singular: contact, plural: contacts, short: c}\n"),
		// One casing rule: every declared name is camelCase.
		"capitalised property": typ(`  names: {singular: contact, plural: contacts}
  properties: {FirstName: {type: string}}
`),
		"snake property": typ(`  names: {singular: contact, plural: contacts}
  properties: {first_name: {type: string}}
`),
		"snake reference": typ(`  names: {singular: contact, plural: contacts}
  properties: {work_place: {type: reference, kind: contact}}
`),
		"snake stamp": typ(`  names: {singular: contact, plural: contacts}
  properties: {m: {type: state, states: [a, b], transitions: [{from: a, to: b, stamps: {done_at: now}}]}}
`),
		"snake capability property": head + `kind: substrate.reamde.dev/core/trait
metadata: {id: x.example.com/x/ranked}
data:
  authority: x.example.com
  package: x
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
		// A declared stamp target must hold what the engine writes into it: a
		// single datetime.
		"stamp target declared as a string": typ(`  names: {singular: contact, plural: contacts}
  properties:
    doneAt: {type: string}
    m: {type: state, states: [a, b], transitions: [{from: a, to: b, stamps: {doneAt: now}}]}
`),
		"stamp target declared repeated": typ(`  names: {singular: contact, plural: contacts}
  properties:
    doneAt: {type: datetime, repeated: true}
    m: {type: state, states: [a, b], transitions: [{from: a, to: b, stamps: {doneAt: now}}]}
`),
		"stamp target declared keyed": typ(`  names: {singular: contact, plural: contacts}
  properties:
    doneAt: {type: datetime, keyed: true}
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
		"snake onDelete": typ(`  names: {singular: contact, plural: contacts}
  properties: {org: {type: reference, kind: contact, on_delete: cascade}}
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
		// Object fields nest to MaxFieldDepth and hold declared shapes: never
		// json/secret/digest/state/blobref, never a snake name, never a reserved
		// one — and an object is never a refinement base.
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
		// A level-5 field: the dialect admits four, and the guards that refuse a
		// narrowing walk exactly that many jsonb notches.
		"field nested past the depth": typ(`  names: {singular: contact, plural: contacts}
  properties:
    deep:
      type: object
      fields:
        l2: {type: object, fields: {l3: {type: object, fields: {l4: {type: object, fields: {l5: {type: string}}}}}}}
`),
		"json field at depth": typ(`  names: {singular: contact, plural: contacts}
  properties:
    deep: {type: object, fields: {l2: {type: object, fields: {raw: {type: json}}}}}
`),
		"secret field at depth": typ(`  names: {singular: contact, plural: contacts}
  properties:
    deep: {type: object, fields: {l2: {type: object, fields: {key: {type: secret}}}}}
`),
		"blobref field at depth": typ(`  names: {singular: contact, plural: contacts}
  properties:
    deep: {type: object, fields: {l2: {type: object, fields: {bytes: {type: blobref}}}}}
`),
		// keyed and repeated are the two containers, and a declaration is one.
		"keyed and repeated": typ(`  names: {singular: contact, plural: contacts}
  properties: {scopes: {type: string, keyed: true, repeated: true}}
`),
		// A keyed map of maps has no second node to declare: the value's shape IS
		// the declaration, so leaving the fields out is refused by name.
		"keyed object without fields": typ(`  names: {singular: contact, plural: contacts}
  properties: {variants: {type: object, keyed: true}}
`),
		"keyed field of maps": typ(`  names: {singular: contact, plural: contacts}
  properties:
    spec: {type: object, fields: {variants: {type: object, keyed: true}}}
`),
		"keyed json": typ(`  names: {singular: contact, plural: contacts}
  properties: {raw: {type: json, keyed: true}}
`),
		"keyed secret": typ(`  names: {singular: contact, plural: contacts}
  properties: {keys: {type: secret, keyed: true}}
`),
		"keyPattern without keyed": typ(`  names: {singular: contact, plural: contacts}
  properties: {scopes: {type: string, keyPattern: camel}}
`),
		"unknown keyPattern": typ(`  names: {singular: contact, plural: contacts}
  properties: {scopes: {type: string, keyed: true, keyPattern: snake}}
`),
		"fts on a keyed map": typ(`  names: {singular: contact, plural: contacts}
  properties: {scopes: {type: string, keyed: true, fts: true}}
`),
		"embed on a keyed map": typ(`  names: {singular: contact, plural: contacts}
  properties: {scopes: {type: string, keyed: true, embed: true}}
`),
		// refersTo marks what a STRING names; a typed pointer is a reference.
		"refersTo on an int": typ(`  names: {singular: contact, plural: contacts}
  properties: {count: {type: int, refersTo: kind}}
`),
		"refersTo on a reference": typ(`  names: {singular: contact, plural: contacts}
  properties: {target: {type: reference, kind: any, refersTo: kind}}
`),
		"unknown refersTo": typ(`  names: {singular: contact, plural: contacts}
  properties: {emit: {type: string, repeated: true, refersTo: widget}}
`),
		// managed says the ENGINE stamps a property; a field is a position
		// inside one, and nothing stamps a position.
		"managed on a field": typ(`  names: {singular: contact, plural: contacts}
  properties: {spec: {type: object, fields: {version: {type: string, managed: true}}}}
`),
		"object refinement base": head + `kind: substrate.reamde.dev/core/propertytype
metadata: {id: x.example.com/x/shape}
data:
  authority: x.example.com
  package: x
  base: object
`,
		// The column-backed `title` and temporal properties may not be
		// redeclared: two declarations of one name means the write path
		// validates against the wrong one. `body` is declarable (#68), but
		// only text-family: the hot column is text, so a non-text `body` names
		// a column that cannot hold its value.
		"title is reserved": typ(`  names: {singular: contact, plural: contacts}
  properties: {title: {type: string}}
`),
		"body must be text-family": typ(`  names: {singular: contact, plural: contacts}
  properties: {body: {type: int}}
`),
		// repeated text and keyed text are ordinarily fine; on `body` they name a
		// list or map the single scalar column cannot hold, so the body guard
		// refuses them where the datatype alone would pass.
		"body cannot be repeated": typ(`  names: {singular: contact, plural: contacts}
  properties: {body: {type: text, repeated: true}}
`),
		"body cannot be keyed": typ(`  names: {singular: contact, plural: contacts}
  properties: {body: {type: text, keyed: true}}
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
		"old one_of": `kind: substrate.reamde.dev/core/package
metadata: {id: x.example.com/x}
data: {authority: x.example.com, package: x, version: 1}
---
kind: substrate.reamde.dev/core/trait
metadata: {id: x.example.com/x/ranked}
data:
  authority: x.example.com
  package: x
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
		"9f2k", "samples.substrate.reamde.dev/calendar/calendarevent", "people-c123", "people/c123",
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

// The actor grammar admits the closed domain's flat words and the machine
// hands, which carry the full authority AND the package (records 0025 and
// 0047). It still admits the retired `connector:<label>` spelling, because an
// actor DECLARATION carrying it is stored in every repository written before
// the rename and has to keep loading.
func TestValidActor(t *testing.T) {
	for _, ok := range []string{
		"api", "console", "substratectl", "substrate",
		"bundle:core", "bundle:samples.substrate.reamde.dev:web",
		"function:samples.substrate.reamde.dev:web:harvestUrls",
		"agent:samples.substrate.reamde.dev:web:librarian",
		"connector:gmail",
	} {
		if !vocabulary.ValidActor(ok) {
			t.Errorf("%q should be a legal actor", ok)
		}
	}
	for _, bad := range []string{
		"", "Bundle:web", "bundle:web/harvest", "function:web.example.com:Web:harvest",
		"substrate.oauth", "bundle:", "a:b:c:d:e", "bundle:web bundles",
	} {
		if vocabulary.ValidActor(bad) {
			t.Errorf("%q should not be a legal actor", bad)
		}
	}
}

// An actor is DERIVED from the identity that writes, never authored: a
// declaration naming a dispatch hand is refused, so no authority can set the
// tier another authority's callable writes at.
func TestActorsCarryTheFullAuthority(t *testing.T) {
	authority := "fn.example.com/fn"
	if got := vocabulary.PackageActor(authority); got != "bundle:fn.example.com:fn" {
		t.Fatalf("authority actor %q", got)
	}
	_, err := vocabulary.LoadFS(fstest.MapFS{"a.yaml": &fstest.MapFile{Data: []byte(
		`kind: substrate.reamde.dev/core/package
metadata:
  id: ` + authority + `
data:
  authority: fn.example.com
  package: fn
  version: 1
---
kind: substrate.reamde.dev/core/actor
metadata:
  id: function:fn.example.com:fn:mirror
data:
  authority: fn.example.com
  package: fn
  tier: owner
`)}})
	if err == nil || !strings.Contains(err.Error(), "minted at dispatch") {
		t.Fatalf("a declared function actor must refuse: %v", err)
	}
}

// actorDeclaration renders one package declaring one actor at one tier — the
// shape a bundle would ship to claim a hand.
func actorDeclaration(pkg, actor, tier string) []byte {
	authority, name, _ := strings.Cut(pkg, "/")
	return []byte(`kind: substrate.reamde.dev/core/package
metadata:
  id: ` + pkg + `
data:
  authority: ` + authority + `
  package: ` + name + `
  version: 1
---
kind: substrate.reamde.dev/core/actor
metadata:
  id: ` + actor + `
data:
  authority: ` + authority + `
  package: ` + name + `
  tier: ` + tier + `
`)
}

// A DECLARED BUNDLE ACTOR IS ITS OWN PACKAGE'S. A tier is declared data and
// the registry answers with it before the engine's reserved-name fallback, so
// a package that could declare `bundle:<somebody else>` would decide the tier
// that bundle's install and mapping writes stand at — owner tier pins against
// the owner's own recompute. The declarer and the named package must be the
// same.
func TestADeclaredBundleActorBelongsToItsAuthority(t *testing.T) {
	const evil, victim = "evil.example.com/evil", "victim.example.com/victim"

	_, err := vocabulary.LoadFS(fstest.MapFS{"a.yaml": &fstest.MapFile{
		Data: actorDeclaration(evil, vocabulary.PackageActor(victim), "owner"),
	}})
	if err == nil || !strings.Contains(err.Error(), "belongs to the package it names") {
		t.Fatalf("one package declared another's bundle hand: %v", err)
	}

	// Its own hand is the legal declaration, and the tier it declares is the
	// tier the registry answers with.
	r, err := vocabulary.LoadFS(fstest.MapFS{"a.yaml": &fstest.MapFile{
		Data: actorDeclaration(evil, vocabulary.PackageActor(evil), "machine"),
	}})
	if err != nil {
		t.Fatalf("an authority must be able to declare its own hand: %v", err)
	}
	tier, ok := r.ActorTier(vocabulary.PackageActor(evil))
	if !ok || tier != substrate.TierMachine {
		t.Fatalf("actor tier %q (%v), want machine", tier, ok)
	}
	// And it cannot answer for the hand it does not own, at any tier.
	if _, ok := r.ActorTier(vocabulary.PackageActor(victim)); ok {
		t.Fatal("the registry answers for a package it never loaded")
	}
}

// --- the shipped tree ----------------------------------------------------

// The one test against the real files: what the engine cannot boot without.
// Everything else about the vocabulary is editorial and belongs in the files'
// own review, not in the loader's suite.
func TestShippedSchemaLoads(t *testing.T) {
	r, err := vocabulary.LoadDir("../../kinds/substrate.reamde.dev/core")
	if err != nil {
		t.Fatalf("load shipped schema: %v", err)
	}
	for _, ident := range []string{
		// The meta-model the projections write, and the machinery the engine
		// addresses by name.
		"substrate.reamde.dev/core/kind", "substrate.reamde.dev/core/authority",
		"substrate.reamde.dev/core/trait", "substrate.reamde.dev/core/propertytype",
		"substrate.reamde.dev/core/recordmapping",
		"substrate.reamde.dev/core/actor", "substrate.reamde.dev/core/repository", "substrate.reamde.dev/core/token",
		"substrate.reamde.dev/core/recordmerge", "substrate.reamde.dev/core/recordsplit",
	} {
		if _, ok := r.ByIdentity(ident); !ok {
			t.Errorf("%s missing", ident)
		}
	}
	// `projectionpolicy` does not exist in this build:
	// projection is a same-named copy with latest-write-wins, and nothing
	// ranks sources.
	if _, ok := r.ByIdentity("substrate.reamde.dev/core/projectionpolicy"); ok {
		t.Error("substrate.reamde.dev/core/projectionpolicy is not part of this build")
	}
	if _, err := r.ResolveTrait("substrate.reamde.dev/core", "temporal"); err != nil {
		t.Errorf("temporal capability: %v", err)
	}
	// The runtime the substrate maintains is core's too (2026-08-12): the
	// delivery plumbing and the agent loop's data, folded out of the former
	// automation.substrate.reamde.dev / ai.substrate.reamde.dev authorities.
	for _, ident := range []string{
		"substrate.reamde.dev/core/trigger", "substrate.reamde.dev/core/run",
		"substrate.reamde.dev/core/llmprovider", "substrate.reamde.dev/core/llmthread", "substrate.reamde.dev/core/llmmessage",
		"substrate.reamde.dev/core/agent", "substrate.reamde.dev/core/function", "substrate.reamde.dev/core/bundle",
	} {
		if _, ok := r.ByIdentity(ident); !ok {
			t.Errorf("%s missing", ident)
		}
	}
	// THE SEEDED TREE IS THE CORE PACKAGE ALONE, beside the authority row that
	// owns it. Every other package is one a repository installs; a domain
	// package reappearing here would silently go back to being seeded into
	// every new repository.
	for _, g := range r.PackageList() {
		if g.IsAuthority() {
			continue
		}
		if g.Identity != vocabulary.PackageCore {
			t.Errorf("the seeded tree declares %s — only %s is seeded; vocabulary ships as an installable package",
				g.Identity, vocabulary.PackageCore)
		}
	}
	// The actor domain is closed and flat: the three doors
	// and the substrate's own hand.
	for _, a := range []string{"api", "console", "substratectl", "substrate"} {
		if !contains(r.Actors(), a) {
			t.Errorf("actor %q missing", a)
		}
	}
	// Identity is the kind reference <authority>/<package>/<name>, everywhere.
	for _, ty := range r.Kinds() {
		if ty.Identity != ty.Package+"/"+ty.Name {
			t.Errorf("identity %q is not %s/%s", ty.Identity, ty.Package, ty.Name)
		}
		if ty.SourceYAML == "" {
			t.Errorf("%s carries no source", ty.Identity)
		}
		// Every shipped property carries a description: the console's hover
		// tooltip, one short sentence. Implicit properties (machine stamps) are
		// declared by a transition, not an author.
		for _, pn := range ty.PropOrder {
			if p := ty.Props[pn]; !p.Implicit && p.Description == "" {
				t.Errorf("%s.%s carries no description", ty.Identity, pn)
			}
		}
	}
	for _, g := range r.PackageList() {
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
	roots := append([]string{"../../kinds/substrate.reamde.dev/core"}, shippedVocabularyDirs...)
	for _, root := range roots {
		if err := walk(root); err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if docs == 0 {
		t.Fatal("no shipped documents found; the walk is looking in the wrong place")
	}
}

// shippedVocabularyDirs are the SAMPLE packages the binary ships and a
// repository installs — the vocabulary the creation seed used to write and no
// longer does. `scheduling` is the traits calendar and tasks bind, so it
// admits alongside them.
var shippedVocabularyDirs = []string{
	"../../samples/calendar",
	"../../samples/messaging",
	"../../samples/people",
	"../../samples/scheduling",
	"../../samples/tasks",
}

// The shipped vocabulary is no longer seeded, so its manifests are held to the
// same bar where they now live: they admit TOGETHER (messaging and calendar
// point at people) and they carry the shape a sample package has — kinds, no
// inputs, no callables. They install as the repository's own
// (`source: installed`), which is what makes their GraphQL names
// `People_Person` and `Tasks_Task`.
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
	authorities, err := vocabulary.BuildPackages(docs, vocabulary.SourceInstalled)
	if err != nil {
		t.Fatalf("build the shipped vocabulary: %v", err)
	}
	// Into a repository that holds core and nothing else, all four at once —
	// the import order a fresh repository actually faces.
	r, err := vocabulary.LoadDir("../../kinds/substrate.reamde.dev/core")
	if err != nil {
		t.Fatalf("load the seeded tree: %v", err)
	}
	if err := r.InstallAll(authorities); err != nil {
		t.Fatalf("import the shipped vocabulary: %v", err)
	}
	want := map[string]string{
		"samples.substrate.reamde.dev/calendar":   "samples.substrate.reamde.dev/calendar",
		"samples.substrate.reamde.dev/messaging":  "samples.substrate.reamde.dev/messaging",
		"samples.substrate.reamde.dev/people":     "samples.substrate.reamde.dev/people",
		"samples.substrate.reamde.dev/scheduling": "samples.substrate.reamde.dev/scheduling",
		"samples.substrate.reamde.dev/tasks":      "samples.substrate.reamde.dev/tasks",
	}
	for _, g := range authorities {
		id, ok := want[g.Identity]
		if !ok {
			if g.IsAuthority() {
				continue // the authority row travels with every closure
			}
			t.Errorf("unexpected package %s", g.Identity)
			continue
		}
		if g.Bundle == nil {
			t.Errorf("%s ships no bundle document — it would not be importable", g.Identity)
			continue
		}
		if len(g.Bundle.Inputs) != 0 {
			t.Errorf("%s: a sample package configures nothing, got %d inputs", g.Identity, len(g.Bundle.Inputs))
		}
		if g.Bundle.Identity() != id {
			t.Errorf("%s bundle id = %q, want %q", g.Identity, g.Bundle.Identity(), id)
		}
		if g.Source != vocabulary.SourceInstalled {
			t.Errorf("%s source = %q — an installed package is the repository's", g.Identity, g.Source)
		}
		if len(g.Functions) != 0 || len(g.Agents) != 0 {
			t.Errorf("%s ships callables — these sample packages are kinds and nothing else", g.Identity)
		}
	}
	// Everything that maps onto people says so, so an import into a
	// core-only repository is refused with a legible reason.
	for _, name := range []string{"samples.substrate.reamde.dev/calendar", "samples.substrate.reamde.dev/messaging"} {
		g, ok := r.PackageByName(name)
		if !ok || !contains(g.Bundle.Requires, "samples.substrate.reamde.dev/people") {
			t.Errorf("%s does not require samples.substrate.reamde.dev/people", name)
		}
	}
	// Person carries no structured name parts (owner ruling): the full name
	// and the friendly one, nothing else name-shaped. Pronouns exist by a
	// later ruling (the mneme unification), free text with empty meaning
	// unknown, and never an enum.
	person, ok := r.ByIdentity("samples.substrate.reamde.dev/people/person")
	if !ok {
		t.Fatal("samples.substrate.reamde.dev/people/person missing")
	}
	for _, p := range []string{"name", "displayName", "pronouns"} {
		if prop, ok := person.Prop(p); !ok || prop.Datatype != vocabulary.DatatypeString {
			t.Errorf("person.%s missing", p)
		}
	}
	for _, p := range []string{"firstName", "middleName", "lastName"} {
		if _, ok := person.Prop(p); ok {
			t.Errorf("person.%s must not exist", p)
		}
	}
	// An installed kind carries its PACKAGE in GraphQL, so two packages may
	// declare a `person` without either renaming the other.
	if got := vocabulary.GraphQLName("samples.substrate.reamde.dev/people/person", person.Source); got != "People_Person" {
		t.Errorf("GraphQL name = %q, want People_Person", got)
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
		return fstest.MapFS{"g.yaml": &fstest.MapFile{Data: []byte(`kind: substrate.reamde.dev/core/package
metadata: {id: g.example.com/g}
data: {authority: g.example.com, package: g, version: 1}
---
kind: substrate.reamde.dev/core/kind
metadata: {id: g.example.com/g/thing}
data:
  authority: g.example.com
  package: g
  names: {singular: thing, plural: things}
  properties:
` + props)}}
	}

	t.Run("admitted and parsed", func(t *testing.T) {
		r, err := vocabulary.LoadFS(mk("    label: {type: string, renamedFrom: caption}\n"))
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		thing, _ := r.ByIdentity("g.example.com/g/thing")
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

// A repository's own authority is the kind grammar's authority within the DNS
// length limits, and the default is the username under the request host with
// the port gone (decision record 0046).
func TestRepositoryAuthorityGrammar(t *testing.T) {
	for _, ok := range []string{"ada.example.com", "geoah.me", "ada.localhost", "a.b", "ada.127.0.0.1", "kv-t3.tail83e66.ts.net"} {
		if !vocabulary.ValidRepositoryAuthority(ok) {
			t.Errorf("vocabulary.ValidRepositoryAuthority(%q) = false", ok)
		}
	}
	long := strings.Repeat("a", 64) + ".example.com"
	for _, bad := range []string{"", "ada", "Ada.example.com", "ada.example.com/", "ada..example.com", "-ada.example.com", long, strings.Repeat("ab.", 90) + "com"} {
		if vocabulary.ValidRepositoryAuthority(bad) {
			t.Errorf("vocabulary.ValidRepositoryAuthority(%q) = true", bad)
		}
	}
	for host, want := range map[string]string{
		"substrate.example":      "ada.substrate.example",
		"substrate.example:8080": "ada.substrate.example",
		"Substrate.Example.":     "ada.substrate.example",
		"127.0.0.1:5173":         "ada.127.0.0.1",
		"[::1]:8080":             "ada.::1",
		"":                       "",
	} {
		if got := vocabulary.DefaultRepositoryAuthority("ada", host); got != want {
			t.Errorf("vocabulary.DefaultRepositoryAuthority(ada, %q) = %q, want %q", host, got, want)
		}
	}
	if vocabulary.ValidRepositoryAuthority(vocabulary.DefaultRepositoryAuthority("ada", "[::1]:8080")) {
		t.Error("an IPv6 literal host produced a default that passes the grammar")
	}
}
