package engine

// Repository migration 3: quantities and metrics name their units by unit
// records (decision record 0186).
//
// Before 0186 a quantity declaration named a measure of the engine's unit
// table (`measures: mass`), a kind binding core's `metric` trait declared its
// `unit` as a UCUM string beside a `measures` string, and both `observed`
// variants carried a managed `measure` the engine stamped on every value. The
// observations and health samples shipped those shapes, and a repository that
// imported them holds copies under its own authority. This binary's loader
// does not admit `measures`, and core's new `metric` trait does not compile
// beside a metric kind whose `unit` is a string, so without this the shipped
// upgrade of core is refused and the repository stays on its old core.
//
// The migration rewrites, in every package the repository itself publishes
// or imported (never the seeded core, llm and console):
//
//   - a quantity declaration's `measures` (on a kind's property at any depth,
//     or a quantity propertytype) into `unit`, the unit the measure's values
//     were converted through, `delta: true` for a temperature difference, and
//     nothing for `arbitrary`, which had no conversions;
//   - a metric kind's `unit` and `units` into references to core's `unit`
//     kind, its `measures` dropped, and `units` declared where it is absent;
//   - an observed kind's managed `measure` dropped and listed under
//     `retired`;
//   - each live metric record's `unit` string into a reference to the unit
//     record that answers the spelling, writing that record when the
//     repository lacks it: the shipped one where the shipped seed answers
//     the spelling, else a unit of its own when the spelling is a valid record
//     id. Its `measures` goes;
//   - each live record of an observed kind loses its `measure`.
//
// What it cannot map it leaves as it is and logs: a measure this binary has
// no unit for, and a metric unit no unit answers whose spelling is not a
// valid record id. A metric left with a string unit refuses its own next
// write until the owner writes a unit record and points it there. The stored
// declaration versions stay, and an imported copy that was pristine has its
// origin digest stamped again over the rewritten rows, so the catalog still
// reads it as the version it imported and offers the shipped upgrade.
//
// The stored values themselves keep the table codes they hold (`[lb_av]`,
// `/min`): every reader resolves a code through the graph, so no value moves.
// Every write goes through the fold as an ordinary record write, so a rebuild
// replays it, and the runner asks the pass behind the open to re-derive the
// projections of every kind it folded. A second run finds nothing to do.

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	preGraphUnitKind      = "substrate.reamde.dev/core/unit"
	preGraphMetricTrait   = "substrate.reamde.dev/core/metric"
	preGraphObservedTrait = "substrate.reamde.dev/core/observed"
)

// preGraphMeasureUnits is the unit each measure of the old table now
// anchors to: a unit of the shipped seed in the linear component the
// measure's values converted through. `arbitrary` is absent on purpose: its
// units never converted to one another.
var preGraphMeasureUnits = map[string]string{
	"mass": "kg", "length": "m", "area": "m2", "volume": "m3", "duration": "s",
	"temperature": "K", "temperature-difference": "K",
	"energy": "kJ", "power": "W", "apparent-power": "V.A", "reactive-power": "var",
	"pressure": "Pa", "speed": "m/s", "pace": "s/m", "acceleration": "m/s2",
	"frequency": "Hz", "rate": "1/min", "count": "count", "fraction": "percent",
	"amount-fraction": "mmol/mol", "score": "score", "amount-of-substance": "mol",
	"amount-concentration": "mol/L", "mass-concentration": "g/L",
	"number-concentration": "1/L", "catalytic-concentration": "U/L",
	"catalytic-activity": "kat", "illuminance": "lx", "luminous-flux": "lm",
	"luminous-intensity": "cd", "luminance": "cd/m2", "irradiance": "W/m2",
	"level": "dB", "sound-pressure-level": "dBSPL", "power-level": "dBW",
	"acidity": "pH", "electric-potential": "V", "electric-current": "A",
	"electric-resistance": "Ohm", "electric-charge": "C",
	"electric-conductivity": "uS/cm", "information": "bit", "data-rate": "bit/s",
	"angle": "deg", "volume-flow": "L/min", "mass-flow": "g/s", "force": "N",
	"torque": "N.m", "activity-concentration": "Bq/m3", "absorbed-dose": "Gy",
	"dose-equivalent": "Sv", "dose-equivalent-rate": "uSv/h", "radioactivity": "Bq",
	"body-mass-index": "kg/m2", "oxygen-uptake-rate": "mL/kg/min",
	"specific-power": "W/kg", "specific-energy": "kcal/kg",
	"energy-per-distance": "kW.h/100km", "fuel-consumption": "L/100km",
	"fuel-economy": "mpg", "precipitation-rate": "mm/h",
}

// preGraphDeclaration is one stored kind or propertytype row and what the
// rewrite made of it.
type preGraphDeclaration struct {
	ref      eref
	pkg      string
	props    map[string]any
	changed  bool
	metric   bool
	observed bool
	// left are the measures the rewrite could not map, for the log.
	left []string
}

func migrateNameUnitsByUnitRecords(t *txn) (string, error) {
	ds := t.ds
	decls, err := t.preGraphDeclarations()
	if err != nil {
		return "", err
	}
	// The digest each touched package's stored closure had before the
	// rewrite, read before the first write: a copy that matched its origin
	// stamp is stamped again afterwards.
	pristine := map[string]bool{}
	for _, d := range decls {
		if !d.changed || pristine[d.pkg] {
			continue
		}
		ok, err := t.closureMatchesOrigin(d.pkg)
		if err != nil {
			return "", err
		}
		if ok {
			pristine[d.pkg] = true
		}
	}
	var rewrote, unitRows, metricRows, measureRows int
	packages := map[string]bool{}
	for _, d := range decls {
		for _, m := range d.left {
			ds.svc.log.Warn("substrate: a repository migration dropped a quantity's measure without naming a unit, because no unit stands for the measure; the property admits every unit",
				"repository", logSafeID(ds.info.ID), "declaration", logSafeID(d.ref.ID), "measure", logSafeText(m))
		}
		if !d.changed {
			continue
		}
		if err := t.migrateRow(d.ref, d.props, false); err != nil {
			return "", fmt.Errorf("rewrite %s: %w", d.ref.ID, err)
		}
		rewrote++
		packages[d.pkg] = true
	}

	units, err := t.newPreGraphUnits()
	if err != nil {
		return "", err
	}
	for _, d := range decls {
		if d.ref.Kind != kindKind {
			continue
		}
		if d.metric {
			n, err := t.migrateMetricRows(d.ref.ID, units)
			if err != nil {
				return "", err
			}
			metricRows += n
		}
		if d.observed && !declaresProperty(d.props, "measure") {
			n, err := t.dropStoredMeasures(d.ref.ID)
			if err != nil {
				return "", err
			}
			measureRows += n
		}
	}
	unitRows = units.written

	for _, pkg := range sortedKeys(packages) {
		if !pristine[pkg] {
			continue
		}
		if err := t.restampOrigin(pkg); err != nil {
			return "", err
		}
	}
	if rewrote+metricRows+measureRows+unitRows == 0 {
		return "no declaration or record names a measure or a unit string", nil
	}
	did := fmt.Sprintf("rewrote %d declarations across %d packages, %d metric records and %d observed records, and wrote %d unit records",
		rewrote, len(packages), metricRows, measureRows, unitRows)
	if len(packages) > 0 {
		// The rewritten closures name core's unit kind, which a core from
		// before decision 0186 does not declare: the load quarantines them
		// until this open's shipped upgrade declares it, and the reload
		// after the upgrade admits them.
		did += "; the rewritten packages load once this open upgrades core to declare its unit kind"
	}
	return did, nil
}

// preGraphDeclarations reads every live kind and propertytype row of a
// package the repository publishes or imported, rewritten.
func (t *txn) preGraphDeclarations() ([]preGraphDeclaration, error) {
	var refs []eref
	for _, kind := range []string{kindKind, kindPropertyType} {
		ids, err := t.liveIDs(kind)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			refs = append(refs, eref{Kind: kind, ID: id})
		}
	}
	var out []preGraphDeclaration
	for _, ref := range refs {
		row, err := t.loadRow(ref, true)
		if err != nil {
			return nil, err
		}
		if row == nil {
			continue
		}
		authority, _ := row.Props["authority"].(string)
		name, _ := row.Props["package"].(string)
		pkg := authority + "/" + name
		if authority == "" || name == "" || seededPackageName(pkg) {
			continue
		}
		d := preGraphDeclaration{ref: ref, pkg: pkg}
		d.props, d.changed, d.metric, d.observed, d.left = rewritePreGraphDeclaration(ref.Kind, row.Props)
		out = append(out, d)
	}
	return out, nil
}

// rewritePreGraphDeclaration is the declaration half of the migration, over
// one row's properties: the rewritten copy, whether anything changed, which
// of the two traits the kind binds, and the measures it could not map.
func rewritePreGraphDeclaration(kind string, props map[string]any) (out map[string]any, changed, metric, observed bool, left []string) {
	out = copyMapDeep(props)
	if kind == kindPropertyType {
		if base, _ := out["base"].(string); base == string(vocabulary.DatatypeQuantity) {
			changed, left = rewriteMeasures(out)
		}
		return out, changed, false, false, left
	}
	for _, tr := range anyStrings(out["traits"]) {
		if i := strings.IndexByte(tr, '('); i >= 0 {
			tr = tr[:i]
		}
		switch tr {
		case preGraphMetricTrait:
			metric = true
		case preGraphObservedTrait:
			observed = true
		}
	}
	declared, _ := out["properties"].(map[string]any)
	for _, name := range sortedKeys(declared) {
		p, ok := declared[name].(map[string]any)
		if !ok {
			continue
		}
		c, l := rewriteQuantityDeclarations(p)
		changed = changed || c
		left = append(left, l...)
	}
	if metric && declared != nil {
		if rewriteMetricUnitDeclarations(declared) {
			changed = true
		}
	}
	if observed && declared != nil {
		if p, ok := declared["measure"].(map[string]any); ok && p["managed"] == true {
			delete(declared, "measure")
			retireProperty(out, "measure")
			changed = true
		}
	}
	return out, changed, metric, observed, left
}

// rewriteQuantityDeclarations rewrites `measures` on one property and every
// field and link property under it.
func rewriteQuantityDeclarations(p map[string]any) (bool, []string) {
	var changed bool
	var left []string
	if p["type"] == string(vocabulary.DatatypeQuantity) {
		changed, left = rewriteMeasures(p)
	}
	for _, nested := range []string{"fields", "properties"} {
		m, _ := p[nested].(map[string]any)
		for _, name := range sortedKeys(m) {
			inner, ok := m[name].(map[string]any)
			if !ok {
				continue
			}
			c, l := rewriteQuantityDeclarations(inner)
			changed = changed || c
			left = append(left, l...)
		}
	}
	return changed, left
}

// rewriteMeasures turns one quantity declaration's `measures` into the
// `unit` it anchors to, keeping a `unit` already declared.
func rewriteMeasures(p map[string]any) (bool, []string) {
	raw, held := p["measures"]
	if !held {
		return false, nil
	}
	delete(p, "measures")
	var names []string
	switch v := raw.(type) {
	case string:
		names = []string{v}
	case []any:
		names = anyStrings(v)
	}
	if _, anchored := p["unit"]; anchored || len(names) != 1 {
		return true, nil
	}
	unit, ok := preGraphMeasureUnits[names[0]]
	if !ok {
		if names[0] == "arbitrary" {
			return true, nil
		}
		return true, names
	}
	p["unit"] = unit
	if names[0] == "temperature-difference" {
		p["delta"] = true
	}
	return true, nil
}

// rewriteMetricUnitDeclarations makes a metric kind's `unit` and `units`
// references to core's unit kind, as the trait now contracts them, and drops
// its `measures`.
func rewriteMetricUnitDeclarations(props map[string]any) bool {
	var changed bool
	if _, held := props["measures"]; held {
		delete(props, "measures")
		changed = true
	}
	unitDecl := func(name string, repeated bool, description string) {
		p, _ := props[name].(map[string]any)
		if p != nil && p["type"] == "reference" {
			return
		}
		next := map[string]any{"type": "reference", "kind": preGraphUnitKind, "description": description}
		if repeated {
			next["repeated"] = true
		}
		if p != nil {
			if d, ok := p["description"].(string); ok && d != "" && !repeated {
				next["description"] = d
			}
		}
		props[name] = next
		changed = true
	}
	unitDecl("unit", false, "the unit writers store values in and the aggregate read answers in by default, such as kg or 1/min")
	unitDecl("units", true, "the units a value may be written in, besides unit; absent admits every unit that converts exactly to unit")
	return changed
}

// retireProperty lists name under the kind's `retired.properties`.
func retireProperty(decl map[string]any, name string) {
	retired, _ := decl["retired"].(map[string]any)
	if retired == nil {
		retired = map[string]any{}
	}
	list := anyStrings(retired["properties"])
	for _, have := range list {
		if have == name {
			return
		}
	}
	list = append(list, name)
	sort.Strings(list)
	items := make([]any, len(list))
	for i, s := range list {
		items[i] = s
	}
	retired["properties"] = items
	decl["retired"] = retired
}

func declaresProperty(decl map[string]any, name string) bool {
	props, _ := decl["properties"].(map[string]any)
	_, ok := props[name]
	return ok
}

// migrateRow replaces one row's properties through the fold and appends the
// entry describing it; created marks a row the migration writes new.
func (t *txn) migrateRow(ref eref, props map[string]any, created bool) error {
	return t.migrateRowAs(substrate.ActorSystem, ref, props, created, "")
}

func (t *txn) migrateRowAs(actor substrate.Actor, ref eref, props map[string]any, created bool, title string) error {
	var before, row *erow
	if !created {
		var err error
		row, err = t.loadRow(ref, true)
		if err != nil {
			return err
		}
		if row == nil {
			return fmt.Errorf("no %s row %s", ref.Kind, ref.ID)
		}
		before = row.clone()
	} else {
		row = &erow{ID: ref.ID, Kind: ref.Kind, Title: title, States: map[string]string{}, Labels: map[string]any{}, CreatedAt: t.now, UpdatedAt: t.now}
	}
	row.Props = props
	res, err := t.foldRow(before, row, false, false)
	if err != nil || !res.changed {
		return err
	}
	payload := map[string]any{"properties": sortedKeys(props)}
	op := substrate.OpPatch
	if created {
		op = substrate.OpPut
		payload["created"] = true
	}
	return t.appendChange(actor, op, ref.ID, ref.Kind, payload)
}

// preGraphUnits resolves a metric's unit spelling to a unit id: the
// repository's own units first, then the shipped seed, whose record is
// written where the repository lacks it, then a unit of its own for a
// spelling that is a valid record id.
type preGraphUnits struct {
	repo, shipped *vocabulary.UnitGraph
	shippedByID   map[string]shippedUnitRecord
	stored        map[eref]seedRow
	resolved      map[string]string
	written       int
}

func (t *txn) newPreGraphUnits() (*preGraphUnits, error) {
	stored, _, err := readSeedRows(t.ctx, t.tx)
	if err != nil {
		return nil, err
	}
	live := map[eref]map[string]any{}
	for ref, r := range stored {
		if !r.deleted {
			live[ref] = r.props
		}
	}
	repo, _ := seedGraph(live)
	shipped, err := shippedUnitRecords()
	if err != nil {
		return nil, err
	}
	all := map[eref]map[string]any{}
	byID := map[string]shippedUnitRecord{}
	for _, s := range shipped {
		all[s.ref()] = s.props
		if s.kind == preGraphUnitKind {
			byID[s.id] = s
		}
	}
	seed, _ := seedGraph(all)
	return &preGraphUnits{repo: repo, shipped: seed, shippedByID: byID, stored: stored, resolved: map[string]string{}}, nil
}

// unitFor answers the unit id a spelling names, writing the unit record it
// needs, or why it cannot.
func (t *txn) unitFor(u *preGraphUnits, spelling string) (string, string, error) {
	if id, ok := u.resolved[spelling]; ok {
		return id, "", nil
	}
	if id, ok := u.repo.Resolve(spelling); ok {
		u.resolved[spelling] = id
		return id, "", nil
	}
	ref := func(id string) eref { return eref{Kind: preGraphUnitKind, ID: id} }
	if id, ok := u.shipped.Resolve(spelling); ok {
		if _, held := u.stored[ref(id)]; held {
			return "", fmt.Sprintf("the shipped unit %s answers it, and the repository deleted that unit", id), nil
		}
		s := u.shippedByID[id]
		name, _ := s.props["name"].(string)
		if err := t.migrateRowAs(substrate.ActorSeed, ref(id), copyMapDeep(s.props), true, name); err != nil {
			return "", "", fmt.Errorf("write the unit %s: %w", id, err)
		}
		u.stored[ref(id)] = seedRow{props: s.props}
		u.written++
		u.resolved[spelling] = id
		return id, "", nil
	}
	props := map[string]any{"name": spelling, "code": spelling}
	if _, err := vocabulary.CheckUnitRecord(spelling, props); err != nil || !vocabulary.ValidID(spelling) {
		return "", "no unit answers it and it is not a valid unit record id", nil
	}
	if _, held := u.stored[ref(spelling)]; held {
		return "", "no unit answers it and the repository deleted the unit of that id", nil
	}
	if err := t.migrateRowAs(substrate.ActorSystem, ref(spelling), props, true, spelling); err != nil {
		return "", "", fmt.Errorf("write the unit %s: %w", spelling, err)
	}
	u.stored[ref(spelling)] = seedRow{props: props}
	u.written++
	u.resolved[spelling] = spelling
	return spelling, "", nil
}

// migrateMetricRows rewrites the live records of one metric kind.
func (t *txn) migrateMetricRows(kind string, units *preGraphUnits) (int, error) {
	ids, err := t.liveIDs(kind)
	if err != nil {
		return 0, err
	}
	var n int
	for _, id := range ids {
		row, err := t.loadRow(eref{Kind: kind, ID: id}, true)
		if err != nil {
			return 0, err
		}
		if row == nil {
			continue
		}
		props := copyMapDeep(row.Props)
		changed := false
		if _, held := props["measures"]; held {
			delete(props, "measures")
			changed = true
		}
		asRef := func(spelling string) (any, string, error) {
			uid, why, err := t.unitFor(units, spelling)
			if err != nil || why != "" {
				return nil, why, err
			}
			return map[string]any{"ref": preGraphUnitKind + "/" + uid}, "", nil
		}
		leave := func(what, spelling, why string) {
			t.ds.svc.log.Warn("substrate: a repository migration left a metric's unit as written; write a unit record for it and point the metric at it",
				"repository", logSafeID(t.ds.info.ID), "record", logSafeID(vocabulary.RecordPath(kind, id)),
				"property", what, "unit", logSafeText(spelling), "reason", why)
		}
		if s, ok := props["unit"].(string); ok {
			v, why, err := asRef(s)
			if err != nil {
				return 0, err
			}
			if why != "" {
				leave("unit", s, why)
			} else {
				props["unit"] = v
				changed = true
			}
		}
		if list, ok := props["units"].([]any); ok {
			out := make([]any, 0, len(list))
			ok := true
			moved := false
			for _, item := range list {
				s, isString := item.(string)
				if !isString {
					out = append(out, item)
					continue
				}
				v, why, err := asRef(s)
				if err != nil {
					return 0, err
				}
				if why != "" {
					leave("units", s, why)
					ok = false
					break
				}
				out = append(out, v)
				moved = true
			}
			if ok && moved {
				props["units"] = out
				changed = true
			}
		}
		if !changed {
			continue
		}
		if err := t.migrateRow(eref{Kind: kind, ID: id}, props, false); err != nil {
			return 0, fmt.Errorf("rewrite %s: %w", vocabulary.RecordPath(kind, id), err)
		}
		n++
	}
	return n, nil
}

// dropStoredMeasures removes the managed `measure` from every live record of
// one observed kind that holds it.
func (t *txn) dropStoredMeasures(kind string) (int, error) {
	rows, err := t.query(`SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL AND props ? 'measure' ORDER BY id`, kind)
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
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return 0, err
	}
	for _, id := range ids {
		row, err := t.loadRow(eref{Kind: kind, ID: id}, true)
		if err != nil {
			return 0, err
		}
		if row == nil {
			continue
		}
		props := make(map[string]any, len(row.Props))
		for k, v := range row.Props {
			if k != "measure" {
				props[k] = v
			}
		}
		if err := t.migrateRow(eref{Kind: kind, ID: id}, props, false); err != nil {
			return 0, fmt.Errorf("rewrite %s: %w", vocabulary.RecordPath(kind, id), err)
		}
	}
	return len(ids), nil
}

// closureMatchesOrigin reports whether an imported package's stored closure
// still hashes to the digest its import stamped.
func (t *txn) closureMatchesOrigin(pkg string) (bool, error) {
	row, err := t.loadRow(eref{Kind: kindPackage, ID: pkg}, false)
	if err != nil || row == nil {
		return false, err
	}
	stamped, _ := row.Props[propPackageOriginDigest].(string)
	if stamped == "" {
		return false, nil
	}
	have, err := t.ds.packageClosureDigest(t.ctx, t.tx, pkg)
	if err != nil {
		return false, err
	}
	return have == stamped, nil
}

// restampOrigin writes the digest of the rewritten closure as the package's
// origin digest.
func (t *txn) restampOrigin(pkg string) error {
	digest, err := t.ds.packageClosureDigest(t.ctx, t.tx, pkg)
	if err != nil {
		return err
	}
	row, err := t.loadRow(eref{Kind: kindPackage, ID: pkg}, true)
	if err != nil || row == nil {
		return err
	}
	props := make(map[string]any, len(row.Props))
	for k, v := range row.Props {
		props[k] = v
	}
	props[propPackageOriginDigest] = digest
	return t.migrateRow(eref{Kind: kindPackage, ID: pkg}, props, false)
}

// copyMapDeep copies a row's properties, every nested map and list with
// them, so a rewrite never edits the row the fold diffs against.
func copyMapDeep(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = copyValueDeep(v)
	}
	return out
}

func copyValueDeep(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return copyMapDeep(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = copyValueDeep(item)
		}
		return out
	}
	return v
}
