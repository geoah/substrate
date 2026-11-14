package engine

// WHAT A PUBLISH DOES WHERE A NAME STANDS (decision record 0289). After a
// publish nothing live names the package or its kinds in `~`. Every place a
// name can stand is listed already: a declaration's positions
// (vocabulary/positions.go, answered by vocabulary.PublishDocuments) and the
// substrate's own records' (recordNamePositions). This file gives every one
// of the records' positions one of three answers, in publishRecordPositions:
//
//   - MOVES: the name moves by the publish's own rule (vocabulary.PublishedName
//     and its kin), and a pattern over every package of `~` (`~/*`) keeps the
//     published package's glob beside it, so nothing reaches less, and no
//     gate lifts, because a package left `~`;
//   - STOPS: a record that names the package there refuses the publish, and
//     the preview lists it;
//   - HISTORY: left as written, with its reason.
//
// Beside the table, every reference value at a declaration of the package
// moves, in a record of any kind but a HISTORY one, and every reference at a
// record of its kinds moves with the kind move (move.go). A position the
// table does not answer fails TestEveryNamePositionHasAPublishAnswer.
//
// A rename is written as a patch whose line says `repointed`, which no
// trigger delivers (functions.go movedLine). A kind whose envelope a write
// may not change (an approval's call, a change request's proposal) moves its
// names through the fold, as the move's own repoint does, never through the
// write path's guards, which would refuse the same record under its new name.

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// publishRename is one publish's names: the local package (`~/recipes`), the
// package it becomes (`ada.example.com/recipes`), the package's name and the
// authority apart, the names that spelled `~` before it, and every kind it
// moves, old to new. texts collects, as the rename runs, the records whose
// author's text it leaves naming the package (a listed position), for the
// plan.
type publishRename struct {
	from, to       string
	pkg, authority string
	spellings      []string
	kinds          map[string]string
	texts          []string
}

// publishPosition is the publish's answer at one position. A MOVES position
// moves the strings at its end by names, each into one name or a name and
// the glob beside it, or the value at its end whole by value. why says why a
// STOPS or a HISTORY position is one. A HISTORY position is two more things
// where it says so: listed, its record named in the plan while the value
// names the package, for its author to read (data a callable is handed);
// and open, a stop while the record is still to be acted on, when the value
// is read again then (an approval's call before it runs).
type publishPosition struct {
	answer vocabulary.PublishAnswer
	names  func(r *publishRename, s string) []string
	value  func(r *publishRename, v any) (any, bool)
	why    string
	listed bool
	open   func(row *erow) bool
}

// recordWhole is the path that answers for a whole record, and recordID the
// one that answers for its id.
const (
	recordWhole = "*"
	recordID    = "@id"
)

var (
	moveName     = publishPosition{answer: vocabulary.PublishMoves, names: publishName}
	movePattern  = publishPosition{answer: vocabulary.PublishMoves, names: publishPattern}
	moveRefsIn   = publishPosition{answer: vocabulary.PublishMoves, value: publishRefsIn}
	moveCodeIn   = publishPosition{answer: vocabulary.PublishMoves, value: publishCodeIn}
	transcript   = "a transcript: what was said and done in a turn, under the names it had then"
	stopSettings = "a setting or a secret of the package: this build carries none to a published name; delete it, publish, and write it again"
)

// publishRecordPositions answers every position of recordNamePositions, by
// kind and path ("/"-joined), and the positions only a publish reads: a
// trigger's guard and arguments, which name kinds as code does, a collection
// view's filter and a change request's proposal, which hold reference values
// inside a json, and the ids that are names.
var publishRecordPositions = map[string]map[string]publishPosition{
	kindToken: {
		"scopes":        {answer: vocabulary.PublishMoves, names: publishScope},
		"agents/agent":  moveName,
		"agents/reads":  movePattern,
		"agents/writes": movePattern,
	},
	typeTrigger: {
		"source/record/kinds": movePattern,
		// A guard is code the substrate runs, so the kinds it spells move.
		"when": moveCodeIn,
		// Arguments are data handed to a callable: a string in them may be
		// a name of this repository's or anybody's key.
		"arguments": {answer: vocabulary.PublishHistory, listed: true, why: "data the trigger hands its callable, the author's to read: listed in the plan where it names the package"},
	},
	typeTriggerRun: {
		recordWhole: {answer: vocabulary.PublishHistory, why: "a run says what ran, under the name it ran by"},
	},
	vocabulary.KindRecordPatchPolicy: {
		"selector/kinds": movePattern,
		// An agent's or a function's name: a gate on the package's agent
		// keeps holding it under the published name.
		"selector/agents":    moveName,
		"selector/functions": moveName,
	},
	vocabulary.KindRecordPatchRequest: {
		"targetKind":   moveName,
		"diff":         moveRefsIn,
		"adjustedDiff": moveRefsIn,
	},
	vocabulary.CoreKind("consolepreference"): {
		"collapsed":        moveName,
		"favorites":        moveName,
		"views/collection": moveName,
	},
	vocabulary.KindLLMApproval: {
		"writes": movePattern,
		// The call as the model asked it: an approval still to run hands
		// these to the function when it is approved (approvals.go), so one
		// that names the package stops the publish until it is decided.
		"arguments": {
			answer: vocabulary.PublishHistory, open: approvalOpen,
			why: "the call as the model asked it, which an approval still pending, approved or running hands to the function: decide it, or let it run, before publishing",
		},
		"output": {answer: vocabulary.PublishHistory, why: "what the call returned"},
	},
	kindRole: {
		"rules/kinds": {answer: vocabulary.PublishMoves, names: publishRolePattern},
	},
	typeMessage: {
		recordWhole: {answer: vocabulary.PublishHistory, why: transcript},
	},
	vocabulary.PackageConsole + "/collectionview": {
		"where": moveRefsIn,
	},
	kindSetting: {
		recordID: {answer: vocabulary.PublishStops, why: stopSettings},
		"value":  {answer: vocabulary.PublishHistory, why: "a setting's value is its writer's, read as the package that declares it reads it"},
	},
	kindSecret: {recordID: {answer: vocabulary.PublishStops, why: stopSettings}},
	// The record of a hand that wrote as the package in `~`: its name is the
	// hand's key (vocabulary.ActorKeyFrom), its writes before the publish
	// link it, and nothing writes as it again (actors.go ensureActor).
	kindActor: {
		recordWhole: {answer: vocabulary.PublishHistory, why: "the record of a hand that wrote under the package's name in ~, which the writes before the publish link"},
	},
}

// publishFrozen are the kinds whose envelope no write changes once written
// (approvals.go guardApproval, write.go guardImmutableEnvelope): their names
// move through the fold.
var publishFrozen = map[string]bool{
	vocabulary.KindLLMApproval: true, vocabulary.KindRecordPatchRequest: true,
}

// approvalOpen reports whether an approval's call is still to run.
func approvalOpen(row *erow) bool {
	switch row.States[propApprovalState] {
	case approvalPending, approvalApproved, approvalRunning:
		return true
	}
	return false
}

func publishName(r *publishRename, s string) []string {
	return []string{vocabulary.PublishedName(s, r.pkg, r.authority)}
}

// publishPattern is a kind pattern moved, and the published package's glob
// beside a pattern over every package of `~`.
func publishPattern(r *publishRename, s string) []string {
	if beside := vocabulary.PublishedBeside(s, r.pkg, r.authority); beside != "" {
		return []string{s, beside}
	}
	return publishName(r, s)
}

// publishScope is a token's scope (`read:~/recipes/*`, a trait's
// `read:implements(~/recipes/searchable)`) with its pattern moved, and the
// same verb over the published package beside one over every package of `~`.
func publishScope(r *publishRename, s string) []string {
	parsed, err := scope.Parse(s)
	if err != nil {
		return []string{s}
	}
	if trait, ok := parsed.Trait(); ok {
		parsed.Pattern = scope.TraitPattern(vocabulary.PublishedRef(trait, r.pkg, r.authority))
		return []string{parsed.String()}
	}
	if beside := vocabulary.PublishedBeside(parsed.Pattern, r.pkg, r.authority); beside != "" {
		other := parsed
		other.Pattern = beside
		return []string{s, other.String()}
	}
	parsed.Pattern = vocabulary.PublishedRef(parsed.Pattern, r.pkg, r.authority)
	return []string{parsed.String()}
}

// publishRolePattern is a role's kind pattern, read as a scope's is.
func publishRolePattern(r *publishRename, s string) []string {
	moved := publishScope(r, string(scope.Read)+":"+s)
	out := make([]string, 0, len(moved))
	for _, m := range moved {
		out = append(out, strings.TrimPrefix(m, string(scope.Read)+":"))
	}
	return out
}

// publishRefsIn is a json value with every reference value in it at a record
// of a moved kind, or at a declaration of the package, moved.
func publishRefsIn(r *publishRename, v any) (any, bool) {
	changed := false
	for _, old := range sortedKeys(r.kinds) {
		if next, c := repointValue(v, old, r.kinds[old], nil); c {
			v, changed = next, true
		}
	}
	if next, c := publishedDeclarationRefs(v, r.from, r.to); c {
		v, changed = next, true
	}
	return v, changed
}

// publishCodeIn is a value whose strings are code, with the kind references
// they spell moved by the rule a declaration's code moves by.
func publishCodeIn(r *publishRename, v any) (any, bool) {
	switch x := v.(type) {
	case string:
		moved := vocabulary.PublishedCode(x, r.pkg, r.authority, r.spellings...)
		return moved, moved != x
	case []any:
		out, changed := make([]any, len(x)), false
		for i, e := range x {
			var c bool
			out[i], c = publishCodeIn(r, e)
			changed = changed || c
		}
		return out, changed
	case map[string]any:
		out, changed := make(map[string]any, len(x)), false
		for k, e := range x {
			var c bool
			out[k], c = publishCodeIn(r, e)
			changed = changed || c
		}
		return out, changed
	}
	return v, false
}

// publishAt applies a MOVES position p at path in v, copying what it
// changes: objects by key, lists transparently, and at the end a string, a
// list of strings (each into its names, kept once) or, for p.value, the
// value whole.
func publishAt(r *publishRename, v any, path []string, p publishPosition) (any, bool) {
	if len(path) == 0 && p.value != nil {
		return p.value(r, v)
	}
	switch x := v.(type) {
	case string:
		if len(path) == 0 && p.names != nil {
			names := p.names(r, x)
			return names[0], names[0] != x
		}
	case []any:
		if len(path) == 0 && p.names != nil {
			out, changed := make([]any, 0, len(x)), false
			for _, e := range x {
				s, ok := e.(string)
				if !ok {
					out = append(out, e)
					continue
				}
				for _, n := range p.names(r, s) {
					if n != s {
						changed = true
					}
					if !slices.Contains(out, any(n)) {
						out = append(out, n)
					}
				}
			}
			return out, changed
		}
		out, changed := make([]any, len(x)), false
		for i, e := range x {
			var c bool
			out[i], c = publishAt(r, e, path, p)
			changed = changed || c
		}
		return out, changed
	case map[string]any:
		if len(path) == 0 {
			return v, false
		}
		inner, ok := x[path[0]]
		if !ok {
			return v, false
		}
		next, changed := publishAt(r, inner, path[1:], p)
		if !changed {
			return v, false
		}
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = e
		}
		out[path[0]] = next
		return out, true
	}
	return v, false
}

// namesPackage reports whether a string names the package from, under `~`.
func (r *publishRename) namesPackage(s string) bool {
	return vocabulary.PublishedName(s, r.pkg, r.authority) != s
}

// valuesAt are the values at path in v, lists walked through.
func valuesAt(v any, path []string) []any {
	if len(path) == 0 {
		return []any{v}
	}
	switch x := v.(type) {
	case map[string]any:
		if inner, ok := x[path[0]]; ok {
			return valuesAt(inner, path[1:])
		}
	case []any:
		var out []any
		for _, e := range x {
			out = append(out, valuesAt(e, path)...)
		}
		return out
	}
	return nil
}

// publishRecordNames moves the names the live records hold, the triggers'
// alone (before the batch's guards count them) or every other record's
// (after its projection, so the declarations a moved name points at exist),
// and refuses the publish on a STOPS position that names the package,
// listing every record it found.
func (t *txn) publishRecordNames(r *publishRename, triggers bool) error {
	visit, err := t.publishRecords(r, triggers)
	if err != nil {
		return err
	}
	var stops []string
	for _, ref := range visit {
		positions := publishRecordPositions[ref.Kind]
		if p, ok := positions[recordID]; ok && p.answer == vocabulary.PublishStops && r.namesPackage(ref.ID) {
			stops = append(stops, fmt.Sprintf("%s: %s", vocabulary.RecordPath(ref.Kind, ref.ID), p.why))
			continue
		}
		row, err := t.loadRow(ref, true)
		if err != nil {
			return err
		}
		if row == nil || row.DeletedAt != nil {
			continue
		}
		changed := map[string]any{}
		for _, path := range sortedKeys(positions) {
			p := positions[path]
			if p.answer == vocabulary.PublishHistory && (p.listed || p.open != nil) && path != recordWhole && path != recordID {
				names := false
				for _, v := range valuesAt(row.Props, strings.Split(path, "/")) {
					names = names || vocabulary.NamesPackage(v, r.pkg, r.authority, r.spellings)
				}
				switch {
				case !names:
				case p.open != nil && p.open(row):
					stops = append(stops, fmt.Sprintf("%s: %s", vocabulary.RecordPath(ref.Kind, ref.ID), p.why))
				case p.listed:
					r.texts = append(r.texts, vocabulary.RecordPath(ref.Kind, ref.ID))
				}
			}
			if p.answer != vocabulary.PublishMoves {
				continue
			}
			parts := strings.Split(path, "/")
			v, ok := row.Props[parts[0]]
			if prior, held := changed[parts[0]]; held {
				v, ok = prior, true
			}
			if !ok {
				continue
			}
			if next, c := publishAt(r, v, parts[1:], p); c {
				changed[parts[0]] = next
			}
		}
		// Every reference value at a declaration of the package, wherever
		// the record holds one.
		for name, v := range row.Props {
			if prior, held := changed[name]; held {
				v = prior
			}
			if next, c := publishedDeclarationRefs(v, r.from, r.to); c {
				changed[name] = next
			}
		}
		if len(changed) == 0 {
			continue
		}
		if err := t.writeRenamed(ref, row, changed, r); err != nil {
			return fmt.Errorf("substrate/engine: publish: %s: %w", vocabulary.RecordPath(ref.Kind, ref.ID), err)
		}
	}
	if !triggers {
		parked, err := t.parkedOnPackage(r)
		if err != nil {
			return err
		}
		stops = append(stops, parked...)
	}
	if len(stops) > 0 {
		sort.Strings(stops)
		return fmt.Errorf("%w: %s", substrate.ErrGuard, strings.Join(stops, "; "))
	}
	return nil
}

// parkedOnPackage are the parked deliveries of a line of one of the
// package's kinds: a retry replays the line as it was written, under the
// kind it names, which the publish takes away, so each stops the publish
// until it is retried or forgotten.
func (t *txn) parkedOnPackage(r *publishRename) ([]string, error) {
	kinds := sortedKeys(r.kinds)
	rows, err := t.query(`SELECT f.trigger_id, c.kind, c.record_id FROM trigger_failures f
		JOIN changelog c ON c.seq = f.seq
		WHERE f.fire_id = '' AND c.kind = ANY($1::text[])
		ORDER BY f.trigger_id, c.kind, c.record_id`, kinds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var trigger, kind, id string
		if err := rows.Scan(&trigger, &kind, &id); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("trigger %s holds a parked delivery of %s, which a retry replays under the kind it names: retry it or forget it before publishing",
			trigger, vocabulary.RecordPath(kind, id)))
	}
	return out, rows.Err()
}

// publishRecords are the live records a publish reads for names: every
// record of a kind the table answers for, but a HISTORY one, and every
// record with a reference value at a declaration of the package, each once,
// in a fixed order.
func (t *txn) publishRecords(r *publishRename, triggers bool) ([]eref, error) {
	keep := func(kind string) bool {
		if (kind == typeTrigger) != triggers {
			return false
		}
		p, whole := publishRecordPositions[kind][recordWhole]
		return !whole || p.answer != vocabulary.PublishHistory
	}
	seen := map[eref]bool{}
	var out []eref
	add := func(ref eref) {
		if !seen[ref] && keep(ref.Kind) {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	for _, kind := range sortedKeys(publishRecordPositions) {
		if !keep(kind) {
			continue
		}
		ids, err := t.liveIDsOf(kind)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			add(eref{Kind: kind, ID: id})
		}
	}
	declarations := make([]string, 0, len(vocabularyRecordKinds))
	for k := range vocabularyRecordKinds {
		declarations = append(declarations, k)
	}
	rows, err := t.query(`SELECT DISTINCT r.src_kind, r.src FROM refs r
		JOIN records x ON x.kind = r.src_kind AND x.id = r.src
		WHERE x.deleted_at IS NULL AND r.dst_kind = ANY($1::text[])
		  AND (r.dst = $2 OR starts_with(r.dst, $3))
		ORDER BY r.src_kind, r.src`, declarations, r.from, r.from+"/")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var src eref
		if err := rows.Scan(&src.Kind, &src.ID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		add(src)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	return out, rows.Close()
}

// writeRenamed writes the moved names of one record: through the fold for a
// frozen kind, as the move's own repoint is (move.go repointRecord), and as
// a patch otherwise, so a trigger's cursor is pinned and a token's cache
// fenced as at any edit. Either line says `repointed`.
func (t *txn) writeRenamed(ref eref, row *erow, changed map[string]any, r *publishRename) error {
	// The engine's hand whatever its caller's: a rename says the same thing
	// under the name the publish gave it, decided by the person the publish
	// is held to (mayPublish).
	prev := t.internal
	t.internal = true
	defer func() { t.internal = prev }()
	moved := map[string]string{r.from: r.to}
	if !publishFrozen[ref.Kind] {
		return t.repointWith(moved, func() error {
			_, err := t.patch(ref, substrate.PatchInput{Properties: changed})
			return err
		})
	}
	before := row.clone()
	names := sortedKeys(changed)
	for _, name := range names {
		if reflect.DeepEqual(row.Props[name], changed[name]) {
			continue
		}
		row.Props[name] = changed[name]
	}
	if _, err := t.foldRow(before, row, false, false); err != nil {
		return err
	}
	return t.appendChange(t.actor, substrate.OpPatch, ref.ID, ref.Kind, map[string]any{
		"properties": names,
		"repointed":  moved,
	})
}
