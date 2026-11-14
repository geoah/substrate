package engine

// THE REPOSITORY'S OWN HANDS, MOVED TO `~` (decision record 0259).
//
// A package of the repository's own, and each of its functions and agents,
// writes with a hand that carries its package's authority as the package
// spells it: `bundle:~:tasks`, `function:~:tasks:remind`. History written
// before held them under the repository's name, `bundle:ada.example.com:tasks`,
// and since actors are records (decision 0270) a line names a hand by the id
// of its actor record, which is derived from the hand. So the conversion maps
// each such id to the id of the same hand under `~` (actorMovesOf) and
// rewrites it where it stands. The map is read from the history alone: every
// package, function and agent its declarations ever named under the name,
// whether or not it still exists, and every actor record it holds whose hand
// carries the name. An id at one of the positions below whose hand carries the
// name and that the map does not hold stops the conversion, naming the line.
//
// THE POSITIONS. An id is rewritten where the engine wrote one, and nowhere
// else: a person's own text, a judge's input, a callable's result and a
// message the engine composed stay byte for byte.
//
//   - The line's `actor` and `onBehalfOf`.
//   - A bare id at a path actorIDPositions lists, by where it sits: a mark's
//     hands (decision 0284), in a line, a resync's rows and a merge's moved
//     marks.
//   - A reference value at an actor record (`{"ref": "…/core/actor/<id>"}`)
//     at a path actorRefPositions lists, or wherever the declaration of its
//     record's kind in force at the line puts a reference.
//   - An actor record's own effects: its id, its `name` and its title, which
//     name the hand's package or callable; its link to that package,
//     function or agent is a reference, which the namespace rule moves.
//   - A payload a record keeps of its own (actorPayloads), read as a line's.

import (
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// actorIDPositions are the paths at which a line holds an actor record's id as
// a bare string: in a payload, under an effect of a kind, and in what a record
// of a kind sets. A path's keys are joined by "/"; it walks into objects by key
// and through lists, and "*" into every value of an object. A new position is
// one line here.
var actorIDPositions = map[string][]string{
	"payload":       {"triggeredBy", "moved/marks/setBy"},
	"mark":          {"setBy", "onBehalfOf"},
	"resync":        {"rows/marks/setBy", "rows/marks/onBehalfOf"},
	kindRecordMerge: {"moved/marks/setBy"},
}

// actorRefPositions are the properties at which a record of a kind holds a
// reference value at an actor record whatever its declaration said at the
// line: the links decision 0270 wrote into history under a declaration that
// still read them as text. A new position is one line here.
var actorRefPositions = map[string][]string{
	kindPackage:    {substrate.PackageDeclaredBy},
	kindBlob:       {blobPropCreatedBy},
	typeTriggerRun: {"caller"},
}

// actorPayloads are the properties of a record of a kind that keep a payload
// of their own, its effects included: a split's `result`.
var actorPayloads = map[string]string{kindRecordSplit: "result"}

// actorMoves maps the id of every actor record whose hand carries the
// repository's name, home, to the id of the same hand under `~`.
type actorMoves struct {
	home  string
	moves map[string]string
	// known is the hand behind every actor record the history holds.
	known map[string]substrate.Actor
}

// localHand is key with `~` in place of home, and whether key is a hand of
// the repository's own spelled with its name.
func localHand(key substrate.Actor, home string) (substrate.Actor, bool) {
	s := string(key)
	for _, prefix := range []string{substrate.BundleActorPrefix, substrate.FunctionActorPrefix, substrate.AgentActorPrefix} {
		rest, ok := strings.CutPrefix(s, prefix)
		if !ok {
			continue
		}
		authority, tail, ok := strings.Cut(rest, ":")
		if !ok || authority != home || home == "" {
			return "", false
		}
		return substrate.Actor(prefix + vocabulary.Local + ":" + tail), true
	}
	return "", false
}

// actorMovesOf builds the map from what the history holds: declarations,
// every version of every declaration record, and actors, every version of
// every actor record.
func actorMovesOf(home string, declarations, actors []declarationVersion) actorMoves {
	m := actorMoves{home: home, moves: map[string]string{}, known: map[string]substrate.Actor{}}
	add := func(key substrate.Actor) {
		if local, ok := localHand(key, home); ok {
			m.moves[vocabulary.ActorID(key)] = vocabulary.ActorID(local)
		}
	}
	for _, v := range declarations {
		authority, rest, ok := strings.Cut(v.ID, "/")
		if !ok || authority != home {
			continue
		}
		pkg, name, callable := strings.Cut(rest, "/")
		switch {
		case v.Ref == kindPackage && !callable:
			add(substrate.BundleActor(home, pkg))
		case v.Ref == kindFunction && callable:
			add(substrate.FunctionActor(home, pkg, name))
		case v.Ref == kindAgent && callable:
			add(substrate.AgentActor(home, pkg, name))
		}
	}
	for _, v := range actors {
		if v.Deleted {
			continue
		}
		if key, ok := vocabulary.ActorKeyFrom(v.Properties); ok {
			m.known[v.ID] = key
			add(key)
		}
	}
	return m
}

// id is where an actor record's id moves, at line seq.
func (m actorMoves) id(seq int64, id string) (string, error) {
	if to, ok := m.moves[id]; ok {
		return to, nil
	}
	key, ok := m.known[id]
	if !ok {
		key, ok = vocabulary.ActorKeyOf(id)
	}
	if ok {
		if _, local := localHand(key, m.home); local {
			return "", fmt.Errorf("seq %d names actor record %s, the hand %s of the repository's own, and the history declares no package, function or agent it is the hand of", seq, id, key)
		}
	}
	return id, nil
}

// path is a reference path with the actor record it names moved.
func (m actorMoves) path(seq int64, path string) (string, error) {
	id, ok := strings.CutPrefix(path, kindActor+"/")
	if !ok {
		return path, nil
	}
	to, err := m.id(seq, id)
	return kindActor + "/" + to, err
}

// handName is an actor record's `name` or title under `~`: the package or
// callable it names, spelled with the repository's name before.
func (m actorMoves) handName(s string) string {
	if rest, ok := strings.CutPrefix(s, m.home+"/"); ok && m.home != "" {
		return vocabulary.Local + "/" + rest
	}
	return s
}

// actorMover rewrites one line's actor ids.
type actorMover struct {
	moves actorMoves
	refs  referencePositions
	seq   int64
	txn   int64
	err   error
}

func (a *actorMover) fail(err error) {
	if a.err == nil {
		a.err = err
	}
}

// at rewrites the strings at path in v by fold.
func (a *actorMover) at(v any, path string, fold func(string) (string, error)) any {
	switch x := v.(type) {
	case []any:
		for i, y := range x {
			x[i] = a.at(y, path, fold)
		}
		return x
	case map[string]any:
		if path == "" {
			return x
		}
		key, rest, _ := strings.Cut(path, "/")
		if key == "*" {
			for k, y := range x {
				x[k] = a.at(y, rest, fold)
			}
			return x
		}
		if y, ok := x[key]; ok {
			x[key] = a.at(y, rest, fold)
		}
		return x
	case string:
		if path != "" {
			return x
		}
		out, err := fold(x)
		if err != nil {
			a.fail(err)
			return x
		}
		return out
	}
	return v
}

func (a *actorMover) ids(v any, where string) {
	for _, p := range actorIDPositions[where] {
		a.at(v, p, func(s string) (string, error) { return a.moves.id(a.seq, s) })
	}
}

// refValue moves the actor record a reference value names.
func (a *actorMover) refValue(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	if path, ok := m[vocabulary.ReferenceValueKey].(string); ok {
		to, err := a.moves.path(a.seq, path)
		if err != nil {
			a.fail(err)
			return v
		}
		m[vocabulary.ReferenceValueKey] = to
	}
	return m
}

// payload rewrites a payload: the line's, or one a record keeps.
func (a *actorMover) payload(p map[string]any) {
	a.ids(p, "payload")
	effects, _ := p[foldPayloadKey].([]any)
	for _, raw := range effects {
		ef, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := ef["kind"].(string)
		a.ids(ef, kind)
		ref, _ := ef["ref"].(string)
		if ref == kindActor {
			a.at(ef, "id", func(s string) (string, error) { return a.moves.id(a.seq, s) })
		}
		for _, list := range []string{"scope"} {
			a.addresses(ef[list])
		}
		if rows, ok := ef["rows"].(map[string]any); ok {
			for _, key := range []string{"annotations", "marks"} {
				a.addresses(rows[key])
			}
		}
		delta, _ := ef["delta"].(map[string]any)
		if ref == kindActor && delta != nil {
			if title, ok := delta["title"].(string); ok {
				delta["title"] = a.moves.handName(title)
			}
		}
		set, _ := delta["set"].(map[string]any)
		if set == nil {
			continue
		}
		a.record(ref, set)
	}
}

// addresses moves the actor record a row of a list is about.
func (a *actorMover) addresses(v any) {
	list, _ := v.([]any)
	for _, raw := range list {
		if row, ok := raw.(map[string]any); ok && row["kind"] == kindActor {
			a.at(row, "id", func(s string) (string, error) { return a.moves.id(a.seq, s) })
		}
	}
}

// record rewrites what a record of kind sets.
func (a *actorMover) record(kind string, set map[string]any) {
	a.ids(set, kind)
	for _, p := range actorRefPositions[kind] {
		a.refAt(set, p)
	}
	if positions, err := a.refs.at(kind, a.seq, a.txn); err == nil {
		for name, decl := range positions {
			if v, ok := set[name]; ok {
				set[name] = localizeReferencesWith(decl, v, func(path string) string {
					to, err := a.moves.path(a.seq, path)
					if err != nil {
						a.fail(err)
						return path
					}
					return to
				})
			}
		}
	}
	if kind == kindActor {
		if name, ok := set["name"].(string); ok {
			set["name"] = a.moves.handName(name)
		}
	}
	if field, ok := actorPayloads[kind]; ok {
		if inner, ok := set[field].(map[string]any); ok {
			a.payload(inner)
		}
	}
}

// refAt moves the reference values at path in v.
func (a *actorMover) refAt(v any, path string) any {
	switch x := v.(type) {
	case []any:
		for i, y := range x {
			x[i] = a.refAt(y, path)
		}
		return x
	case map[string]any:
		if path == "" {
			return a.refValue(x)
		}
		key, rest, _ := strings.Cut(path, "/")
		if y, ok := x[key]; ok {
			x[key] = a.refAt(y, rest)
		}
		return x
	}
	return v
}
