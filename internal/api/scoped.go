package api

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// scopedDataset is the dataset an app's token reaches (decision 0194): the
// engine's, held to the token's scopes. It is the second of the API's two
// boundaries, behind the route groups: every method is spelled out and
// nothing is embedded, so a method added to substrate.Dataset does not
// compile here until somebody decides what an app's token may do with it.
//
// What it allows: reading the kinds the grant reads (Get, List, Search,
// Kinds, KindByRef, TypesImplementing), writing the kinds it writes (Put,
// Patch, Delete), the unit table and conversions. Under `agents:` and
// `decide:` (decision 0195) it also reads the rows those reach, chats with
// and stops the matching agents, makes the few writes those verbs carry and
// tails the change feed held to the same rows (scopedagents.go). Under
// `webhook:` it lists that trigger's parked deliveries, and a grant that
// writes a kind declaring a blob-ref property stores blobs
// (docs/plans/app-setup.md). A kind the grant cannot read is answered exactly
// as a kind the repository never declared, so a refusal does not tell an app
// which kinds exist. Every record handed back, a write's answer included, is
// cut to what the grant reads, and a trigger never carries its webhook key.
// Everything else is ErrForbidden.
type scopedDataset struct {
	inner substrate.Dataset
	grant scope.Grant
	// actor is the app's own derived actor, `app:<host>`: every write goes
	// out under it whatever actor the handler passes.
	actor substrate.Actor

	// The registry snapshot, read once per request (a scopedDataset lives for
	// one request), so a kind's purpose is known without a lookup per record.
	once     sync.Once
	registry map[string]substrate.KindInfo
	regErr   error

	// mu guards the memos of the row rules: each thread's agent and the feed
	// rows found readable, which a watch consults for as long as it runs.
	mu           sync.Mutex
	threadAgents map[string]string
	seen         map[string]bool
}

var _ substrate.Dataset = (*scopedDataset)(nil)

func newScopedDataset(inner substrate.Dataset, grant scope.Grant, actor substrate.Actor) *scopedDataset {
	return &scopedDataset{inner: inner, grant: grant, actor: actor}
}

// errScope is the refusal of everything outside the scoped surface.
func errScope(what string) error {
	return fmt.Errorf("%w: an app's token does not reach %s", substrate.ErrForbidden, what)
}

// errUnknownKind answers a kind the grant cannot read the way an undeclared
// kind is answered.
func errUnknownKind(kind string) error {
	return fmt.Errorf("%w: unknown kind %s", substrate.ErrNotFound, kind)
}

func (s *scopedDataset) kinds(ctx context.Context) (map[string]substrate.KindInfo, error) {
	s.once.Do(func() {
		list, err := s.inner.Kinds(ctx)
		if err != nil {
			s.regErr = err
			return
		}
		s.registry = make(map[string]substrate.KindInfo, len(list))
		for _, k := range list {
			s.registry[k.Identity] = k
		}
	})
	return s.registry, s.regErr
}

func internalKind(k substrate.KindInfo) bool {
	p, _ := k.Definition["purpose"].(string)
	return p == vocabulary.PurposeInternal
}

// reads reports whether the grant reads kind; a kind the registry does not
// hold is never readable.
func (s *scopedDataset) reads(ctx context.Context, kind string) (bool, error) {
	reg, err := s.kinds(ctx)
	if err != nil {
		return false, err
	}
	k, ok := reg[kind]
	return ok && s.grant.Reads(kind, internalKind(k)), nil
}

func (s *scopedDataset) writes(ctx context.Context, kind string) (bool, error) {
	reg, err := s.kinds(ctx)
	if err != nil {
		return false, err
	}
	k, ok := reg[kind]
	return ok && s.grant.Writes(kind, internalKind(k)), nil
}

// readable is the sorted list of every kind the grant reads today.
func (s *scopedDataset) readable(ctx context.Context) ([]string, error) {
	reg, err := s.kinds(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for id, k := range reg {
		if s.grant.Reads(id, internalKind(k)) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

// kindOfPath is the kind of a record path, `<authority>/<package>/<name>/<id>`:
// the first three segments, since an id may carry a slash of its own
// (`health/heart_rate`, `m/s`). "" when it is not a record path.
func kindOfPath(path string) string {
	kind, _, ok := vocabulary.SplitRecordPath(path)
	if !ok {
		return ""
	}
	return kind
}

// cut returns rec as the grant may see it: a copy whose inbound links and
// property provenance name only readable kinds. A reference VALUE inside
// rec stays: the target's address is part of the record the grant reads.
func (s *scopedDataset) cut(ctx context.Context, rec *substrate.Record) (*substrate.Record, error) {
	if rec == nil {
		return nil, nil
	}
	reg, err := s.kinds(ctx)
	if err != nil {
		return nil, err
	}
	allowKind := func(kind string) bool {
		k, ok := reg[kind]
		return ok && s.grant.Reads(kind, internalKind(k))
	}
	cp := *rec
	if len(rec.LinkedFrom) > 0 {
		cp.LinkedFrom = nil
		for _, l := range rec.LinkedFrom {
			if allowKind(l.Kind) {
				cp.LinkedFrom = append(cp.LinkedFrom, l)
			}
		}
	}
	if len(rec.PropertyMeta) > 0 {
		cp.PropertyMeta = substrate.MetaWithinKinds(rec.PropertyMeta, func(path string) bool {
			return allowKind(kindOfPath(path))
		})
	}
	if rec.Kind == kindTrigger {
		withoutWebhookKeys(&cp)
	}
	if !s.grant.Reads(kindBlob, true) {
		withoutBlobManifests(&cp, reg[rec.Kind])
	}
	return &cp, nil
}

// kindBlob is a blob's manifest, which holds the name and media type its
// first upload gave it. An app's token that does not read the kind reads a
// blob reference as its digest, size and status alone: the bytes dedupe by
// digest, so an app holding another's bytes could otherwise name the digest
// in a record and read the manifest it never saw.
const kindBlob = corePackage + "/blob"

// withoutBlobManifests drops the manifest's `name` and `mediaType` from every
// blob reference a record's declared blobref properties resolve to. Every
// map it changes is a copy.
func withoutBlobManifests(rec *substrate.Record, k substrate.KindInfo) {
	props, _ := k.Definition["properties"].(map[string]any)
	copied := false
	for name, decl := range props {
		d, _ := decl.(map[string]any)
		if t, _ := d["type"].(string); t != "blobref" {
			continue
		}
		v, ok := rec.Properties[name]
		if !ok {
			continue
		}
		if cut, changed := withoutManifest(v); changed {
			if !copied {
				rec.Properties, copied = maps.Clone(rec.Properties), true
			}
			rec.Properties[name] = cut
		}
	}
}

// withoutManifest is a blob reference, or a list of them, without the
// manifest's name and media type, and whether there were any to drop.
func withoutManifest(v any) (any, bool) {
	switch ref := v.(type) {
	case map[string]any:
		_, hasName := ref["name"]
		_, hasType := ref["mediaType"]
		if !hasName && !hasType {
			return v, false
		}
		ref = maps.Clone(ref)
		delete(ref, "name")
		delete(ref, "mediaType")
		return ref, true
	case []any:
		out, changed := ref, false
		for i, item := range ref {
			if cut, ok := withoutManifest(item); ok {
				if !changed {
					out, changed = slices.Clone(ref), true
				}
				out[i] = cut
			}
		}
		return out, changed
	}
	return v, false
}

// kindTrigger is the kind whose records hold a webhook's key, which no app's
// token receives whatever its scopes: an app delivers under `webhook:` with
// its own token (docs/plans/app-setup.md).
const kindTrigger = corePackage + "/trigger"

// triggerSource is the trigger property the key lives under.
const triggerSource = "source"

// withoutWebhookKeys drops `source.webhook.key` from a trigger, in its value
// and in any offer its provenance carries. Every map it changes is a copy:
// the record it was cut from may be one the inner dataset shares.
func withoutWebhookKeys(rec *substrate.Record) {
	if src, ok := withoutWebhookKey(rec.Properties[triggerSource]); ok {
		rec.Properties = maps.Clone(rec.Properties)
		rec.Properties[triggerSource] = src
	}
	m, ok := rec.PropertyMeta[triggerSource]
	if !ok || len(m.Alternatives) == 0 {
		return
	}
	alts := slices.Clone(m.Alternatives)
	changed := false
	for i := range alts {
		if v, ok := withoutWebhookKey(alts[i].Value); ok {
			alts[i].Value, changed = v, true
		}
	}
	if changed {
		m.Alternatives = alts
		rec.PropertyMeta = maps.Clone(rec.PropertyMeta)
		rec.PropertyMeta[triggerSource] = m
	}
}

// withoutWebhookKey is a trigger's source without its webhook key, and
// whether there was one to drop.
func withoutWebhookKey(v any) (any, bool) {
	src, _ := v.(map[string]any)
	hook, _ := src["webhook"].(map[string]any)
	if _, ok := hook["key"]; !ok {
		return v, false
	}
	hook = maps.Clone(hook)
	delete(hook, "key")
	src = maps.Clone(src)
	src["webhook"] = hook
	return src, true
}

// checkTriggerSource refuses a condition or an order on a trigger's source
// in a list that reaches triggers: the engine compares the property's stored
// text, key included, and a cursor carries the last row's sort values, so
// either would answer what the record no longer shows.
func checkTriggerSource(kinds []string, q substrate.Query) error {
	if !slices.Contains(kinds, kindTrigger) {
		return nil
	}
	onSource := func(name string) bool {
		return name == triggerSource || strings.HasPrefix(name, triggerSource+".")
	}
	for name := range q.Filter.Properties {
		if onSource(name) {
			return fmt.Errorf("%w: an app's token does not filter %s by its source", substrate.ErrForbidden, kindTrigger)
		}
	}
	for _, o := range q.OrderBy {
		if onSource(o.Property) {
			return fmt.Errorf("%w: an app's token does not order %s by its source", substrate.ErrForbidden, kindTrigger)
		}
	}
	return nil
}

// requireRead answers the refusal for a kind the grant cannot read.
func (s *scopedDataset) requireRead(ctx context.Context, kind string) error {
	ok, err := s.reads(ctx, kind)
	if err != nil {
		return err
	}
	if !ok {
		return errUnknownKind(kind)
	}
	return nil
}

// requireWrite answers the refusal for a kind the grant cannot write: as
// unknown when it cannot read it either, else forbidden.
func (s *scopedDataset) requireWrite(ctx context.Context, kind string) error {
	if gated, err := s.rowGated(ctx, kind); err != nil {
		return err
	} else if gated {
		return fmt.Errorf("%w: an app's token does not write %s", substrate.ErrForbidden, kind)
	}
	if err := s.requireRead(ctx, kind); err != nil {
		return err
	}
	ok, err := s.writes(ctx, kind)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: an app's token does not write %s", substrate.ErrForbidden, kind)
	}
	return nil
}

// narrow holds a kind list to the grant: an empty list becomes every
// readable kind, a named kind the grant cannot read is unknown. It answers
// false when nothing is left, which the caller answers with an empty result
// rather than passing on, because no kinds means every kind below here.
func (s *scopedDataset) narrow(ctx context.Context, kinds []string) ([]string, bool, error) {
	if len(kinds) == 0 {
		all, err := s.readable(ctx)
		if err != nil {
			return nil, false, err
		}
		return all, len(all) > 0, nil
	}
	for _, k := range kinds {
		if err := s.requireRead(ctx, k); err != nil {
			return nil, false, err
		}
	}
	return kinds, true, nil
}

// --- the allowed surface ---

func (s *scopedDataset) Repository() substrate.RepositoryInfo { return s.inner.Repository() }

func (s *scopedDataset) Kinds(ctx context.Context) ([]substrate.KindInfo, error) {
	reg, err := s.kinds(ctx)
	if err != nil {
		return nil, err
	}
	all, err := s.inner.Kinds(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]substrate.KindInfo, 0, len(all))
	for _, k := range all {
		if r, ok := reg[k.Identity]; ok && (s.grant.Reads(k.Identity, internalKind(r)) || s.rowKind(k.Identity)) {
			out = append(out, k)
		}
	}
	return out, nil
}

func (s *scopedDataset) KindByRef(ctx context.Context, ref string) (substrate.KindInfo, error) {
	// The package and bundle kinds resolve for a token that keeps a package,
	// so the record path reaches Get, which admits that package's rows alone.
	if s.keepsPackage() && (ref == vocabulary.CoreKind(vocabulary.DocPackage) || ref == vocabulary.CoreKind(vocabulary.DocBundle)) {
		return s.inner.KindByRef(ctx, ref)
	}
	if ok, err := s.reaches(ctx, ref); err != nil {
		return substrate.KindInfo{}, err
	} else if !ok {
		return substrate.KindInfo{}, errUnknownKind(ref)
	}
	return s.inner.KindByRef(ctx, ref)
}

func (s *scopedDataset) TypesImplementing(ctx context.Context, trait string) ([]substrate.KindInfo, error) {
	all, err := s.inner.TypesImplementing(ctx, trait)
	if err != nil {
		return nil, err
	}
	out := make([]substrate.KindInfo, 0, len(all))
	for _, k := range all {
		if ok, err := s.reaches(ctx, k.Identity); err != nil {
			return nil, err
		} else if ok {
			out = append(out, k)
		}
	}
	return out, nil
}

func (s *scopedDataset) Units(ctx context.Context) (substrate.UnitTable, error) {
	return s.inner.Units(ctx)
}

func (s *scopedDataset) ConvertUnit(ctx context.Context, in substrate.UnitConvert) (substrate.UnitConversion, error) {
	return s.inner.ConvertUnit(ctx, in)
}

func (s *scopedDataset) Get(ctx context.Context, typ, id string) (*substrate.Record, error) {
	// A package an app keeps the declarations of tells it the version it
	// holds, through its package and bundle rows (decision 0203).
	if (typ == vocabulary.CoreKind(vocabulary.DocPackage) || typ == vocabulary.CoreKind(vocabulary.DocBundle)) &&
		s.grant.DeclaresIn(id, s.inner.Repository().Authority) {
		return s.inner.Get(ctx, typ, id)
	}
	gated, err := s.rowGated(ctx, typ)
	if err != nil {
		return nil, err
	}
	if !gated {
		if err := s.requireRead(ctx, typ); err != nil {
			return nil, err
		}
	}
	rec, err := s.inner.Get(ctx, typ, id)
	if err != nil {
		return nil, err
	}
	if gated {
		if ok, err := s.rowVisible(ctx, rec); err != nil {
			return nil, err
		} else if !ok {
			return nil, fmt.Errorf("%w: %s", substrate.ErrNotFound, vocabulary.RecordPath(typ, id))
		}
	}
	return s.cut(ctx, rec)
}

func (s *scopedDataset) List(ctx context.Context, q substrate.Query) (*substrate.Page, error) {
	if gated, err := s.anyRowGated(ctx, q.Filter.Kinds); err != nil {
		return nil, err
	} else if gated {
		return s.listRows(ctx, q)
	}
	kinds, any, err := s.narrow(ctx, q.Filter.Kinds)
	if err != nil {
		return nil, err
	}
	if any {
		kinds, any, err = s.narrowByRegistry(ctx, kinds, q.Filter.Implements, q.Filter.Purposes)
		if err != nil {
			return nil, err
		}
	}
	if !any {
		return &substrate.Page{Records: []*substrate.Record{}}, nil
	}
	q.Filter.Kinds = kinds
	if err := checkTriggerSource(kinds, q); err != nil {
		return nil, err
	}
	if err := s.checkReferenceFilters(ctx, kinds, q.Filter.Properties); err != nil {
		return nil, err
	}
	if ref := q.Filter.Referencing; ref != nil {
		for _, target := range append([]string{ref.Ref}, ref.Refs...) {
			if target == "" {
				continue
			}
			if err := s.requireRead(ctx, kindOfPath(target)); err != nil {
				return nil, err
			}
		}
	}
	page, err := s.inner.List(ctx, q)
	if err != nil {
		return nil, err
	}
	return s.cutPage(ctx, page)
}

// narrowByRegistry applies `implements` and `purposes` to an already
// narrowed kind list here, against the kinds the grant reads, so the engine
// never resolves a trait or a purpose across kinds the app cannot see: a
// trait only hidden kinds implement, a declared trait nothing implements and
// an unknown trait all answer the same empty result.
func (s *scopedDataset) narrowByRegistry(ctx context.Context, kinds []string, implements string, purposes []string) ([]string, bool, error) {
	if implements != "" {
		impl, err := s.inner.TypesImplementing(ctx, implements)
		if err != nil {
			if errors.Is(err, substrate.ErrValidation) || errors.Is(err, substrate.ErrNotFound) {
				return nil, false, nil
			}
			return nil, false, err
		}
		have := make(map[string]bool, len(impl))
		for _, k := range impl {
			have[k.Identity] = true
		}
		kinds = slices.DeleteFunc(slices.Clone(kinds), func(k string) bool { return !have[k] })
	}
	if len(purposes) > 0 {
		reg, err := s.kinds(ctx)
		if err != nil {
			return nil, false, err
		}
		kinds = slices.DeleteFunc(slices.Clone(kinds), func(k string) bool {
			return !slices.Contains(purposes, purposeOf(reg[k]))
		})
	}
	return kinds, len(kinds) > 0, nil
}

func purposeOf(k substrate.KindInfo) string {
	if p, _ := k.Definition["purpose"].(string); p != "" {
		return p
	}
	return vocabulary.PurposePrimary
}

// checkReferenceFilters refuses a property condition on a reference unless
// every record it names is of a kind the grant reads. The engine resolves a
// reference condition through the target's merge history, so a match on a
// kind the app cannot read would tell it which records were merged. The
// refusal is decided by the grant and the value the app sent, never by what
// the repository holds.
func (s *scopedDataset) checkReferenceFilters(ctx context.Context, kinds []string, props map[string]substrate.Cond) error {
	if len(props) == 0 {
		return nil
	}
	reg, err := s.kinds(ctx)
	if err != nil {
		return err
	}
	for name, c := range props {
		if strings.Contains(name, ".") {
			// A link property's value is data on the link, not a record.
			continue
		}
		for _, k := range kinds {
			declared, isRef := referenceProperty(reg[k], name)
			if !isRef {
				continue
			}
			if declared == "any" {
				declared = ""
			}
			for _, v := range condValues(c) {
				str, ok := v.(string)
				target := targetKind(str, declared)
				if !ok || target == "" {
					return fmt.Errorf("%w: an app's token filters %s only by a record of a kind it reads", substrate.ErrForbidden, name)
				}
				if read, err := s.reads(ctx, target); err != nil {
					return err
				} else if !read {
					return fmt.Errorf("%w: an app's token filters %s only by a record of a kind it reads", substrate.ErrForbidden, name)
				}
			}
		}
	}
	return nil
}

// checkReferenceValues refuses a write whose reference values name a record
// of a kind the grant does not read. The engine loads the referent to check
// it exists and to render a title from it, so a reference into a hidden kind
// would answer whether that record exists and what it is called. It walks the
// declaration the way the engine's normalizeReferencesIn does (a keyed map's
// values, a list's items, an object's declared fields, to any depth), so a
// reference nested in an object is held like a top-level one. The refusal is
// decided by the grant and the value the app sent, never by what the
// repository holds.
func (s *scopedDataset) checkReferenceValues(ctx context.Context, kind string, props map[string]any) error {
	if len(props) == 0 {
		return nil
	}
	reg, err := s.kinds(ctx)
	if err != nil {
		return err
	}
	defs, _ := reg[kind].Definition["properties"].(map[string]any)
	for name, v := range props {
		decl, _ := defs[name].(map[string]any)
		if v == nil || decl == nil || !declHoldsReference(decl) {
			continue
		}
		var sites []referenceSite
		collectReferences(decl, v, &sites)
		for _, site := range sites {
			k := targetKind(site.value, site.pin)
			read := false
			if site.value != "" && k != "" {
				if read, err = s.reads(ctx, k); err != nil {
					return err
				}
			}
			if !read {
				return fmt.Errorf("%w: an app's token writes %s only with references to kinds it reads", substrate.ErrForbidden, name)
			}
		}
	}
	return nil
}

// referenceSite is one reference value a write carries, beside the kind its
// declaration pins ("" for none). A value of a shape nobody can read is a
// site with no value, which the check refuses.
type referenceSite struct{ value, pin string }

// targetKind is the kind one reference value names: a full record path
// carries its own (`<authority>/<package>/<name>/<id>`, and an id may hold a
// slash of its own, `health/heart_rate`), anything else is a bare id under the
// declaration's pin. "" when neither says.
func targetKind(value, pin string) string {
	if kind, _, ok := vocabulary.SplitRecordPath(value); ok {
		return kind
	}
	return pin
}

// declHoldsReference reports whether an authored declaration carries a
// reference anywhere inside it, as the engine's holdsReference does.
func declHoldsReference(decl map[string]any) bool {
	if t, _ := decl["type"].(string); t == "reference" {
		return true
	}
	fields, _ := decl["fields"].(map[string]any)
	for _, f := range fields {
		if fd, ok := f.(map[string]any); ok && declHoldsReference(fd) {
			return true
		}
	}
	return false
}

// collectReferences gathers the reference sites in one value of a
// declaration, descending its container first: a keyed map's values, or a
// list's items.
func collectReferences(decl map[string]any, v any, out *[]referenceSite) {
	if v == nil {
		return
	}
	keyed, _ := decl["keyed"].(bool)
	repeated, _ := decl["repeated"].(bool)
	switch {
	case keyed:
		m, ok := v.(map[string]any)
		if !ok {
			*out = append(*out, referenceSite{})
			return
		}
		for _, e := range m {
			collectReferenceValue(decl, e, out)
		}
	case repeated:
		list, ok := v.([]any)
		if !ok {
			*out = append(*out, referenceSite{})
			return
		}
		for _, e := range list {
			collectReferenceValue(decl, e, out)
		}
	default:
		collectReferenceValue(decl, v, out)
	}
}

// collectReferenceValue handles one value: the reference itself (a path or
// an id, or the `{ref: …}` object carrying link properties), or an object
// whose declared fields are walked in turn.
func collectReferenceValue(decl map[string]any, v any, out *[]referenceSite) {
	if v == nil {
		return
	}
	if t, _ := decl["type"].(string); t == "reference" {
		pin, _ := decl["kind"].(string)
		if pin == "any" {
			pin = ""
		}
		switch x := v.(type) {
		case string:
			*out = append(*out, referenceSite{value: x, pin: pin})
		case map[string]any:
			ref, _ := x["ref"].(string)
			*out = append(*out, referenceSite{value: ref, pin: pin})
		default:
			*out = append(*out, referenceSite{})
		}
		return
	}
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	fields, _ := decl["fields"].(map[string]any)
	for fname, fv := range m {
		fd, _ := fields[fname].(map[string]any)
		if fv == nil || fd == nil || !declHoldsReference(fd) {
			continue
		}
		collectReferences(fd, fv, out)
	}
}

// referenceProperty reports whether a kind declares name as a reference, and
// the kind it points at when it pins one.
func referenceProperty(k substrate.KindInfo, name string) (string, bool) {
	props, _ := k.Definition["properties"].(map[string]any)
	p, _ := props[name].(map[string]any)
	if t, _ := p["type"].(string); t != "reference" {
		return "", false
	}
	declared, _ := p["kind"].(string)
	return declared, true
}

// condValues is every value a condition compares against.
func condValues(c substrate.Cond) []any {
	var out []any
	for _, v := range []any{c.Eq, c.Gt, c.Gte, c.Lt, c.Lte, c.Contains} {
		if v != nil {
			out = append(out, v)
		}
	}
	if c.Prefix != "" {
		out = append(out, c.Prefix)
	}
	return append(out, c.In...)
}

// cutPage cuts every record of a page and drops what the grant cannot read:
// an expanded referent, a row of another kind. The changelog head and
// generation are dropped too, since they count the whole repository's writes
// (decision 0194); an app's tail discloses them on its bookmark instead
// (0195), so it opens the watch before it reads its lists.
func (s *scopedDataset) cutPage(ctx context.Context, page *substrate.Page) (*substrate.Page, error) {
	out := *page
	out.Head, out.Generation = 0, ""
	out.Records = make([]*substrate.Record, 0, len(page.Records))
	for _, rec := range page.Records {
		if ok, err := s.visible(ctx, rec); err != nil {
			return nil, err
		} else if !ok {
			continue
		}
		c, err := s.cut(ctx, rec)
		if err != nil {
			return nil, err
		}
		out.Records = append(out.Records, c)
	}
	if page.Included != nil {
		out.Included = make(map[string]*substrate.Record, len(page.Included))
		for path, rec := range page.Included {
			if rec == nil {
				continue
			}
			if ok, err := s.visible(ctx, rec); err != nil {
				return nil, err
			} else if !ok {
				continue
			}
			c, err := s.cut(ctx, rec)
			if err != nil {
				return nil, err
			}
			out.Included[path] = c
		}
	}
	return &out, nil
}

func (s *scopedDataset) Search(ctx context.Context, in substrate.SearchInput) (substrate.SearchResult, error) {
	// The semantic arm embeds the query with the owner's provider, which is
	// the owner's money: an app searches by words alone.
	switch in.Mode {
	case "", substrate.SearchHybrid, substrate.SearchLexical:
		in.Mode = substrate.SearchLexical
	default:
		return substrate.SearchResult{}, errScope("semantic search, which spends the owner's embedding budget")
	}
	for _, k := range in.Kinds {
		if gated, err := s.rowGated(ctx, k); err != nil {
			return substrate.SearchResult{}, err
		} else if gated {
			return substrate.SearchResult{}, fmt.Errorf("%w: an app's token reads %s through a list, which its agent scopes narrow record by record",
				substrate.ErrForbidden, k)
		}
	}
	kinds, any, err := s.narrow(ctx, in.Kinds)
	if err != nil {
		return substrate.SearchResult{}, err
	}
	if any {
		if kinds, any, err = s.narrowByRegistry(ctx, kinds, "", in.Purposes); err != nil {
			return substrate.SearchResult{}, err
		}
	}
	if !any {
		return substrate.SearchResult{Hits: []substrate.Hit{}}, nil
	}
	in.Kinds = kinds
	res, err := s.inner.Search(ctx, in)
	if err != nil {
		return substrate.SearchResult{}, err
	}
	hits := make([]substrate.Hit, 0, len(res.Hits))
	for _, h := range res.Hits {
		if h.Record == nil {
			continue
		}
		if ok, err := s.reads(ctx, h.Record.Kind); err != nil {
			return substrate.SearchResult{}, err
		} else if !ok {
			continue
		}
		c, err := s.cut(ctx, h.Record)
		if err != nil {
			return substrate.SearchResult{}, err
		}
		h.Record = c
		// A score is a number computed over the whole repository; the
		// engine already weighs an app's terms alike, and the hits come in
		// rank order, so the scores themselves are left out.
		h.Lexical, h.Semantic = 0, 0
		hits = append(hits, h)
	}
	res.Hits = hits
	return res, nil
}

func (s *scopedDataset) Put(ctx context.Context, _ substrate.Actor, in substrate.PutInput) (*substrate.Record, error) {
	if err := s.requireWrite(ctx, in.Kind); err != nil {
		return nil, err
	}
	if err := s.checkReferenceValues(ctx, in.Kind, in.Properties); err != nil {
		return nil, err
	}
	rec, err := s.inner.Put(ctx, s.actor, in)
	if err != nil {
		return nil, err
	}
	return s.cut(ctx, rec)
}

func (s *scopedDataset) Patch(ctx context.Context, _ substrate.Actor, typ, id string, in substrate.PatchInput) (*substrate.Record, error) {
	// No scope writes a kind the agent verbs reach (they are core's and
	// llm's), so their few writes are the agent verbs' even where a read
	// scope reads the kind whole.
	if s.rowKind(typ) {
		return s.patchRow(ctx, typ, id, in)
	}
	if err := s.requireWrite(ctx, typ); err != nil {
		return nil, err
	}
	if err := s.checkReferenceValues(ctx, typ, in.Properties); err != nil {
		return nil, err
	}
	rec, err := s.inner.Patch(ctx, s.actor, typ, id, in)
	if err != nil {
		return nil, err
	}
	return s.cut(ctx, rec)
}

func (s *scopedDataset) Delete(ctx context.Context, _ substrate.Actor, typ, id string, in substrate.DeleteInput) (*substrate.Record, error) {
	if err := s.requireWrite(ctx, typ); err != nil {
		return nil, err
	}
	rec, err := s.inner.Delete(ctx, s.actor, typ, id, in)
	if err != nil {
		return nil, err
	}
	return s.cut(ctx, rec)
}

// --- everything else is the owner's ---

func (s *scopedDataset) Merge(context.Context, substrate.Actor, substrate.MergeInput) (*substrate.Record, error) {
	return nil, errScope("merge")
}

func (s *scopedDataset) Split(context.Context, substrate.Actor, substrate.SplitInput) (*substrate.Record, error) {
	return nil, errScope("split")
}

func (s *scopedDataset) Window(context.Context, substrate.WindowQuery) (*substrate.WindowPage, error) {
	return nil, errScope("window reads")
}

func (s *scopedDataset) Aggregate(context.Context, substrate.AggregateQuery) (*substrate.AggregateResult, error) {
	return nil, errScope("aggregate reads")
}

// Changes is the feed held to the grant, every row carrying names alone
// (heldChanges). The API opens only its live tail to an app (decision 0195).
func (s *scopedDataset) Changes(ctx context.Context, after int64, f substrate.ChangeFilter, limit int) ([]substrate.Change, error) {
	kept, _, _, err := s.heldChanges(ctx, after, f, limit)
	return kept, err
}

// Head is the tail's bookmark: a seq counts the whole repository's writes,
// which the feed's own row seqs disclose anyway (decision 0195).
func (s *scopedDataset) Head(ctx context.Context) (substrate.ChangelogHead, error) {
	return s.inner.Head(ctx)
}

func (s *scopedDataset) WatchSignal(ctx context.Context) <-chan int64 {
	return s.inner.WatchSignal(ctx)
}

func (s *scopedDataset) MintToken(context.Context, substrate.MintTokenInput) (substrate.TokenInfo, string, error) {
	return substrate.TokenInfo{}, "", errScope("minting tokens")
}

func (s *scopedDataset) Tokens(context.Context) ([]substrate.TokenInfo, error) {
	return nil, errScope("the token list")
}

func (s *scopedDataset) RunGC(context.Context) (int, error) { return 0, errScope("the collector") }

func (s *scopedDataset) ProcessEmbedQueue(context.Context, int) (int, error) {
	return 0, errScope("the embed queue")
}

func (s *scopedDataset) Reembed(context.Context, bool) (substrate.ReembedReport, error) {
	return substrate.ReembedReport{}, errScope("re-embedding")
}

func (s *scopedDataset) ChangesBefore(context.Context, int64, substrate.ChangeFilter, int) ([]substrate.Change, error) {
	return nil, errScope("the change feed")
}

func (s *scopedDataset) ChangeTriggers(context.Context, []substrate.Change) (map[int64][]substrate.ChangeTrigger, error) {
	return nil, errScope("the change feed")
}

// ApplyVocabularyDocuments admits a batch that declares only packages the
// grant's `declare:` scopes keep (decision 0203): their package, bundle and
// kind documents, under the repository's own authority, and nothing that
// runs code or reaches past the package. The engine's admission still holds
// it to the package's source (a shipped or published package is refused) and
// refuses a lossy change, which only the owner may confirm.
func (s *scopedDataset) ApplyVocabularyDocuments(ctx context.Context, _ substrate.Actor, docs []map[string]any) ([]*substrate.Record, error) {
	if err := s.declarable(docs); err != nil {
		return nil, err
	}
	return s.inner.ApplyVocabularyDocuments(ctx, s.actor, docs)
}

// keepsPackage reports whether the grant holds a declare scope.
func (s *scopedDataset) keepsPackage() bool {
	return slices.ContainsFunc(s.grant.Scopes(), func(raw string) bool { return strings.HasPrefix(raw, string(scope.Declare)+":") })
}

// declarable is the declare scope's rule over one batch.
func (s *scopedDataset) declarable(docs []map[string]any) error {
	if len(docs) == 0 {
		return errScope("the vocabulary")
	}
	home := s.inner.Repository().Authority
	for _, doc := range docs {
		kind, _ := doc["kind"].(string)
		meta, _ := doc["metadata"].(map[string]any)
		id, _ := meta["id"].(string)
		var pkg string
		switch kind {
		case vocabulary.CoreKind(vocabulary.DocPackage), vocabulary.CoreKind(vocabulary.DocBundle):
			pkg = id
		case vocabulary.CoreKind(vocabulary.DocKind):
			pkg = vocabulary.KindPackage(id)
		default:
			return fmt.Errorf("%w: an app's token declares packages, bundles and kinds, not %s", substrate.ErrForbidden, kind)
		}
		if !s.grant.DeclaresIn(pkg, home) {
			return fmt.Errorf("%w: an app's token does not declare %s/%s: it holds no declare scope for the package", substrate.ErrForbidden, kind, id)
		}
	}
	return nil
}

func (s *scopedDataset) PlanVocabularyApply(context.Context, substrate.Actor, []map[string]any) (substrate.VocabularyPlan, error) {
	return substrate.VocabularyPlan{}, errScope("the vocabulary")
}

func (s *scopedDataset) PlanVocabularyApplyWith(context.Context, substrate.Actor, []map[string]any, substrate.VocabularyApply) (substrate.VocabularyPlan, error) {
	return substrate.VocabularyPlan{}, errScope("the vocabulary")
}

func (s *scopedDataset) ApplyVocabularyDocumentsWith(context.Context, substrate.Actor, []map[string]any, substrate.VocabularyApply) ([]*substrate.Record, error) {
	return nil, errScope("the vocabulary")
}

func (s *scopedDataset) PlanShippedUpgrade(context.Context) ([]substrate.ShippedUpgrade, error) {
	return nil, errScope("the vocabulary")
}

func (s *scopedDataset) TriggerStatuses(context.Context) ([]substrate.TriggerStatus, error) {
	return nil, errScope("triggers")
}

func (s *scopedDataset) SyncStatuses(context.Context) ([]substrate.SyncStatus, error) {
	return nil, errScope("sync status")
}

func (s *scopedDataset) ReplayTrigger(context.Context, string, int64) error {
	return errScope("triggers")
}

func (s *scopedDataset) RunTrigger(context.Context, string, string, string) (int, error) {
	return 0, errScope("triggers")
}

func (s *scopedDataset) WakeTrigger(context.Context, string) (int, error) {
	return 0, errScope("triggers")
}

// TriggerFailures lists the parked deliveries of the one trigger a
// `webhook:` scope names, whoever sent them (docs/plans/app-setup.md).
func (s *scopedDataset) TriggerFailures(ctx context.Context, id string) ([]substrate.TriggerFailure, error) {
	if !s.grant.Delivers(id) {
		return nil, errScope("triggers")
	}
	return s.inner.TriggerFailures(ctx, id)
}

func (s *scopedDataset) RetryTriggerFailure(context.Context, string, int64) (int, error) {
	return 0, errScope("triggers")
}

func (s *scopedDataset) ForgetTriggerFailure(context.Context, string, int64) error {
	return errScope("triggers")
}

func (s *scopedDataset) CallFunction(context.Context, substrate.Actor, string, any) (any, int, error) {
	return nil, 0, errScope("functions")
}

func (s *scopedDataset) ProcessTriggers(context.Context) (int, error) {
	return 0, errScope("triggers")
}

func (s *scopedDataset) CallAgent(context.Context, string, any) (*substrate.AgentResult, error) {
	return nil, errScope("agents")
}

// ChatAgent opens or continues a chat with an agent the grant talks to. The
// user message is the app's, under its own actor; the turn runs the agent
// under the agent's permissions, so the scoped mark stops here and the
// agent's writes meet the gates an owner's chat meets (decision 0195).
func (s *scopedDataset) ChatAgent(ctx context.Context, _ substrate.Actor, name string, in substrate.ChatInput, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	if !s.grant.Talks() {
		return nil, errScope("agents")
	}
	if authority, _, _ := vocabulary.SplitKindRef(name); authority == "" {
		return nil, fmt.Errorf("%w: an app's token names an agent in full, <authority>/<package>/<name>, not %q",
			substrate.ErrValidation, name)
	}
	if !s.grant.TalksTo(name) {
		return nil, fmt.Errorf("%w: agent %s", substrate.ErrNotFound, name)
	}
	if in.Thread != "" {
		if err := s.requireThread(ctx, in.Thread); err != nil {
			return nil, err
		}
	}
	return s.inner.ChatAgent(substrate.WithoutScopes(ctx), s.actor, name, in, emit)
}

func (s *scopedDataset) AttachAgentTurn(ctx context.Context, threadID string, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	if err := s.requireThread(ctx, threadID); err != nil {
		return nil, err
	}
	return s.inner.AttachAgentTurn(ctx, threadID, emit)
}

func (s *scopedDataset) StopAgentTurn(ctx context.Context, _ substrate.Actor, threadID string) (*substrate.AgentResult, error) {
	if err := s.requireThread(ctx, threadID); err != nil {
		return nil, err
	}
	return s.inner.StopAgentTurn(ctx, s.actor, threadID)
}

func (s *scopedDataset) SweepResolutions(context.Context) (int, error) {
	return 0, errScope("interactions")
}

func (s *scopedDataset) BundleStatuses(context.Context) ([]substrate.BundleStatus, error) {
	return nil, errScope("bundles")
}

func (s *scopedDataset) BundleStatusesUncounted(context.Context) ([]substrate.BundleStatus, error) {
	return nil, errScope("bundles")
}

func (s *scopedDataset) BundleStatus(context.Context, string) (substrate.BundleStatus, error) {
	return substrate.BundleStatus{}, errScope("bundles")
}

func (s *scopedDataset) BundlePackage(context.Context, string) (string, error) {
	return "", errScope("bundles")
}

func (s *scopedDataset) DisableBundle(context.Context, string) error { return errScope("bundles") }

func (s *scopedDataset) EnableBundle(context.Context, string) error { return errScope("bundles") }

func (s *scopedDataset) BindBundleInput(context.Context, string, string, string) error {
	return errScope("bundles")
}

func (s *scopedDataset) UninstallBundle(context.Context, string) error { return errScope("bundles") }

func (s *scopedDataset) PurgeBundle(context.Context, string) (int, error) {
	return 0, errScope("bundles")
}

func (s *scopedDataset) StartOAuth(context.Context, substrate.Actor, string) (string, error) {
	return "", errScope("connecting a provider")
}

func (s *scopedDataset) InstallBundleClosure(context.Context, substrate.Actor, []map[string]any, []substrate.PutInput, substrate.BundleInstall) ([]*substrate.Record, error) {
	return nil, errScope("the catalog")
}

func (s *scopedDataset) PlanBundleUpgrade(context.Context, []map[string]any) (substrate.BundleUpgrade, error) {
	return substrate.BundleUpgrade{}, errScope("the catalog")
}

func (s *scopedDataset) RefreshOAuthTokens(context.Context) (int, error) {
	return 0, errScope("provider connections")
}

func (s *scopedDataset) ProcessOAuthFinalizers(context.Context) (int, error) {
	return 0, errScope("provider connections")
}

// PutBlob stores bytes for a grant that writes a kind declaring a blob-ref
// property, which is where such a write finds them (docs/plans/app-setup.md).
// The answer is the digest and size of what the app sent and the name and
// media type it sent: a blob that already held those bytes keeps what its
// first upload said, and that is not the app's to read.
func (s *scopedDataset) PutBlob(ctx context.Context, _ substrate.Actor, up substrate.BlobUpload, data []byte, digest string) (*substrate.BlobInfo, error) {
	if err := s.admitBlobs(ctx); err != nil {
		return nil, err
	}
	info, err := s.inner.PutBlob(ctx, s.actor, up, data, digest)
	if err != nil {
		return nil, err
	}
	return &substrate.BlobInfo{
		Digest: info.Digest, Size: int64(len(data)),
		Name: up.Name, MediaType: up.MediaType, Status: info.Status,
	}, nil
}

// admitBlobs is the blob rule: the grant writes a kind that declares a
// blob-ref property. The PUT asks it before it reads a body, so a token the
// rule refuses never holds an upload slot.
func (s *scopedDataset) admitBlobs(ctx context.Context) error {
	reg, err := s.kinds(ctx)
	if err != nil {
		return err
	}
	for id, k := range reg {
		if s.grant.Writes(id, internalKind(k)) && declaresBlobRef(k) {
			return nil
		}
	}
	return errScope("blobs")
}

// declaresBlobRef reports whether a kind declares a blob-ref property. A
// blob-ref is never a field of an object, so the top level is all of them.
func declaresBlobRef(k substrate.KindInfo) bool {
	props, _ := k.Definition["properties"].(map[string]any)
	for _, raw := range props {
		p, _ := raw.(map[string]any)
		if t, _ := p["type"].(string); t == string(vocabulary.DatatypeBlobRef) {
			return true
		}
	}
	return false
}

func (s *scopedDataset) GetBlob(context.Context, string) (*substrate.BlobInfo, []byte, error) {
	return nil, nil, errScope("blobs")
}

func (s *scopedDataset) StatBlob(context.Context, string) (*substrate.BlobInfo, error) {
	return nil, errScope("blobs")
}

func (s *scopedDataset) Export(context.Context) (substrate.Export, error) {
	return nil, errScope("the export")
}
