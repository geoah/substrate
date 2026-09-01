package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// document is one manifest in the pinned envelope
// : kind, metadata, data,
// status. `kind` is the kind REFERENCE — "tasks.substrate.reamde.dev/task", or a bare
// "task" for a repository-local kind. `status` holds server-set data on output
// and is ignored on input, which keeps `get -o yaml` output directly
// apply-able.
type document struct {
	Kind     string          `yaml:"kind" json:"kind"`
	Metadata documentMeta    `yaml:"metadata" json:"metadata"`
	Data     documentData    `yaml:"data" json:"data"`
	Status   *documentStatus `yaml:"status,omitempty" json:"status,omitempty"`
}

// documentMeta is the identity block: the record id, the authored namespaced
// key spaces, and the version precondition — which sits here because `data` is
// authored content and a precondition is not content.
type documentMeta struct {
	ID          string         `yaml:"id,omitempty" json:"id,omitempty"`
	Labels      map[string]any `yaml:"labels,omitempty" json:"labels,omitempty"`
	Annotations map[string]any `yaml:"annotations,omitempty" json:"annotations,omitempty"`
	// IfVersion is the CAS guard, input-only: never emitted by a read.
	IfVersion *int64 `yaml:"ifVersion,omitempty" json:"ifVersion,omitempty"`
}

// documentData is everything authored, which is properties and nothing else.
// Everything authored IS a property: `title`, `body`, the temporal properties
// and every pointer at another record sit in Properties beside the declared
// ones, and so does a state's current value — moving one is a patch rather
// than an apply.
type documentData struct {
	Properties map[string]any `yaml:"properties,omitempty" json:"properties,omitempty"`
}

// documentStatus is the server-set block: never sent back on apply. FormerIDs
// is the merge trail — the ids this record used to answer to, which is store
// bookkeeping. Properties is the managed-property bookkeeping a single-record
// read carries (wire `propertyMeta`), absent on lists and when nothing manages
// anything. Incoming references are not part of the document; they page on
// their own resource.
type documentStatus struct {
	Version    int64                     `yaml:"version" json:"version"`
	CreatedAt  time.Time                 `yaml:"createdAt" json:"createdAt"`
	UpdatedAt  time.Time                 `yaml:"updatedAt" json:"updatedAt"`
	DeletedAt  *time.Time                `yaml:"deletedAt,omitempty" json:"deletedAt,omitempty"`
	Finalizers []string                  `yaml:"finalizers,omitempty" json:"finalizers,omitempty"`
	FormerIDs  []string                  `yaml:"formerIds,omitempty" json:"formerIds,omitempty"`
	Properties map[string]statusProperty `yaml:"properties,omitempty" json:"properties,omitempty"`
}

// statusProperty is one managed property: its manager (the actor whose write
// stands — `owner` for a hand edit, a bundle's own actor for a mapped one), its
// tier (`owner` | `bundle` | `machine` — machine means recompute may
// replace the value, the other two hold), when it last changed, and the
// alternatives — every live mapping-source offer whose value differs from
// the stored one.
// Server-set through and through, so it rides in status and is ignored on
// input like the rest of the block.
type statusProperty struct {
	Manager   string         `yaml:"manager" json:"manager"`
	Tier      substrate.Tier `yaml:"tier,omitempty" json:"tier,omitempty"`
	UpdatedAt time.Time      `yaml:"updatedAt" json:"updatedAt"`
	// Alternatives are what the other live sources would write instead —
	// adopting one is just writing it.
	Alternatives []statusAlternative `yaml:"alternatives,omitempty" json:"alternatives,omitempty"`
}

type statusAlternative struct {
	Actor     string    `yaml:"actor" json:"actor"`
	Value     any       `yaml:"value" json:"value"`
	UpdatedAt time.Time `yaml:"updatedAt" json:"updatedAt"`
}

func (d *document) putInput() (substrate.PutInput, error) {
	if d.Kind == "" {
		return substrate.PutInput{}, fmt.Errorf("document has no `kind`")
	}
	in := substrate.PutInput{
		Kind:        d.Kind,
		Properties:  d.Data.Properties,
		Labels:      d.Metadata.Labels,
		Annotations: d.Metadata.Annotations,
		IfVersion:   d.Metadata.IfVersion,
	}
	return in, nil
}

// recordDocument renders a stored record as an apply-able manifest plus its
// server-set status block. The envelope's `kind` is the record's kind
// reference, verbatim. meta is the wire's `propertyMeta` — populated only on a
// single-record read, so lists pass nil.
func recordDocument(e *substrate.Record, meta map[string]statusProperty) *document {
	d := &document{
		Kind: e.Kind,
		Metadata: documentMeta{
			ID:          e.ID,
			Labels:      normalizeMap(e.Labels),
			Annotations: normalizeMap(e.Annotations),
		},
		Data: documentData{
			// Everything authored is here already: the wire's property map
			// carries title, body and the temporal properties beside the
			// declared ones, so the document is a copy, not a reassembly.
			Properties: normalizeMap(e.Properties),
		},
		Status: &documentStatus{
			Version:    e.Version,
			CreatedAt:  e.CreatedAt,
			UpdatedAt:  e.UpdatedAt,
			DeletedAt:  e.DeletedAt,
			Finalizers: e.Finalizers,
			FormerIDs:  e.FormerIDs,
			Properties: normalizeMeta(meta),
		},
	}
	return d
}

// --- declarations ---

// declarationDocument is a VOCABULARY declaration in the shape its author
// writes: the same four-key envelope, but `data` holds the declaration's own
// keys — `authority`, `names`, `properties`, `version` — directly.
//
// A declaration is two things at once, and that is the whole difficulty. It is
// a RECORD, so it reads back through the ordinary collection API and arrives
// here as a substrate.Record whose Properties map holds those keys. It is also
// the INPUT to /vocabulary/apply, which takes the authored shape. Rendering it
// as an ordinary record nests every key one level too deep under
// `data.properties`, and `apply -f` of that output is refused for a missing
// `data.authority` — a read whose output the writer will not take.
type declarationDocument struct {
	Kind     string          `yaml:"kind" json:"kind"`
	Metadata documentMeta    `yaml:"metadata" json:"metadata"`
	Data     map[string]any  `yaml:"data" json:"data"`
	Status   *documentStatus `yaml:"status,omitempty" json:"status,omitempty"`
}

// documentOf renders a stored record for output: a declaration in its authored
// shape, everything else as an ordinary record. This is the one place that
// choice is made, so `get -o yaml`, the `---` stream and `-o json` cannot
// disagree about it.
func documentOf(e *substrate.Record, meta map[string]statusProperty) any {
	if short, ok := declarationKindOf(e.Kind); ok {
		return declarationDocumentOf(short, e, meta)
	}
	return recordDocument(e, meta)
}

// declarationKindOf reports whether a kind reference names one of the core
// meta-kinds, and which. It is the same test `apply` uses to route a document
// to /vocabulary/apply, so the reader and the writer agree on what a
// declaration is by construction.
func declarationKindOf(kind string) (string, bool) {
	authority, name := vocabulary.SplitKindRef(kind)
	if authority != vocabulary.AuthorityCore || !vocabulary.VocabularyDocumentKind(name) {
		return "", false
	}
	return name, true
}

// declarationDocumentOf renders one declaration row as its authored document.
//
// `data` is a WHITELIST, not the property map with a few keys removed. A
// declaration ROW carries more than its document does — the engine stamps an
// origin and a version, quarantine marks, the bundle lifecycle bools, and the
// record projection adds the derived display title on top — and every one of
// those is refused by /vocabulary/apply as an unknown key. Subtracting the
// ones known today would leave the next stamped property to break the round
// trip silently, so the set that admits is the set that renders:
// vocabulary.DeclarationDataKeys, which exists for exactly this and says so.
func declarationDocumentOf(short string, e *substrate.Record, meta map[string]statusProperty) *declarationDocument {
	admitted := vocabulary.DeclarationDataKeys(short)
	data := map[string]any{}
	for k, v := range normalizeMap(e.Properties) {
		if admitted[k] {
			data[k] = v
		}
	}
	return &declarationDocument{
		Kind: e.Kind,
		Metadata: documentMeta{
			ID:          e.ID,
			Labels:      normalizeMap(e.Labels),
			Annotations: normalizeMap(e.Annotations),
		},
		Data: data,
		Status: &documentStatus{
			Version:    e.Version,
			CreatedAt:  e.CreatedAt,
			UpdatedAt:  e.UpdatedAt,
			DeletedAt:  e.DeletedAt,
			Finalizers: e.Finalizers,
			FormerIDs:  e.FormerIDs,
			Properties: normalizeMeta(meta),
		},
	}
}

// --- numbers ---
//
// A property map is `map[string]any`, so every number in one arrives untyped.
// The wire keeps it verbatim (the response decoder is UseNumber), and this is
// where it becomes a Go value: an integer stays an integer, so a repository's
// `totpStep` renders as `59545831` rather than the float64 round trip's
// `5.9545831e+07` — which is not the value the substrate holds, is not what a
// document applies back, and loses digits entirely past 2^53.

// normalizeNumbers types every number under v, recursively.
func normalizeNumbers(v any) any {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		if f, err := t.Float64(); err == nil {
			return f
		}
		// Too big for either: the digits are the value, so keep them.
		return t.String()
	case float64:
		// A pre-typed float (a decode that was not UseNumber, a hand-built
		// map): an integral one is an integer that lost its type on the way.
		if t == math.Trunc(t) && math.Abs(t) < 1<<53 {
			return int64(t)
		}
		return t
	case map[string]any:
		return normalizeMap(t)
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = normalizeNumbers(item)
		}
		return out
	}
	return v
}

// normalizeMap types every number in one map, returning nil for nil so an
// absent block stays absent (`omitempty` is doing real work in the document).
func normalizeMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = normalizeNumbers(v)
	}
	return out
}

// normalizeMeta types the numbers inside the managed-property block: an
// alternative's value is an untyped property value like any other.
func normalizeMeta(meta map[string]statusProperty) map[string]statusProperty {
	if meta == nil {
		return nil
	}
	out := make(map[string]statusProperty, len(meta))
	for k, sp := range meta {
		alts := make([]statusAlternative, len(sp.Alternatives))
		for i, alt := range sp.Alternatives {
			alt.Value = normalizeNumbers(alt.Value)
			alts[i] = alt
		}
		if len(alts) > 0 {
			sp.Alternatives = alts
		}
		out[k] = sp
	}
	return out
}

// marshalDocument renders one manifest at the two-space indent the format's
// examples are written in. It takes `any` because a declaration renders
// through declarationDocument, whose `data` is the authored map rather than
// the record document's property block.
func marshalDocument(d any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(d); err != nil {
		return nil, fmt.Errorf("encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode yaml: %w", err)
	}
	return buf.Bytes(), nil
}

// --- decoding ---

// envelopeProbe reads the keys that decide whether a document is a manifest at
// all: the envelope's own, plus every spelling the envelope replaced, so the
// error can name the replacement rather than the absence. The `data` keys it
// carries are the ones the wire no longer has: decoding silently ignores an
// unknown key, and a document whose whole property block went unread would
// apply clean having written nothing.
type envelopeProbe struct {
	Kind       string `yaml:"kind"`
	Authority  string `yaml:"authority"`
	Type       string `yaml:"type"`
	APIVersion string `yaml:"apiVersion"`
	ID         string `yaml:"id"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Data struct {
		Props     map[string]any `yaml:"props"`
		States    map[string]any `yaml:"states"`
		IfVersion any            `yaml:"ifVersion"`
		Title     any            `yaml:"title"`
		Body      any            `yaml:"body"`
		At        any            `yaml:"at"`
		EndsAt    any            `yaml:"endsAt"`
		DueAt     any            `yaml:"dueAt"`
	} `yaml:"data"`
}

// nodeDocument turns one parsed YAML document into a manifest. where names the
// document for errors ("task.yaml document 2").
func nodeDocument(node *yaml.Node, where string) (*document, error) {
	var probe envelopeProbe
	if err := node.Decode(&probe); err != nil {
		return nil, fmt.Errorf("parse %s: %w", where, err)
	}
	if err := renamedKeyError(where, node, &probe); err != nil {
		return nil, err
	}
	if probe.Kind == "" {
		return nil, envelopeError(where, probe)
	}
	if probe.ID != "" {
		return nil, fmt.Errorf("%s mixes the envelope with the pre-envelope key `id`;\n"+
			"the envelope already carries it: `metadata.id` is the record id", where)
	}

	// Key PRESENCE, not value: `data.title: null` is as much the old shape as
	// `data.title: x`, and reading past it would silently drop the intent.
	dataNode := mappingChild(node, "data")
	for _, name := range []string{"title", "body", "at", "endsAt", "dueAt"} {
		if hasKey(dataNode, name) {
			return nil, fmt.Errorf("%s writes `data.%s`, which is `data.properties.%s`:\n"+
				"everything authored is a property", where, name, name)
		}
	}
	if hasKey(dataNode, "props") {
		return nil, fmt.Errorf("%s writes `data.props`, which is `data.properties`; rename the key", where)
	}
	if hasKey(dataNode, "edges") {
		return nil, fmt.Errorf("%s writes `data.edges`, which is gone:\n"+
			"a pointer at another record is a `type: reference` property, so it lives in\n"+
			"`data.properties` as `<name>: <kind>/<id>` (a list for a repeated one)", where)
	}
	if hasKey(dataNode, "states") {
		return nil, fmt.Errorf("%s writes `data.states`, which is not a block:\n"+
			"a state is a property, so its value lives in `data.properties`,\n"+
			"and a transition travels as `substratectl patch --state <name>=<state>`", where)
	}
	if hasKey(dataNode, "ifVersion") {
		return nil, fmt.Errorf("%s writes `data.ifVersion`, which is `metadata.ifVersion`:\n"+
			"a precondition is not authored content", where)
	}
	var d document
	if err := node.Decode(&d); err != nil {
		return nil, fmt.Errorf("parse %s: %w", where, err)
	}
	return &d, nil
}

// renamedKeyError names the spellings the envelope replaced. They are checked
// before the envelope's own keys are: a document written in the old envelope
// has a `kind` and a `spec` too, and being told what those are now beats being
// told it has no `authority`.
func renamedKeyError(where string, node *yaml.Node, p *envelopeProbe) error {
	if hasKey(node, "apiVersion") {
		return fmt.Errorf("%s writes `apiVersion`, which is gone — the version left the envelope entirely;\n"+
			"there is one served version and nothing routes on it, so write `kind: <authority>/<name>`", where)
	}
	if hasKey(node, "group") || hasKey(node, "type") {
		return fmt.Errorf("%s writes `group`/`type`, which are one key now: `kind`, the kind reference\n"+
			"(`kind: tasks.substrate.reamde.dev/task`, or a bare `kind: task` for a repository-local kind)", where)
	}
	if hasKey(node, "spec") {
		return fmt.Errorf("%s writes `spec`, which is `data` — and everything authored is a property,\n"+
			"so `title`, `body` and the temporal properties sit in `data.properties`", where)
	}
	if p.Metadata.Name != "" {
		return fmt.Errorf("%s writes `metadata.name`, which is `metadata.id` — the record id; rename the key", where)
	}
	// Identity is minimal now: a record's id IS the writer's key, and nothing
	// alias-shaped exists to accept. Reading past one of these would silently
	// drop the block — the one failure this dialect must not have.
	for _, key := range []string{"byAlias", "aliases", "identifying"} {
		if hasKey(node, key) || hasKey(mappingChild(node, "data"), key) || hasKey(mappingChild(node, "metadata"), key) {
			return fmt.Errorf("%s writes `%s`, which is deleted — identity is minimal:\n"+
				"a record's id is the writer's own key (`metadata.id`), and nothing matches on values", where, key)
		}
	}
	return nil
}

// mappingChild returns the mapping node under key, or nil.
func mappingChild(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// hasKey reports a top-level mapping key, for the blocks a typed probe cannot
// see (an empty `spec:` decodes to nothing at all).
func hasKey(node *yaml.Node, key string) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
}

// unwrapNode drops the document wrapper yaml.v3 puts around a decoded node.
func unwrapNode(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		return node.Content[0]
	}
	return node
}

// emptyNode reports a document with nothing in it (`---` alone, or comments).
func emptyNode(node *yaml.Node) bool {
	return node.Kind == 0 || (node.Kind == yaml.ScalarNode && node.Tag == "!!null")
}

// envelopeError explains a document that is missing the envelope. The
// pre-envelope format — a full identity in `type:`, beside a top-level `id:` —
// is a hard error, not a silent fallback, and the message shows the same
// document in the shape it now needs.
func envelopeError(where string, p envelopeProbe) error {
	kind := p.Type
	if p.Authority != "" {
		kind = p.Authority + "/" + p.Type
	}
	if kind == "" {
		kind = "<authority>/<name>"
	}
	id := p.ID
	if id == "" {
		id = "<id>"
	}
	return fmt.Errorf("%s has no `kind`; every manifest wears the kind/metadata/data envelope,\n"+
		"so write it as\n\n"+
		"  kind: %s\n"+
		"  metadata:\n"+
		"    id: %s\n"+
		"  data:\n"+
		"    properties: {…}", where, kind, id)
}
