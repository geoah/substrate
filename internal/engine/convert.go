package engine

// A CONVERSION IS ORDINARY RECORD WRITES (decisions 0063 and 0066). Three
// declaration changes rewrite live records instead of refusing with a count:
//
//   - a rename: a property naming its previous name with `renamedFrom:` takes
//     every live record's value under the old name (rename.go has the rows
//     that travel with the value);
//   - a backfill: a property that becomes `required:` with a `default:` beside
//     it, or is added as one, has the default written onto every live record
//     holding no value for it (the key absent, or an empty value, exactly the
//     rows the write path would refuse `required` on: emptyValue);
//   - a remap: an enum value naming its previous spelling with `renamedFrom:`
//     takes every live record's value under the old spelling, in a scalar, a
//     list or a keyed map.
//
// The three compose. Every step a batch declares against one kind runs in one
// pass over that kind's records, in id order, and a record any step touches is
// rewritten ONCE: renames first, so a backfill and a remap read the property
// under the name the candidate declares; then backfills; then remaps. One
// record effect and one changelog entry per rewritten record, a `patch` whose
// payload names the properties and says which step moved them (`renamed`,
// `backfilled`, `remapped`), through the same fold every write takes. The
// fold reads no declaration, so a fresh replay reproduces the converted
// records without ever reading the declaration that converted them. The
// payload keys are descriptive: the fold replays the effects, never the keys,
// so a binary before this one reads the entry as the patch it is and needs no
// changelog dialect rung.
//
// Who wrote it: a backfilled value is a write by the hand that applied the
// declaration, so its manager row is that actor at the transaction's tier, as
// a create that fell back to the default would record. A renamed value keeps
// its manager (rename.go moveManager), and a remapped one keeps its manager
// too: the value's spelling moved, not who last wrote it. Offer rows describe
// what a mapping's SOURCE would write, so a remap and a backfill leave them
// alone (a rebuild derives them again from the sources); a rename rekeys them
// because they are keyed by the renamed property.
//
// What is refused: a remap onto a value the stored declaration still admits
// (the live records holding either spelling would become one set), because
// nothing here may discard a stored distinction. It is refused by declaration,
// without a count, under substrate.ErrLossyConversion; the confirmation that
// would admit one is issue #152's.
//
// The bound is the live count: a kind with N records a step touches costs N
// entries and N row rewrites in one transaction, under the vocabulary write
// mutex and the exclusive registry-dependency lock. Nothing caps it.

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// propertyBackfill is one backfill a batch declares: the kind as the candidate
// declares it and the property whose declared default every live record
// lacking a value receives. The value is the declaration's own (Default),
// coerced at the write as a create's would be.
type propertyBackfill struct {
	kind *vocabulary.Kind
	prop string
}

// enumRemap is one value rename a batch declares: the kind as the candidate
// declares it, the property (under its candidate name), the spelling live
// records still hold and the one that takes it.
type enumRemap struct {
	kind *vocabulary.Kind
	prop string
	from string
	to   string
}

// conversionPlan is every conversion a batch declares, classified against the
// stored declarations, plus the lossy refusals decided without a count.
type conversionPlan struct {
	renames   []propertyRename
	backfills []propertyBackfill
	remaps    []enumRemap
	// lossy names each value rename onto a value the stored declaration still
	// admits: a guard line, refused by declaration (convert.go's header).
	lossy []string
}

// empty reports a plan with nothing to rewrite.
func (p conversionPlan) empty() bool {
	return len(p.renames) == 0 && len(p.backfills) == 0 && len(p.remaps) == 0
}

// classifyConversions lists the conversions a batch declares against the
// stored declarations. It walks the kinds classifyNarrowingsExcept walks and
// skips the same ones, so a kind the boot upgrade holds at its stored version
// converts nothing. A `renamedFrom` naming a property or a value no stored
// declaration had is not a conversion: it is stored and does nothing.
func classifyConversions(current, candidate *vocabulary.Registry, touched, skip map[string]bool) conversionPlan {
	var plan conversionPlan
	for _, aname := range sortedKeys(touched) {
		cur, _ := current.PackageByName(aname)
		cand, _ := candidate.PackageByName(aname)
		if cur == nil || cand == nil {
			continue
		}
		for _, tn := range cur.KindOrder {
			candT := cand.Kinds[tn]
			if candT == nil || skip[candT.Identity] {
				continue
			}
			curT := cur.Kinds[tn]
			for _, pname := range curT.PropOrder {
				if candT.Props[pname] != nil {
					continue
				}
				if to := renamedTo(candT, pname); to != "" {
					plan.renames = append(plan.renames, propertyRename{kind: candT, from: pname, to: to})
				}
			}
			for _, pname := range candT.PropOrder {
				candP := candT.Props[pname]
				// The stored declaration the candidate property answers to: the
				// same name, or on a rename the old one, whose rows the rename
				// moves under this name before any other step reads them.
				curP := curT.Props[pname]
				if curP == nil && candP.RenamedFrom != "" && curT.Props[candP.RenamedFrom] != nil {
					curP = curT.Props[candP.RenamedFrom]
				}
				if candP.Required && !candP.IsState() && backfillable(candT, candP) && (curP == nil || !curP.Required) {
					plan.backfills = append(plan.backfills, propertyBackfill{kind: candT, prop: pname})
				}
				if curP == nil || curP.IsState() || candP.IsState() {
					continue
				}
				// A container or datatype flip is a kind change (schemadiff.go
				// propertyNarrowings), classified by the values it strands; a
				// value set is compared only within one shape.
				if curP.Datatype != candP.Datatype || curP.Repeated != candP.Repeated || curP.Keyed != candP.Keyed {
					continue
				}
				stored := curP.ValueStrings()
				for _, old := range removedStrings(stored, candP.ValueStrings()) {
					to := valueRenamedTo(candP, old)
					if to == "" {
						continue // stranded: the narrowing counts it
					}
					if slices.Contains(stored, to) {
						plan.lossy = append(plan.lossy, fmt.Sprintf(
							"type %s: property %q renames value %q onto %q, which the stored declaration still admits: the records holding either would become one set, and a lossy conversion is refused; rename it onto a new value, or rewrite the records and drop %q",
							candT.Identity, pname, old, to, old))
						continue
					}
					plan.remaps = append(plan.remaps, enumRemap{kind: candT, prop: pname, from: old, to: to})
				}
			}
		}
	}
	return plan
}

// backfillable reports whether admitting p as required strands nothing because
// the apply writes its default onto every row lacking a value. A property with
// no default keeps the count. The hot-column arm mirrors missingValueCount: no
// declaration reaches it today (a trait-bound property cannot declare a
// default), and the two must not drift if one ever does.
func backfillable(ty *vocabulary.Kind, p *vocabulary.Property) bool {
	if p.Default == nil {
		return false
	}
	if _, hot := hotColumns[p.Name]; hot && ty.UsesHot(p.Name) {
		return false
	}
	return true
}

// valueRenamedTo reports the candidate value (if any) that declares the given
// spelling as its renamedFrom.
func valueRenamedTo(candP *vocabulary.Property, from string) string {
	for _, v := range candP.Values {
		if v.RenamedFrom == from {
			return v.Value
		}
	}
	return ""
}

// unrenamedValues keeps the removed values no candidate value takes: the ones
// a removal strands, which the narrowing counts. A value some candidate value
// names is a remap or a lossy refusal (classifyConversions), never a count.
func unrenamedValues(candP *vocabulary.Property, removed []string) []string {
	var out []string
	for _, old := range removed {
		if valueRenamedTo(candP, old) == "" {
			out = append(out, old)
		}
	}
	return out
}

// kindConversion is every step of a plan against one kind.
type kindConversion struct {
	kind      *vocabulary.Kind
	renames   []propertyRename
	backfills []propertyBackfill
	remaps    []enumRemap
}

// convertRecords performs every conversion a batch declares and reports how
// many records it rewrote. It runs after the declaration rows projected and
// before the refs index re-derives, so the index reads the converted
// properties.
func (t *txn) convertRecords(candidate *vocabulary.Registry, plan conversionPlan) (int64, error) {
	if plan.empty() {
		return 0, nil
	}
	// The fold reads the transaction's declarations for the search bands and
	// the refs index (foldRecordOp), and a row rewritten to the new shape has
	// to index and project under the declaration that names it. The apply
	// door sets the candidate for its whole transaction; the boot upgrade
	// sets none, so it is set here for the rewrite alone.
	prev := t.writeReg
	t.writeReg = candidate
	defer func() { t.writeReg = prev }()
	// Grouped by kind, so a record several steps touch is rewritten once and
	// appends one entry.
	byKind := map[string]*kindConversion{}
	group := func(k *vocabulary.Kind) *kindConversion {
		kc := byKind[k.Identity]
		if kc == nil {
			kc = &kindConversion{kind: k}
			byKind[k.Identity] = kc
		}
		return kc
	}
	for _, r := range plan.renames {
		kc := group(r.kind)
		kc.renames = append(kc.renames, r)
	}
	for _, b := range plan.backfills {
		kc := group(b.kind)
		kc.backfills = append(kc.backfills, b)
	}
	for _, m := range plan.remaps {
		kc := group(m.kind)
		kc.remaps = append(kc.remaps, m)
	}
	var total int64
	for _, ident := range sortedKeys(byKind) {
		n, err := t.convertKind(byKind[ident])
		if err != nil {
			return total, fmt.Errorf("substrate/engine: convert records of %s: %w", ident, err)
		}
		total += n
	}
	return total, nil
}

// convertKind rewrites every live record of one kind that any step touches,
// in id order. The id query is the union of what each step reads: a record
// carrying a renamed property's old name, one holding no value for a
// backfilled property, one carrying a remapped property at all (whether it
// holds the old spelling is decided in Go, value by value, where the
// container shapes are).
func (t *txn) convertKind(kc *kindConversion) (int64, error) {
	args := []any{kc.kind.Identity}
	var holds []string
	bind := func(v string) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	for _, r := range kc.renames {
		holds = append(holds, "props ? "+bind(r.from))
	}
	for _, b := range kc.backfills {
		p := bind(b.prop)
		holds = append(holds, "(NOT props ? "+p+" OR props->"+p+" IN "+emptyJSONValues+")")
	}
	for _, m := range kc.remaps {
		holds = append(holds, "props ? "+bind(m.prop))
	}
	rows, err := t.query(`SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL AND (`+
		strings.Join(holds, " OR ")+`) ORDER BY id`, args...)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()
	var n int64
	for _, id := range ids {
		moved, err := t.convertRecord(kc, eref{Kind: kc.kind.Identity, ID: id})
		if err != nil {
			return n, fmt.Errorf("record %s: %w", id, err)
		}
		if moved {
			n++
		}
	}
	// Offer rows are recompute's projection of the live sources, keyed by
	// property (mapping.go syncOffers). A mapping onto the old name cannot
	// compile against the candidate, so every row under it describes the value
	// that just moved; rekeyed, the alternatives a reader sees follow it.
	for _, r := range kc.renames {
		// A stale row under the new name (a mapping onto a property dropped in
		// an earlier life) would collide with the rekey; nothing live backs it.
		if _, err := t.exec(`DELETE FROM property_offers WHERE record_kind = $1 AND property = $2`,
			kc.kind.Identity, r.to); err != nil {
			return n, err
		}
		if _, err := t.exec(`UPDATE property_offers SET property = $3 WHERE record_kind = $1 AND property = $2`,
			kc.kind.Identity, r.from, r.to); err != nil {
			return n, err
		}
	}
	return n, nil
}

// convertRecord rewrites one record: every step that finds something to move
// on it lands in a single record effect, renames first, then backfills, then
// remaps. It reports false when the record is gone or no step touches it,
// which the id query mostly excludes (a remapped property may be carried
// without the old spelling) and a concurrent write cannot produce under the
// locks the apply holds.
func (t *txn) convertRecord(kc *kindConversion, ref eref) (bool, error) {
	row, err := t.loadRow(ref, true)
	if err != nil || row == nil || row.DeletedAt != nil {
		return false, err
	}
	before := row.clone()
	kind := kc.kind
	touched := map[string]bool{}
	var renamed map[string]string
	for _, r := range kc.renames {
		value, held := row.Props[r.from]
		if !held {
			continue
		}
		row.Props[r.to] = value
		delete(row.Props, r.from)
		if renamed == nil {
			renamed = map[string]string{}
		}
		renamed[r.from] = r.to
		touched[r.from], touched[r.to] = true, true
	}
	var backfilled []string
	for _, b := range kc.backfills {
		if !emptyValue(row.Props[b.prop]) {
			continue
		}
		p := kind.Props[b.prop]
		// Coerced as a create's default is (withDefaults, then coerceValue), so
		// the backfilled row holds exactly what a create would have stored.
		// checkDeclaredDefaults admitted the literal, so this cannot refuse.
		value, err := coerceValue(p, p.Default)
		if err != nil {
			return false, fmt.Errorf("backfill %q: %w", b.prop, err)
		}
		row.Props[b.prop] = value
		backfilled = append(backfilled, b.prop)
		touched[b.prop] = true
	}
	var remapped map[string]map[string]string
	for _, m := range kc.remaps {
		value, ok := remapValue(row.Props[m.prop], m.from, m.to)
		if !ok {
			continue
		}
		row.Props[m.prop] = value
		if remapped == nil {
			remapped = map[string]map[string]string{}
		}
		if remapped[m.prop] == nil {
			remapped[m.prop] = map[string]string{}
		}
		remapped[m.prop][m.from] = m.to
		touched[m.prop] = true
	}
	if len(touched) == 0 {
		return false, nil
	}
	// The title renders under the candidate: a displayTemplate naming the new
	// property, or the backfilled one, finds its value only once it is there.
	title, err := t.deriveTitle(kind, row)
	if err != nil {
		return false, err
	}
	row.Title = title
	// The rewritten row was validated against the candidate declaration (the
	// narrowing guards counted it under the old shape), so it carries that
	// declaration's version, as a write through apply would (decision 0060).
	row.KindVersion = kind.Version
	res, err := t.foldRow(before, row, false, false)
	if err != nil {
		return false, err
	}
	if !res.changed {
		return false, nil
	}

	for _, r := range kc.renames {
		if renamed[r.from] == "" {
			continue
		}
		if err := t.moveManager(ref, r.from, r.to); err != nil {
			return false, err
		}
		if err := t.moveEmbeddings(ref, r.from, r.to, kind.Props[r.to].Embed); err != nil {
			return false, err
		}
	}
	for _, name := range backfilled {
		// A write by the hand that applied the declaration, as a create that
		// fell back to the default would record it.
		if err := t.setManager(ref, name, t.actor, t.tier); err != nil {
			return false, err
		}
		if kind.Props[name].Embed {
			if err := t.enqueueEmbed(ref, name); err != nil {
				return false, err
			}
		}
	}
	for name := range remapped {
		// The manager stays: the spelling moved, not who wrote the value. The
		// text changed where the property embeds, so the worker re-embeds it.
		if kind.Props[name].Embed {
			if err := t.enqueueEmbed(ref, name); err != nil {
				return false, err
			}
		}
	}

	properties := sortedKeys(touched)
	payload := map[string]any{"properties": properties}
	if len(renamed) > 0 {
		payload["renamed"] = renamed
	}
	if len(backfilled) > 0 {
		sort.Strings(backfilled)
		payload["backfilled"] = backfilled
	}
	if len(remapped) > 0 {
		payload["remapped"] = remapped
	}
	// One entry per record, as a patch: the record's properties changed, and
	// the step keys say the apply moved them rather than a writer.
	return true, t.appendChange(t.actor, substrate.OpPatch, ref.ID, ref.Kind, payload)
}

// remapValue rewrites one stored value's old spelling to the new one in the
// three shapes a value set is declared in: a scalar, a list of values and a
// keyed map of values. It answers a NEW value (the loaded row's containers are
// shared with the clone the fold diffs against, so nothing is rewritten in
// place) and false when the value holds no old spelling.
func remapValue(v any, from, to string) (any, bool) {
	switch x := v.(type) {
	case string:
		if x == from {
			return to, true
		}
	case []any:
		var out []any
		for i, e := range x {
			if s, ok := e.(string); ok && s == from {
				if out == nil {
					out = append(make([]any, 0, len(x)), x...)
				}
				out[i] = to
			}
		}
		if out != nil {
			return out, true
		}
	case map[string]any:
		var out map[string]any
		for k, e := range x {
			if s, ok := e.(string); ok && s == from {
				if out == nil {
					out = make(map[string]any, len(x))
					for k2, e2 := range x {
						out[k2] = e2
					}
				}
				out[k] = to
			}
		}
		if out != nil {
			return out, true
		}
	}
	return nil, false
}
