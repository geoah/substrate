package catalog

import (
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The facts an approval reads about what installing a closure brings, read
// once from the documents the install applies, so a reader never walks the
// shipped YAML itself. Every map they carry is the catalog's, shared by every
// repository it serves: a reader never changes one.

// KindFact is one kind a closure declares.
type KindFact struct {
	Identity string
	// Label is the kind's declared label, nil when it declares none.
	Label *substrate.KindLabel
	// Purpose is the declared purpose, empty where it declares none, which
	// reads as primary.
	Purpose string
	// Definition is the declaration's data as shipped.
	Definition map[string]any
}

// FunctionFact is one function a closure ships: code that runs on the
// owner's server once installed, with the kinds it may write, each named in
// full or as a glob.
type FunctionFact struct {
	ID     string
	Writes []string
}

// TriggerFact is one trigger a closure ships, as the install writes it.
type TriggerFact struct {
	ID string
	// Callable is the reference path of what it fires.
	Callable string
	// Enabled is how it ships: an enabled record or schedule trigger runs on
	// its own once installed.
	Enabled bool
	// Webhook is whether its source is a webhook.
	Webhook bool
	// Properties are its properties as shipped.
	Properties map[string]any
}

// Kinds are the kinds the closure declares, in document order.
func (b *Bundle) Kinds() []KindFact { return b.kinds }

// Functions are the functions the closure ships, in document order.
func (b *Bundle) Functions() []FunctionFact { return b.functions }

// Triggers are the triggers the closure ships, in document order.
func (b *Bundle) Triggers() []TriggerFact { return b.triggers }

// Kind is one kind the closure declares, by its full reference.
func (b *Bundle) Kind(identity string) (KindFact, bool) {
	for _, k := range b.kinds {
		if k.Identity == identity {
			return k, true
		}
	}
	return KindFact{}, false
}

// Trigger is one trigger the closure ships, by its id.
func (b *Bundle) Trigger(id string) (TriggerFact, bool) {
	for _, t := range b.triggers {
		if t.ID == id {
			return t, true
		}
	}
	return TriggerFact{}, false
}

// readFacts fills b's facts from its documents: the declarations for kinds
// and functions, the data documents for triggers.
func (b *Bundle) readFacts() {
	for _, d := range b.vocabularyDocs {
		data := mmap(d, "data")
		switch vocabulary.KindName(mstr(d, "kind")) {
		case vocabulary.DocKind:
			b.kinds = append(b.kinds, KindFact{
				Identity: docID(d), Label: kindLabel(data), Purpose: mstr(data, "purpose"), Definition: data,
			})
		case vocabulary.DocFunction:
			b.functions = append(b.functions, FunctionFact{ID: docID(d), Writes: functionWrites(docID(d), data)})
		}
	}
	for _, d := range b.dataDocs {
		if mstr(d, "kind") != kindTrigger {
			continue
		}
		props := mmap(mmap(d, "data"), "properties")
		enabled, _ := props["enabled"].(bool)
		b.triggers = append(b.triggers, TriggerFact{
			ID: docID(d), Callable: vocabulary.ReferencePath(props["callable"]), Enabled: enabled,
			Webhook: vocabulary.TriggerWebhook(props) != nil, Properties: props,
		})
	}
}

// kindLabel is a declaration's label, nil unless it names both forms.
func kindLabel(data map[string]any) *substrate.KindLabel {
	l := mmap(data, "label")
	singular, plural := mstr(l, "singular"), mstr(l, "plural")
	if singular == "" || plural == "" {
		return nil
	}
	return &substrate.KindLabel{Singular: singular, Plural: plural}
}

// functionWrites is a function's `permissions.writes`, each named in full: a
// bare name is a kind of the function's own package, and a glob stays one.
func functionWrites(id string, data map[string]any) []string {
	entries := vocabulary.ReferentIDs(mslice(mmap(data, "permissions"), "writes"), vocabulary.CoreKind(vocabulary.DocKind))
	authority, pkg := vocabulary.SplitPackageRef(vocabulary.KindPackage(id))
	out := make([]string, 0, len(entries))
	for _, w := range entries {
		switch {
		case w == "":
			continue
		case !vocabulary.Qualified(w) && !vocabulary.IsTypeGlob(w):
			w = vocabulary.KindRef(authority, pkg, w)
		}
		out = append(out, w)
	}
	return out
}
