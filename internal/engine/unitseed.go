package engine

// THE UNIT SEED (decision 0186): every repository holds the shipped unit and
// conversion records (internal/engine/unitseed), written at registration in
// the creation transaction and caught up at every open, right after the
// shipped vocabulary upgrade.
//
// The seed writes with apply semantics as the seed actor. A shipped record
// the repository lacks is written, and one whose row differs from it is
// refreshed, unless the owner ever wrote or deleted the record (ownerWrote's
// rule, the human doors alone): an owner's edit and an owner's deletion
// stand. A shipped record whose write would make the graph name a problem it
// did not name before (a spelling the owner's own unit answers, a cycle the
// owner's factor contradicts) is skipped and reported, so the transaction's
// own end (checkUnitGraph) passes and the open never fails for it.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/engine/unitseed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// shippedUnitRecord is one record of the shipped seed.
type shippedUnitRecord struct {
	kind, id string
	props    map[string]any
}

func (r shippedUnitRecord) ref() eref { return eref{Kind: r.kind, ID: r.id} }

var (
	shippedUnitsOnce sync.Once
	shippedUnits     []shippedUnitRecord
	shippedUnitsErr  error
)

// shippedUnitRecords is the seed, units first and then conversions, each in
// file order.
func shippedUnitRecords() ([]shippedUnitRecord, error) {
	shippedUnitsOnce.Do(func() {
		for _, file := range [][]byte{unitseed.Units(), unitseed.Conversions()} {
			recs, err := parseShippedUnits(file)
			if err != nil {
				shippedUnitsErr = err
				return
			}
			shippedUnits = append(shippedUnits, recs...)
		}
	})
	return shippedUnits, shippedUnitsErr
}

func parseShippedUnits(file []byte) ([]shippedUnitRecord, error) {
	dec := yaml.NewDecoder(bytes.NewReader(file))
	var out []shippedUnitRecord
	for {
		var doc struct {
			Kind     string `yaml:"kind"`
			Metadata struct {
				ID string `yaml:"id"`
			} `yaml:"metadata"`
			Data struct {
				Properties map[string]any `yaml:"properties"`
			} `yaml:"data"`
		}
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("substrate/engine: read the unit seed: %w", err)
		}
		if !isUnitGraphKind(doc.Kind) || doc.Metadata.ID == "" {
			return nil, fmt.Errorf("substrate/engine: the unit seed holds a %q record %q", doc.Kind, doc.Metadata.ID)
		}
		out = append(out, shippedUnitRecord{kind: doc.Kind, id: doc.Metadata.ID, props: doc.Data.Properties})
	}
}

// seedRow is one stored unit or conversion row, live or tombstoned.
type seedRow struct {
	props   map[string]any
	deleted bool
}

// readSeedRows reads every unit and conversion row q sees, and which of
// those records the owner ever wrote or deleted (a changelog entry under a
// human door), a purged one included.
func readSeedRows(ctx context.Context, q dbx) (map[eref]seedRow, map[eref]bool, error) {
	kinds := []string{vocabulary.KindUnit, vocabulary.KindConversion}
	rows, err := q.QueryContext(ctx, `SELECT kind, id, props, deleted_at IS NOT NULL FROM records WHERE kind = ANY($1::text[])`, kinds)
	if err != nil {
		return nil, nil, fmt.Errorf("substrate/engine: read the units: %w", err)
	}
	stored := map[eref]seedRow{}
	for rows.Next() {
		var ref eref
		var raw []byte
		var deleted bool
		if err := rows.Scan(&ref.Kind, &ref.ID, &raw, &deleted); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		props, err := decodeNumberPreserving(raw)
		if err != nil {
			_ = rows.Close()
			return nil, nil, fmt.Errorf("substrate/engine: read %s %s: %w", ref.Kind, ref.ID, err)
		}
		stored[ref] = seedRow{props: props, deleted: deleted}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, nil, err
	}
	humans := make([]string, 0, len(substrate.HumanActors))
	for a := range substrate.HumanActors {
		humans = append(humans, string(a))
	}
	owned, err := q.QueryContext(ctx, `
		SELECT DISTINCT kind, record_id FROM changelog
		WHERE kind = ANY($1::text[]) AND actor = ANY($2::text[])`, kinds, humans)
	if err != nil {
		return nil, nil, fmt.Errorf("substrate/engine: read who wrote the units: %w", err)
	}
	owner := map[eref]bool{}
	for owned.Next() {
		var ref eref
		if err := owned.Scan(&ref.Kind, &ref.ID); err != nil {
			_ = owned.Close()
			return nil, nil, err
		}
		owner[ref] = true
	}
	return stored, owner, errors.Join(owned.Err(), owned.Close())
}

// planUnitSeed decides what the seed writes: every shipped record the
// repository lacks or holds differently, the owner's records left out, and
// of those every one the graph admits. It tries them all at once first and,
// where that names a new problem, one at a time in seed order, so a
// conversion whose unit was skipped is skipped too.
func planUnitSeed(shipped []shippedUnitRecord, stored map[eref]seedRow, owner map[eref]bool) ([]shippedUnitRecord, []substrate.UnseededRecord) {
	live := map[eref]map[string]any{}
	for ref, r := range stored {
		if !r.deleted {
			live[ref] = r.props
		}
	}
	var cands []shippedUnitRecord
	for _, s := range shipped {
		r, held := stored[s.ref()]
		if held && !r.deleted && coversProps(r.props, s.props) {
			continue
		}
		if owner[s.ref()] {
			continue
		}
		cands = append(cands, s)
	}
	if len(cands) == 0 {
		return nil, nil
	}
	written := func(set map[eref]map[string]any, s shippedUnitRecord) map[eref]map[string]any {
		next := make(map[eref]map[string]any, len(set)+1)
		for k, v := range set {
			next[k] = v
		}
		merged := map[string]any{}
		if r, ok := stored[s.ref()]; ok && !r.deleted {
			for k, v := range r.props {
				merged[k] = v
			}
		}
		for k, v := range s.props {
			merged[k] = v
		}
		next[s.ref()] = merged
		return next
	}
	base := seedProblems(live)
	all := live
	for _, c := range cands {
		all = written(all, c)
	}
	if len(newProblems(seedProblems(all), base)) == 0 {
		return cands, nil
	}
	var writes []shippedUnitRecord
	var skipped []substrate.UnseededRecord
	cur, curP := live, base
	for _, c := range cands {
		next := written(cur, c)
		p := seedProblems(next)
		if fresh := newProblems(p, curP); len(fresh) > 0 {
			skipped = append(skipped, substrate.UnseededRecord{Kind: c.kind, ID: c.id, Reason: strings.Join(fresh, "; ")})
			continue
		}
		writes = append(writes, c)
		cur, curP = next, p
	}
	return writes, skipped
}

// coversProps reports whether a put of shipped onto a row holding stored
// would change nothing: every shipped property is stored with its value.
func coversProps(stored, shipped map[string]any) bool {
	for k, v := range shipped {
		if !jsonEqual(stored[k], v) {
			return false
		}
	}
	return true
}

// seedProblems is every problem the graph of a set of rows names, as text.
func seedProblems(set map[eref]map[string]any) map[string]bool {
	refs := make([]eref, 0, len(set))
	for ref := range set {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Kind != refs[j].Kind {
			return refs[i].Kind < refs[j].Kind
		}
		return refs[i].ID < refs[j].ID
	})
	var units []vocabulary.UnitRecord
	var convs []vocabulary.ConversionRecord
	out := map[string]bool{}
	for _, ref := range refs {
		u, c, err := readUnitGraphRecord(ref.Kind, ref.ID, set[ref])
		switch {
		case err != nil:
			out[vocabulary.UnitProblem{Kind: ref.Kind, ID: ref.ID, Message: err.Error()}.Error()] = true
		case u != nil:
			units = append(units, *u)
		case c != nil:
			convs = append(convs, *c)
		}
	}
	_, problems := vocabulary.BuildUnitGraph(units, convs)
	for _, p := range problems {
		out[p.Error()] = true
	}
	return out
}

// newProblems lists the problems of now that was does not name, sorted.
func newProblems(now, was map[string]bool) []string {
	var out []string
	for p := range now {
		if !was[p] {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// seedUnits writes the seed inside the transaction, as its actor, and
// answers what it skipped. A binary whose tree declares neither kind, or a
// service that seeds no units, writes nothing.
func (t *txn) seedUnits() ([]substrate.UnseededRecord, error) {
	if !t.ds.svc.seedUnits {
		return nil, nil
	}
	for _, kind := range []string{vocabulary.KindUnit, vocabulary.KindConversion} {
		if _, err := t.resolveType(kind); err != nil {
			return nil, nil
		}
	}
	shipped, err := shippedUnitRecords()
	if err != nil {
		return nil, err
	}
	stored, owner, err := readSeedRows(t.ctx, t.tx)
	if err != nil {
		return nil, err
	}
	writes, skipped := planUnitSeed(shipped, stored, owner)
	for _, w := range writes {
		if _, err := t.put(substrate.PutInput{Kind: w.kind, ID: w.id, Properties: w.props}); err != nil {
			return nil, fmt.Errorf("substrate/engine: seed %s %s: %w", w.kind, w.id, err)
		}
	}
	return skipped, nil
}

// ensureShippedUnits is the open's catch-up: the plan is read outside any
// transaction first, so an open with nothing to seed takes no changelog
// lock, and the seed transaction plans again under it. A failure is logged
// and never fails the open: the repository opens on the units it holds.
func (ds *dataset) ensureShippedUnits(ctx context.Context) error {
	if ds.svc.readOnly || !ds.svc.seedUnits {
		return nil
	}
	reg := ds.registry()
	for _, kind := range []string{vocabulary.KindUnit, vocabulary.KindConversion} {
		if _, ok := reg.ByIdentity(kind); !ok {
			return nil
		}
	}
	shipped, err := shippedUnitRecords()
	if err != nil {
		return err
	}
	stored, owner, err := readSeedRows(ctx, ds.db)
	if err != nil {
		return err
	}
	writes, skipped := planUnitSeed(shipped, stored, owner)
	if len(writes) > 0 {
		err := ds.inTx(ctx, substrate.ActorSeed, true, func(t *txn) error {
			s, err := t.seedUnits()
			skipped = s
			return err
		})
		if err != nil {
			ds.svc.log.Error("substrate: the shipped units could not be seeded; the repository opens on the units it holds",
				"repository", ds.scope.Repository, "error", err)
			return nil
		}
		ds.svc.log.Info("substrate: seeded the shipped units", "repository", ds.scope.Repository, "records", len(writes)-len(skipped))
	}
	for _, s := range skipped {
		ds.svc.log.Warn("substrate: a shipped unit record contradicts the repository's graph and was not seeded",
			"repository", ds.scope.Repository, "kind", s.Kind, "id", s.ID, "reason", s.Reason)
	}
	ds.setUnseededUnits(skipped)
	return nil
}

func (ds *dataset) setUnseededUnits(s []substrate.UnseededRecord) {
	ds.unseededMu.Lock()
	defer ds.unseededMu.Unlock()
	ds.unseeded = append([]substrate.UnseededRecord(nil), s...)
}

func (ds *dataset) unseededUnits() []substrate.UnseededRecord {
	ds.unseededMu.Lock()
	defer ds.unseededMu.Unlock()
	return append([]substrate.UnseededRecord(nil), ds.unseeded...)
}
