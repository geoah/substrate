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
	"time"

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
// Window, Aggregate, Kinds, KindByRef, TypesImplementing), writing the kinds
// it writes (Put, Patch, Delete), the unit table and conversions. Under
// `agents:` and `decide:` (decision 0195) it also reads the rows those
// reach, chats with and stops the matching agents, makes the few writes
// those verbs carry and reads the change feed, its history included, held
// to the same rows (scopedagents.go). Under `webhook:` it lists that
// trigger's parked deliveries; a grant that writes a kind declaring a
// blob-ref property stores blobs (docs/plans/app-setup.md), one that reads
// such a kind reads the blobs its records name (scopedreads.go), and under
// `call:` it runs the functions whose reach its scopes cover (scopedcall.go,
// decision 0219). A kind the grant cannot read is answered exactly as a kind
// the repository never declared, so a refusal does not tell an app which
// kinds exist. Every record handed back, a write's answer included, is cut
// to what the grant reads, and a trigger never carries its webhook key.
// Everything else is ErrForbidden.
type scopedDataset struct {
	inner substrate.Dataset
	grant viewReach
	// actor is the app's own derived actor, `app:<host>`: every write goes
	// out under it whatever actor the handler passes. A person's view has
	// none and passes on the actor it is handed, the person's or, beneath an
	// app's view, the app's.
	actor substrate.Actor
	// person is whether this is a person's view (newPersonDataset): their
	// own sign-ins are theirs to list, mint and revoke whatever their roles.
	person bool

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
	// agentCovered is talksTo's answer per agent: whether the grant's reads
	// cover everything the agent reads (decision 0214).
	agentCovered map[string]bool
}

var _ substrate.Dataset = (*scopedDataset)(nil)

func newScopedDataset(inner substrate.Dataset, grant scope.Grant, actor substrate.Actor) *scopedDataset {
	return &scopedDataset{inner: inner, grant: appReach{grant}, actor: actor}
}

// newPersonDataset is the view a person who is not an admin reaches the
// repository through: held to the rules of the roles they hold (decision
// 0276), as an app's token is held to its scopes.
func newPersonDataset(inner substrate.Dataset, roles scope.Roles) *scopedDataset {
	return &scopedDataset{inner: inner, grant: personReach{roles: roles}, person: true}
}

// declarations is what the view reads a declaration through to hold a
// call or a chat to what the code reaches: the engine under every view. A
// person's view below an app's reads no declaration, and the check reads
// what the code declares, not what the caller reads.
func (s *scopedDataset) declarations() substrate.Dataset {
	if in, ok := s.inner.(*scopedDataset); ok {
		return in.declarations()
	}
	return s.inner
}

// writer is the actor a write goes out under: the app's own, or for a
// person's view the one it was handed.
func (s *scopedDataset) writer(handed substrate.Actor) substrate.Actor {
	if s.person {
		return handed
	}
	return s.actor
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

// KindRetentions is the owner's: a scoped app's token reads no kind's
// retention, as it reads no declaration it was not granted. A full-access
// app's token is not scoped (decision 0204) and never reaches this.
func (s *scopedDataset) KindRetentions(context.Context) ([]substrate.KindRetention, error) {
	return nil, errScope("the retention of the repository's kinds")
}

func internalKind(k substrate.KindInfo) bool {
	p, _ := k.Definition["purpose"].(string)
	return p == vocabulary.PurposeInternal
}

// scopeKind is a kind as a grant reads it (scope.KindOf): as the registry
// declares it now, whether internal and the traits it binds, or, for a kind
// the registry does not hold yet, internal and binding nothing, so only its
// exact scope reaches it.
func scopeKind(kind string, registry map[string]substrate.KindInfo) scope.Kind {
	k, held := registry[kind]
	if !held {
		return scope.Kind{Ref: kind, Internal: true}
	}
	return scopeKindOf(kind, k)
}

// local folds the repository's full name into `~` in a reference the app
// sent, so the grant, which names the repository's own kinds under `~`, is
// asked about the kind the engine would resolve (decision record 0259).
func (s *scopedDataset) local(ref string) string {
	return vocabulary.LocalizeRef(ref, s.inner.Repository().LocalNames()...)
}

// reads reports whether the grant reads kind; a kind the registry does not
// hold is never readable.
func (s *scopedDataset) reads(ctx context.Context, kind string) (bool, error) {
	reg, err := s.kinds(ctx)
	if err != nil {
		return false, err
	}
	kind = s.local(kind)
	k, ok := reg[kind]
	return ok && s.grant.Reads(scopeKindOf(kind, k)), nil
}

func (s *scopedDataset) writes(ctx context.Context, kind string) (bool, error) {
	reg, err := s.kinds(ctx)
	if err != nil {
		return false, err
	}
	kind = s.local(kind)
	k, ok := reg[kind]
	return ok && s.grant.Writes(scopeKindOf(kind, k)), nil
}

// readable is the sorted list of every kind the grant reads today.
func (s *scopedDataset) readable(ctx context.Context) ([]string, error) {
	reg, err := s.kinds(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for id, k := range reg {
		if s.grant.Reads(scopeKindOf(id, k)) {
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
		return ok && s.grant.Reads(scopeKindOf(kind, k))
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
	if len(rec.Marks) > 0 {
		cp.Marks = substrate.MarksWithinKinds(rec.Marks, func(path string) bool {
			return allowKind(kindOfPath(path))
		})
	}
	if rec.Kind == kindTrigger {
		withoutWebhookKeys(&cp)
	}
	if !s.grant.Reads(scope.Kind{Ref: kindBlob, Internal: true}) {
		withoutBlobManifests(&cp, reg[rec.Kind])
	}
	withoutHeldTo(&cp)
	return &cp, nil
}

// heldKinds are a run's own records, each of which names in `heldTo` whose
// rules hold its work: the persons it acts for and the tokens their apps
// reached it with (decision 0276). That is the engine's bookkeeping of other
// people's tokens, which a view held to a person's roles or an app's grant
// never shows, whatever it reads of the record.
var heldKinds = map[string]bool{
	llmPackage + "/thread": true, llmPackage + "/message": true,
	llmPackage + "/interaction": true, llmPackage + "/approval": true,
}

// withoutHeldTo drops `heldTo`, and its mark, from a run's record. The maps
// it changes are copies.
func withoutHeldTo(rec *substrate.Record) {
	if !heldKinds[rec.Kind] {
		return
	}
	if _, ok := rec.Properties["heldTo"]; ok {
		props := make(map[string]any, len(rec.Properties))
		for k, v := range rec.Properties {
			if k != "heldTo" {
				props[k] = v
			}
		}
		rec.Properties = props
	}
	if _, ok := rec.Marks["heldTo"]; ok {
		marks := make(map[string]substrate.PropertyMark, len(rec.Marks))
		for k, v := range rec.Marks {
			if k != "heldTo" {
				marks[k] = v
			}
		}
		rec.Marks = marks
	}
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
	m, ok := rec.Marks[triggerSource]
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
		rec.Marks = maps.Clone(rec.Marks)
		rec.Marks[triggerSource] = m
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
		// A person's refusal is a rule's: another person, or this one with
		// another role, may make the same write (`not_permitted`).
		if s.person {
			return fmt.Errorf("%w: %w: no rule of the roles you hold writes %s", substrate.ErrForbidden, substrate.ErrNotPermitted, kind)
		}
		return fmt.Errorf("%w: an app's token does not write %s", substrate.ErrForbidden, kind)
	}
	return nil
}

// narrow holds a kind list to the grant: an empty list becomes every
// readable kind, a named kind the grant cannot read is unknown. It answers
// false when nothing is left, which the caller answers with an empty result
// rather than passing on, because no kinds means every kind below here. The
// kinds it answers are as the registry keys them, `~` for an own kind, so the
// checks after it read the declarations they mean.
func (s *scopedDataset) narrow(ctx context.Context, kinds []string) ([]string, bool, error) {
	if len(kinds) == 0 {
		all, err := s.readable(ctx)
		if err != nil {
			return nil, false, err
		}
		return all, len(all) > 0, nil
	}
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		if err := s.requireRead(ctx, k); err != nil {
			return nil, false, err
		}
		out = append(out, s.local(k))
	}
	return out, true, nil
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
		if r, ok := reg[k.Identity]; ok && (s.grant.Reads(scopeKindOf(k.Identity, r)) || s.rowKind(k.Identity)) {
			out = append(out, k)
		}
	}
	return out, nil
}

func (s *scopedDataset) KindByRef(ctx context.Context, ref string) (substrate.KindInfo, error) {
	ref = s.local(ref)
	// The package kind is known to a grant that could name a package, so the
	// one row Get serves it resolves; Get decides which row.
	if ref == kindPackageRow && s.grant.NamesAnyPackage() {
		return s.inner.KindByRef(ctx, ref)
	}
	// Every repository declares the kind of kind declarations, so naming it
	// tells a token nothing; Get decides which of its rows it reads.
	if ref == kindDeclarationRow {
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
	if err := s.requireUnitGraph(ctx); err != nil {
		return substrate.UnitTable{}, err
	}
	return s.inner.Units(ctx)
}

func (s *scopedDataset) ConvertUnit(ctx context.Context, in substrate.UnitConvert) (substrate.UnitConversion, error) {
	if err := s.requireUnitGraph(ctx); err != nil {
		return substrate.UnitConversion{}, err
	}
	return s.inner.ConvertUnit(ctx, in)
}

// requireUnitGraph holds a person to the units and conversions their rules
// read: the graph is those records. An app's token converts with it
// whatever it reads, as before roles.
func (s *scopedDataset) requireUnitGraph(ctx context.Context) error {
	if !s.person {
		return nil
	}
	for _, kind := range []string{vocabulary.CoreKind("unit"), vocabulary.CoreKind("conversion")} {
		if err := s.requireRead(ctx, kind); err != nil {
			return err
		}
	}
	return nil
}

func (s *scopedDataset) Get(ctx context.Context, typ, id string) (*substrate.Record, error) {
	typ = s.local(typ)
	// The row of a package the grant's scopes reach into says which version
	// the repository holds, which an app reads to know whether its build may
	// write (scope.NamesPackage); every other declaration row stays the
	// owner's.
	if typ == kindPackageRow && s.grant.NamesPackage(s.local(id)) {
		return s.inner.Get(ctx, typ, id)
	}
	// The declaration row of a kind the grant reads says what the token
	// already sees of the kind's records: its label, its properties and their
	// order. An app over kinds it did not ship (a group kind it was approved
	// to read) learns them here (docs/plans/tasks-sources.md).
	if typ == kindDeclarationRow {
		if ok, err := s.reads(ctx, id); err != nil {
			return nil, err
		} else if ok {
			return s.inner.Get(ctx, typ, id)
		}
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
	// A cursor is bound to the filter the client sent; what the grant
	// narrows it to below can change between pages with no change of the
	// client's (substrate.WithAskedFilter).
	if _, ok := substrate.AskedFilterFrom(ctx); !ok {
		ctx = substrate.WithAskedFilter(ctx, q.Filter)
	}
	if s.person && slices.Equal(q.Filter.Kinds, []string{kindDeclarationRow}) {
		return s.listDeclarations(ctx, q)
	}
	if gated, err := s.anyRowGated(ctx, q.Filter.Kinds); err != nil {
		return nil, err
	} else if gated {
		return s.listRows(ctx, q)
	}
	q, any, err := s.holdQuery(ctx, q)
	if err != nil {
		return nil, err
	}
	if !any {
		return noneReached(q)
	}
	page, err := s.inner.List(ctx, q)
	if err != nil {
		return nil, err
	}
	return s.cutPage(ctx, page)
}

// listDeclarations is a person's list of kind declarations (the console's
// and `substratectl kinds`' first read): the declarations of the kinds their
// rules read, each whole, as the one-declaration read serves it. It pages as
// the engine pages every declaration, so a page may come back short of its
// size, and never empty while more follow: the CLI ends its walk at an
// empty page. The size of the whole set is not said, since it counts what
// the person does not read. Every declaration, read whole, stays an admin's.
func (s *scopedDataset) listDeclarations(ctx context.Context, q substrate.Query) (*substrate.Page, error) {
	if q.Count {
		return nil, fmt.Errorf("%w: a person's list of kind declarations carries no count: it holds the kinds their roles read", substrate.ErrValidation)
	}
	for {
		page, err := s.inner.List(ctx, q)
		if err != nil {
			return nil, err
		}
		out := *page
		out.Head, out.Generation = 0, ""
		out.Records = make([]*substrate.Record, 0, len(page.Records))
		for _, rec := range page.Records {
			ok, err := s.reads(ctx, rec.ID)
			if err != nil {
				return nil, err
			}
			if ok {
				out.Records = append(out.Records, rec)
			}
		}
		if out.Included, err = s.cutIncluded(ctx, page.Included); err != nil {
			return nil, err
		}
		if len(out.Records) > 0 || page.Cursor == "" || q.Offset > 0 {
			return &out, nil
		}
		q.After = page.Cursor
	}
}

// noneReached answers a request the grant now resolves to no kind or no row
// at all. Fresh, it is an empty page. A continuation is still a valid request
// whose walk cannot go on, because what its cursor stood among is gone: it
// lists again, as every other such continuation does (410).
func noneReached(q substrate.Query) (*substrate.Page, error) {
	if q.After != "" {
		return nil, fmt.Errorf("%w: the request reaches nothing it can read now, which it did when its cursor was minted; list again", substrate.ErrStaleHistory)
	}
	return &substrate.Page{Records: []*substrate.Record{}}, nil
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
			k := s.local(targetKind(site.value, site.pin))
			read := false
			if site.value != "" && k != "" {
				if read, err = s.reads(ctx, k); err != nil {
					return err
				}
			}
			if read {
				if read, err = s.pinReadable(ctx, k, site.pin, site.trait); err != nil {
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

// referenceSite is one reference value a write carries, beside the kind or
// the trait its declaration pins ("" for none). A value of a shape nobody can
// read is a site with no value, which the check refuses.
type referenceSite struct{ value, pin, trait string }

// pinReadable reports whether the grant reads every kind a reference of kind
// k may land on under its pin. A path the pin does not admit is not refused
// by the engine when a record mapping leads from k to a kind the pin admits:
// the engine stores the mapped subject instead and titles the record from
// it, so the grant must read the pinned kind, or for a trait pin every kind
// implementing the trait, as well as k.
func (s *scopedDataset) pinReadable(ctx context.Context, k, pin, trait string) (bool, error) {
	switch {
	case trait != "":
		impl, err := s.implementers(ctx, trait)
		if err != nil {
			return false, err
		}
		if slices.Contains(impl, k) {
			return true, nil
		}
		for _, i := range impl {
			if read, err := s.reads(ctx, i); err != nil || !read {
				return false, err
			}
		}
		return true, nil
	case pin != "" && pin != k:
		return s.reads(ctx, pin)
	}
	return true, nil
}

// implementers lists the kinds implementing trait; an unknown trait has
// none.
func (s *scopedDataset) implementers(ctx context.Context, trait string) ([]string, error) {
	impl, err := s.inner.TypesImplementing(ctx, trait)
	if err != nil {
		if errors.Is(err, substrate.ErrValidation) || errors.Is(err, substrate.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(impl))
	for _, k := range impl {
		out = append(out, k.Identity)
	}
	return out, nil
}

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
		trait, _ := decl["trait"].(string)
		switch x := v.(type) {
		case string:
			*out = append(*out, referenceSite{value: x, pin: pin, trait: trait})
		case map[string]any:
			ref, _ := x["ref"].(string)
			*out = append(*out, referenceSite{value: ref, pin: pin, trait: trait})
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
	var err error
	if out.Records, err = s.cutRecords(ctx, page.Records); err != nil {
		return nil, err
	}
	if out.Included, err = s.cutIncluded(ctx, page.Included); err != nil {
		return nil, err
	}
	return &out, nil
}

// Search is every mode of the search over the kinds the grant reads, the
// semantic arm included (decision 0219): it embeds the query with the
// owner's provider, which the approval page says the scope spends. The embed
// queue's pending count is a number over the whole repository, so it is left
// out as the head is.
func (s *scopedDataset) Search(ctx context.Context, in substrate.SearchInput) (substrate.SearchResult, error) {
	if err := s.refuseRowGated(ctx, in.Kinds); err != nil {
		return substrate.SearchResult{}, err
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
	res.Pending = 0
	return res, nil
}

func (s *scopedDataset) Put(ctx context.Context, actor substrate.Actor, in substrate.PutInput) (*substrate.Record, error) {
	// Every check below reads the registry, which keys an own kind by `~`.
	in.Kind = s.local(in.Kind)
	if err := s.requireWrite(ctx, in.Kind); err != nil {
		return nil, err
	}
	if err := s.checkReferenceValues(ctx, in.Kind, in.Properties); err != nil {
		return nil, err
	}
	rec, err := s.inner.Put(ctx, s.writer(actor), in)
	if err != nil {
		return nil, err
	}
	return s.cut(ctx, rec)
}

func (s *scopedDataset) Patch(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.PatchInput) (*substrate.Record, error) {
	typ = s.local(typ)
	// No scope writes a kind the agent verbs reach (they are core's and
	// llm's), so an app's few writes of them are the agent verbs' even where
	// a read scope reads the kind whole. A person's role may write one whole,
	// and then writes it as any other.
	if s.rowKind(typ) {
		whole, err := s.writes(ctx, typ)
		if err != nil {
			return nil, err
		}
		if !whole {
			return s.patchRow(ctx, actor, typ, id, in)
		}
	}
	if err := s.requireWrite(ctx, typ); err != nil {
		return nil, err
	}
	if err := s.checkReferenceValues(ctx, typ, in.Properties); err != nil {
		return nil, err
	}
	rec, err := s.inner.Patch(ctx, s.writer(actor), typ, id, in)
	if err != nil {
		return nil, err
	}
	return s.cut(ctx, rec)
}

func (s *scopedDataset) Delete(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.DeleteInput) (*substrate.Record, error) {
	typ = s.local(typ)
	// A person revokes their own sign-ins whatever their roles; the engine
	// tells their own from another's (decision 0275).
	if s.person && typ == vocabulary.CoreKind("token") {
		return s.inner.Delete(ctx, s.writer(actor), typ, id, in)
	}
	if err := s.requireWrite(ctx, typ); err != nil {
		return nil, err
	}
	rec, err := s.inner.Delete(ctx, s.writer(actor), typ, id, in)
	if err != nil {
		return nil, err
	}
	return s.cut(ctx, rec)
}

// --- everything else is an admin's ---

// Pin and Release are writes on the record: a grant or a person's roles
// that write its kind may pin and release its properties, and the answer is
// cut to the view.
func (s *scopedDataset) Pin(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.MarkInput) (*substrate.Record, error) {
	if err := s.requireWrite(ctx, typ); err != nil {
		return nil, err
	}
	rec, err := s.inner.Pin(ctx, s.writer(actor), typ, id, in)
	if err != nil {
		return nil, err
	}
	return s.cut(ctx, rec)
}

func (s *scopedDataset) Release(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.MarkInput) (*substrate.Record, error) {
	if err := s.requireWrite(ctx, typ); err != nil {
		return nil, err
	}
	rec, err := s.inner.Release(ctx, s.writer(actor), typ, id, in)
	if err != nil {
		return nil, err
	}
	return s.cut(ctx, rec)
}

func (s *scopedDataset) Merge(context.Context, substrate.Actor, substrate.MergeInput) (*substrate.Record, error) {
	return nil, errScope("merge")
}

func (s *scopedDataset) Split(context.Context, substrate.Actor, substrate.SplitInput) (*substrate.Record, error) {
	return nil, errScope("split")
}

// Changes is the feed held to the grant, every row carrying names alone
// (heldChanges).
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

func (s *scopedDataset) MintToken(ctx context.Context, in substrate.MintTokenInput) (substrate.TokenInfo, string, error) {
	if s.person {
		return s.inner.MintToken(ctx, in)
	}
	return substrate.TokenInfo{}, "", errScope("minting tokens")
}

// Tokens are, for a person's view, their own sign-ins alone.
func (s *scopedDataset) Tokens(ctx context.Context) ([]substrate.TokenInfo, error) {
	if !s.person {
		return nil, errScope("the token list")
	}
	all, err := s.inner.Tokens(ctx)
	if err != nil {
		return nil, err
	}
	identity := substrate.IdentityFrom(ctx)
	out := make([]substrate.TokenInfo, 0, len(all))
	for _, tok := range all {
		if identity != "" && tok.Identity == identity {
			out = append(out, tok)
		}
	}
	return out, nil
}

// PersonRoles are the roles of the person the request acts for.
func (s *scopedDataset) PersonRoles(ctx context.Context) ([]substrate.RoleRules, error) {
	return s.inner.PersonRoles(ctx)
}

func (s *scopedDataset) RunGC(context.Context) (int, error) { return 0, errScope("the collector") }

func (s *scopedDataset) ProcessEmbedQueue(context.Context, int) (int, error) {
	return 0, errScope("the embed queue")
}

func (s *scopedDataset) Reembed(context.Context, bool) (substrate.ReembedReport, error) {
	return substrate.ReembedReport{}, errScope("re-embedding")
}

// ChangeTriggers is each trigger's stance on a row, which is the owner's
// automation: an app's rows carry none.
func (s *scopedDataset) ChangeTriggers(context.Context, []substrate.Change) (map[int64][]substrate.ChangeTrigger, error) {
	return map[int64][]substrate.ChangeTrigger{}, nil
}

// ApplyVocabularyDocuments is refused: no scoped token applies vocabulary
// (decision 0214). An app's own kinds arrive as a package its approval
// installs, from the binary's catalog or its publisher's authority document.
func (s *scopedDataset) ApplyVocabularyDocuments(context.Context, substrate.Actor, []map[string]any) ([]*substrate.Record, error) {
	return nil, errScope("the vocabulary")
}

func (s *scopedDataset) TakeOverPackage(context.Context, substrate.Actor, string, string) (*substrate.Record, error) {
	return nil, errScope("the vocabulary")
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
func (s *scopedDataset) ChatAgent(ctx context.Context, actor substrate.Actor, name string, in substrate.ChatInput, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	if !s.grant.Talks() {
		return nil, errScope("agents")
	}
	if authority, _, _ := vocabulary.SplitKindRef(name); authority == "" {
		return nil, fmt.Errorf("%w: an app's token names an agent in full, <authority>/<package>/<name>, not %q",
			substrate.ErrValidation, name)
	}
	if ok, err := s.talksTo(ctx, name); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("%w: agent %s", substrate.ErrNotFound, name)
	}
	if in.Thread != "" {
		if err := s.requireThread(ctx, in.Thread); err != nil {
			return nil, err
		}
	}
	// The engine reads each context record under the agent's grant and
	// stores it on a row this token reads, so the token must read each one
	// itself, and a refusal is the scoped read's own answer, not an
	// existence oracle.
	if len(in.Context) > substrate.MaxChatContextRecords {
		return nil, fmt.Errorf("%w: context names %d records, and a turn is about at most %d",
			substrate.ErrValidation, len(in.Context), substrate.MaxChatContextRecords)
	}
	for _, path := range in.Context {
		kind, id, ok := vocabulary.SplitRecordPath(path)
		if !ok {
			return nil, fmt.Errorf("%w: context %q is not a record path, <authority>/<package>/<kind>/<id>", substrate.ErrValidation, path)
		}
		if _, err := s.Get(ctx, kind, id); err != nil {
			return nil, err
		}
	}
	return s.inner.ChatAgent(substrate.WithoutScopes(ctx), s.writer(actor), name, in, emit)
}

func (s *scopedDataset) AttachAgentTurn(ctx context.Context, threadID string, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	if err := s.requireThread(ctx, threadID); err != nil {
		return nil, err
	}
	return s.inner.AttachAgentTurn(ctx, threadID, emit)
}

func (s *scopedDataset) StopAgentTurn(ctx context.Context, actor substrate.Actor, threadID string) (*substrate.AgentResult, error) {
	if err := s.requireThread(ctx, threadID); err != nil {
		return nil, err
	}
	return s.inner.StopAgentTurn(ctx, s.writer(actor), threadID)
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
func (s *scopedDataset) PutBlob(ctx context.Context, actor substrate.Actor, up substrate.BlobUpload, data []byte, digest string) (*substrate.BlobInfo, error) {
	if err := s.admitBlobs(ctx); err != nil {
		return nil, err
	}
	info, err := s.inner.PutBlob(ctx, s.writer(actor), up, data, digest)
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
		if s.grant.Writes(scopeKindOf(id, k)) && declaresBlobRef(k) {
			return nil
		}
	}
	return errScope("blobs")
}

func (s *scopedDataset) Export(context.Context) (substrate.Export, error) {
	return nil, errScope("the export")
}

// Publishing is the owner's decision (decision record 0289): an app's token
// neither plans nor runs it. What the repository publishes is public, so the
// releases read through.
func (s *scopedDataset) PlanPublish(context.Context, substrate.Actor, substrate.PublishInput) (substrate.PublishPlan, error) {
	return substrate.PublishPlan{}, errScope("publishing a package")
}

func (s *scopedDataset) Publish(context.Context, substrate.Actor, substrate.PublishInput) (substrate.PublishPlan, error) {
	return substrate.PublishPlan{}, errScope("publishing a package")
}

func (s *scopedDataset) ReleasePackage(context.Context, substrate.Actor, string) (substrate.Release, error) {
	return substrate.Release{}, errScope("releasing a package")
}

func (s *scopedDataset) Releases(ctx context.Context, authority string) ([]substrate.Release, error) {
	return s.inner.Releases(ctx, authority)
}

func (s *scopedDataset) ReleaseClosure(ctx context.Context, pkg string, version int64) ([]byte, error) {
	return s.inner.ReleaseClosure(ctx, pkg, version)
}

func (s *scopedDataset) DomainState(context.Context) (substrate.DomainState, error) {
	return substrate.DomainState{}, errScope("domain verification")
}

func (s *scopedDataset) StartDomainVerification(context.Context) (substrate.DomainState, error) {
	return substrate.DomainState{}, errScope("domain verification")
}

func (s *scopedDataset) MarkDomainVerified(context.Context, string) (time.Time, error) {
	return time.Time{}, errScope("domain verification")
}

// RecordAppBuild writes only the calling token's own build facts, which the
// engine reads off the request's principal.
func (s *scopedDataset) RecordAppBuild(ctx context.Context, packages map[string]int64, softwareVersion string) error {
	return s.inner.RecordAppBuild(ctx, packages, softwareVersion)
}

func (s *scopedDataset) CompleteAppAddon(context.Context, string, string) error {
	return errScope("completing add-on approvals")
}

// OperatorDefaults is the owner's: which slots the operator pays for, and
// today's allowance, say nothing about the kinds a grant reads.
func (s *scopedDataset) OperatorDefaults(context.Context) (substrate.OperatorDefaults, error) {
	return substrate.OperatorDefaults{}, errScope("the operator defaults read")
}

// ModelOffers answers from the provider rows and the model records together,
// so a grant reads it only where it reads both kinds; elsewhere it answers
// none, as a List of the rows it cannot read would. App approval plans through
// the owner's dataset, so this is a guard rather than a path a plan takes.
func (s *scopedDataset) ModelOffers(ctx context.Context) (map[string][]string, error) {
	for _, kind := range []string{vocabulary.LLMKind("provider"), vocabulary.LLMKind("model")} {
		ok, err := s.reads(ctx, kind)
		if err != nil {
			return nil, err
		}
		if !ok {
			return map[string][]string{}, nil
		}
	}
	return s.inner.ModelOffers(ctx)
}
