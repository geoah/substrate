package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/reach"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The rows an app's `agents:` and `decide:` scopes reach (decision 0195).
// Those scopes name agents, not kinds, so what they make readable is decided
// record by record: an agent row whose id matches, a thread whose agent
// matches, that thread's messages, interactions and approvals (decision
// 0254), and the change requests such a thread proposed. A kind a read or write scope reaches whole is
// answered exactly as before; one the agent verbs alone reach is ROW-GATED.
const (
	kindAgent       = corePackage + "/agent"
	kindThread      = llmPackage + "/thread"
	kindMessage     = llmPackage + "/message"
	kindInteraction = llmPackage + "/interaction"
	kindApproval    = vocabulary.KindLLMApproval
	kindRequest     = vocabulary.KindRecordPatchRequest
)

// agentListPage is the page size the grant's agent list is read in.
const agentListPage = 500

// visibleCacheMax bounds the per-request memo of rows found readable: a watch
// lives as long as its client, and the memo must not grow with it.
const visibleCacheMax = 4096

// rowKind reports whether the grant's agent verbs reach records of kind.
func (s *scopedDataset) rowKind(kind string) bool {
	switch kind {
	case kindAgent, kindThread, kindMessage, kindInteraction, kindApproval:
		return s.grant.Talks()
	case kindRequest:
		return s.grant.DecidesAny()
	}
	return false
}

// rowGated reports whether kind is reached row by row: the agent verbs reach
// it and no read or write scope reaches it whole.
func (s *scopedDataset) rowGated(ctx context.Context, kind string) (bool, error) {
	if !s.rowKind(kind) {
		return false, nil
	}
	reg, err := s.kinds(ctx)
	if err != nil {
		return false, err
	}
	k, ok := reg[kind]
	return ok && !s.grant.Reads(scopeKindOf(kind, k)), nil
}

// reaches reports whether the grant reaches any record of kind, whole or row
// by row: the kinds the kind reads (KindByRef, Kinds) answer for.
func (s *scopedDataset) reaches(ctx context.Context, kind string) (bool, error) {
	kind = s.local(kind)
	if ok, err := s.reads(ctx, kind); err != nil || ok {
		return ok, err
	}
	return s.rowGated(ctx, kind)
}

// visible reports whether the grant reads rec: its kind whole, or the row
// through an agent verb.
func (s *scopedDataset) visible(ctx context.Context, rec *substrate.Record) (bool, error) {
	if rec == nil {
		return false, nil
	}
	if ok, err := s.reads(ctx, rec.Kind); err != nil || ok {
		return ok, err
	}
	gated, err := s.rowGated(ctx, rec.Kind)
	if err != nil || !gated {
		return false, err
	}
	return s.rowVisible(ctx, rec)
}

// rowVisible is the row rule of a row-gated kind.
func (s *scopedDataset) rowVisible(ctx context.Context, rec *substrate.Record) (bool, error) {
	switch rec.Kind {
	case kindAgent:
		return s.talksTo(ctx, rec.ID)
	case kindThread:
		return s.talksTo(ctx, referencedID(rec.Properties["agent"], kindAgent))
	case kindMessage, kindInteraction, kindApproval:
		agent, err := s.agentOfThread(ctx, referencedID(rec.Properties["thread"], kindThread))
		if err != nil {
			return false, err
		}
		return s.talksTo(ctx, agent)
	case kindRequest:
		agent, err := s.agentOfThread(ctx, referencedID(rec.Properties["thread"], kindThread))
		return err == nil && s.grant.Decides(agent), err
	}
	return false, nil
}

// threadReached reports whether a row of kind on thread is one the grant
// reads: a message or interaction of a thread whose agent it talks to, a
// change request of one whose agent it decides for.
func (s *scopedDataset) threadReached(ctx context.Context, kind, thread string) (bool, error) {
	agent, err := s.agentOfThread(ctx, thread)
	if err != nil {
		return false, err
	}
	if kind == kindRequest {
		return s.grant.Decides(agent), nil
	}
	return s.talksTo(ctx, agent)
}

// agentOfThread is the id of the agent a thread ran, "" for a thread that does
// not resolve. A thread's agent is fixed at its creation, so the answer is
// kept for the request; a miss is not, since the thread may land later in a
// watch.
func (s *scopedDataset) agentOfThread(ctx context.Context, thread string) (string, error) {
	if thread == "" {
		return "", nil
	}
	s.mu.Lock()
	agent, ok := s.threadAgents[thread]
	s.mu.Unlock()
	if ok {
		return agent, nil
	}
	rec, err := s.rowOf(ctx, kindThread, thread)
	if err != nil || rec == nil {
		return "", err
	}
	agent = referencedID(rec.Properties["agent"], kindAgent)
	s.mu.Lock()
	if s.threadAgents == nil || len(s.threadAgents) >= visibleCacheMax {
		s.threadAgents = map[string]string{}
	}
	s.threadAgents[thread] = agent
	s.mu.Unlock()
	return agent, nil
}

// rowOf reads one record live or tombstoned, nil when there is none: a feed
// row of a deleted record is still decided by what the record held.
func (s *scopedDataset) rowOf(ctx context.Context, kind, id string) (*substrate.Record, error) {
	rec, err := s.inner.Get(ctx, kind, id)
	if err == nil {
		return rec, nil
	}
	if !errors.Is(err, substrate.ErrNotFound) {
		return nil, err
	}
	deleted := true
	page, err := s.inner.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{kind}, IDs: []string{id}, Deleted: &deleted},
		First:  1,
	})
	if err != nil || len(page.Records) == 0 {
		return nil, err
	}
	return page.Records[0], nil
}

// talkedAgents is the id of every agent the grant talks to today, sorted.
func (s *scopedDataset) talkedAgents(ctx context.Context) ([]string, error) {
	var out []string
	q := substrate.Query{Filter: substrate.Filter{Kinds: []string{kindAgent}}, First: agentListPage}
	for {
		page, err := s.inner.List(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, rec := range page.Records {
			if rec.Kind != kindAgent {
				continue
			}
			if ok, err := s.talksTo(ctx, rec.ID); err != nil {
				return nil, err
			} else if ok {
				out = append(out, rec.ID)
			}
		}
		if page.Cursor == "" || len(page.Records) == 0 {
			break
		}
		q.After = page.Cursor
	}
	sort.Strings(out)
	return out, nil
}

// referenceValues reads a reference value's targets: the stored `{ref: …}`
// object, the written path or bare id, or a list of either. Anything else
// reads as one empty target, so a caller expecting exactly one names none.
func referenceValues(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case map[string]any:
		ref, _ := x["ref"].(string)
		return []string{ref}
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, referenceValues(e)...)
		}
		return out
	}
	return []string{""}
}

// referencedID is the id a reference value names on a kind, "" when it
// names none or one of another kind. It reads the stored object and the
// written shorthands alike: a path, or a bare id the declaration completes.
func referencedID(v any, kind string) string {
	targets := referenceValues(v)
	if len(targets) != 1 || targets[0] == "" {
		return ""
	}
	if k, id, ok := vocabulary.SplitRecordPath(targets[0]); ok {
		if k != kind {
			return ""
		}
		return id
	}
	return targets[0]
}

// condRecordIDs reads a condition on a reference property as the ids it
// names: eq or in alone, each a bare id or a path of kind.
func condRecordIDs(name string, c substrate.Cond, kind string) ([]string, error) {
	refuse := fmt.Errorf("%w: an app's token filters %s by eq or in, naming records of %s", substrate.ErrForbidden, name, kind)
	if c.Prefix != "" || c.Gt != nil || c.Gte != nil || c.Lt != nil || c.Lte != nil ||
		c.Contains != nil || c.Exists != nil || c.Match != "" {
		return nil, refuse
	}
	values := c.In
	if c.Eq != nil {
		values = append([]any{c.Eq}, values...)
	}
	if len(values) == 0 {
		return nil, refuse
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		str, ok := v.(string)
		if !ok || str == "" {
			return nil, refuse
		}
		id := referencedID(str, kind)
		if id == "" {
			return nil, refuse
		}
		out = append(out, id)
	}
	return out, nil
}

// anyRowGated reports whether a list names a row-gated kind, which it must
// name alone: the condition that narrows it is that kind's own.
func (s *scopedDataset) anyRowGated(ctx context.Context, kinds []string) (bool, error) {
	for _, k := range kinds {
		gated, err := s.rowGated(ctx, k)
		if err != nil {
			return false, err
		}
		if gated {
			if len(kinds) != 1 {
				return false, fmt.Errorf("%w: an app's token lists %s on its own: its agent scopes reach it record by record",
					substrate.ErrForbidden, k)
			}
			return true, nil
		}
	}
	return false, nil
}

// listRows is the list of one row-gated kind: the query narrowed to the rows
// the grant reads, by a condition the engine applies, and every row checked
// again on the way out. A condition the app writes on the narrowing property
// must name only records the grant reads, and naming one it does not is
// answered 404, as the record itself would be.
func (s *scopedDataset) listRows(ctx context.Context, q substrate.Query) (*substrate.Page, error) {
	kind := q.Filter.Kinds[0]
	if q.Filter.Referencing != nil {
		return nil, fmt.Errorf("%w: an app's token reads %s without a referencing filter", substrate.ErrForbidden, kind)
	}
	if q.Filter.Implements != "" || len(q.Filter.Purposes) > 0 {
		_, any, err := s.narrowByRegistry(ctx, q.Filter.Kinds, q.Filter.Implements, q.Filter.Purposes)
		if err != nil {
			return nil, err
		}
		if !any {
			return noneReached(q)
		}
	}
	props := make(map[string]substrate.Cond, len(q.Filter.Properties)+1)
	for name, c := range q.Filter.Properties {
		props[name] = c
	}
	// narrowed names the property this kind is narrowed by, which the
	// generic reference check below leaves to the rule here. Every branch
	// narrows the engine's query to rows the grant reads, so a page, its
	// cursor and a count pass over nothing else.
	narrowed := ""
	switch kind {
	case kindAgent:
		talked, err := s.talkedAgents(ctx)
		if err != nil {
			return nil, err
		}
		ids := talked
		if len(q.Filter.IDs) > 0 {
			ids = slices.DeleteFunc(slices.Clone(q.Filter.IDs), func(id string) bool { return !slices.Contains(talked, id) })
		}
		if len(ids) == 0 {
			return noneReached(q)
		}
		q.Filter.IDs = ids
	case kindThread:
		narrowed = "agent"
		if c, named := props["agent"]; named {
			agents, err := condRecordIDs("agent", c, kindAgent)
			if err != nil {
				return nil, err
			}
			for _, a := range agents {
				if ok, err := s.talksTo(ctx, a); err != nil {
					return nil, err
				} else if !ok {
					return nil, fmt.Errorf("%w: agent %s", substrate.ErrNotFound, a)
				}
			}
			break
		}
		talked, err := s.talkedAgents(ctx)
		if err != nil {
			return nil, err
		}
		if len(talked) == 0 {
			return noneReached(q)
		}
		in := make([]any, len(talked))
		for i, a := range talked {
			in[i] = vocabulary.RecordPath(kindAgent, a)
		}
		props["agent"] = substrate.Cond{In: in}
	case kindMessage, kindInteraction, kindApproval, kindRequest:
		narrowed = "thread"
		c, named := props["thread"]
		if !named {
			// Unnamed threads would be filtered here, after the engine paged
			// and wrote its cursor, and the cursor carries the last scanned
			// row's sort values: another agent's message read through a
			// cursor. The threads are named, so the engine filters before it
			// pages, unless a read scope reaches the whole kind.
			whole, err := s.reads(ctx, kind)
			if err != nil {
				return nil, err
			}
			if !whole {
				return nil, fmt.Errorf("%w: an app's token lists %s under filter.properties.thread (eq or in), naming the threads it reads",
					substrate.ErrForbidden, kind)
			}
		} else {
			threads, err := condRecordIDs("thread", c, kindThread)
			if err != nil {
				return nil, err
			}
			for _, th := range threads {
				ok, err := s.threadReached(ctx, kind, th)
				if err != nil {
					return nil, err
				}
				if !ok {
					return nil, fmt.Errorf("%w: thread %s", substrate.ErrNotFound, th)
				}
			}
		}
	}
	rest := make(map[string]substrate.Cond, len(props))
	for name, c := range props {
		if name != narrowed {
			rest[name] = c
		}
	}
	if err := s.checkReferenceFilters(ctx, q.Filter.Kinds, rest); err != nil {
		return nil, err
	}
	if len(props) > 0 {
		q.Filter.Properties = props
	}
	page, err := s.inner.List(ctx, q)
	if err != nil {
		return nil, err
	}
	return s.cutPage(ctx, page)
}

// requireRow reads one record of a kind the agent verbs reach for one of
// their writes: a row they do not reach is missing, or forbidden where a read
// scope reads it anyway.
func (s *scopedDataset) requireRow(ctx context.Context, kind, id string) error {
	readable, err := s.reads(ctx, kind)
	if err != nil {
		return err
	}
	rec, err := s.inner.Get(ctx, kind, id)
	if err != nil {
		if !readable && errors.Is(err, substrate.ErrNotFound) {
			return fmt.Errorf("%w: %s", substrate.ErrNotFound, vocabulary.RecordPath(kind, id))
		}
		return err
	}
	if ok, err := s.rowVisible(ctx, rec); err != nil {
		return err
	} else if !ok {
		if readable {
			return fmt.Errorf("%w: an app's token writes %s only of the agents its scopes name", substrate.ErrForbidden, vocabulary.RecordPath(kind, id))
		}
		return fmt.Errorf("%w: %s", substrate.ErrNotFound, vocabulary.RecordPath(kind, id))
	}
	return nil
}

// rowPatchable names what an app's patch of a row-gated kind may carry: a
// thread's rename, an interaction's answer or dismissal, a change request's
// decision. Nothing else of those kinds is written through the agent verbs.
var rowPatchable = map[string][]string{
	kindThread:      {"subject"},
	kindInteraction: {"state", "answers"},
	kindApproval:    {"state"},
	kindRequest:     {"decision", "adjustedDiff"},
}

// patchRow is an app's patch of a row-gated kind.
func (s *scopedDataset) patchRow(ctx context.Context, actor substrate.Actor, kind, id string, in substrate.PatchInput) (*substrate.Record, error) {
	// rowKind holds a kind only under the verb that reaches it, so a thread
	// here is under agents and a request under decide.
	allowed, ok := rowPatchable[kind]
	if !ok {
		return nil, fmt.Errorf("%w: an app's token does not write %s", substrate.ErrForbidden, kind)
	}
	if len(in.Labels) > 0 || len(in.AddFinalizers) > 0 || len(in.RemoveFinalizers) > 0 ||
		(len(in.Annotations) > 0 && kind != kindRequest) {
		return nil, fmt.Errorf("%w: an app's token changes only %s of a %s", substrate.ErrForbidden, strings.Join(allowed, " and "), kind)
	}
	for name := range in.Properties {
		if !slices.Contains(allowed, name) {
			return nil, fmt.Errorf("%w: an app's token changes only %s of a %s, not %s",
				substrate.ErrForbidden, strings.Join(allowed, " and "), kind, name)
		}
	}
	if err := s.requireRow(ctx, kind, id); err != nil {
		return nil, err
	}
	if kind == kindRequest {
		// The engine opens its accept gates for this one request alone: the
		// grant was held to it above (decision 0195).
		ctx = substrate.WithDecision(ctx, vocabulary.RecordPath(kind, id))
	}
	rec, err := s.inner.Patch(ctx, s.writer(actor), kind, id, in)
	if err != nil {
		return nil, err
	}
	return s.cut(ctx, rec)
}

// requireThread holds a thread-addressed call (attach, stop, continuing a
// chat) to the threads the grant talks to.
func (s *scopedDataset) requireThread(ctx context.Context, thread string) error {
	if !s.grant.Talks() {
		return errScope("agents")
	}
	// A read scope over threads reads every thread, but talks only to the
	// agents its agents scopes name.
	agent, err := s.agentOfThread(ctx, thread)
	if err != nil {
		return err
	}
	if ok, err := s.talksTo(ctx, agent); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: thread %s", substrate.ErrNotFound, thread)
	}
	return nil
}

// --- the change feed ---

// changeFilter holds a feed filter to the grant: no text search and no
// values for an app's token, which read across kinds and into payloads, and
// the kinds narrowed to the ones it reaches. A person searches and reads
// values of the history of what their rules read whole, and a search is
// narrowed to those kinds, since it matches a row's payload. ok is false
// when it reaches none.
func (s *scopedDataset) changeFilter(ctx context.Context, f substrate.ChangeFilter) (substrate.ChangeFilter, bool, error) {
	if f.Q != "" && !s.person {
		return f, false, errScope("a text search of the change feed, which reads across kinds")
	}
	// A person reads the values of the history of what their rules read,
	// as an admin does (heldRow); an app's token reads names alone.
	if f.Values && !s.person {
		return f, false, errScope("the change feed's values")
	}
	reaches := s.reaches
	if f.Q != "" {
		reaches = s.reads
	}
	if len(f.Kinds) > 0 {
		kinds := make([]string, 0, len(f.Kinds))
		for _, k := range f.Kinds {
			ok, err := reaches(ctx, k)
			if err != nil {
				return f, false, err
			}
			if !ok {
				return f, false, errUnknownKind(k)
			}
			kinds = append(kinds, s.local(k))
		}
		f.Kinds = kinds
		return f, true, nil
	}
	reg, err := s.kinds(ctx)
	if err != nil {
		return f, false, err
	}
	var kinds []string
	for id, k := range reg {
		if s.grant.Reads(scopeKindOf(id, k)) || (f.Q == "" && s.rowKind(id)) {
			kinds = append(kinds, id)
		}
	}
	sort.Strings(kinds)
	f.Kinds = kinds
	return f, len(kinds) > 0, nil
}

// heldChanges is one batch of the feed as the grant reads it: the rows of
// records it reads, each carrying names alone. scanned is how many rows the
// batch read and last the highest seq among them, so a tail advances past
// the rows it may not show instead of reading them again.
func (s *scopedDataset) heldChanges(ctx context.Context, after int64, f substrate.ChangeFilter, limit int) (kept []substrate.Change, scanned int, last int64, err error) {
	f, ok, err := s.changeFilter(ctx, f)
	if err != nil || !ok {
		return []substrate.Change{}, 0, after, err
	}
	batch, err := s.inner.Changes(ctx, after, f, limit)
	if err != nil {
		return nil, 0, after, err
	}
	kept = make([]substrate.Change, 0, len(batch))
	last = after
	for _, c := range batch {
		last = c.Seq
		row, show, err := s.heldRow(ctx, c, f.Values)
		if err != nil {
			return nil, 0, after, err
		}
		if show {
			kept = append(kept, row)
		}
	}
	return kept, len(batch), last, nil
}

// heldRow is one feed row as the grant reads it: whether its record is one
// the grant reads, and the row carrying names alone, its affected records
// cut to the ones the grant reads. A person who asked for values reads the
// values of a row whose kind their rules read whole.
func (s *scopedDataset) heldRow(ctx context.Context, c substrate.Change, values bool) (substrate.Change, bool, error) {
	show, err := s.recordVisible(ctx, c.Kind, c.RecordID)
	if err != nil || !show {
		return substrate.Change{}, false, err
	}
	// The token is the owner's to read: an app's grant would learn other
	// apps' token ids from it.
	row := substrate.Change{Seq: c.Seq, TS: c.TS, Actor: c.Actor, OnBehalfOf: c.OnBehalfOf, Op: c.Op, RecordID: c.RecordID, Kind: c.Kind}
	if values && s.person {
		whole, err := s.reads(ctx, c.Kind)
		if err != nil {
			return substrate.Change{}, false, err
		}
		if whole {
			row.Payload = withoutHeldToEffects(c.Payload)
		}
	}
	for _, a := range c.Affected {
		show, err := s.recordVisible(ctx, a.Kind, a.ID)
		if err != nil {
			return substrate.Change{}, false, err
		}
		if show {
			row.Affected = append(row.Affected, substrate.AffectedRecord{Kind: a.Kind, ID: a.ID, Version: a.Version, Deleted: a.Deleted})
		}
	}
	return row, true, nil
}

// ChangesBefore is the feed's history as the grant reads it (decision 0219):
// the newest limit rows below before of records it reads, each carrying
// names alone. Rows of a row-gated kind are decided after the engine paged,
// so the batch is read again below the rows it held back until limit rows
// are kept or the bottom is reached: a short page then means the bottom,
// which is what the history route reads it as.
func (s *scopedDataset) ChangesBefore(ctx context.Context, before int64, f substrate.ChangeFilter, limit int) ([]substrate.Change, error) {
	f, ok, err := s.changeFilter(ctx, f)
	if err != nil || !ok {
		return []substrate.Change{}, err
	}
	kept := []substrate.Change{}
	cur := before
	for {
		batch, err := s.inner.ChangesBefore(ctx, cur, f, limit)
		if err != nil {
			return nil, err
		}
		for _, c := range batch {
			row, show, err := s.heldRow(ctx, c, f.Values)
			if err != nil {
				return nil, err
			}
			if show {
				kept = append(kept, row)
				if len(kept) == limit {
					return kept, nil
				}
			}
		}
		if len(batch) == 0 || len(batch) < limit {
			return kept, nil
		}
		cur = batch[len(batch)-1].Seq
	}
}

// recordVisible decides one feed row's record by kind and id: a kind read
// whole needs no read, a row-gated one is read, tombstone included.
func (s *scopedDataset) recordVisible(ctx context.Context, kind, id string) (bool, error) {
	if ok, err := s.reads(ctx, kind); err != nil || ok {
		return ok, err
	}
	gated, err := s.rowGated(ctx, kind)
	if err != nil || !gated {
		return false, err
	}
	if kind == kindAgent {
		return s.talksTo(ctx, id)
	}
	path := vocabulary.RecordPath(kind, id)
	s.mu.Lock()
	seen := s.seen[path]
	s.mu.Unlock()
	if seen {
		return true, nil
	}
	rec, err := s.rowOf(ctx, kind, id)
	if err != nil || rec == nil {
		return false, err
	}
	ok, err := s.rowVisible(ctx, rec)
	if err != nil || !ok {
		return false, err
	}
	// A message, an interaction, an approval or a request never moves to
	// another thread,
	// so a row found readable stays readable for the rest of the watch.
	s.mu.Lock()
	if s.seen == nil || len(s.seen) >= visibleCacheMax {
		s.seen = map[string]bool{}
	}
	s.seen[path] = true
	s.mu.Unlock()
	return true, nil
}

// talksTo is the grant's agents rule for one agent, with the channel rule on
// top (decision 0214, as 0207 held it for an app's agent, now for every
// agent): the token reaches the agent (chats with it, reads its row, its
// threads, messages and interactions) only when its own read scopes cover
// every kind the agent reads, so the agent is no channel to data the caller
// could not read itself. An agent the repository does not hold is reached by
// nothing.
func (s *scopedDataset) talksTo(ctx context.Context, agent string) (bool, error) {
	agent = s.local(agent)
	if !s.grant.TalksTo(agent) {
		return false, nil
	}
	s.mu.Lock()
	known, ok := s.agentCovered[agent]
	s.mu.Unlock()
	if ok {
		return known, nil
	}
	covered, err := s.coversAgentReads(ctx, agent)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	if s.agentCovered == nil {
		s.agentCovered = map[string]bool{}
	}
	s.agentCovered[agent] = covered
	s.mu.Unlock()
	return covered, nil
}

// coversAgentReads reports whether the grant reads everything the agent
// reads, including its tools and subagents transitively: each kind or glob
// is covered by a read or write scope at least as wide
// (scope.Grant.Covers). A kind the registry lacks, or declares internal, is
// covered by its exact scope alone, as a glob never reaches one. What the
// agent then reads and writes is held by the engine, read by read and write
// by write, to the persons and the apps its turn works for (decision 0276).
func (s *scopedDataset) coversAgentReads(ctx context.Context, agent string) (bool, error) {
	rec, err := s.declarations().Get(ctx, kindAgent, agent)
	if errors.Is(err, substrate.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if rec.DeletedAt != nil {
		return false, nil
	}
	reg, err := s.kinds(ctx)
	if err != nil {
		return false, err
	}
	entries, err := reach.Callable(ctx, s.declarations(), rec)
	if err != nil {
		return false, err
	}
	for _, r := range entries {
		if r.Write {
			continue
		}
		if !s.grant.Covers(scope.Read, r.Pattern, scopeKind(r.Pattern, reg), reg) {
			return false, nil
		}
	}

	return true, nil
}

// withoutHeldToEffects is a feed row's payload without the `heldTo` a run's
// record took in it (withoutHeldTo): every effect on one of those records
// has the value taken out of what it set. The maps it changes are copies.
func withoutHeldToEffects(payload map[string]any) map[string]any {
	effects, ok := payload["fold"].([]any)
	if !ok {
		return payload
	}
	var out []any
	changed := false
	for _, e := range effects {
		op, ok := e.(map[string]any)
		delta, _ := op["delta"].(map[string]any)
		set, _ := delta["set"].(map[string]any)
		ref, _ := op["ref"].(string)
		if _, held := set["heldTo"]; !ok || !held || !heldKinds[ref] {
			out = append(out, e)
			continue
		}
		nextSet := make(map[string]any, len(set))
		for k, v := range set {
			if k != "heldTo" {
				nextSet[k] = v
			}
		}
		nextDelta := make(map[string]any, len(delta))
		for k, v := range delta {
			nextDelta[k] = v
		}
		nextDelta["set"] = nextSet
		nextOp := make(map[string]any, len(op))
		for k, v := range op {
			nextOp[k] = v
		}
		nextOp["delta"] = nextDelta
		out = append(out, nextOp)
		changed = true
	}
	if !changed {
		return payload
	}
	next := make(map[string]any, len(payload))
	for k, v := range payload {
		next[k] = v
	}
	next["fold"] = out
	return next
}
