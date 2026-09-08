package engine

// Retired names (decision 0055). A package header's `retired.kinds` and a
// kind's `retired.{properties,values,states}` are reservations the author
// writes; this file is what makes them bind across declarations:
//
//   - carryRetirements copies a STORED retirement into the incoming document of
//     the same package or kind before versions resolve, so a document that
//     omits the list does not lift it. The apply verb merges and never prunes,
//     and a retirement is the one declaration key that is permanent.
//   - retirementGuards refuses a candidate registry that declares a retired
//     name, or drops one from a stored list, against the current registry. It
//     runs on every admission door beside classifyNarrowings: the apply verb and
//     InstallBundleClosure (stageVocabularyBatch), the upgrade preview
//     (PlanBundleUpgrade) and the boot upgrade of the shipped tree (seed.go),
//     which has no document step to merge in and so is where the dropped-list
//     branch actually fires.
//
// No count: a retired name refuses whether or not any row exists, which is what
// separates it from a narrowing.

import (
	"fmt"

	"github.com/geoah/substrate/internal/vocabulary"
)

// retiredDeclaredAgain is the loader's sentence for the same refusal
// (vocabulary/retired.go), so every door says it the same way.
const retiredDeclaredAgain = "a retired name is never declared again"

// retirementPermanent is the sentence for a stored retirement the candidate
// drops.
const retirementPermanent = "a retirement is permanent"

// retirementGuards renders the retired-name refusals of the candidate against
// the current registry, over the touched packages. skip names the declaration
// ids (kinds and package headers) the caller will not rewrite, exactly as
// classifyNarrowingsExcept takes them: a declaration held at its stored
// version keeps whatever it says.
func retirementGuards(current, candidate *vocabulary.Registry, touched, skip map[string]bool) []string {
	var out []string
	for _, aname := range sortedKeys(touched) {
		cur, _ := current.PackageByName(aname)
		cand, _ := candidate.PackageByName(aname)
		if cand == nil {
			continue // removed whole: the reservations go with the package
		}
		var stored []string
		if cur != nil {
			stored = cur.RetiredKinds
		}
		if !skip[aname] {
			for _, name := range missingStrings(stored, cand.RetiredKinds) {
				out = append(out, fmt.Sprintf("package %s: kind name %q is retired and this declaration drops it; %s",
					aname, name, retirementPermanent))
			}
		}
		for _, name := range unionStrings(stored, cand.RetiredKinds) {
			t := cand.Kinds[name]
			if t == nil || skip[t.Identity] {
				continue
			}
			out = append(out, fmt.Sprintf("kind %s: the name is retired in package %s; %s",
				t.Identity, aname, retiredDeclaredAgain))
		}
		for _, tn := range cand.KindOrder {
			candT := cand.Kinds[tn]
			if skip[candT.Identity] {
				continue
			}
			var curT *vocabulary.Kind
			if cur != nil {
				curT = cur.Kinds[tn]
			}
			out = append(out, kindRetirementGuards(curT, candT)...)
		}
	}
	return out
}

// heldRetirementGuards is the boot door's extra branch: a shipped header that
// retires a kind name while this repository still declares the kind. The apply
// and install doors prune what a closure stops declaring, so there the drop and
// the retirement land in one batch; the boot upgrade never prunes, so the
// header would land beside the kind row and the next open would refuse the
// stored closure as retired-and-declared, which for core is a repository that
// no longer opens. Refused before any row moves, naming the kind to delete
// first.
func heldRetirementGuards(current, candidate *vocabulary.Registry, touched map[string]bool) []string {
	var out []string
	for _, aname := range sortedKeys(touched) {
		cur, _ := current.PackageByName(aname)
		cand, _ := candidate.PackageByName(aname)
		if cur == nil || cand == nil {
			continue
		}
		for _, name := range cand.RetiredKinds {
			held := cur.Kinds[name]
			if held == nil || cand.Kinds[name] != nil {
				continue
			}
			out = append(out, fmt.Sprintf("kind %s: the shipped package retires the name while this repository still declares the kind; the boot upgrade never prunes, so delete the kind first",
				held.Identity))
		}
	}
	return out
}

// kindRetirementGuards is one kind's half: the stored list may not shrink, and
// the candidate may not declare a name either list covers. curT is nil for a
// kind the repository does not hold yet, whose own block the loader already
// held to itself.
func kindRetirementGuards(curT, candT *vocabulary.Kind) []string {
	var out []string
	ident := candT.Identity
	var stored vocabulary.KindRetirement
	if curT != nil {
		stored = curT.Retired
	}
	for _, name := range missingStrings(stored.Properties, candT.Retired.Properties) {
		out = append(out, fmt.Sprintf("kind %s: property %q is retired and this declaration drops it; %s",
			ident, name, retirementPermanent))
	}
	for _, pname := range sortedKeys(mapOfLists(stored.Values)) {
		for _, v := range missingStrings(stored.Values[pname], candT.Retired.Values[pname]) {
			out = append(out, fmt.Sprintf("kind %s: property %q: enum value %q is retired and this declaration drops it; %s",
				ident, pname, v, retirementPermanent))
		}
	}
	for _, pname := range sortedKeys(mapOfLists(stored.States)) {
		for _, v := range missingStrings(stored.States[pname], candT.Retired.States[pname]) {
			out = append(out, fmt.Sprintf("kind %s: property %q: state %q is retired and this declaration drops it; %s",
				ident, pname, v, retirementPermanent))
		}
	}
	// An implicit stamp target counts as declared: a transition writes it.
	for _, name := range unionStrings(stored.Properties, candT.Retired.Properties) {
		if candT.Props[name] != nil {
			out = append(out, fmt.Sprintf("kind %s: property %q is retired; %s", ident, name, retiredDeclaredAgain))
		}
	}
	values := unionByProperty(stored.Values, candT.Retired.Values)
	for _, pname := range sortedKeys(mapOfLists(values)) {
		p := candT.Props[pname]
		if p == nil || p.Datatype != vocabulary.DatatypeEnum {
			continue
		}
		live := map[string]bool{}
		for _, ev := range p.Values {
			live[ev.Value] = true
		}
		for _, v := range values[pname] {
			if live[v] {
				out = append(out, fmt.Sprintf("kind %s: property %q: enum value %q is retired; %s",
					ident, pname, v, retiredDeclaredAgain))
			}
		}
	}
	states := unionByProperty(stored.States, candT.Retired.States)
	for _, pname := range sortedKeys(mapOfLists(states)) {
		p := candT.Props[pname]
		if p == nil || p.Machine == nil {
			continue
		}
		live := map[string]bool{}
		for _, s := range p.Machine.States {
			live[s] = true
		}
		for _, v := range states[pname] {
			if live[v] {
				out = append(out, fmt.Sprintf("kind %s: property %q: state %q is retired; %s",
					ident, pname, v, retiredDeclaredAgain))
			}
		}
	}
	return out
}

// carryRetirements copies each stored `retired` block into the batch document
// of the same package or kind, union-wise, before the versions resolve: an
// incoming document that says nothing about retirement keeps every stored
// entry, and one that adds entries keeps the stored ones beside them. A
// document that DECLARES a carried name then fails the loader on its own terms
// (a name both retired and declared), which is the apply door's refusal.
//
// Copy-on-write, like resolveDeclarationVersions: a bundle's closure documents
// are the catalog's cached maps and must not be written into.
func carryRetirements(b *vocabularyBatch, existing map[string]vocabulary.Document) {
	for i, d := range b.docs {
		if d.Kind != vocabulary.DocPackage && d.Kind != vocabulary.DocKind {
			continue
		}
		// An empty list is no retirement. The row regenerator writes the key
		// only for a non-empty list, so a document carrying `kinds: []` would
		// otherwise never compare equal to its row and bump the version on
		// every re-apply.
		if normalized, changed := normalizeRetired(d.Data); changed {
			d.Data = normalized
			b.docs[i] = d
		}
		stored, has := existing[docKey(d)]
		if !has {
			continue
		}
		storedBlock := mapOrNil(stored.Data["retired"])
		if len(storedBlock) == 0 {
			continue
		}
		// A malformed incoming block is left as written, so the loader refuses
		// it by name rather than the merge quietly replacing it.
		incoming, present := d.Data["retired"]
		incomingBlock := mapOrNil(incoming)
		if present && incomingBlock == nil {
			continue
		}
		merged, ok := mergeRetiredBlocks(storedBlock, incomingBlock)
		if !ok {
			continue
		}
		if present && retiredBlocksEqual(merged, incomingBlock) {
			continue
		}
		data := make(map[string]any, len(d.Data)+1)
		for k, v := range d.Data {
			data[k] = v
		}
		data["retired"] = merged
		d.Data = data
		b.docs[i] = d
	}
}

// normalizeRetired drops the empty parts of a document's `retired` block: an
// empty list, a property whose list is empty, and the key itself once nothing
// is left. Only a well-formed block is touched; a malformed one is the
// loader's to refuse. Returns the data with a fresh map when anything moved.
func normalizeRetired(data map[string]any) (map[string]any, bool) {
	raw, present := data["retired"]
	if !present {
		return data, false
	}
	block := mapOrNil(raw)
	if block == nil {
		return data, false
	}
	normalized, ok := mergeRetiredBlocks(block, nil)
	if !ok || declarationDataEqual(block, normalized) {
		return data, false
	}
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}
	if len(normalized) == 0 {
		delete(out, "retired")
	} else {
		out["retired"] = normalized
	}
	return out, true
}

// mergeRetiredBlocks unions two `retired` blocks key by key: a list key
// (`kinds`, `properties`) unions its names, a by-property key (`values`,
// `states`) unions per property. Stored entries come first, then the incoming
// additions, so a re-applied document projects the row it read. The stored
// block is a row the loader admitted; an incoming value whose shape disagrees
// with it (a scalar where a list is, a list where a map is) fails the merge,
// and the caller leaves the document for the loader to refuse.
func mergeRetiredBlocks(stored, incoming map[string]any) (map[string]any, bool) {
	out := map[string]any{}
	for _, key := range sortedKeys(unionKeys(stored, incoming)) {
		sv, iv := stored[key], incoming[key]
		switch {
		case isList(sv) || isList(iv):
			if (sv != nil && !isList(sv)) || (iv != nil && !isList(iv)) {
				return nil, false
			}
			if names := unionStrings(anyStrings(sv), anyStrings(iv)); len(names) > 0 {
				out[key] = anyList(names)
			}
		default:
			sm, im := mapOrNil(sv), mapOrNil(iv)
			if (sv != nil && sm == nil) || (iv != nil && im == nil) {
				return nil, false
			}
			byProp := map[string]any{}
			for _, pname := range sortedKeys(unionKeys(sm, im)) {
				if (sm[pname] != nil && !isList(sm[pname])) || (im[pname] != nil && !isList(im[pname])) {
					return nil, false
				}
				if names := unionStrings(anyStrings(sm[pname]), anyStrings(im[pname])); len(names) > 0 {
					byProp[pname] = anyList(names)
				}
			}
			if len(byProp) > 0 {
				out[key] = byProp
			}
		}
	}
	return out, true
}

// retiredBlocksEqual compares two blocks as the merge renders them.
func retiredBlocksEqual(a, b map[string]any) bool {
	normalized, ok := mergeRetiredBlocks(b, nil)
	return ok && declarationDataEqual(a, normalized)
}

func isList(v any) bool {
	_, ok := v.([]any)
	return ok
}

// mapOrNil reads a decoded block as a map, nil for anything else.
func mapOrNil(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func anyList(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

func unionKeys(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a))
	for k := range a {
		out[k] = nil
	}
	for k := range b {
		out[k] = nil
	}
	return out
}

// missingStrings lists the members of stored that cand no longer carries, in
// stored's order.
func missingStrings(stored, cand []string) []string {
	return removedStrings(stored, cand)
}

// unionStrings lists a's members then b's additions, without duplicates.
func unionStrings(a, b []string) []string {
	seen := make(map[string]bool, len(a))
	var out []string
	for _, list := range [][]string{a, b} {
		for _, s := range list {
			if seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// unionByProperty unions two by-property maps.
func unionByProperty(a, b map[string][]string) map[string][]string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := map[string][]string{}
	for _, m := range []map[string][]string{a, b} {
		for pname, vals := range m {
			out[pname] = unionStrings(out[pname], vals)
		}
	}
	return out
}

func mapOfLists(m map[string][]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
