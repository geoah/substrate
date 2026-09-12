package vocabulary

import "fmt"

// A KIND MOVE IS ORDINARY RECORD WRITES (decision record 0078). A kind that
// used to be spelled somewhere else says so with `movedFrom:`, and admitting it
// where the repository still declares that old kind carries every live row onto
// this kind, with the same id and the same properties, in the transaction that
// admits the declaration.
//
// The loader's half is the GRAMMAR, and only the grammar. Whether the move is
// safe, whether the old kind is even present, and whether this door performs
// moves at all are decided against the stored declarations, which the loader
// never sees: the engine refuses an incompatible move (engine/move.go) and this
// refuses a reference nothing could ever name.
//
// The key is reserved by name, not tolerated by prefix (record 0020): it is in
// typeDataKeys, so a binary that did not know it would quarantine the package
// rather than store it inert.

// parseMovedFrom reads a kind's `movedFrom:`. The reference is fully qualified
// and under this kind's OWN authority: a move carries rows from one package to
// another inside one publisher's tree, and a declaration that could name
// another authority's kind would be a declaration that moves somebody else's
// records.
func (l *loader) parseMovedFrom(where string, d map[string]any, t *Kind) {
	raw, present := d["movedFrom"]
	if !present {
		return
	}
	ref, ok := raw.(string)
	if !ok || ref == "" {
		l.errf("%s: data.movedFrom: must be the kind reference this kind used to be spelled as", where)
		return
	}
	if !ValidKindReference(ref) || !Qualified(ref) {
		l.errf("%s: data.movedFrom: %q is not a fully qualified kind reference (authority/package/kind)", where, ref)
		return
	}
	if ref == t.Identity {
		l.errf("%s: data.movedFrom: names this kind itself", where)
		return
	}
	mine, _, _ := SplitKindRef(t.Identity)
	if authority, _, _ := SplitKindRef(ref); authority != mine {
		l.errf("%s: data.movedFrom: %q is under another authority; a kind moves inside the authority that publishes it",
			where, ref)
		return
	}
	t.MovedFrom = ref
}

// MovedFromProblems reports what a move cannot honor, given the declaration the
// repository HOLDS for the old kind. The two property sets must be IDENTICAL.
//
// A property the old kind declares and the new one does not would be a value
// dropped by a key nobody wrote, because the rows travel with their properties
// untouched and under their own names. A property the NEW kind declares and the
// old one does not would be a shape the plan never said it was installing: the
// carried rows hold no value for it, so a `required` one refuses every row at
// the put and an optional one is a change that belongs in its own apply, where
// the ordinary guards and the ordinary conversions can see it. A property the
// new kind takes over under a new name is refused too: the move copies the old
// key and the destination's coercion refuses a key it does not declare.
//
// Move first, change after. `old` may be nil, which is the ordinary case on a
// fresh repository: there is nothing to move and nothing to refuse.
func MovedFromProblems(old, moved *Kind) []string {
	if old == nil || moved == nil || moved.MovedFrom == "" {
		return nil
	}
	var problems []string
	for _, pname := range old.PropOrder {
		if _, ok := moved.Props[pname]; !ok {
			problems = append(problems, fmt.Sprintf(
				"kind %s: data.movedFrom names %s, which declares %q and this kind does not: a move carries every property across under its own name, so declare it here and change it in a later apply",
				moved.Identity, moved.MovedFrom, pname))
		}
	}
	for _, pname := range moved.PropOrder {
		if _, ok := old.Props[pname]; !ok {
			problems = append(problems, fmt.Sprintf(
				"kind %s: data.movedFrom names %s, which does not declare %q: a move carries the rows unchanged, so add the property in a later apply where the ordinary guards can see it",
				moved.Identity, moved.MovedFrom, pname))
		}
	}
	return problems
}
