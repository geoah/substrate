package engine

// A KIND MOVE IS ORDINARY RECORD WRITES (record 0078), composed with the four
// conversions convert.go performs and running before all of them, because every
// one of those rewrites the rows a move has carried.
//
// A declaration carrying `movedFrom` whose named kind the repository still
// declares carries that kind's live rows onto it: same id, same properties,
// states, labels, annotations, body, temporal columns, property managers and
// sealed material, with every reference at a moving kind repointed. The old
// rows are tombstoned and the old kind is left declared and empty; nothing is
// pruned and no name is retired (record 0055).
//
// HONORED ON THE BOOT UPGRADE OF THE SEEDED PACKAGES, AND NOWHERE ELSE. The
// key is reserved by name everywhere (record 0020) and stored wherever it is
// written, but only the shipped tree's own upgrade performs the move: the apply
// door refuses a batch that would imply one (vocabularywrite.go), because it
// publishes its candidate registry before the rewrite runs and because a move
// is the substrate's own hand reaching past the write path's admission rules,
// which is a license no repository token holds.
//
// WHAT IT REFUSES, as plan blockers the boot reports and skips on:
//   - a property set that is not identical to the old kind's, in either
//     direction (the rows travel with their properties untouched and under
//     their own names);
//   - ANY destination row at a moving id, live or tombstoned, so the write path
//     never merges into a standing record and never resurrects a dead one;
//   - an old kind that is a mapping source or declares a `subject:` reference,
//     whose recompute would mint records under the move's admission bypass;
//   - a reference declaration left pinned at the old kind;
//   - a REQUIRED `mustExist` cycle among the rows being carried, which no order
//     of writes satisfies.
//
// IDEMPOTENT: the move is classified from the LIVE rows, so a second boot finds
// none and plans nothing.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// kindMove is one kind's rows moving to the kind that named it with
// `movedFrom`. `from` is the old identity as the repository still declares it;
// `to` is the kind the candidate declares.
type kindMove struct {
	from string
	to   *vocabulary.Kind
	// ids are the live rows to carry, resolved once at planning so the counts
	// and the writes agree.
	ids []string
}

// movedRow is one row write, in the order the plan resolved. deferred names the
// references this row cannot carry at its put because their `mustExist` target
// is a row written later; they are patched back once every row exists.
type movedRow struct {
	move     *kindMove
	id       string
	deferred map[string]bool
}

// movesPlanned is what the planner resolved: the kinds (for the counted steps),
// the row writes in dependency order, and the sources whose references follow.
type movesPlanned struct {
	moves    []kindMove
	order    []movedRow
	repoints []repoint
}

func (p movesPlanned) empty() bool { return len(p.order) == 0 && len(p.repoints) == 0 }

// repoint is one source record whose reference values follow a move.
type repoint struct{ src eref }

// classifyKindMoves lists the moves a batch declares against the stored
// declarations: a candidate kind carrying `movedFrom` whose named kind the
// repository still declares. A `movedFrom` naming a kind no repository ever had
// is not a move, exactly as a `renamedFrom` naming a property no stored
// declaration had is not a rename.
func classifyKindMoves(current, candidate *vocabulary.Registry, touched, skip map[string]bool) []kindMove {
	var moves []kindMove
	for _, aname := range sortedKeys(touched) {
		cand, _ := candidate.PackageByName(aname)
		if cand == nil {
			continue
		}
		for _, tn := range cand.KindOrder {
			candT := cand.Kinds[tn]
			if candT == nil || candT.MovedFrom == "" || skip[candT.Identity] {
				continue
			}
			if _, held := current.ByIdentity(candT.MovedFrom); !held {
				continue
			}
			moves = append(moves, kindMove{from: candT.MovedFrom, to: candT})
		}
	}
	return moves
}

// movedTargets maps each moving kind's OLD identity to its new one. The
// narrowing guards read it so a reference at a kind this same transaction moves
// is not counted as pointing elsewhere (schemadiff.go acceptedTargets).
func movedTargets(moves []kindMove) map[string]string {
	if len(moves) == 0 {
		return nil
	}
	out := make(map[string]string, len(moves))
	for _, m := range moves {
		out[m.from] = m.to.Identity
	}
	return out
}

// userDoorMoveGuards is the apply door's whole answer to `movedFrom`: the key
// is stored, and any move it would imply is refused. See the file header.
func userDoorMoveGuards(moves []kindMove) []string {
	var problems []string
	for _, m := range moves {
		problems = append(problems, fmt.Sprintf(
			"kind %s: data.movedFrom names %s, which this repository still declares: movedFrom is honored for the shipped vocabulary only in this build, so the key is stored and no records move",
			m.to.Identity, m.from))
	}
	return problems
}

// moveGuards is everything a move is refused for that needs no live row: the
// shapes the two declarations cannot reconcile, the sets of moves that cannot
// be ordered at all, and the old kinds whose recompute would write under the
// move's admission bypass.
func moveGuards(current *vocabulary.Registry, moves []kindMove) []string {
	var problems []string
	seen := map[string]string{}
	arriving := map[string]bool{}
	for _, m := range moves {
		arriving[m.to.Identity] = true
	}
	for _, m := range moves {
		if other, dup := seen[m.from]; dup {
			problems = append(problems, fmt.Sprintf(
				"kind %s: data.movedFrom names %s, which %s also moves; one kind's rows go to one place",
				m.to.Identity, m.from, other))
			continue
		}
		seen[m.from] = m.to.Identity
		if arriving[m.from] {
			problems = append(problems, fmt.Sprintf(
				"kind %s: data.movedFrom names %s, which is itself a destination of this batch; a move is not a swap",
				m.to.Identity, m.from))
			continue
		}
		old, ok := current.ByIdentity(m.from)
		if !ok {
			continue
		}
		problems = append(problems, vocabulary.MovedFromProblems(old, m.to)...)
		problems = append(problems, mappingMoveGuards(current, m, old)...)
	}
	return problems
}

// mappingMoveGuards refuses a move out of a kind the mapping graph touches. A
// mapping source's write ENSURES its subject (write.go ensureSubject), minting
// a target record if none stands, and the move writes under an admission bypass
// meant for carrying a record that already existed: a mapping firing there
// would create records nobody asked for, attributed to the engine, outside the
// plan that said how much would move.
func mappingMoveGuards(current *vocabulary.Registry, m kindMove, old *vocabulary.Kind) []string {
	var problems []string
	for _, pname := range old.PropOrder {
		if p := old.Props[pname]; p.Subject {
			problems = append(problems, fmt.Sprintf(
				"kind %s: data.movedFrom names %s, which declares the subject reference %q: a mapping source is not moved in this build",
				m.to.Identity, m.from, pname))
		}
	}
	if len(current.MappingsFrom(m.from)) > 0 || len(current.MappingsTo(m.from)) > 0 {
		problems = append(problems, fmt.Sprintf(
			"kind %s: data.movedFrom names %s, which a recordmapping names: a mapped kind is not moved in this build",
			m.to.Identity, m.from))
	}
	return problems
}

// planMoves resolves every move's rows, the order they are written in, the
// references that follow them, and the guards the store answers. It runs under
// whichever reader the door holds, so the preview and the boot see the same
// numbers.
func planMoves(q sqlReader, candidate *vocabulary.Registry, moves []kindMove) (movesPlanned, []string, error) {
	var out movesPlanned
	if len(moves) == 0 {
		return out, nil, nil
	}
	out.moves = make([]kindMove, len(moves))
	copy(out.moves, moves)
	var problems []string
	targets := movedTargets(moves)
	for i := range out.moves {
		ids, err := liveIDs(q, out.moves[i].from)
		if err != nil {
			return out, nil, err
		}
		out.moves[i].ids = ids
		// ANY row at the destination id, live or tombstoned. A live one would
		// be merged into; a tombstoned one would be resurrected, and both are
		// the write path doing something to a record this plan never counted.
		for _, id := range ids {
			held, err := anyRow(q, out.moves[i].to.Identity, id)
			if err != nil {
				return out, nil, err
			}
			if held {
				problems = append(problems, fmt.Sprintf(
					"kind %s already holds a record %q, which the move from %s would write over: delete and purge it first",
					out.moves[i].to.Identity, id, out.moves[i].from))
			}
		}
	}
	points, refProblems, err := repointsFor(q, candidate, targets)
	if err != nil {
		return out, nil, err
	}
	out.repoints = points
	problems = append(problems, refProblems...)
	order, orderProblems, err := orderRows(q, out.moves, targets)
	if err != nil {
		return out, nil, err
	}
	out.order = order
	return out, append(problems, orderProblems...), nil
}

// repointsFor finds the live records whose references name a moving kind, and
// refuses the declarations that cannot follow. EVERY reference at the old kind
// is repointed, whatever its target: the narrowing guard counts them all
// (schemadiff.go acceptedTargets), so one left behind would sit outside the pin
// the same upgrade installs. A value naming a row that was already dangling
// stays dangling, now at the kind its declaration admits.
func repointsFor(q sqlReader, candidate *vocabulary.Registry, targets map[string]string) ([]repoint, []string, error) {
	var problems []string
	seen := map[eref]bool{}
	var out []repoint
	for _, from := range sortedKeys(targets) {
		rows, err := q.query(`SELECT DISTINCT r.src_kind, r.src, r.property FROM refs r
			JOIN records x ON x.kind = r.src_kind AND x.id = r.src
			WHERE r.dst_kind = $1 AND x.deleted_at IS NULL
			ORDER BY r.src_kind, r.src, r.property`, from)
		if err != nil {
			return nil, nil, err
		}
		type site struct {
			ref      eref
			property string
		}
		var sites []site
		for rows.Next() {
			var s site
			if err := rows.Scan(&s.ref.Kind, &s.ref.ID, &s.property); err != nil {
				_ = rows.Close()
				return nil, nil, err
			}
			sites = append(sites, s)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, nil, err
		}
		refused := map[string]bool{}
		for _, s := range sites {
			// A source that is ITSELF moving carries its own references across
			// in the put (moveRecord), under the declaration it arrives at.
			if _, itselfMoving := targets[s.ref.Kind]; itselfMoving {
				continue
			}
			ty, ok := candidate.ByIdentity(s.ref.Kind)
			if !ok {
				continue
			}
			p, ok := ty.Prop(s.property)
			if !ok {
				continue
			}
			switch pin := p.To; {
			case pin == from:
				// The declaration did not follow the move. Repointing its
				// values would put them outside its own pin, and leaving them
				// points them at a kind with no rows: say so and move nothing.
				key := s.ref.Kind + "." + s.property
				if !refused[key] {
					refused[key] = true
					problems = append(problems, fmt.Sprintf(
						"kind %s: reference %q is still pinned at %s, which this upgrade empties into %s: repin it in the same batch",
						s.ref.Kind, s.property, from, targets[from]))
				}
			case pin == "" || pin == vocabulary.ToAny || pin == targets[from]:
				if !seen[s.ref] {
					seen[s.ref] = true
					out = append(out, repoint{src: s.ref})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].src.Kind != out[j].src.Kind {
			return out[i].src.Kind < out[j].src.Kind
		}
		return out[i].src.ID < out[j].src.ID
	})
	return out, problems, nil
}

// rowKey addresses one moving row inside the ordering.
type rowKey struct{ kind, id string }

// orderRows puts every carried row in an order where each row's `mustExist`
// references at OTHER CARRIED ROWS are already written. The dependency is
// per-row, not per-kind: a thread whose `parent` names another thread depends on
// that thread and on nothing else of its kind, so a kind-level order would
// still write the child first half the time.
//
// What cannot be ordered is deferred: the row is written without the reference
// and patched once every row exists. Only a REQUIRED reference in a genuine
// cycle is refused, because no order and no pass creates a row without the
// value its own declaration demands.
func orderRows(q sqlReader, moves []kindMove, targets map[string]string) ([]movedRow, []string, error) {
	byOld := map[string]*kindMove{}
	carried := map[rowKey]bool{}
	for i := range moves {
		byOld[moves[i].from] = &moves[i]
		for _, id := range moves[i].ids {
			carried[rowKey{moves[i].from, id}] = true
		}
	}
	// deps[r] is every carried row r must follow, by the property that says so.
	deps := map[rowKey]map[rowKey][]string{}
	for _, from := range sortedKeys(targets) {
		rows, err := q.query(`SELECT r.src, r.property, r.dst_kind, r.dst FROM refs r
			JOIN records x ON x.kind = r.src_kind AND x.id = r.src
			WHERE r.src_kind = $1 AND x.deleted_at IS NULL
			ORDER BY r.src, r.property, r.dst`, from)
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var src, property, dstKind, dst string
			if err := rows.Scan(&src, &property, &dstKind, &dst); err != nil {
				_ = rows.Close()
				return nil, nil, err
			}
			m := byOld[from]
			// The declaration the row ARRIVES under decides whether the
			// reference must resolve at its write.
			p, ok := m.to.Prop(property)
			if !ok || !p.MustExist {
				continue
			}
			target := rowKey{dstKind, dst}
			if !carried[target] || target == (rowKey{from, src}) {
				continue // not moving, or its own row: nothing to wait for
			}
			key := rowKey{from, src}
			if deps[key] == nil {
				deps[key] = map[rowKey][]string{}
			}
			deps[key][target] = append(deps[key][target], property)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, nil, err
		}
	}

	var problems []string
	var order []movedRow
	written := map[rowKey]bool{}
	remaining := make([]rowKey, 0, len(carried))
	for _, m := range moves {
		for _, id := range m.ids {
			remaining = append(remaining, rowKey{m.from, id})
		}
	}
	for len(remaining) > 0 {
		var next []rowKey
		progressed := false
		for _, r := range remaining {
			ready := true
			for d := range deps[r] {
				if !written[d] {
					ready = false
				}
			}
			if !ready {
				next = append(next, r)
				continue
			}
			order = append(order, movedRow{move: byOld[r.kind], id: r.id})
			written[r], progressed = true, true
		}
		if progressed {
			remaining = next
			continue
		}
		// Nothing became ready, so what is left holds a cycle. Every remaining
		// row defers the references into the cycle; a required one is refused.
		for _, r := range remaining {
			deferred := map[string]bool{}
			for d, names := range deps[r] {
				if written[d] {
					continue
				}
				for _, pname := range names {
					if byOld[r.kind].to.Props[pname].Required {
						problems = append(problems, fmt.Sprintf(
							"kind %s: record %q holds the required mustExist reference %q at %s, which moves in the same batch and points back: no order of writes creates either row",
							byOld[r.kind].to.Identity, r.id, pname, vocabulary.RecordPath(d.kind, d.id)))
						continue
					}
					deferred[pname] = true
				}
			}
			order = append(order, movedRow{move: byOld[r.kind], id: r.id, deferred: deferred})
			written[r] = true
		}
		remaining = nil
	}
	return order, problems, nil
}

// countMoves turns the planned moves into the plan's steps and its share of the
// work. A move with no live row is not a step.
//
// THE WORK IS THE UPPER BOUND ON THE ENTRIES (record 0067): a put and a delete
// per carried row, a patch per deferred row, a patch per repointed source, and
// a patch per grant the move rewrites. Counting the rows alone would let a move
// past the ceiling write more than the ceiling admitted.
func countMoves(p movesPlanned, grants int64) ([]substrate.ConversionStep, int64) {
	var steps []substrate.ConversionStep
	var work int64
	for _, m := range p.moves {
		if len(m.ids) == 0 {
			continue
		}
		n := int64(len(m.ids))
		steps = append(steps, substrate.ConversionStep{
			Step: substrate.StepMove, Kind: m.to.Identity,
			From: m.from, To: m.to.Identity, Records: n,
		})
		work += 2 * n
	}
	for _, r := range p.order {
		if len(r.deferred) > 0 {
			work++
		}
	}
	work += int64(len(p.repoints)) + grants
	return steps, work
}

// countGrants counts the agent and function declarations whose kind grants the
// move rewrites: a patch each, and part of the work the ceiling bounds.
func countGrants(q sqlReader, targets map[string]string) (int64, error) {
	if len(targets) == 0 {
		return 0, nil
	}
	var total int64
	for _, from := range sortedKeys(targets) {
		for _, kind := range []string{kindAgent, kindFunction} {
			var n int64
			if err := q.row(`SELECT count(*) FROM records
				WHERE kind = $1 AND deleted_at IS NULL
				  AND (props->'permissions')::text LIKE '%' || $2 || '%'`, kind, from).Scan(&n); err != nil {
				return 0, err
			}
			total += n
		}
	}
	return total, nil
}

// moveRecords performs the planned move: the rows in dependency order, the
// references they deferred, the sources that point at them, and the grants that
// name them.
func (t *txn) moveRecords(candidate *vocabulary.Registry, p movesPlanned) (int64, error) {
	if p.empty() {
		return 0, nil
	}
	prev := t.writeReg
	t.writeReg = candidate
	defer func() { t.writeReg = prev }()

	targets := movedTargets(p.moves)
	moving := map[string]map[string]bool{}
	for _, m := range p.moves {
		moving[m.from] = idSet(m.ids)
	}
	var total int64
	type deferredWrite struct {
		ref   eref
		props map[string]any
	}
	var later []deferredWrite
	for _, r := range p.order {
		held, err := t.moveRecord(r, targets, moving)
		if err != nil {
			return total, fmt.Errorf("substrate/engine: move %s to %s: %w", r.move.from, r.move.to.Identity, err)
		}
		if len(held) > 0 {
			later = append(later, deferredWrite{ref: eref{Kind: r.move.to.Identity, ID: r.id}, props: held})
		}
		total++
	}
	for _, d := range later {
		if _, err := t.patch(d.ref, substrate.PatchInput{Properties: d.props}); err != nil {
			return total, fmt.Errorf("substrate/engine: write the deferred references of %s: %w",
				vocabulary.RecordPath(d.ref.Kind, d.ref.ID), err)
		}
	}
	for _, pt := range p.repoints {
		if err := t.repointRecord(pt.src, targets, moving); err != nil {
			return total, fmt.Errorf("substrate/engine: repoint %s: %w",
				vocabulary.RecordPath(pt.src.Kind, pt.src.ID), err)
		}
	}
	// The kind references a declaration spells as a grant rather than as a
	// reference at the kind itself: nothing else moves those, and a stored
	// agent whose `ask` grant still named the old interaction kind would be
	// refused by the loader at the next open and its package quarantined.
	for _, m := range p.moves {
		if err := t.repointGrants(m.from, m.to.Identity); err != nil {
			return total, fmt.Errorf("substrate/engine: repoint the grants naming %s: %w", m.from, err)
		}
	}
	return total, nil
}

// moveRecord writes one row under the new kind and tombstones the old one,
// returning the deferred reference values its put could not carry.
func (t *txn) moveRecord(r movedRow, targets map[string]string, moving map[string]map[string]bool) (map[string]any, error) {
	m := r.move
	from := eref{Kind: m.from, ID: r.id}
	row, err := t.loadRow(from, true)
	if err != nil || row == nil || row.DeletedAt != nil {
		return nil, err
	}
	managers, err := t.managersOf(from)
	if err != nil {
		return nil, err
	}
	annotations, err := t.annotationsOf(from)
	if err != nil {
		return nil, err
	}
	props := map[string]any{}
	held := map[string]any{}
	for k, v := range row.Props {
		// A reference at a kind this batch carries arrives naming where that
		// kind went, because the declaration it arrives under pins the new one.
		for oldKind, newKind := range targets {
			if next, changed := repointValue(v, oldKind, newKind, moving[oldKind]); changed {
				v = next
			}
		}
		// A secret is re-sealed under its new owner: the sealed row is bound to
		// the record that owns it (sealedAAD), so a carried reference would be
		// stored as a plaintext string and open as its own name.
		if p, ok := m.to.Prop(k); ok && p.Secret() {
			if s, isStr := v.(string); isStr && strings.HasPrefix(s, secretRefPrefix) {
				owned, err := t.sealedRefOf(s, from)
				if err != nil {
					return nil, err
				}
				if owned {
					plain, err := t.openSealedRef(s)
					if err != nil {
						return nil, fmt.Errorf("open the sealed %s.%s: %w", m.from, k, err)
					}
					v = string(plain)
				}
			}
		}
		if r.deferred[k] {
			held[k] = v
			continue
		}
		props[k] = v
	}
	// `body` and the temporal columns are columns on the row rather than keys
	// under props, and a state is neither: all three travel as the declared
	// properties they are, or the moved record arrives without them.
	if row.Body != "" {
		props["body"] = row.Body
	}
	for name, v := range map[string]*time.Time{"at": row.At, "endsAt": row.EndsAt, "dueAt": row.DueAt} {
		if v != nil {
			props[name] = v.UTC().Format(time.RFC3339Nano)
		}
	}
	for name, state := range row.States {
		if _, declared := m.to.Machines[name]; declared {
			props[name] = state
		}
	}
	to := eref{Kind: m.to.Identity, ID: r.id}
	if err := t.putMovedRow(m, to, props, row.Labels, annotations); err != nil {
		return nil, fmt.Errorf("write %s as %s: %w", vocabulary.RecordPath(m.from, r.id), m.to.Identity, err)
	}
	// The managers follow the values, deferred ones included: who last wrote a
	// property, at which tier and behind which token, is a fact about the value
	// and not about the hand that carried it, so a hand edit still outranks a
	// later sync on the moved row.
	for _, name := range sortedKeys(managers) {
		mr := managers[name]
		if _, carried := props[name]; !carried {
			if _, deferred := held[name]; !deferred {
				continue
			}
		}
		if err := t.setManagerAs(to, name, substrate.Actor(mr.actor), mr.tier, mr.principal); err != nil {
			return nil, err
		}
	}
	// The old sealed rows go with the old record: the move re-sealed their
	// plaintext under the new owner, so leaving them would keep a second copy
	// of a secret nothing reads.
	if err := t.dropSealedOf(from); err != nil {
		return nil, err
	}
	if _, err := t.tombstone(from, ""); err != nil {
		return nil, err
	}
	if err := t.appendChange(t.actor, substrate.OpDelete, r.id, m.from, map[string]any{
		"finalizers": row.Finalizers,
		// Where the record went, so a reader of the changelog alone can follow
		// it. The fold replays the tombstone and reads none of this.
		"movedTo": m.to.Identity,
	}); err != nil {
		return nil, err
	}
	return held, t.afterTombstone(from)
}

// putMovedRow is the one write the move's admission bypass covers, and it
// covers NOTHING ELSE: the flag is set immediately before the put and cleared
// immediately after, so the deferred patches, the repoints and the grant
// rewrites are judged as the ordinary writes they are.
//
// The bypass is the creation-only contracts (an interaction's batch, a change
// request's reviewed envelope) and the guards that ask WHO is writing: this
// record was admitted once already, under the old kind, so carrying it is not a
// second authorship.
func (t *txn) putMovedRow(m *kindMove, to eref, props, labels, annotations map[string]any) error {
	prevInternal, prevAsk, prevMoving := t.internal, t.interactionThread, t.movingRecords
	t.internal, t.interactionThread, t.movingRecords = true, true, true
	defer func() {
		t.internal, t.interactionThread, t.movingRecords = prevInternal, prevAsk, prevMoving
	}()
	_, err := t.putKind(m.to, substrate.PutInput{
		Kind: to.Kind, ID: to.ID, Properties: props, Labels: labels, Annotations: annotations,
	})
	return err
}

// dropSealedOf removes a record's sealed rows. The move opened each one and
// re-sealed its plaintext under the new owner, so what stays here is a payload
// bound by AAD to a record that no longer exists.
func (t *txn) dropSealedOf(ref eref) error {
	rows, err := t.query(`SELECT ref FROM sealed WHERE record_kind = $1 AND record_id = $2 ORDER BY ref`,
		ref.Kind, ref.ID)
	if err != nil {
		return err
	}
	var refs []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			_ = rows.Close()
			return err
		}
		refs = append(refs, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, r := range refs {
		if _, err := t.exec(`DELETE FROM sealed WHERE ref = $1`, r); err != nil {
			return err
		}
		t.mirrorSealedDelete(r)
	}
	return nil
}

// repointRecord rewrites one source record's reference values and appends the
// patch that says so. The rewritten values go through the ordinary reference
// validation, so a repoint that would land outside its own declaration's pin
// fails the transaction rather than committing a value no write could make.
func (t *txn) repointRecord(ref eref, targets map[string]string, moving map[string]map[string]bool) error {
	row, err := t.loadRow(ref, true)
	if err != nil || row == nil || row.DeletedAt != nil {
		return err
	}
	ty, err := t.resolveType(ref.Kind)
	if err != nil {
		return err
	}
	before := row.clone()
	var moved []string
	rewritten := map[string]any{}
	for _, name := range sortedKeys(row.Props) {
		v := row.Props[name]
		changed := false
		for _, oldKind := range sortedKeys(targets) {
			next, c := repointValue(v, oldKind, targets[oldKind], moving[oldKind])
			if c {
				v, changed = next, true
			}
		}
		if !changed {
			continue
		}
		rewritten[name] = v
		moved = append(moved, name)
	}
	if len(moved) == 0 {
		return nil
	}
	if err := t.validateReferences(ty, rewritten); err != nil {
		return fmt.Errorf("the repointed references of %s: %w", vocabulary.RecordPath(ref.Kind, ref.ID), err)
	}
	for name, v := range rewritten {
		row.Props[name] = v
	}
	if _, err := t.foldRow(before, row, false, false); err != nil {
		return err
	}
	return t.appendChange(t.actor, substrate.OpPatch, ref.ID, ref.Kind, map[string]any{
		"properties": moved,
		"repointed":  targets,
	})
}

// repointGrants rewrites the kind references an agent or a function declaration
// spells in its `permissions`: the write grant and the read grant. They name a
// kind's own DECLARATION record, so the refs index keys them on `core/kind`
// rather than on the kind granted and the reference repoint never sees them.
//
// It is a RECORD write and not a declaration change: the grant says the same
// thing about the same kind, under the name that kind now has. The declaration
// `version` stays put because it keys the shipped upgrade, and both closures
// that can hold a grant re-declare themselves anyway (the seeded agents come
// from the tree at every boot, a sample's come from a re-import).
func (t *txn) repointGrants(from, to string) error {
	for _, kind := range []string{kindAgent, kindFunction} {
		ids, err := t.liveIDsOf(kind)
		if err != nil {
			return err
		}
		for _, id := range ids {
			ref := eref{Kind: kind, ID: id}
			row, err := t.loadRow(ref, true)
			if err != nil {
				return err
			}
			if row == nil || row.DeletedAt != nil {
				continue
			}
			next, changed := replaceGrant(row.Props["permissions"], from, to)
			if !changed {
				continue
			}
			before := row.clone()
			row.Props["permissions"] = next
			if _, err := t.foldRow(before, row, false, false); err != nil {
				return err
			}
			if err := t.appendChange(t.actor, substrate.OpPatch, id, kind, map[string]any{
				"properties": []string{"permissions"},
				"repointed":  map[string]string{from: to},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// repointValue rewrites every `{ref: "<from>/<id>"}` under v to name `to`, at
// any depth and inside any container. The stored shape of a reference is the
// one reserved key (record 0044), so nothing else in a record's properties is
// touched. `ids` is advisory: a value naming a row outside it is still
// repointed, because the narrowing guard counts every reference at the old kind
// and one left behind would sit outside the pin this upgrade installs.
func repointValue(v any, from, to string, ids map[string]bool) (any, bool) {
	switch x := v.(type) {
	case map[string]any:
		changed := false
		out := make(map[string]any, len(x))
		for k, inner := range x {
			if k == vocabulary.ReferenceValueKey {
				if s, ok := inner.(string); ok && strings.HasPrefix(s, from+"/") {
					out[k] = vocabulary.RecordPath(to, strings.TrimPrefix(s, from+"/"))
					changed = true
					continue
				}
			}
			next, c := repointValue(inner, from, to, ids)
			out[k] = next
			changed = changed || c
		}
		if !changed {
			return v, false
		}
		return out, true
	case []any:
		changed := false
		out := make([]any, len(x))
		for i, inner := range x {
			next, c := repointValue(inner, from, to, ids)
			out[i] = next
			changed = changed || c
		}
		if !changed {
			return v, false
		}
		return out, true
	default:
		return v, false
	}
}

// replaceGrant rewrites a kind reference inside a grant, at any depth, in both
// spellings a stored grant takes: the bare identity as authored, and the
// reference the projection stores it as, which names the kind's own declaration
// record.
func replaceGrant(v any, from, to string) (any, bool) {
	switch x := v.(type) {
	case string:
		switch x {
		case from:
			return to, true
		case vocabulary.RecordPath(kindKind, from):
			return vocabulary.RecordPath(kindKind, to), true
		}
		return v, false
	case map[string]any:
		changed := false
		out := make(map[string]any, len(x))
		for k, inner := range x {
			next, c := replaceGrant(inner, from, to)
			out[k], changed = next, changed || c
		}
		if !changed {
			return v, false
		}
		return out, true
	case []any:
		changed := false
		out := make([]any, len(x))
		for i, inner := range x {
			next, c := replaceGrant(inner, from, to)
			out[i], changed = next, changed || c
		}
		if !changed {
			return v, false
		}
		return out, true
	default:
		return v, false
	}
}

// liveIDs is liveIDsOf over whichever reader the door holds.
func liveIDs(q sqlReader, kind string) ([]string, error) {
	rows, err := q.query(`SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL ORDER BY id`, kind)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// anyRow reports whether a record stands at this kind and id, tombstoned or
// not: a move writes over neither.
func anyRow(q sqlReader, kind, id string) (bool, error) {
	var one int
	err := q.row(`SELECT 1 FROM records WHERE kind = $1 AND id = $2`, kind, id).Scan(&one)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	default:
		return false, err
	}
}

func idSet(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// annotationsOf reads a record's annotations so a move carries them with the
// row: they live in their own table rather than under props, so a move that
// read only the row would drop every note an owner left on it.
func (t *txn) annotationsOf(ref eref) (map[string]any, error) {
	rows, err := t.query(
		`SELECT key, value FROM annotations WHERE record_kind = $1 AND record_id = $2 ORDER BY key`,
		ref.Kind, ref.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]any{}
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		out[key] = v
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
