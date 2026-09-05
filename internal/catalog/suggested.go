package catalog

// The SUGGESTED MAPPINGS half of a catalog entry (decision record 0049): the
// mappings a SAMPLE declares onto its own kinds from a PROVIDER's mirrors, and
// what each one is doing in one repository.
//
// The state is the MAPPING RECORD's, not the provider's. A provider install
// lands mirrors and nothing else, so "GitHub is installed" and "GitHub
// identities reach my people" are two different answers, and a reader told the
// first would sit in front of an empty person list wondering. The four states
// are substrate.SuggestedMapping's; this file decides them and prunes the
// closure to match.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// coreMappingKind is the declaration record a landed mapping IS: the state
// read is an ordinary record read, because that record is the only thing that
// says a projection is running.
const coreMappingKind = vocabulary.PackageCore + "/" + vocabulary.DocRecordMapping

// suggestedView is WHICH SPELLING a caller is asking about, because a suggested
// mapping has two identities and they are not interchangeable:
// `samples.substrate.reamde.dev/people/githubuserperson` as the tree ships it,
// and `<home>/people/githubuserperson` as an import rehomes it.
//
// A door must report the one it WRITES, or it names a declaration that does not
// exist: Install applies the shipped documents verbatim, Import applies the
// rehomed ones. A read has no such commitment and asks for both, preferring
// whichever this repository actually holds.
type suggestedView int

const (
	// viewHeld is the read's: prefer the rehomed identity (what an import
	// would land), but report the shipped one where that is what is here.
	viewHeld suggestedView = iota
	// viewShipped is Catalog.Install's: the closure lands verbatim.
	viewShipped
	// viewRehomed is Catalog.Import's: the closure lands under this
	// repository's own authority.
	viewRehomed
)

// suggestedResolution is one mapping's answer: what to report, the id the
// document carries in the tree (what a prune matches on), and whether the
// document belongs in the batch a door applies.
type suggestedResolution struct {
	wire    substrate.SuggestedMapping
	shipped string
	// keep is false for a mapping the door drops: waiting (its provider is
	// absent) or blocked (its provider is here but the mapping does not fit
	// the version installed). Either would be refused by admission and cost
	// the reader the whole import.
	keep bool
}

// SuggestedMappingStates reports each suggested mapping's state as a READ of
// this repository: what the catalog listing shows before anybody presses
// anything. The ids and targets are rehomed onto the repository's own
// authority, because that is where the record each state is read from lives.
//
// A read that fails for any reason OTHER than a kind's or a record's absence
// is a fault and is returned: reporting a database error as "waiting" would
// tell the reader to install a provider they already have.
func (b *Bundle) SuggestedMappingStates(ctx context.Context, ds substrate.Dataset) ([]substrate.SuggestedMapping, error) {
	resolved, err := b.resolveSuggested(ctx, ds, viewHeld)
	if err != nil {
		return nil, err
	}
	out := make([]substrate.SuggestedMapping, 0, len(resolved))
	for _, r := range resolved {
		out = append(out, r.wire)
	}
	return out, nil
}

// admitted is the closure BOTH doors apply and the report they answer with.
//
// The two come from ONE decision, deliberately. The documents are the closure
// minus every mapping this repository cannot admit, and the report says
// `landed` for exactly the mappings left in it, because a door reports only
// after its apply COMMITTED. Reading the states back afterwards instead would
// let a provider installed in between make the door claim a mapping landed
// that was never in the batch. The opposite race is admission's to refuse: a
// provider uninstalled between this decision and the commit fails the whole
// apply, and then there is no report at all.
func (b *Bundle) admitted(ctx context.Context, ds substrate.Dataset, view suggestedView) ([]map[string]any, []substrate.SuggestedMapping, error) {
	resolved, err := b.resolveSuggested(ctx, ds, view)
	if err != nil {
		return nil, nil, err
	}
	drop := map[string]bool{}
	report := make([]substrate.SuggestedMapping, 0, len(resolved))
	for _, r := range resolved {
		if r.keep {
			// In the batch, and the batch commits whole or not at all.
			r.wire.State = substrate.SuggestedMappingLanded
			r.wire.Problems = nil
		} else {
			drop[r.shipped] = true
		}
		report = append(report, r.wire)
	}
	return vocabulary.WithoutMappings(b.vocabularyDocs, drop), report, nil
}

// resolveSuggested answers every suggested mapping this closure ships.
func (b *Bundle) resolveSuggested(ctx context.Context, ds substrate.Dataset, view suggestedView) ([]suggestedResolution, error) {
	if len(b.suggested) == 0 {
		return nil, nil
	}
	home := ds.Repository().Authority
	out := make([]suggestedResolution, 0, len(b.suggested))
	for _, sm := range b.suggested {
		r, err := b.resolveOne(ctx, ds, sm, home, view)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// resolveOne decides one mapping's state, in the order a reader needs:
//
//  1. the source kind is absent, so its provider is: WAITING, and nothing
//     further can be said about a mapping whose source does not exist;
//  2. the mapping DECLARATION is here, whichever id it landed under: LANDED,
//     which is the only state in which the projection actually runs;
//  3. it does not fit the source kind this repository holds: BLOCKED, naming
//     the problems, because keeping it would refuse the whole closure;
//  4. otherwise READY: importing the sample is what lands it.
func (b *Bundle) resolveOne(ctx context.Context, ds substrate.Dataset, sm vocabulary.SuggestedMapping, home string, view suggestedView) (suggestedResolution, error) {
	ids := b.spellings(sm.ID, home, view)
	tos := b.spellings(sm.To, home, view)
	wire := substrate.SuggestedMapping{
		ID:      ids[0],
		From:    sm.From,
		To:      tos[0],
		Package: sm.Package,
	}
	from, err := ds.KindByRef(ctx, sm.From)
	switch {
	case errors.Is(err, substrate.ErrNotFound):
		wire.State = substrate.SuggestedMappingWaiting
		return suggestedResolution{wire: wire, shipped: sm.ID}, nil
	case err != nil:
		return suggestedResolution{}, err
	}
	// The TARGET first, because it settles the spelling both fields report:
	// where this repository holds the subject kind, the mapping's declaration
	// is under that same authority.
	to, err := b.heldKind(ctx, ds, tos)
	if err != nil {
		return suggestedResolution{}, err
	}
	if to != nil {
		wire.To = to.Identity
	}
	held, err := b.heldMapping(ctx, ds, ids)
	if err != nil {
		return suggestedResolution{}, err
	}
	if held != "" {
		wire.ID = held
		wire.State = substrate.SuggestedMappingLanded
		return suggestedResolution{wire: wire, shipped: sm.ID, keep: true}, nil
	}
	if problems := fitProblems(sm, from, to); len(problems) > 0 {
		wire.State = substrate.SuggestedMappingBlocked
		wire.Problems = problems
		return suggestedResolution{wire: wire, shipped: sm.ID}, nil
	}
	wire.State = substrate.SuggestedMappingReady
	return suggestedResolution{wire: wire, shipped: sm.ID, keep: true}, nil
}

// heldMapping is the id the mapping declaration has HERE, out of the
// candidates the view asked for, or "" when this repository holds none.
func (b *Bundle) heldMapping(ctx context.Context, ds substrate.Dataset, ids []string) (string, error) {
	for _, id := range ids {
		_, err := ds.Get(ctx, coreMappingKind, id)
		switch {
		case err == nil:
			return id, nil
		case errors.Is(err, substrate.ErrNotFound):
			continue
		default:
			return "", err
		}
	}
	return "", nil
}

// heldKind resolves the mapping's TARGET kind out of the same candidates, or
// nil where this repository has not taken the sample yet.
func (b *Bundle) heldKind(ctx context.Context, ds substrate.Dataset, refs []string) (*substrate.KindInfo, error) {
	for _, ref := range refs {
		info, err := ds.KindByRef(ctx, ref)
		switch {
		case err == nil:
			return &info, nil
		case errors.Is(err, substrate.ErrNotFound):
			continue
		default:
			return nil, err
		}
	}
	return nil, nil
}

// spellings is the identities this view is about, in preference order. A door
// gets exactly ONE, the spelling it applies, so its report can never name a
// declaration it did not write; a read gets both, rehomed first, because an
// import is the sample door and a verbatim install is the other way a sample
// can be here.
func (b *Bundle) spellings(shipped, home string, view suggestedView) []string {
	landed := b.landedSpelling(shipped, home)
	switch {
	case view == viewShipped:
		return []string{shipped}
	case view == viewRehomed:
		return []string{landed}
	case landed == shipped:
		return []string{shipped}
	default:
		return []string{landed, shipped}
	}
}

// landedSpelling rewrites one of this closure's own identities onto the
// authority it lands under: a SAMPLE's placeholder becomes the repository's
// own authority, exactly as the import's rehome does. An identity outside this
// closure's authority (a provider mirror in `from`) is returned untouched.
func (b *Bundle) landedSpelling(id, home string) string {
	if b.Tier != substrate.TierSample || home == "" || b.Authority == "" {
		return id
	}
	rest, ok := strings.CutPrefix(id, b.Authority+"/")
	if !ok {
		return id
	}
	return home + "/" + rest
}

// fitProblems reports why a suggested mapping would NOT resolve against the
// source kind this repository holds, empty when nothing says it would not.
//
// It is the door's own check and a NECESSARY condition rather than the whole
// of admission: the loader type-checks every path against both declared kinds
// and stays the authority. What this catches is the case install order
// creates, a provider OLDER than the sample was written against. Linear at
// version 11 declares `issue` without the `task` subject slot, so the mapping
// the tasks sample ships names a property that is not there; keeping it in the
// batch would refuse the whole import with a message about a mapping the
// reader never wrote.
func fitProblems(sm vocabulary.SuggestedMapping, from substrate.KindInfo, to *substrate.KindInfo) []string {
	var problems []string
	errf := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	props := declaredProps(from.Definition)
	subject := asMap(props[sm.Property])
	switch {
	case len(subject) == 0:
		errf("%s declares no property %q, the subject reference this mapping fills", sm.From, sm.Property)
	case subject["type"] != string(vocabulary.DatatypeReference):
		errf("%s.%s is %v, not a reference: a subject is a `type: reference` property", sm.From, sm.Property, subject["type"])
	case subject["subject"] != true:
		errf("%s.%s is not marked `subject: true`, so no mapping may fill it", sm.From, sm.Property)
	}
	for i, mv := range mslice(sm.Data, "match") {
		if p := pathProblem(sm.From, props, mstr(asMap(mv), "from")); p != "" {
			errf("match[%d].from: %s", i, p)
		}
	}
	for _, name := range sortedKeys(mmap(sm.Data, "map")) {
		rule := asMap(mmap(sm.Data, "map")[name])
		if p := pathProblem(sm.From, props, mstr(rule, "path")); p != "" {
			errf("map.%s: %s", name, p)
		}
		if to == nil {
			continue // the sample is not here yet, and its own kinds are its business
		}
		if _, ok := declaredProps(to.Definition)[name]; !ok && !columnProp(name) {
			errf("map.%s: %s declares no property %q", name, to.Identity, name)
		}
	}
	return problems
}

// pathProblem answers why a match or map path does not resolve against a
// kind's declared properties, empty where it does.
func pathProblem(kindRef string, props map[string]any, raw string) string {
	p, err := vocabulary.ParsePath(raw)
	if err != nil {
		return err.Error()
	}
	decl := asMap(props[p.Prop])
	if len(decl) == 0 {
		if columnProp(p.Prop) {
			return "" // title and the temporals are storage, not declarations
		}
		return fmt.Sprintf("%s declares no property %q", kindRef, p.Prop)
	}
	if p.Field == "" {
		return ""
	}
	if _, ok := asMap(decl["fields"])[p.Field]; !ok {
		return fmt.Sprintf("%s.%s declares no field %q", kindRef, p.Prop, p.Field)
	}
	return ""
}

// columnProp names the properties a record carries that no declaration
// mentions: `title`, and the three temporals.
//
// It is DELIBERATELY LOOSER than the loader, which admits a temporal only
// where the kind binds it as a hot property (vocabulary's own columnProp asks
// UsesHot). This one accepts all three unconditionally, and the direction is
// the safe one: the worst it does is call a mapping ready that the loader then
// refuses on the path, which is the ordinary refusal a reader would have got
// anyway. Tightening it would mean the opposite, blocking a mapping that would
// have admitted.
func columnProp(name string) bool {
	switch name {
	case "title", "at", "endsAt", "dueAt":
		return true
	}
	return false
}

// sortedKeys is a map's keys in name order, so a blocked mapping's problems
// read the same way twice.
func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// declaredProps is a kind declaration's `properties` map.
func declaredProps(definition map[string]any) map[string]any {
	return mmap(definition, "properties")
}

func asMap(v any) map[string]any {
	out, _ := v.(map[string]any)
	return out
}
