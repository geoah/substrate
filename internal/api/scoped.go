package api

import (
	"context"
	"errors"
	"fmt"
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
// Patch, Delete), the unit table and conversions. A kind the grant cannot
// read is answered exactly as a kind the repository never declared, so a
// refusal does not tell an app which kinds exist. Every record handed back,
// a write's answer included, is cut to what the grant reads. Everything else
// is ErrForbidden.
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
	return &cp, nil
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
		if r, ok := reg[k.Identity]; ok && s.grant.Reads(k.Identity, internalKind(r)) {
			out = append(out, k)
		}
	}
	return out, nil
}

func (s *scopedDataset) KindByRef(ctx context.Context, ref string) (substrate.KindInfo, error) {
	if err := s.requireRead(ctx, ref); err != nil {
		return substrate.KindInfo{}, err
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
		if ok, err := s.reads(ctx, k.Identity); err != nil {
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
	if err := s.requireRead(ctx, typ); err != nil {
		return nil, err
	}
	rec, err := s.inner.Get(ctx, typ, id)
	if err != nil {
		return nil, err
	}
	return s.cut(ctx, rec)
}

func (s *scopedDataset) List(ctx context.Context, q substrate.Query) (*substrate.Page, error) {
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
			for _, v := range condValues(c) {
				str, ok := v.(string)
				target := declared
				if ok && strings.Contains(str, "/") {
					target = kindOfPath(str)
				}
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
// would answer whether that record exists and what it is called. The refusal
// is decided by the grant and the value the app sent, never by what the
// repository holds.
func (s *scopedDataset) checkReferenceValues(ctx context.Context, kind string, props map[string]any) error {
	if len(props) == 0 {
		return nil
	}
	reg, err := s.kinds(ctx)
	if err != nil {
		return err
	}
	for name, v := range props {
		declared, isRef := referenceProperty(reg[kind], name)
		if !isRef || v == nil {
			continue
		}
		for _, target := range referenceTargets(v) {
			k := declared
			if strings.Contains(target, "/") {
				k = kindOfPath(target)
			}
			read := false
			if k != "" {
				if read, err = s.reads(ctx, k); err != nil {
					return err
				}
			}
			if !read {
				return fmt.Errorf("%w: an app's token writes %s only as a reference to a kind it reads", substrate.ErrForbidden, name)
			}
		}
	}
	return nil
}

// referenceTargets is every record a reference value names: a path or an id,
// a `{ref: …}` object carrying link properties, or a list of either. A value
// of any other shape names a target nobody can check, and is answered as one
// the grant does not read.
func referenceTargets(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case map[string]any:
		ref, _ := x["ref"].(string)
		return []string{ref}
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, referenceTargets(e)...)
		}
		return out
	}
	return []string{""}
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
// and the tail they hand off to is not open to an app.
func (s *scopedDataset) cutPage(ctx context.Context, page *substrate.Page) (*substrate.Page, error) {
	out := *page
	out.Head, out.Generation = 0, ""
	out.Records = make([]*substrate.Record, 0, len(page.Records))
	for _, rec := range page.Records {
		if ok, err := s.reads(ctx, rec.Kind); err != nil {
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
			if ok, err := s.reads(ctx, kindOfPath(path)); err != nil {
				return nil, err
			} else if !ok || rec == nil {
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

func (s *scopedDataset) Changes(context.Context, int64, substrate.ChangeFilter, int) ([]substrate.Change, error) {
	return nil, errScope("the change feed")
}

func (s *scopedDataset) Head(context.Context) (substrate.ChangelogHead, error) {
	return substrate.ChangelogHead{}, errScope("the change feed")
}

// WatchSignal answers a closed channel: the tail is refused before it is
// reached, and a closed channel ends any loop that waits on it.
func (s *scopedDataset) WatchSignal(context.Context) <-chan int64 {
	ch := make(chan int64)
	close(ch)
	return ch
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

func (s *scopedDataset) ApplyVocabularyDocuments(context.Context, substrate.Actor, []map[string]any) ([]*substrate.Record, error) {
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

func (s *scopedDataset) TriggerFailures(context.Context, string) ([]substrate.TriggerFailure, error) {
	return nil, errScope("triggers")
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

func (s *scopedDataset) ChatAgent(context.Context, substrate.Actor, string, string, string, func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	return nil, errScope("agents")
}

func (s *scopedDataset) AttachAgentTurn(context.Context, string, func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	return nil, errScope("agents")
}

func (s *scopedDataset) StopAgentTurn(context.Context, substrate.Actor, string) (*substrate.AgentResult, error) {
	return nil, errScope("agents")
}

func (s *scopedDataset) SweepResolutions(context.Context) (int, error) {
	return 0, errScope("interactions")
}

func (s *scopedDataset) BundleStatuses(context.Context) ([]substrate.BundleStatus, error) {
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

func (s *scopedDataset) PutBlob(context.Context, substrate.Actor, substrate.BlobUpload, []byte, string) (*substrate.BlobInfo, error) {
	return nil, errScope("blobs")
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
