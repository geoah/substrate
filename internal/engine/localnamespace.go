package engine

// THE LOCAL NAMESPACE, IN HISTORY AND AT THE WRITE (decision record 0259).
//
// A repository's own packages sit under `~`, so a history written before held
// them under the repository's name: `ada.example.com/tasks/task` in a line's
// kind, in an effect's ref, in a reference value, in a declaration, in a
// token's scopes. The conversion moves each to `~` once: localizeLine rewrites
// one history line, and carryLocal the rows outside history that bind an own
// kind or a named record (a sealed value's binding, a vector's key). Both are
// idempotent: a line or a row already under `~` comes back unchanged.
//
// A string moves because of what the engine wrote it as, never because of
// what it looks like:
//
//   - A kind, a record id that is a name (vocabulary.NamedID: a declaration's,
//     a setting's, a secret's), and the kinds and ids an effect names.
//   - A reference value, at a position a declaration of its kind makes a
//     reference (referencePositions), in a record of any kind.
//   - A declaration's name keys (vocabulary.LocalizeDeclaration).
//   - The named positions of the substrate's own records (recordNamePositions):
//     a token's scopes, a trigger's source kinds, a policy's selector, an
//     approval's grant, and the rest of that table.
//
// Everything else stays as it was written: the person's own values, a `json`
// property's inside however it is shaped, a credential's bytes, a
// description, a prompt, a function's source, and actor texts
// (`bundle:ada.example.com:tasks`), which change in a step of their own.
// localizeRecord is the one rule the write path and the conversion share.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// namePosition is one property path of a record whose strings name a kind, a
// package or a callable, and how one such string folds. A path walks into
// objects by key and through lists transparently; at its end a string, or
// each string of a list, folds.
type namePosition struct {
	path []string
	fold func(s string, names ...string) string
}

// recordNamePositions are the properties of the substrate's own records that
// hold names the engine wrote, each read as its declaration describes it.
// Nothing else in those records is read as a name: a patch request's diff, a
// trigger's arguments and guard, an approval's arguments and output, a
// setting's or a secret's value are their writers'.
var recordNamePositions = map[string][]namePosition{
	kindToken: {
		{path: []string{"scopes"}, fold: localizeScope},
		{path: []string{"agents", "agent"}, fold: vocabulary.LocalizeRef},
		{path: []string{"agents", "reads"}, fold: vocabulary.LocalizeRef},
		{path: []string{"agents", "writes"}, fold: vocabulary.LocalizeRef},
	},
	typeTrigger: {
		{path: []string{"source", "record", "kinds"}, fold: vocabulary.LocalizeRef},
	},
	typeTriggerRun: {
		{path: []string{"callable"}, fold: vocabulary.LocalizeRef},
	},
	vocabulary.CoreKind("recordpatchpolicy"): {
		{path: []string{"selector", "kinds"}, fold: vocabulary.LocalizeRef},
		{path: []string{"selector", "agents"}, fold: vocabulary.LocalizeRef},
		{path: []string{"selector", "functions"}, fold: vocabulary.LocalizeRef},
	},
	vocabulary.CoreKind("recordpatchrequest"): {
		{path: []string{"targetKind"}, fold: vocabulary.LocalizeRef},
	},
	vocabulary.CoreKind("consolepreference"): {
		{path: []string{"collapsed"}, fold: localizeNamespace},
		{path: []string{"favorites"}, fold: vocabulary.LocalizeRef},
		{path: []string{"views", "collection"}, fold: vocabulary.LocalizeRef},
	},
	vocabulary.PackageLLM + "/approval": {
		{path: []string{"writes"}, fold: vocabulary.LocalizeRef},
	},
	// A role's rules name kinds as a scope does (decision 0275).
	kindRole: {
		{path: []string{"rules", "kinds"}, fold: localizePattern},
	},
	typeMessage: {
		// A tool call's callable is the record path of the function or agent
		// it called (decision 0270).
		{path: []string{"callable"}, fold: vocabulary.LocalizePath},
		{path: []string{"toolCalls", "callable"}, fold: vocabulary.LocalizePath},
		{path: []string{"changes", "kind"}, fold: vocabulary.LocalizeRef},
		{path: []string{"contextRecords", "kind"}, fold: vocabulary.LocalizeRef},
		{path: []string{"mentions", "kind"}, fold: vocabulary.LocalizeRef},
		{path: []string{"subagentWrites", "kinds"}, fold: vocabulary.LocalizeRef},
	},
}

// localizeScope is one scope of a token, `<verb>:<pattern>`, its pattern
// folded (`read:~/tasks/*`); what does not parse as a scope stays.
func localizeScope(s string, names ...string) string {
	sc, err := scope.Parse(s)
	if err != nil {
		return s
	}
	return sc.Localize(names...).String()
}

// localizePattern is a kind pattern as a scope names it (a reference, a glob
// or a trait), folded as a scope's is (scope.Scope.Localize).
func localizePattern(s string, names ...string) string {
	if sc, err := scope.Parse(string(scope.Read) + ":" + s); err == nil {
		return sc.Localize(names...).Pattern
	}
	return vocabulary.LocalizeRef(s, names...)
}

// localizeNamespace is an authority or a package reference: the name alone is
// the authority of the repository's own packages, which is `~`.
func localizeNamespace(s string, names ...string) string {
	if s != "" && slices.Contains(names, s) {
		return vocabulary.Local
	}
	return vocabulary.LocalizeRef(s, names...)
}

// localizeRecord is props, a record of kind's properties, with the
// repository's names (its name, then the former names it still answers to)
// folded into `~` where the engine wrote a name: a
// declaration's name keys, or the kind's named positions. It is the rule the
// write path and the conversion share; a reference value is the reference
// fold's (normalizeReferencesIn at the write, referencePositions in the
// conversion).
func localizeRecord(kind string, props map[string]any, names ...string) (map[string]any, error) {
	if len(names) == 0 || names[0] == "" || props == nil {
		return props, nil
	}
	if _, declaration := vocabularyRecordKinds[kind]; declaration {
		v, err := vocabulary.LocalizeDeclaration(kind, props, names...)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", substrate.ErrValidation, err)
		}
		out, _ := v.(map[string]any)
		return out, nil
	}
	positions := recordNamePositions[kind]
	if len(positions) == 0 {
		return props, nil
	}
	var v any = props
	for _, p := range positions {
		v = localizeAt(v, p.path, func(s string) string { return p.fold(s, names...) })
	}
	out, _ := v.(map[string]any)
	return out, nil
}

// localizeCopied is a record a sample's closure carries beside its
// declarations (a trigger, a setting, a record of its own kinds), copied out
// of the sample's authority, from, into `~` (decision records 0048 and 0259).
// from is read where the write reads a name: the record's kind, its id where
// a reference names it (NamedID), the substrate's own named positions
// (recordNamePositions) and the reference properties its kind declares in the
// candidate registry, the copied closure's own kinds included. Every other
// string is the sample author's and stays as written.
func (t *txn) localizeCopied(in substrate.PutInput, from string) (substrate.PutInput, error) {
	in.Kind = vocabulary.LocalizeRef(in.Kind, from)
	in.ID = vocabulary.LocalizeID(in.Kind, in.ID, from)
	props, err := localizeRecord(in.Kind, in.Properties, from)
	if err != nil {
		return in, err
	}
	if ty, err := t.resolveType(in.Kind); err == nil && props != nil {
		out := make(map[string]any, len(props))
		for name, v := range props {
			if p, ok := ty.Props[name]; ok {
				v = localizeAuthoredReferences(p, v, from)
			}
			out[name] = v
		}
		props = out
	}
	in.Properties = props
	return in, nil
}

// localizeAuthoredReferences is v, a value as a closure's author wrote it for
// property p, with from folded into `~` in every reference p declares: the
// value itself, a list's items, a keyed map's values, an object's declared
// fields, as a "<kind>/<id>" path or a `{"ref": …}` object.
func localizeAuthoredReferences(p *vocabulary.Property, v any, from string) any {
	one := func(e any) any {
		switch {
		case p.Datatype == vocabulary.DatatypeReference:
			if s, ok := e.(string); ok {
				return vocabulary.LocalizePath(s, from)
			}
			if m, ok := e.(map[string]any); ok {
				if s, ok := m[vocabulary.ReferenceValueKey].(string); ok {
					out := make(map[string]any, len(m))
					for k, x := range m {
						out[k] = x
					}
					out[vocabulary.ReferenceValueKey] = vocabulary.LocalizePath(s, from)
					return out
				}
			}
		case len(p.Fields) > 0:
			if m, ok := e.(map[string]any); ok {
				out := make(map[string]any, len(m))
				for k, x := range m {
					if f, ok := p.Fields[k]; ok {
						x = localizeAuthoredReferences(f, x, from)
					}
					out[k] = x
				}
				return out
			}
		}
		return e
	}
	switch {
	case p.Keyed:
		if m, ok := v.(map[string]any); ok {
			out := make(map[string]any, len(m))
			for k, e := range m {
				out[k] = one(e)
			}
			return out
		}
	case p.Repeated:
		if list, ok := v.([]any); ok {
			out := make([]any, len(list))
			for i, e := range list {
				out[i] = one(e)
			}
			return out
		}
	}
	return one(v)
}

// localizeAt is v with fold applied at path, copying what it changes.
func localizeAt(v any, path []string, fold func(string) string) any {
	switch t := v.(type) {
	case string:
		if len(path) == 0 {
			return fold(t)
		}
		return t
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = localizeAt(val, path, fold)
		}
		return out
	case map[string]any:
		if len(path) == 0 {
			return t
		}
		val, ok := t[path[0]]
		if !ok {
			return t
		}
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = x
		}
		out[path[0]] = localizeAt(val, path[1:], fold)
		return out
	default:
		return v
	}
}

// referencePositions say where a record of a kind holds references: per kind
// (and per trait a kind binds), every version of its declaration the history
// holds, in seq order, keyed by the kind as the converted line names it.
type referencePositions struct {
	kinds, traits map[string][]declaredReferences
}

// declaredReferences is one version of one kind's or trait's declaration:
// the line that wrote it, whether it is a deletion, the properties it
// declares, those of them that hold a reference anywhere inside them, and the
// traits it binds.
type declaredReferences struct {
	seq      int64
	deleted  bool
	declared map[string]bool
	props    map[string]map[string]any
	traits   []string
}

// referencePositionsOf indexes the repository's declarations as the
// conversion runner hands them (declarationVersion: every version of every
// declaration record the history holds, in seq order, in the dialect the
// step reads its lines in, so under the repository's name, home, not yet
// `~`). It is the one place that reads them: a different shape from the
// runner costs this function alone. A version's properties are read raw,
// so one an older release wrote and this binary's vocabulary no longer
// parses (Unread) still says where its references sit.
func referencePositionsOf(versions []declarationVersion, home string) referencePositions {
	out := referencePositions{kinds: map[string][]declaredReferences{}, traits: map[string][]declaredReferences{}}
	for _, v := range versions {
		var into map[string][]declaredReferences
		switch v.Ref {
		case kindKind:
			into = out.kinds
		case kindTrait:
			into = out.traits
		default:
			continue
		}
		id := vocabulary.LocalizeRef(v.ID, home)
		d := declaredReferences{seq: v.Seq, deleted: v.Deleted, declared: map[string]bool{}, props: map[string]map[string]any{}}
		if !v.Deleted {
			props, _ := v.Properties["properties"].(map[string]any)
			for name, raw := range props {
				d.declared[name] = true
				if decl, ok := raw.(map[string]any); ok && declHoldsReference(decl) {
					d.props[name] = decl
				}
			}
			list, _ := v.Properties["traits"].([]any)
			for _, t := range list {
				if s, ok := t.(string); ok {
					name, _, _ := strings.Cut(s, "(")
					d.traits = append(d.traits, vocabulary.LocalizeRef(strings.TrimSpace(name), home))
				}
			}
		}
		into[id] = append(into[id], d)
	}
	return out
}

// governing is the version of a declaration that governs a line at seq whose
// transaction ends at txn. It is the one in force at the line, the latest
// version at or before seq that is no deletion. Where there is none, it is
// the first version written later in the line's own transaction, which
// declares the kind and writes its records at once: a repository's creation
// writes the core's declaration records before the core declares their
// kinds. No other version is read. One written after the line's
// transaction, or one a deletion ended before it, says nothing about what
// the line holds, and the answer is false.
func governing(versions []declaredReferences, seq, txn int64) (declaredReferences, bool) {
	inForce := -1
	for i, v := range versions {
		if v.seq > seq {
			break
		}
		inForce = i
	}
	if inForce >= 0 && !versions[inForce].deleted {
		return versions[inForce], true
	}
	for _, v := range versions[inForce+1:] {
		if v.seq > txn {
			break
		}
		if !v.deleted {
			return v, true
		}
	}
	return declaredReferences{}, false
}

// lastDeclared is where a record of kind holds references by the last
// version of its declaration that is no deletion: what a record stored
// outside history, which carries no line, is read by. A kind never declared
// holds none.
func (rp referencePositions) lastDeclared(kind string) map[string]map[string]any {
	versions := rp.kinds[kind]
	for i := len(versions) - 1; i >= 0; i-- {
		if versions[i].deleted {
			continue
		}
		out, err := rp.at(kind, versions[i].seq, versions[i].seq)
		if err != nil {
			return nil
		}
		return out
	}
	return nil
}

// at is where a record of kind written at seq, in a transaction ending at
// txn, holds references: by the declaration of its kind that governs the
// line, and by those of the traits it binds that govern the line for the
// properties the kind does not declare itself. A line no declaration
// governs is refused, naming the kind and the seq: nothing is guessed.
func (rp referencePositions) at(kind string, seq, txn int64) (map[string]map[string]any, error) {
	d, ok := governing(rp.kinds[kind], seq, txn)
	if !ok {
		return nil, fmt.Errorf("seq %d writes a record of %s, and no declaration of %s governs it: none was in force at that line and none was written in its transaction", seq, kind, kind)
	}
	out := make(map[string]map[string]any, len(d.props))
	for name, decl := range d.props {
		out[name] = decl
	}
	for _, t := range d.traits {
		tv, ok := governing(rp.traits[t], seq, txn)
		if !ok {
			return nil, fmt.Errorf("seq %d writes a record of %s, which binds %s, and no declaration of %s governs it there", seq, kind, t, t)
		}
		for name, decl := range tv.props {
			if !d.declared[name] {
				out[name] = decl
			}
		}
	}
	return out, nil
}

// declHoldsReference reports whether a property declaration holds a
// reference anywhere inside it: it is one, or an object field holds one.
func declHoldsReference(decl map[string]any) bool {
	if t, _ := decl["type"].(string); t == string(vocabulary.DatatypeReference) {
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

// localizeReferences is v, a stored value of a property declared decl, with
// every reference value inside it folded: where the declaration puts a
// reference (the value itself, a list's items, a keyed map's values, an
// object's declared fields), a `{"ref": "<path>"}` object's path. Nothing
// the declaration does not make a reference is read.
func localizeReferences(decl map[string]any, v any, home string) any {
	return localizeReferencesWith(decl, v, func(path string) string { return vocabulary.LocalizePath(path, home) })
}

// localizeReferencesWith is localizeReferences with each reference's path
// rewritten by fold.
func localizeReferencesWith(decl map[string]any, v any, fold func(string) string) any {
	keyed, _ := decl["keyed"].(bool)
	repeated, _ := decl["repeated"].(bool)
	switch {
	case keyed:
		m, ok := v.(map[string]any)
		if !ok {
			return v
		}
		out := make(map[string]any, len(m))
		for k, e := range m {
			out[k] = localizeReferenceValue(decl, e, fold)
		}
		return out
	case repeated:
		list, ok := v.([]any)
		if !ok {
			return v
		}
		out := make([]any, len(list))
		for i, e := range list {
			out[i] = localizeReferenceValue(decl, e, fold)
		}
		return out
	default:
		return localizeReferenceValue(decl, v, fold)
	}
}

// localizeReferenceValue is one value of a declaration: the reference itself,
// or an object whose declared fields are read in turn.
func localizeReferenceValue(decl map[string]any, v any, fold func(string) string) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	if t, _ := decl["type"].(string); t == string(vocabulary.DatatypeReference) {
		path, ok := m[vocabulary.ReferenceValueKey].(string)
		if !ok {
			return v
		}
		out := make(map[string]any, len(m))
		for k, x := range m {
			out[k] = x
		}
		out[vocabulary.ReferenceValueKey] = fold(path)
		return out
	}
	fields, _ := decl["fields"].(map[string]any)
	if len(fields) == 0 {
		return v
	}
	out := make(map[string]any, len(m))
	for k, x := range m {
		if fd, ok := fields[k].(map[string]any); ok && declHoldsReference(fd) {
			out[k] = localizeReferencesWith(fd, x, fold)
			continue
		}
		out[k] = x
	}
	return out
}

// localizeLine returns the rewrite of one history line of the repository
// named home into the local namespace, reading where a record holds
// references from refs (referencePositionsOf): the declaration of its kind in
// force at the line. The repository's own hands move by moves (localhands.go).
func localizeLine(home string, refs referencePositions, moves actorMoves) func(e *changelogfile.Entry) error {
	return func(e *changelogfile.Entry) error {
		if home == "" {
			return nil
		}
		for _, id := range []*string{&e.Actor, &e.OnBehalfOf} {
			if *id == "" {
				continue
			}
			to, err := moves.id(e.Seq, *id)
			if err != nil {
				return err
			}
			*id = to
		}
		if e.Kind == kindActor {
			to, err := moves.id(e.Seq, e.RecordID)
			if err != nil {
				return err
			}
			e.RecordID = to
		}
		hands := &actorMover{moves: moves, refs: refs, seq: e.Seq, txn: e.Txn}
		// An authority row describes a publisher, and the local namespace has
		// none: the repository's own, written by an older copy of a sample,
		// stays as it is, held to its governing declaration as every line is.
		// Its hands move as every line's do.
		if e.Kind == kindAuthority {
			if err := governedAsWritten(e, refs); err != nil {
				return err
			}
			return rewritePayload(e, func(payload map[string]any) error {
				hands.payload(payload)
				return hands.err
			})
		}
		e.Kind = vocabulary.LocalizeRef(e.Kind, home)
		e.RecordID = vocabulary.LocalizeID(e.Kind, e.RecordID, home)
		if len(e.Payload) == 0 {
			return nil
		}
		var payload map[string]any
		dec := json.NewDecoder(strings.NewReader(string(e.Payload)))
		dec.UseNumber()
		if err := dec.Decode(&payload); err != nil {
			return fmt.Errorf("read the payload: %w", err)
		}
		// A payload the conversion moves nothing in keeps the bytes it had,
		// and so the checksum it had a right to.
		before, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("read the payload: %w", err)
		}
		lc := lineLocalizer{home: home, refs: refs, seq: e.Seq, txn: e.Txn}
		for _, key := range []string{payloadWinner, payloadLoser} {
			if id, ok := payload[key].(string); ok {
				payload[key] = vocabulary.LocalizeID(e.Kind, id, home)
			}
		}
		if effects, ok := payload["fold"].([]any); ok {
			out := make([]any, len(effects))
			for i, ef := range effects {
				var err error
				if out[i], err = lc.effect(ef); err != nil {
					return err
				}
			}
			payload["fold"] = out
		}
		hands.payload(payload)
		if hands.err != nil {
			return hands.err
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("write the payload: %w", err)
		}
		if !bytes.Equal(raw, before) {
			e.Payload = raw
		}
		return nil
	}
}

// rewritePayload runs rewrite over e's payload, and writes it back only where
// it moved something: a payload it leaves as it was keeps its bytes.
func rewritePayload(e *changelogfile.Entry, rewrite func(map[string]any) error) error {
	if len(e.Payload) == 0 {
		return nil
	}
	var payload map[string]any
	dec := json.NewDecoder(strings.NewReader(string(e.Payload)))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return fmt.Errorf("read the payload: %w", err)
	}
	before, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("read the payload: %w", err)
	}
	if err := rewrite(payload); err != nil {
		return err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("write the payload: %w", err)
	}
	if !bytes.Equal(raw, before) {
		e.Payload = raw
	}
	return nil
}

// governedAsWritten holds a line the conversion keeps as written to what it
// holds every line it rewrites to: each record its effects set has a
// declaration that governs it at that line (referencePositions.at).
func governedAsWritten(e *changelogfile.Entry, refs referencePositions) error {
	if len(e.Payload) == 0 {
		return nil
	}
	var payload struct {
		Fold []struct {
			Ref   string `json:"ref"`
			Delta struct {
				Set map[string]any `json:"set"`
			} `json:"delta"`
		} `json:"fold"`
	}
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return fmt.Errorf("read the payload: %w", err)
	}
	for _, ef := range payload.Fold {
		if len(ef.Delta.Set) == 0 {
			continue
		}
		if _, err := refs.at(ef.Ref, e.Seq, e.Txn); err != nil {
			return err
		}
	}
	return nil
}

// lineLocalizer folds the effects of one line (docs/stored-format.md, "The
// effects"), whose keys are closed: the record each lands on, the record
// properties a `record` effect sets, the records a `resync` names and the
// source records a mark is derived from. An annotation's value, a cursor, a
// parked failure and a paged drain's row name no kind.
type lineLocalizer struct {
	home     string
	refs     referencePositions
	seq, txn int64
}

func (lc lineLocalizer) effect(v any) (any, error) {
	ef, ok := v.(map[string]any)
	if !ok {
		return v, nil
	}
	kind, _ := ef["ref"].(string)
	kind = vocabulary.LocalizeRef(kind, lc.home)
	if _, has := ef["ref"]; has {
		ef["ref"] = kind
	}
	if id, ok := ef["id"].(string); ok {
		ef["id"] = vocabulary.LocalizeID(kind, id, lc.home)
	}
	// A line's title is never edited: a writer's is theirs, and one the
	// engine rendered from a template is what it rendered then. The current
	// title is rendered again once the history moved (localtitles.go).
	if delta, ok := ef["delta"].(map[string]any); ok {
		if set, ok := delta["set"].(map[string]any); ok && len(set) > 0 {
			localized, err := lc.properties(kind, set)
			if err != nil {
				return nil, err
			}
			delta["set"] = localized
		}
	}
	if list, ok := ef["scope"].([]any); ok {
		for _, x := range list {
			lc.address(x, "kind", "id")
		}
	}
	// A mark names the source records behind its value (decision 0284), a
	// record path each.
	lc.derivedFrom(ef)
	if rows, ok := ef["rows"].(map[string]any); ok {
		for _, key := range []string{"annotations", "marks"} {
			list, _ := rows[key].([]any)
			for _, x := range list {
				lc.address(x, "kind", "id")
				if key == "marks" {
					if m, ok := x.(map[string]any); ok {
						lc.derivedFrom(m)
					}
				}
			}
		}
		list, _ := rows["formerIds"].([]any)
		for _, x := range list {
			lc.address(x, "kind", "formerId", "recordId")
		}
	}
	return ef, nil
}

// derivedFrom folds the record paths of a mark's `derivedFrom`.
func (lc lineLocalizer) derivedFrom(m map[string]any) {
	list, ok := m["derivedFrom"].([]any)
	if !ok {
		return
	}
	for i, x := range list {
		if path, ok := x.(string); ok {
			list[i] = vocabulary.LocalizePath(path, lc.home)
		}
	}
}

// address folds a row's kind, and the ids beside it where that kind names
// its records.
func (lc lineLocalizer) address(v any, kindKey string, idKeys ...string) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	kind, _ := m[kindKey].(string)
	kind = vocabulary.LocalizeRef(kind, lc.home)
	if _, has := m[kindKey]; has {
		m[kindKey] = kind
	}
	for _, k := range idKeys {
		if id, ok := m[k].(string); ok {
			m[k] = vocabulary.LocalizeID(kind, id, lc.home)
		}
	}
}

// properties is a record effect's set: localizeRecord's names, then every
// reference value where the declaration of the kind governing the line puts
// one (referencePositions.at).
func (lc lineLocalizer) properties(kind string, set map[string]any) (map[string]any, error) {
	set, err := localizeRecord(kind, set, lc.home)
	if err != nil {
		return nil, err
	}
	positions, err := lc.refs.at(kind, lc.seq, lc.txn)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(set))
	for name, val := range set {
		if decl, ok := positions[name]; ok {
			val = localizeReferences(decl, val, lc.home)
		}
		out[name] = val
	}
	// A split keeps the payload it wrote (`result`), whose effects are a
	// line's own: they move as the line's do.
	if field, ok := actorPayloads[kind]; ok {
		if inner, ok := out[field].(map[string]any); ok {
			list, _ := inner["fold"].([]any)
			for i, ef := range list {
				if list[i], err = lc.effect(ef); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}

// resealFunc opens a sealed payload bound to one record and seals it again
// bound to another, under the repository's data key.
type resealFunc func(payload []byte, ref, fromKind, fromID, toKind, toID string) ([]byte, error)

// carryLocal moves the rows outside history that bind a record of an own kind,
// or a machinery record named under the repository's name, to the address
// its converted line names (localizeLine: the kind by LocalizeRef, the id by
// LocalizeID), through tx, which is scoped to the repository named home: a
// sealed value is sealed again bound to the new address (its binding names
// the kind and the id, decision 0023), and a vector, its queue row and a
// pending OAuth flow, kept by their record's kind and id, take it too, so the
// fold the conversion rebuilds still has them and a purge finds them. A row
// already at its address is left alone. In a repository restored without its
// secrets (leftBehind), a sealed value that does not open is one of them: it
// moves to its new binding as it is, where the open that clears it looks.
// A stored Idempotency-Key outcome that is a record, and the locator of one
// too large to store, read as the record reads now (carryOutcomes).
func carryLocal(ctx context.Context, tx *sql.Tx, home string, refs referencePositions, moves actorMoves, reseal resealFunc, leftBehind bool) error {
	if home == "" {
		return nil
	}
	prefix := home + "/"
	moved := func(kind, id string) (string, string, bool) {
		k, i := vocabulary.LocalizeRef(kind, home), vocabulary.LocalizeID(kind, id, home)
		return k, i, k != kind || i != id
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT ref, record_kind, record_id, payload FROM sealed WHERE starts_with(record_kind, $1) OR starts_with(record_id, $1) ORDER BY ref`, prefix)
	if err != nil {
		return fmt.Errorf("read the sealed values of own kinds: %w", err)
	}
	type sealedRow struct {
		ref, kind, id string
		payload       []byte
	}
	var held []sealedRow
	for rows.Next() {
		var r sealedRow
		if err := rows.Scan(&r.ref, &r.kind, &r.id, &r.payload); err != nil {
			_ = rows.Close()
			return err
		}
		held = append(held, r)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range held {
		kind, id, ok := moved(r.kind, r.id)
		if !ok {
			continue
		}
		payload, err := reseal(r.payload, r.ref, r.kind, r.id, kind, id)
		if errors.Is(err, errSealedUnopened) && leftBehind {
			payload, err = r.payload, nil
		}
		if err != nil {
			return fmt.Errorf("seal %s again under %s/%s: %w", r.ref, kind, id, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE sealed SET record_kind = $2, record_id = $3, payload = $4 WHERE ref = $1`, r.ref, kind, id, payload); err != nil {
			return fmt.Errorf("move sealed %s: %w", r.ref, err)
		}
	}
	for _, table := range []string{"embeddings", "embed_queue", "oauth_flows"} {
		if err := carryAddresses(ctx, tx, table, prefix, moved); err != nil {
			return fmt.Errorf("move %s to the local namespace: %w", table, err)
		}
	}
	return carryOutcomes(ctx, tx, home, refs, moves)
}

// recordOutcomes are the operations whose stored outcome is a record's wire
// form (keyedRecord).
var recordOutcomes = map[string]bool{
	string(idemCreate): true, string(idemPut): true, string(idemPatch): true,
	string(idemDelete): true, string(idemMerge): true, string(idemSplit): true,
}

// carryOutcomes moves every stored Idempotency-Key answer that names a record
// to the record as it reads now: a record outcome by localizeOutcome, a
// locator (the record an outcome too large to store was about) by its path.
// The key, its fingerprint, its owner and whether it settled stay, so a
// repeat is still answered from the row and never runs the write again. A
// callable's own output (`function.call`, `agent.call`) is the callable's
// data, read by no declaration, and stays as written.
func carryOutcomes(ctx context.Context, tx *sql.Tx, home string, refs referencePositions, moves actorMoves) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT operation, key, outcome, coalesce(locator, '') FROM idempotency_keys WHERE outcome IS NOT NULL OR locator IS NOT NULL ORDER BY operation, key`)
	if err != nil {
		return fmt.Errorf("read the stored Idempotency-Key outcomes: %w", err)
	}
	type keyed struct {
		op, key, locator string
		outcome          []byte
	}
	var held []keyed
	for rows.Next() {
		var k keyed
		if err := rows.Scan(&k.op, &k.key, &k.outcome, &k.locator); err != nil {
			_ = rows.Close()
			return err
		}
		held = append(held, k)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, k := range held {
		outcome, locator := k.outcome, k.locator
		if recordOutcomes[k.op] && outcome != nil {
			if outcome, err = localizeOutcome(outcome, home, refs, moves); err != nil {
				return fmt.Errorf("the stored outcome of %s key %q: %w", k.op, k.key, err)
			}
		}
		if locator != "" {
			if locator, err = moves.path(0, vocabulary.LocalizePath(locator, home)); err != nil {
				return fmt.Errorf("the locator of %s key %q: %w", k.op, k.key, err)
			}
		}
		if bytes.Equal(outcome, k.outcome) && locator == k.locator {
			continue
		}
		var at *string
		if locator != "" {
			at = &locator
		}
		if _, err := tx.ExecContext(ctx, `UPDATE idempotency_keys SET outcome = $3, locator = $4 WHERE operation = $1 AND key = $2`,
			k.op, k.key, outcome, at); err != nil {
			return fmt.Errorf("move the stored outcome of %s key %q: %w", k.op, k.key, err)
		}
	}
	return nil
}

// localizeOutcome is a record's stored wire form as the record reads after
// the conversion: its kind and its id where a reference names it, its
// properties by the write's rule (localizeRecord, then each reference where
// the last declaration of its kind puts one, and each actor record a hand of
// the repository's own moved from), and the record paths its marks and its
// mapped sources name, each by the same mover. Every other field, numbers
// included, stays as it was stored.
func localizeOutcome(raw []byte, home string, refs referencePositions, moves actorMoves) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var rec map[string]any
	if err := dec.Decode(&rec); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	kind, _ := rec["kind"].(string)
	local := vocabulary.LocalizeRef(kind, home)
	if local != kind {
		rec["kind"] = local
	}
	var failed error
	// An actor record's id is its hand's, hashed: it moves by the hand.
	recordID := func(id string) string {
		if kind != kindActor {
			return vocabulary.LocalizeID(kind, id, home)
		}
		to, err := moves.id(0, id)
		if err != nil {
			failed = err
		}
		return to
	}
	for _, field := range []string{"id", "canonicalId"} {
		if id, ok := rec[field].(string); ok {
			rec[field] = recordID(id)
		}
	}
	if former, ok := rec["formerIds"].([]any); ok {
		for i, f := range former {
			if id, ok := f.(string); ok {
				former[i] = recordID(id)
			}
		}
	}
	// Every record link moves by the one composed mover: the namespace, then
	// the actor record a hand of the repository's own moved from.
	link := func(p string) string {
		to, err := moves.path(0, vocabulary.LocalizePath(p, home))
		if err != nil {
			failed = err
		}
		return to
	}
	if props, ok := rec["properties"].(map[string]any); ok {
		props, err := localizeRecord(kind, props, home)
		if err != nil {
			return nil, err
		}
		for name, decl := range refs.lastDeclared(local) {
			if v, ok := props[name]; ok {
				props[name] = localizeReferencesWith(decl, v, link)
			}
		}
		if kind == kindActor {
			if name, ok := props["name"].(string); ok {
				props["name"] = moves.handName(name)
			}
		}
		rec["properties"] = props
	}
	if marks, ok := rec["marks"].(map[string]any); ok {
		for _, m := range marks {
			mark, _ := m.(map[string]any)
			for _, by := range []string{"setBy", "onBehalfOf"} {
				refAt(mark[by], link)
			}
			for _, from := range asSlice(mark["derivedFrom"]) {
				refAt(from, link)
			}
			for _, alt := range asSlice(mark["alternatives"]) {
				if a, ok := alt.(map[string]any); ok {
					refAt(a["derivedFrom"], link)
				}
			}
		}
	}
	for _, l := range asSlice(rec["linkedFrom"]) {
		from, ok := l.(map[string]any)
		if !ok {
			continue
		}
		refAt(from, link)
		for _, field := range []string{"kind", "mapping"} {
			if s, ok := from[field].(string); ok {
				from[field] = vocabulary.LocalizeRef(s, home)
			}
		}
	}
	if failed != nil {
		return nil, failed
	}
	out, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(out, raw) {
		var before map[string]any
		_ = json.Unmarshal(raw, &before)
		var after map[string]any
		_ = json.Unmarshal(out, &after)
		if jsonEqual(before, after) {
			return raw, nil
		}
	}
	return out, nil
}

// refAt folds the `ref` of a reference value in place.
func refAt(v any, fold func(string) string) {
	if m, ok := v.(map[string]any); ok {
		if p, ok := m[vocabulary.ReferenceValueKey].(string); ok {
			m[vocabulary.ReferenceValueKey] = fold(p)
		}
	}
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// carryAddresses moves every row of table bound to a record whose address
// moved, read by the record's kind and id, which is all the three tables
// carry of it.
func carryAddresses(ctx context.Context, tx *sql.Tx, table, prefix string, moved func(kind, id string) (string, string, bool)) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT DISTINCT record_kind, record_id FROM `+table+` WHERE starts_with(record_kind, $1) OR starts_with(record_id, $1)`, prefix)
	if err != nil {
		return err
	}
	type address struct{ kind, id string }
	var held []address
	for rows.Next() {
		var a address
		if err := rows.Scan(&a.kind, &a.id); err != nil {
			_ = rows.Close()
			return err
		}
		held = append(held, a)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, a := range held {
		kind, id, ok := moved(a.kind, a.id)
		if !ok {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE `+table+` SET record_kind = $3, record_id = $4 WHERE record_kind = $1 AND record_id = $2`, a.kind, a.id, kind, id); err != nil {
			return err
		}
	}
	return nil
}
