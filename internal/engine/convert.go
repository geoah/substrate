package engine

// A CONVERSION IS ORDINARY RECORD WRITES (decisions 0063, 0066 and 0067). Four
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
//     list or a keyed map;
//   - a null: a property the candidate no longer declares, and nothing
//     renames, has its value removed from every live record carrying it.
//
// The four compose. Every step a batch declares against one kind runs in one
// pass over that kind's records, in id order, and a record any step touches is
// rewritten ONCE: renames first, so a backfill and a remap read the property
// under the name the candidate declares; then backfills; then remaps; then
// nulls. One record effect and one changelog entry per rewritten record, a
// `patch` whose payload names the properties and says which step moved them
// (`renamed`, `backfilled`, `remapped`, `nulled`), through the same fold every
// write takes. The fold reads no declaration, so a fresh replay reproduces the
// converted records without ever reading the declaration that converted them.
// The payload keys are descriptive: the fold replays the effects, never the
// keys, so a binary before this one reads the entry as the patch it is and
// needs no changelog dialect rung.
//
// Who wrote it: a backfilled value is a write by the hand that applied the
// declaration, so its manager row is that actor at the transaction's tier, as
// a create that fell back to the default would record. A renamed value keeps
// its manager (rename.go moveManager), and a remapped one keeps its manager
// too: the value's spelling moved, not who last wrote it. A nulled value has
// no manager afterwards, no embedding and no sealed material, exactly as a
// patch clearing it would leave the record. A converted record is a source
// write like any other, so its subjects recompute after its entry
// (recomputeSubjectsOf, as afterTombstone does): a mapped target and its offer
// rows follow a remapped, backfilled or nulled source value, and a rebuild,
// which derives the offers from the sources again, agrees with the live table.
// A rename rekeys the offer rows of the renamed kind itself, because they are
// keyed by the renamed property.
//
// THE PLAN IS COUNTED, HASHED AND JUDGED IN ONE PLACE (conversionPlan.wire),
// so the two previews (PlanBundleUpgrade, PlanVocabularyApply) and the two
// doors (the apply transaction, the boot upgrade) agree on every number. A
// step touching no live record is not a step. A plan is LOSSY when it collapses
// a distinction live records hold: every null (a value leaves the fold), and a
// remap whose target some live record already holds, or that another remap of
// the same property lands its own records on, judged over the whole plan once
// every step is counted. A remap onto a value the declaration keeps but no
// record holds loses nothing and needs no consent. A lossy plan runs only
// with a confirmation naming the plan's hash and the changelog head it was
// previewed at (admitConversion); the boot door has nobody to confirm and
// refuses instead. The old values stay in the changelog either way: a lossy
// step removes them from the fold, and nothing here erases anything.
//
// The bound is the live count: a kind with N records a step touches costs N
// entries and N row rewrites in one transaction, under the vocabulary write
// mutex and the exclusive registry-dependency lock. The deployment's ceiling
// (service.conversionCeiling, in records) refuses a plan whose estimated work
// is above it, on both doors.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// DefaultConversionCeiling is the work ceiling a service runs with when none
// is configured (WithConversionCeiling): the most live records one declaration
// change may rewrite in its transaction.
const DefaultConversionCeiling int64 = 10000

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

// propertyNull is one dropped property whose live values the plan removes:
// the kind as the candidate declares it (without the property), the dropped
// name, and whether the stored declaration typed it secret, so the sealed
// material goes with the value as a clearing patch would take it.
type propertyNull struct {
	kind   *vocabulary.Kind
	prop   string
	secret bool
}

// conversionPlan is every conversion a batch declares, classified against the
// stored declarations. Nothing here is counted: wire counts.
type conversionPlan struct {
	renames   []propertyRename
	backfills []propertyBackfill
	remaps    []enumRemap
	nulls     []propertyNull
}

// empty reports a plan with nothing to rewrite.
func (p conversionPlan) empty() bool {
	return len(p.renames) == 0 && len(p.backfills) == 0 && len(p.remaps) == 0 && len(p.nulls) == 0
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
				curP := curT.Props[pname]
				if to := renamedTo(candT, pname); to != "" {
					plan.renames = append(plan.renames, propertyRename{kind: candT, from: pname, to: to})
					continue
				}
				if nullable(curT, curP) {
					plan.nulls = append(plan.nulls, propertyNull{kind: candT, prop: pname, secret: curP.Secret()})
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
				// Whether a remap collapses a distinction is decided over the
				// live records, not here (wire): the loader cannot see the
				// stored side, and a target the stored list still declares is
				// lossy only while some record holds it.
				for _, old := range removedStrings(curP.ValueStrings(), candP.ValueStrings()) {
					to := valueRenamedTo(candP, old)
					if to == "" {
						continue // stranded: the narrowing counts it
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
// no default keeps the count, and so does one whose default is a value
// `required` itself refuses (emptyValue): the loader refuses that pair
// (parseDefault), and were one ever stored, backfilling it would commit rows
// every later write refuses. The hot-column arm mirrors missingValueCount: no
// declaration reaches it today (a trait-bound property cannot declare a
// default), and the two must not drift if one ever does.
func backfillable(ty *vocabulary.Kind, p *vocabulary.Property) bool {
	if p.Default == nil || emptyValue(p.Default) {
		return false
	}
	if _, hot := hotColumns[p.Name]; hot && ty.UsesHot(p.Name) {
		return false
	}
	return true
}

// nullable reports whether dropping p is a null step rather than a counted
// narrowing: a value the record carries under `props`, which the step can
// remove. A state is not a value (it moves by transition, never by
// assignment), and a property living in its own column (a hot trait
// property, the title, the body) is not under `props`, so the count the
// narrowing takes stays the answer for those.
func nullable(ty *vocabulary.Kind, p *vocabulary.Property) bool {
	if p.IsState() || p.Name == substrate.PropTitle || p.Name == substrate.PropBody {
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
// names is a remap (classifyConversions), never a count.
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
	nulls     []propertyNull
	// filled is each backfilled property's value as the rows receive it: the
	// declared default coerced and, for a reference, validated and normalized
	// as a write's would be (backfillValues). One value per property, computed
	// once, because every row receives the same one.
	filled map[string]any
}

// backfillValues computes what each backfill writes, exactly as a create that
// fell back to the default would store it: coerced (coerceValue) and put
// through the write path's reference validation (validateReferences), so a
// default naming a kind the registry does not hold, a record outside the
// declared pin or a `mustExist` target that is not there would refuse the
// apply as it refuses the create. No reference-holding property can declare a
// default today (referencePropKeys has no `default`, and a default is one
// scalar, never an object), so the reference half holds nothing yet; it is
// here so that reserving the key is not also a hole in the backfill. The
// default is one value for every row, so it is validated once per kind rather
// than once per record. checkDeclaredDefaults admitted the literal, so the
// coercion cannot refuse.
func (t *txn) backfillValues(kc *kindConversion) error {
	if len(kc.backfills) == 0 {
		return nil
	}
	values := make(map[string]any, len(kc.backfills))
	for _, b := range kc.backfills {
		p := kc.kind.Props[b.prop]
		value, err := coerceValue(p, p.Default)
		if err != nil {
			return fmt.Errorf("backfill %q: %w", b.prop, err)
		}
		values[b.prop] = value
	}
	if err := t.validateReferences(kc.kind, values); err != nil {
		return fmt.Errorf("backfill: %w", err)
	}
	kc.filled = values
	return nil
}

// storedName is the name live rows hold a candidate property under before the
// rewrite: the old name where the property takes a rename's values (the rename
// runs first, convertRecord), the property's own name otherwise. Every count
// and every id query reads the rows as they are, so it reads them under this
// name; the candidate's name is what the rows hold only after the rename.
func (kc *kindConversion) storedName(prop string) string {
	for _, r := range kc.renames {
		if r.to == prop {
			return r.from
		}
	}
	return prop
}

// byKind groups the plan's steps by the kind they rewrite, in identity order,
// so a record several steps touch is rewritten once and the steps are listed
// in one order everywhere.
func (p conversionPlan) byKind() []*kindConversion {
	groups := map[string]*kindConversion{}
	group := func(k *vocabulary.Kind) *kindConversion {
		kc := groups[k.Identity]
		if kc == nil {
			kc = &kindConversion{kind: k}
			groups[k.Identity] = kc
		}
		return kc
	}
	for _, r := range p.renames {
		kc := group(r.kind)
		kc.renames = append(kc.renames, r)
	}
	for _, b := range p.backfills {
		kc := group(b.kind)
		kc.backfills = append(kc.backfills, b)
	}
	for _, m := range p.remaps {
		kc := group(m.kind)
		kc.remaps = append(kc.remaps, m)
	}
	for _, n := range p.nulls {
		kc := group(n.kind)
		kc.nulls = append(kc.nulls, n)
	}
	out := make([]*kindConversion, 0, len(groups))
	for _, ident := range sortedKeys(groups) {
		out = append(out, groups[ident])
	}
	return out
}

// wire counts every step over q and answers the plan as the wire carries it:
// the steps that touch a record, the work, the lossy judgment, the hash and
// the changelog head the counts were taken at. The previews read it over the
// bare pool and the doors inside their transaction, through the same queries,
// so the hash a preview handed out is the hash the door recomputes when
// nothing moved in between.
func (p conversionPlan) wire(q sqlReader) (substrate.ConversionPlan, error) {
	var plan substrate.ConversionPlan
	if err := q.row(`SELECT coalesce(max(seq), 0) FROM changelog`).Scan(&plan.ChangelogSeq); err != nil {
		return plan, err
	}
	if p.empty() {
		return plan, nil
	}
	count := func(query string, args ...any) (int64, error) {
		var n int64
		err := q.row(query, args...).Scan(&n)
		return n, err
	}
	add := func(step substrate.ConversionStep, n int64, err error) error {
		if err != nil {
			return err
		}
		if n == 0 {
			return nil // nothing to rewrite, so nothing to plan, confirm or count
		}
		step.Records = n
		plan.Steps = append(plan.Steps, step)
		plan.Work += n
		return nil
	}
	// A remap collapses a distinction only where live records stand on both
	// sides of it: a record holds the target already (a value the stored
	// declaration keeps, or one a restored tombstone carries), or another
	// remap of the same property lands its own records on the same target.
	// Judged over the whole plan, after every step is counted: `landing`
	// counts the remap steps with records per (kind, property, target), and
	// `held` says whether the target is on some record now.
	type target struct{ kind, prop, to string }
	landing := map[target]int{}
	held := map[target]bool{}
	for _, kc := range p.byKind() {
		ident := kc.kind.Identity
		for _, r := range kc.renames {
			n, err := count(countPropQuery, ident, r.from)
			if err := add(substrate.ConversionStep{Step: substrate.StepRename, Kind: ident, Property: r.to, From: r.from, To: r.to}, n, err); err != nil {
				return plan, err
			}
		}
		for _, b := range kc.backfills {
			// Counted under the name the rows hold NOW (storedName): a
			// backfill of a rename's target fills only the rows the old name
			// was missing on, because the rename moves the rest first.
			n, err := count(countMissingPropQuery, ident, kc.storedName(b.prop))
			if err := add(substrate.ConversionStep{Step: substrate.StepBackfill, Kind: ident, Property: b.prop}, n, err); err != nil {
				return plan, err
			}
		}
		for _, m := range kc.remaps {
			// Counted in the property's own container, element by element in
			// a list and value by value in a keyed map, exactly as the
			// narrowing counts a removed value (schemadiff.go valuesAtPath),
			// under the name the rows hold now.
			path := containerPath(nil, kc.kind.Props[m.prop], kc.storedName(m.prop))
			query, args := valuesAtPath(ident, path, []string{m.from})
			n, err := count(query, args...)
			if err != nil {
				return plan, err
			}
			if n == 0 {
				continue
			}
			key := target{ident, m.prop, m.to}
			landing[key]++
			if !held[key] {
				query, args = valuesAtPath(ident, path, []string{m.to})
				t, err := count(query, args...)
				if err != nil {
					return plan, err
				}
				held[key] = t > 0
			}
			if err := add(substrate.ConversionStep{Step: substrate.StepRemap, Kind: ident, Property: m.prop, From: m.from, To: m.to}, n, nil); err != nil {
				return plan, err
			}
		}
		for _, nl := range kc.nulls {
			n, err := count(countPropQuery, ident, nl.prop)
			if err := add(substrate.ConversionStep{Step: substrate.StepNull, Kind: ident, Property: nl.prop, Lossy: true}, n, err); err != nil {
				return plan, err
			}
		}
	}
	for i := range plan.Steps {
		s := &plan.Steps[i]
		if s.Step == substrate.StepRemap {
			key := target{s.Kind, s.Property, s.To}
			s.Lossy = held[key] || landing[key] > 1
		}
		plan.Lossy = plan.Lossy || s.Lossy
	}
	plan.PlanHash = planHash(plan.Steps)
	return plan, nil
}

// packagePlan is the plan as one shipped package reads it: the steps that
// rewrite its own kinds, with the work, the lossy judgment and the hash
// recounted over them. The changelog head is the whole plan's. The boot
// refuses the shipped set whole, so the blockers are shared; the rewrites are
// each package's own.
func packagePlan(plan substrate.ConversionPlan, pkg string) substrate.ConversionPlan {
	out := substrate.ConversionPlan{ChangelogSeq: plan.ChangelogSeq}
	for _, s := range plan.Steps {
		if vocabulary.KindPackage(s.Kind) != pkg {
			continue
		}
		out.Steps = append(out.Steps, s)
		out.Work += s.Records
		out.Lossy = out.Lossy || s.Lossy
	}
	out.PlanHash = planHash(out.Steps)
	return out
}

// planHash identifies a plan by its steps and their counts: the same steps
// over the same live records hash the same, and one record more or less does
// not. A confirmation names this hash, so it covers exactly what the preview
// showed and nothing the data has since become.
func planHash(steps []substrate.ConversionStep) string {
	if len(steps) == 0 {
		return ""
	}
	h := sha256.New()
	for _, s := range steps {
		fmt.Fprintf(h, "%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%d\x1f%t\n", s.Step, s.Kind, s.Property, s.From, s.To, s.Records, s.Lossy)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// describeStep renders one step for a refusal or a log line.
func describeStep(s substrate.ConversionStep) string {
	switch s.Step {
	case substrate.StepRename:
		return fmt.Sprintf("type %s: property %q renamed to %q on %d live records", s.Kind, s.From, s.To, s.Records)
	case substrate.StepBackfill:
		return fmt.Sprintf("type %s: property %q backfilled with its default on %d live records", s.Kind, s.Property, s.Records)
	case substrate.StepRemap:
		line := fmt.Sprintf("type %s: property %q value %q rewritten to %q on %d live records", s.Kind, s.Property, s.From, s.To, s.Records)
		if s.Lossy {
			line += ", a value live records already hold: the records holding either become one set (lossy)"
		}
		return line
	default:
		return fmt.Sprintf("type %s: property %q dropped, its value removed from %d live records (lossy; the values stay in the changelog and leave the fold)", s.Kind, s.Property, s.Records)
	}
}

// lossyLines renders the plan's lossy steps, one per line.
func lossyLines(plan substrate.ConversionPlan) []string {
	var out []string
	for _, s := range plan.Steps {
		if s.Lossy {
			out = append(out, describeStep(s))
		}
	}
	return out
}

// ceilingGuard is the guard line a plan above the work ceiling refuses on, or
// "" when it fits. A ceiling of zero or less is no ceiling.
func ceilingGuard(plan substrate.ConversionPlan, ceiling int64) string {
	if ceiling <= 0 || plan.Work <= ceiling {
		return ""
	}
	return fmt.Sprintf("the conversion rewrites %d live records, above this substrate's ceiling of %d (SUBSTRATE_CONVERSION_CEILING): declare the new shape beside the old one, move the records through ordinary writes at your own pace, then apply the contraction",
		plan.Work, ceiling)
}

// editedCopy is the second thing a batch can lose besides record values: a
// batch that names an ORIGIN (a sample re-import, a hand apply saying where
// its closure came from) replaces the package whole, and when the stored copy
// was edited since its stamp (its closure digest is no longer the stamped
// `originDigest`), every edit goes with the replacement (decision record
// 0070). The current digest is what the consent binds to: bindEditedCopy
// folds it into the plan hash, so a confirmation read off a preview covers
// exactly the edited state the preview saw, and one more edit refuses it the
// way one more write does through the changelog head.
type editedCopy struct {
	pkg    string
	digest string
}

// bindEditedCopy binds the plan's hash to the edited state a batch discards,
// and reports whether it discards one. Nil is a batch that discards nothing,
// which leaves the plan as the steps alone hashed it.
func bindEditedCopy(plan *substrate.ConversionPlan, edited *editedCopy) bool {
	if edited == nil {
		return false
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s\x1fdiscards\x1f%s\x1f%s\n", plan.PlanHash, edited.pkg, edited.digest)
	plan.PlanHash = hex.EncodeToString(h.Sum(nil))
	return true
}

// editedCopy answers whether a batch naming origin replaces a copy edited
// since its stamp: the package the origin's package segment names, as the
// batch spells it, carries a stamp whose digest is no longer the stored
// closure's. A batch with no origin, a package with no stamp and a pristine
// copy all answer nil. The digest is read the way the bundle status reads it
// (packageClosureDigest), under the schema-write mutex every caller holds, so
// the rows it hashes are the rows the replacement is about to prune.
func (ds *dataset) editedCopy(ctx context.Context, origin string, docs []vocabulary.Document) (*editedCopy, error) {
	pkg := originPackage(origin, docs)
	if pkg == "" {
		return nil, nil
	}
	stamp, err := ds.packageStamp(ctx, pkg)
	if err != nil {
		return nil, err
	}
	if stamp.origin == "" || stamp.digest == "" {
		return nil, nil
	}
	current, err := ds.packageClosureDigest(ctx, pkg)
	if err != nil {
		return nil, err
	}
	if current == stamp.digest {
		return nil, nil
	}
	return &editedCopy{pkg: pkg, digest: current}, nil
}

// originPackage is the package in docs that an origin lands on: the one whose
// package word is the origin's, spelled under whatever authority the batch
// declares (the rehome keeps the word and moves the authority). Empty when
// there is no origin or no such package in the batch.
func originPackage(origin string, docs []vocabulary.Document) string {
	if origin == "" {
		return ""
	}
	_, word := vocabulary.SplitPackageRef(origin)
	if word == "" {
		return ""
	}
	for _, d := range docs {
		if d.Kind != vocabulary.DocPackage && d.Kind != vocabulary.DocBundle {
			continue
		}
		if pkg := d.DeclaredPackage(); pkg != "" {
			if _, name := vocabulary.SplitPackageRef(pkg); name == word {
				return pkg
			}
		}
	}
	return ""
}

// admitConversion is the plan's own guard, after the refuse-breakage guards
// passed: the work ceiling, then the confirmation a lossy plan needs, and a
// batch that discards a copy's edits needs the same one (editedCopy). The
// confirmation is bound to what was previewed: the changelog head must still
// be the one the preview counted at (any write moves it, so the counts may
// have too) and the hash must be the one the door just recomputed (the
// consent covers that plan and no other). A lossless plan that discards
// nothing ignores a confirmation: there is nothing to consent to.
func admitConversion(plan substrate.ConversionPlan, ceiling int64, confirm *substrate.ConversionConfirm, edited *editedCopy) error {
	if line := ceilingGuard(plan, ceiling); line != "" {
		return fmt.Errorf("%w: %s", substrate.ErrGuard, line)
	}
	if !plan.Lossy && edited == nil {
		return nil
	}
	switch {
	case confirm == nil && edited != nil:
		loses := "the declarations edited since the copy was imported"
		if plan.Lossy {
			loses += ", and values from the fold (" + strings.Join(lossyLines(plan), "; ") + ")"
		}
		return fmt.Errorf("%w: %w: the batch replaces %s whole and removes %s; it runs only with a confirmation carrying the previewed planHash and changelogSeq (planHash %s at changelogSeq %d)",
			substrate.ErrGuard, substrate.ErrLossyConversion, edited.pkg, loses, plan.PlanHash, plan.ChangelogSeq)
	case confirm == nil:
		return fmt.Errorf("%w: %w: the change removes values from the fold and runs only with a confirmation carrying the previewed planHash and changelogSeq (planHash %s at changelogSeq %d): %s",
			substrate.ErrGuard, substrate.ErrLossyConversion, plan.PlanHash, plan.ChangelogSeq, strings.Join(lossyLines(plan), "; "))
	case confirm.ChangelogSeq != plan.ChangelogSeq:
		return fmt.Errorf("%w: the changelog moved since the plan was previewed (confirmed at seq %d, the head is %d): preview the plan again and confirm what it says now",
			substrate.ErrConflict, confirm.ChangelogSeq, plan.ChangelogSeq)
	case confirm.PlanHash != plan.PlanHash:
		return fmt.Errorf("%w: %w: the confirmation is for another plan (%s; this plan is %s): preview the plan again and confirm what it says now",
			substrate.ErrGuard, substrate.ErrLossyConversion, confirm.PlanHash, plan.PlanHash)
	}
	return nil
}

// convertRecords performs every conversion a batch declares and reports how
// many records it rewrote. It runs after the declaration rows projected and
// before the refs index re-derives, so the index reads the converted
// properties. The caller admitted the plan (admitConversion) first.
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
	var total int64
	for _, kc := range plan.byKind() {
		n, err := t.convertKind(kc)
		if err != nil {
			return total, fmt.Errorf("substrate/engine: convert records of %s: %w", kc.kind.Identity, err)
		}
		total += n
	}
	return total, nil
}

// convertKind rewrites every live record of one kind that any step touches,
// in id order. The id query is the union of what each step reads: a record
// carrying a renamed or a dropped property, one holding no value for a
// backfilled property, one carrying a remapped property at all (whether it
// holds the old spelling is decided in Go, value by value, where the
// container shapes are).
func (t *txn) convertKind(kc *kindConversion) (int64, error) {
	if err := t.backfillValues(kc); err != nil {
		return 0, err
	}
	args := []any{kc.kind.Identity}
	var holds []string
	bind := func(v string) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	for _, r := range kc.renames {
		holds = append(holds, "props ? "+bind(r.from))
	}
	// A backfilled or remapped property is read under the name the rows hold
	// now (storedName), because the rename that gives it the candidate's name
	// runs inside convertRecord, after this query selected the rows.
	for _, b := range kc.backfills {
		p := bind(kc.storedName(b.prop))
		holds = append(holds, "(NOT props ? "+p+" OR props->"+p+" IN "+emptyJSONValues+")")
	}
	for _, m := range kc.remaps {
		holds = append(holds, "props ? "+bind(kc.storedName(m.prop)))
	}
	for _, n := range kc.nulls {
		holds = append(holds, "props ? "+bind(n.prop))
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
// remaps, then nulls. It reports false when the record is gone or no step
// touches it, which the id query mostly excludes (a remapped property may be
// carried without the old spelling) and a concurrent write cannot produce
// under the locks the apply holds.
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
		// The value every row receives, coerced and validated once for the kind
		// (backfillValues), so the backfilled row holds exactly what a create
		// that fell back to the default would have stored.
		row.Props[b.prop] = kc.filled[b.prop]
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
	var nulled []propertyNull
	for _, n := range kc.nulls {
		if _, held := row.Props[n.prop]; !held {
			continue
		}
		delete(row.Props, n.prop)
		nulled = append(nulled, n)
		touched[n.prop] = true
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
	var nulledNames []string
	for _, n := range nulled {
		// What a patch clearing the property leaves behind: no manager row (a
		// released property is nobody's), no vectors and no queue row under a
		// name no declaration has, and no sealed material behind a secret
		// (write.go erases it on an accepted clear).
		if err := t.deleteManager(ref, n.prop); err != nil {
			return false, err
		}
		if err := t.dropEmbeddings(ref, n.prop); err != nil {
			return false, err
		}
		if old, _ := before.Props[n.prop].(string); n.secret && strings.HasPrefix(old, secretRefPrefix) {
			if _, err := t.exec(`DELETE FROM sealed WHERE ref = $1`, old); err != nil {
				return false, err
			}
			t.mirrorSealedDelete(old)
		}
		nulledNames = append(nulledNames, n.prop)
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
	if len(nulledNames) > 0 {
		sort.Strings(nulledNames)
		payload["nulled"] = nulledNames
	}
	// One entry per record, as a patch: the record's properties changed, and
	// the step keys say the apply moved them rather than a writer.
	if err := t.appendChange(t.actor, substrate.OpPatch, ref.ID, ref.Kind, payload); err != nil {
		return false, err
	}
	// The record's subjects, after its entry so the recompute's own patch
	// rides its own: a mapping from this kind projects the converted value
	// onto its target, and the target's offer rows follow. Left alone, the
	// target would hold a value the candidate no longer admits, and a rebuild
	// (which derives from the sources) would disagree with the live fold.
	return true, t.recomputeSubjectsOf(ref)
}

// dropEmbeddings removes a property's vectors and its queue row: the property
// is gone from the record, so a worker mid-flight on it finds no row and
// writes nothing (commitEmbedding), and nothing stays searchable under a name
// no declaration has.
func (t *txn) dropEmbeddings(ref eref, prop string) error {
	if _, err := t.exec(`DELETE FROM embeddings WHERE record_kind = $1 AND record_id = $2 AND property = $3`,
		ref.Kind, ref.ID, prop); err != nil {
		return err
	}
	_, err := t.exec(`DELETE FROM embed_queue WHERE record_kind = $1 AND record_id = $2 AND property = $3`,
		ref.Kind, ref.ID, prop)
	return err
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
