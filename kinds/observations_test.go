package kinds_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"path"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/engine/unitseed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"
)

// The first-party catalogues the tree ships beside the seed (records 0192
// and 0193), and the kinds of the seeded observations package their records
// are.
const (
	healthPackage      = kinds.SeedAuthority + "/health"
	labsPackage        = kinds.SeedAuthority + "/labs"
	environmentPackage = kinds.SeedAuthority + "/environment"
	clinicalPackage    = kinds.SeedAuthority + "/clinical"
	metricKind         = kinds.SeedObservationsPackage + "/metric"
	analyteKind        = labsPackage + "/analyte"
)

// clinicalKinds is every kind the clinical catalogue ships, one per FHIR
// resource it carries (record 0193).
var clinicalKinds = []string{
	"patient", "organization", "encounter", "condition", "allergy", "immunization",
	"medicationrequest", "medicationdispense", "appointment", "referral", "document", "communication",
}

func shippedCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the shipped catalog: %v", err)
	}
	return cat
}

func shippedBundle(t *testing.T, cat *catalog.Catalog, id string) *catalog.Bundle {
	t.Helper()
	b, ok := cat.ByID(id)
	if !ok {
		t.Fatalf("the shipped catalog serves no %s", id)
	}
	return b
}

func bundlesByID(cat *catalog.Catalog) map[string]*catalog.Bundle {
	out := map[string]*catalog.Bundle{}
	for _, b := range cat.Bundles() {
		out[b.ID] = b
	}
	return out
}

func loadSeed(t *testing.T) *vocabulary.Registry {
	t.Helper()
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	return reg
}

// THE BASE KIND NAMES ARE FROZEN (records 0159 and 0192). The observations
// package is seeded into every repository and every provider writes its
// kinds by name, so renaming one is a `movedFrom` upgrade of every repository
// and a new release of every provider that writes it. A new kind is an
// addition, which this pin lets through; a rename or a removal fails here.
func TestObservationsKindNamesAreFrozen(t *testing.T) {
	if !slices.Contains(kinds.SeedPackages, kinds.SeedObservationsPackage) {
		t.Fatalf("%s is not seeded: kinds.SeedPackages is %v", kinds.SeedObservationsPackage, kinds.SeedPackages)
	}
	reg := loadSeed(t)
	for _, name := range []string{"metric", "observation", "trace", "source", "sourcelink"} {
		if _, ok := reg.ByIdentity(kinds.SeedObservationsPackage + "/" + name); !ok {
			t.Errorf("%s/%s is not seeded: the base kind names are frozen", kinds.SeedObservationsPackage, name)
		}
	}
	for name, variant := range map[string]string{"observation": vocabulary.ObservedSingle, "trace": vocabulary.ObservedPacked} {
		k, ok := reg.ByIdentity(kinds.SeedObservationsPackage + "/" + name)
		if !ok {
			continue
		}
		if bind, ok := k.Binding(vocabulary.TraitObservedCore); !ok || bind.Variant != variant {
			t.Errorf("%s/%s binds %+v, want %s(%s)", kinds.SeedObservationsPackage, name, bind, vocabulary.TraitObservedCore, variant)
		}
	}
}

// THE WELL-KNOWN CATALOGUES ARE FIRST-PARTY PACKAGES (record 0192): the
// catalog serves each one PUBLISHED under the seed authority, so it lands
// under the name it ships with and every provider can name its kinds. A
// catalogue that rode the sample door would be rehomed, and the kinds a
// provider's adapter writes would not exist under the names it writes.
func TestFirstPartyCataloguesArePublished(t *testing.T) {
	cat := shippedCatalog(t)
	for _, c := range []struct {
		id        string
		kinds     []string
		functions []string
		requires  []string
		triggers  map[string]string
	}{
		{id: healthPackage, kinds: []string{healthPackage + "/sleep", healthPackage + "/workout"}},
		{
			id:        labsPackage,
			kinds:     []string{analyteKind, labsPackage + "/labreport", labsPackage + "/labresult"},
			functions: []string{labsPackage + "/convertlab"},
			requires:  []string{healthPackage},
			triggers:  map[string]string{"labs-on-labresult": vocabulary.CoreKind(vocabulary.DocFunction) + "/" + labsPackage + "/convertlab"},
		},
		{id: environmentPackage},
		{id: clinicalPackage, kinds: clinicalKindRefs(), requires: []string{labsPackage}},
	} {
		b := shippedBundle(t, cat, c.id)
		if b.Tier != substrate.TierProvider || b.Authority != kinds.SeedAuthority {
			t.Errorf("%s is served as a %s under %s, want a published package under %s", c.id, b.Tier, b.Authority, kinds.SeedAuthority)
		}
		if landed := b.LandedID("ada.example.com"); landed != c.id {
			t.Errorf("%s lands as %s in the repository ada.example.com, want the name it ships with", c.id, landed)
		}
		for _, k := range c.kinds {
			if !slices.Contains(b.Closure.Kinds, k) {
				t.Errorf("%s does not ship %s: its kinds are %v", c.id, k, b.Closure.Kinds)
			}
		}
		if len(c.kinds) == 0 && len(b.Closure.Kinds) != 0 {
			t.Errorf("%s is a catalogue of records and declares kinds %v", c.id, b.Closure.Kinds)
		}
		for _, fn := range c.functions {
			if !slices.Contains(b.Closure.Functions, fn) {
				t.Errorf("%s does not ship %s: its functions are %v", c.id, fn, b.Closure.Functions)
			}
		}
		for _, req := range c.requires {
			if !slices.Contains(b.Requires, req) {
				t.Errorf("%s requires %v, not %s", c.id, b.Requires, req)
			}
		}
		for id, callable := range c.triggers {
			if got := b.Closure.TriggerCallables[id]; got != callable {
				t.Errorf("%s's trigger %s calls %q, want %s", c.id, id, got, callable)
			}
		}
		for _, req := range b.Requires {
			if vocabulary.SeededPackage(req) {
				t.Errorf("%s requires the seeded %s, which every repository already holds", c.id, req)
			}
		}
	}
	// labs installs onto the seed with health, the order the catalog's
	// install brings them in.
	reg := loadSeed(t)
	if err := installClosure(reg, bundlesByID(cat), shippedBundle(t, cat, labsPackage), map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if err := installClosure(reg, bundlesByID(cat), shippedBundle(t, cat, environmentPackage), map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.ResolveFunction(labsPackage + "/convertlab"); err != nil {
		t.Errorf("convertlab did not install: %v", err)
	}
	// clinical installs onto the seed with labs and health, so a consultation
	// can point at a lab report.
	clinical := loadSeed(t)
	if err := installClosure(clinical, bundlesByID(cat), shippedBundle(t, cat, clinicalPackage), map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	for _, k := range append(clinicalKindRefs(), labsPackage+"/labreport", healthPackage+"/workout") {
		if _, ok := clinical.ByIdentity(k); !ok {
			t.Errorf("installing clinical did not declare %s", k)
		}
	}
}

func clinicalKindRefs() []string {
	refs := make([]string, len(clinicalKinds))
	for i, k := range clinicalKinds {
		refs[i] = clinicalPackage + "/" + k
	}
	return refs
}

// A PROVIDER'S ADAPTER LANDS WITH THE PROVIDER AND WRITES THE HEALTH
// CATALOGUE (record 0192). Each health provider requires `health`, so the
// install brings it, and the adapter reads and writes only kinds the
// install guarantees: the provider's own, the seed's, and the packages the
// provider requires. A trigger calling the adapter on the provider's mirror
// rows ships beside it. A floor on health, where one is set, is a version
// the tree ships.
func TestProviderAdaptersRequireHealth(t *testing.T) {
	cat := shippedCatalog(t)
	health := shippedBundle(t, cat, healthPackage)
	for _, c := range []struct{ provider, adapter, trigger string }{
		{"providers.substrate.reamde.dev/applehealth", "adaptapplehealth", "applehealth-to-health"},
		{"providers.substrate.reamde.dev/whoop", "adaptwhoop", "whoop-to-health"},
	} {
		b := shippedBundle(t, cat, c.provider)
		if !slices.Contains(b.Requires, healthPackage) {
			t.Errorf("%s requires %v, not %s", c.provider, b.Requires, healthPackage)
		}
		if floor, ok := b.RequiresAtLeast[healthPackage]; ok && (floor < 1 || floor > health.Version) {
			t.Errorf("%s's floor on %s is %d; the shipped one is version %d", c.provider, healthPackage, floor, health.Version)
		}
		fnID := c.provider + "/" + c.adapter
		if !slices.Contains(b.Closure.Functions, fnID) {
			t.Errorf("%s does not ship %s: its functions are %v", c.provider, fnID, b.Closure.Functions)
		}
		if got, want := b.Closure.TriggerCallables[c.trigger], vocabulary.CoreKind(vocabulary.DocFunction)+"/"+fnID; got != want {
			t.Errorf("%s's trigger %s calls %q, want %s", c.provider, c.trigger, got, want)
		}
		reg := loadSeed(t)
		if err := installClosure(reg, bundlesByID(cat), b, map[string]bool{}); err != nil {
			t.Fatal(err)
		}
		fn, err := reg.ResolveFunction(fnID)
		if err != nil {
			t.Errorf("%s did not install: %v", fnID, err)
			continue
		}
		if !slices.Contains(fn.Caps.Emit, kinds.SeedObservationsPackage+"/observation") ||
			!slices.Contains(fn.Caps.Emit, healthPackage+"/sleep") {
			t.Errorf("%s writes %v, want observations and health nights", fnID, fn.Caps.Emit)
		}
		touched := slices.Clone(fn.Caps.Emit)
		if fn.Caps.Reads != nil {
			touched = append(touched, fn.Caps.Reads.Kinds...)
		}
		for _, k := range touched {
			pkg := vocabulary.KindPackage(k)
			if pkg != c.provider && !vocabulary.SeededPackage(pkg) && !slices.Contains(b.Requires, pkg) {
				t.Errorf("%s names %s, whose package %s neither ships with nor is required by %s", fnID, k, pkg, c.provider)
			}
		}
	}
}

// A METRIC'S ID STARTS WITH THE PACKAGE THAT SHIPS IT (record 0192). A metric
// is a record, and an install writes it by id: two packages shipping one id
// would overwrite each other's row, and an owner's own metrics take another
// prefix. The seeded observations package ships none, because a seed holds
// declarations alone.
func TestEveryShippedMetricIsPrefixedByItsPackage(t *testing.T) {
	shippedBy := map[string]string{}
	err := fs.WalkDir(kinds.All(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".yaml" {
			return err
		}
		pkg := path.Dir(p)
		for id := range decodeRecords(t, kinds.All(), p, metricKind) {
			if pkg == kinds.SeedObservationsPackage {
				t.Errorf("%s ships metric %s; the seeded base ships no metrics", p, id)
			}
			if !strings.HasPrefix(id, path.Base(pkg)+"/") {
				t.Errorf("%s ships metric %s, which does not start with %s/", p, id, path.Base(pkg))
			}
			if prev, dup := shippedBy[id]; dup {
				t.Errorf("metric %s is shipped by %s and %s", id, prev, p)
			}
			shippedBy[id] = p
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the tree: %v", err)
	}
	// The lab metrics are `labs/*` and the climate ones `environment/*`
	// (record 0192): a writer that maps a lab result names the id, so the
	// old spellings are gone and the new ones are where the writers look.
	labs := []string{
		"ldl_cholesterol", "ldl_cholesterol_mass", "hdl_cholesterol", "hdl_cholesterol_mass",
		"total_cholesterol", "total_cholesterol_mass", "triglycerides", "triglycerides_mass",
		"fasting_glucose", "fasting_glucose_mass", "hba1c", "hba1c_ifcc", "tsh",
		"vitamin_d", "vitamin_d_mass", "ferritin", "crp", "haemoglobin",
		"creatinine", "creatinine_mass", "alt", "ast",
	}
	for _, name := range labs {
		if _, ok := shippedBy["labs/"+name]; !ok {
			t.Errorf("labs/%s is not shipped", name)
		}
		if p, ok := shippedBy["health/"+name]; ok {
			t.Errorf("%s still ships health/%s, which is labs/%s", p, name, name)
		}
	}
	for _, name := range []string{"air_temperature", "relative_humidity", "illuminance", "atmospheric_pressure", "energy", "power"} {
		if _, ok := shippedBy["environment/"+name]; !ok {
			t.Errorf("environment/%s is not shipped", name)
		}
	}
	for _, name := range []string{"recovery", "strain", "sleep_performance"} {
		if p := shippedBy["whoop/"+name]; path.Dir(p) != "providers.substrate.reamde.dev/whoop" {
			t.Errorf("whoop/%s is shipped by %q, want the WHOOP provider", name, p)
		}
	}
	if len(shippedBy) == 0 {
		t.Fatal("no shipped metric found; the walk reads the wrong place")
	}
}

// decodeRecords reads the data records of one kind in one file of fsys, by id.
func decodeRecords(t *testing.T, fsys fs.FS, file, kind string) map[string]map[string]any {
	t.Helper()
	raw, err := fs.ReadFile(fsys, file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	out := map[string]map[string]any{}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
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
		if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
			return out
		} else if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if doc.Kind == kind {
			out[doc.Metadata.ID] = doc.Data.Properties
		}
	}
}

// shippedRecords reads the data records of one kind the named packages ship,
// by id, from each package's directory in the tree.
func shippedRecords(t *testing.T, kind string, packages ...string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, pkg := range packages {
		entries, err := fs.ReadDir(kinds.All(), pkg)
		if err != nil {
			t.Fatalf("read %s: %v", pkg, err)
		}
		for _, e := range entries {
			if e.IsDir() || path.Ext(e.Name()) != ".yaml" {
				continue
			}
			for id, props := range decodeRecords(t, kinds.All(), pkg+"/"+e.Name(), kind) {
				out[id] = props
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("%v ship no %s records", packages, kind)
	}
	return out
}

// loincAxes are the LOINC parts of the lab codes the health and labs
// catalogues list, as the LOINC terminology publishes them (a FHIR CodeSystem
// $lookup against http://loinc.org, 2026-10-13): what is measured, the kind
// of quantity, and the specimen.
var loincAxes = map[string]struct{ component, property, system string }{
	"22748-8": {"Cholesterol.in LDL", "SCnc", "Ser/Plas"},
	"2089-1":  {"Cholesterol.in LDL", "MCnc", "Ser/Plas"},
	"13457-7": {"Cholesterol.in LDL", "MCnc", "Ser/Plas"},
	"14646-4": {"Cholesterol.in HDL", "SCnc", "Ser/Plas"},
	"2085-9":  {"Cholesterol.in HDL", "MCnc", "Ser/Plas"},
	"14647-2": {"Cholesterol", "SCnc", "Ser/Plas"},
	"2093-3":  {"Cholesterol", "MCnc", "Ser/Plas"},
	"14927-8": {"Triglyceride", "SCnc", "Ser/Plas"},
	"2571-8":  {"Triglyceride", "MCnc", "Ser/Plas"},
	"14771-0": {"Glucose^post CFst", "SCnc", "Ser/Plas"},
	"1558-6":  {"Glucose^post CFst", "MCnc", "Ser/Plas"},
	"15074-8": {"Glucose", "SCnc", "Bld"},
	"2339-0":  {"Glucose", "MCnc", "Bld"},
	"68438-1": {"Calcidiol+ercalcidiol", "SCnc", "Ser/Plas"},
	"62292-8": {"Calcidiol+ercalcidiol", "MCnc", "Ser/Plas"},
	"1989-3":  {"Calcidiol", "MCnc", "Ser/Plas"},
	"14635-7": {"Calcidiol", "SCnc", "Ser/Plas"},
	"14682-9": {"Creatinine", "SCnc", "Ser/Plas"},
	"2160-0":  {"Creatinine", "MCnc", "Ser/Plas"},
	"2276-4":  {"Ferritin", "MCnc", "Ser/Plas"},
	"718-7":   {"Hemoglobin", "MCnc", "Bld"},
	"1988-5":  {"C reactive protein", "MCnc", "Ser/Plas"},
	"4548-4":  {"Hemoglobin A1c/Hemoglobin.total", "MFr", "Bld"},
	"59261-8": {"Hemoglobin A1c/Hemoglobin.total^^standardized per IFCC-RMP for CDT", "SFr", "Bld"},
}

// loincConcentration is the measure a concentration's LOINC property names.
var loincConcentration = map[string]string{"SCnc": "amount-concentration", "MCnc": "mass-concentration"}

// concentrationOf answers which concentration a metric's unit is, by which
// base its shipped unit converts to through linear conversions:
// "mass-concentration" for g/L, "amount-concentration" for mol/L, else "".
func concentrationOf(t *testing.T) func(props map[string]any) string {
	t.Helper()
	g, problems, err := unitseed.Graph()
	if err != nil || len(problems) > 0 {
		t.Fatalf("the shipped unit graph: %v %v", err, problems)
	}
	return func(props map[string]any) string {
		ref, _ := props["unit"].(map[string]any)
		path, _ := ref["ref"].(string)
		kind, id, ok := vocabulary.SplitRecordPath(path)
		if !ok || kind != vocabulary.KindUnit {
			return ""
		}
		for base, name := range map[string]string{"g/L": "mass-concentration", "mol/L": "amount-concentration"} {
			if _, _, err := g.Linear(id, base, false); err == nil {
				return name
			}
		}
		return ""
	}
}

// A LAB METRIC'S LOINC CODES MEASURE WHAT THE METRIC DOES. A writer maps a
// result by its LOINC code, so a mass concentration code on an amount
// concentration metric files mg/dL results under a mmol/L stream, and a code
// for one analyte on its twin's metric pools two analytes. Every LOINC code on
// a concentration metric is held to the axes LOINC publishes: its property to
// the metric's measure, and the unsuffixed and `_mass` twins of one analyte to
// one component and one specimen.
func TestLabMetricCodesMatchTheirMeasure(t *testing.T) {
	metrics := shippedRecords(t, metricKind, healthPackage, labsPackage)
	type axes struct{ component, system string }
	byMetric := map[string]axes{}
	concentration := concentrationOf(t)
	for id, props := range metrics {
		measure := concentration(props)
		if measure != "amount-concentration" && measure != "mass-concentration" {
			continue
		}
		codes, _ := props["codes"].([]any)
		for _, raw := range codes {
			c, _ := raw.(map[string]any)
			if c["system"] != "http://loinc.org" {
				continue
			}
			code, _ := c["code"].(string)
			ax, known := loincAxes[code]
			switch {
			case !known:
				t.Errorf("%s lists LOINC %s, which this test has no axes for: add them from LOINC", id, code)
				continue
			case loincConcentration[ax.property] != measure:
				t.Errorf("%s measures %s and lists LOINC %s, a %s (%s) code", id, measure, code, ax.property, ax.component)
			}
			got := axes{ax.component, ax.system}
			if prev, seen := byMetric[id]; seen && prev != got {
				t.Errorf("%s lists LOINC codes for %v and %v", id, prev, got)
			}
			byMetric[id] = got
		}
	}
	pairs := 0
	for id, ax := range byMetric {
		twin, ok := byMetric[id+"_mass"]
		if !ok {
			continue
		}
		pairs++
		if twin != ax {
			t.Errorf("%s is %v and %s_mass is %v: the twins of one analyte share a component and a specimen", id, ax, id, twin)
		}
	}
	if pairs == 0 {
		t.Fatal("no amount and mass concentration twins found; the test reads the wrong place")
	}
}

// A GAUGE METRIC CARRIES A HOLD. A writer copies a metric's `hold` onto each
// value, and a gauge value written without one stands until the next value of
// its stream, often a day later: the coverage, the time-weighted mean and the
// priority policy then read a night's last SpO2 reading as the whole next day
// (decision 0158). A spot reading (a weighing, a blood pressure, a night's
// skin temperature, an estimate) holds PT0S: it is a point in time, so an
// average of it is the mean of the readings rather than a mean weighted by
// the gaps between them.
func TestHealthAndLabGaugesCarryAHold(t *testing.T) {
	spot := map[string]bool{
		"health/body_mass": true, "health/body_fat_fraction": true, "health/fat_mass": true,
		"health/muscle_mass": true, "health/body_water_fraction": true, "health/bone_mass": true,
		"health/bmi": true, "health/height": true, "health/vo2_max": true, "health/hrv_sdnn": true,
		"health/skin_temperature": true, "health/skin_temperature_deviation": true,
		"health/systolic_blood_pressure": true, "health/diastolic_blood_pressure": true,
	}
	gauges := 0
	for id, props := range shippedRecords(t, metricKind, healthPackage, labsPackage) {
		if props["form"] != vocabulary.FormGauge {
			continue
		}
		gauges++
		hold, _ := props["hold"].(string)
		switch {
		case hold == "":
			t.Errorf("%s is a gauge with no hold: each value would stand until the next one", id)
		case spot[id] && hold != "PT0S":
			t.Errorf("%s is a spot reading holding %s: an average of it would weigh each reading by the gap after it; hold PT0S", id, hold)
		}
	}
	if gauges == 0 {
		t.Fatal("no gauge metrics found")
	}
}

// A METRIC'S REDUCERS ARE ONES ITS FORM TAKES. `reducers` is the catalogue's
// advice on what answers something useful, and a reader builds its choices
// from it: one the aggregate read refuses for the form is a 422 offered as a
// choice. The table is the read's own (substrate.FormReducers). Every metric
// the tree ships is held, a provider's own included.
func TestShippedMetricReducersAreOnesTheirFormTakes(t *testing.T) {
	metrics := 0
	err := fs.WalkDir(kinds.All(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".yaml" {
			return err
		}
		for id, props := range decodeRecords(t, kinds.All(), p, metricKind) {
			metrics++
			form, _ := props["form"].(string)
			if _, coded := props["codeSet"]; coded {
				form = "coded " + form
			}
			takes, ok := substrate.FormReducers[form]
			if !ok {
				t.Errorf("%s has form %q", id, form)
				continue
			}
			reducers, _ := props["reducers"].([]any)
			for _, r := range reducers {
				if !slices.Contains(takes, r.(string)) {
					t.Errorf("%s lists %s, which the aggregate read refuses for %s %s", id, r, article(form), form)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the tree: %v", err)
	}
	if metrics == 0 {
		t.Fatal("no shipped metric found; the walk reads the wrong place")
	}
}

func article(form string) string {
	if strings.HasPrefix(form, "amount") {
		return "an"
	}
	return "a"
}

// A KIND AN ADAPTER REWRITES HOLDS ITS REVISION. Every observations, health,
// labs and clinical kind that declares `revision` binds `observed` or
// `revised`, so the write path refuses a stale delivery of it (decision 0162)
// rather than reverting a night, a workout, a report or a consultation to an
// older upstream version.
func TestRevisionKindsBindTheRuleThatHoldsIt(t *testing.T) {
	reg := loadSeed(t)
	cat := shippedCatalog(t)
	if err := installClosure(reg, bundlesByID(cat), shippedBundle(t, cat, clinicalPackage), map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, k := range reg.Kinds() {
		switch k.Package {
		case kinds.SeedObservationsPackage, healthPackage, labsPackage, clinicalPackage:
		default:
			continue
		}
		if _, declares := k.Prop(vocabulary.ObservedRevision); !declares {
			continue
		}
		checked++
		_, observed := k.Binding(vocabulary.TraitObservedCore)
		_, revised := k.Binding(vocabulary.TraitRevisedCore)
		if !observed && !revised {
			t.Errorf("%s declares revision and binds neither %s nor %s: a stale delivery would revert it",
				k.Identity, vocabulary.TraitObservedCore, vocabulary.TraitRevisedCore)
		}
	}
	if checked == 0 {
		t.Fatal("no kind declares revision; the test reads the wrong place")
	}
}

// EVERY LAB TWIN HAS ITS ANALYTES. A lab analyte the catalogue lists in both
// unit systems is two metrics (`labs/ldl_cholesterol` in mmol/L and
// `labs/ldl_cholesterol_mass` in mg/dL), and the labs package's `convertlab`
// function writes a result of one in the other from the `analyte` records:
// one per metric of a pair, its LOINC parts the ones LOINC publishes for the
// metric's code, one molar mass on both and a conversion onto the twin.
// HbA1c's two units convert by a formula, not a molar mass. Blood glucose is
// no pair: its units convert (TestHealthGlucoseIsOneMetricInTwoUnits).
func TestLabAnalytesPairEveryLabTwin(t *testing.T) {
	metrics := shippedRecords(t, metricKind, healthPackage, labsPackage)
	analytes := shippedRecords(t, analyteKind, labsPackage)
	metricPath := metricKind + "/"
	conversion := func(id string) (target, formula string) {
		a := analytes[id]
		if a == nil {
			t.Errorf("%s has no analyte record", id)
			return "", ""
		}
		meta, _ := a["metric"].(map[string]any)
		if meta["ref"] != metricPath+id {
			t.Errorf("analyte %s points at %v", id, a["metric"])
		}
		convs, _ := a["conversions"].([]any)
		if len(convs) != 1 {
			t.Errorf("analyte %s lists %d conversions, want the one onto its twin", id, len(convs))
			return "", ""
		}
		c, _ := convs[0].(map[string]any)
		ref, _ := c["target"].(map[string]any)
		target, _ = ref["ref"].(string)
		formula, _ = c["formula"].(string)
		return strings.TrimPrefix(target, metricPath), formula
	}
	loincOf := func(id string) string {
		codes, _ := metrics[id]["codes"].([]any)
		for _, raw := range codes {
			if c, _ := raw.(map[string]any); c["system"] == "http://loinc.org" {
				code, _ := c["code"].(string)
				return code
			}
		}
		return ""
	}
	pairs := 0
	concentration := concentrationOf(t)
	for id, props := range metrics {
		twin := id + "_mass"
		if concentration(props) != "amount-concentration" || metrics[twin] == nil {
			continue
		}
		pairs++
		if !strings.HasPrefix(id, "labs/") {
			t.Errorf("%s and %s are a lab twin outside the labs catalogue", id, twin)
		}
		for _, side := range [][2]string{{id, twin}, {twin, id}} {
			target, formula := conversion(side[0])
			if target != side[1] || formula != "molar-mass" {
				t.Errorf("analyte %s converts onto %q by %q, want %s by molar-mass", side[0], target, formula, side[1])
			}
		}
		if analytes[id]["molarMass"] == nil || analytes[id]["molarMass"] != analytes[twin]["molarMass"] {
			t.Errorf("%s and %s carry molar masses %v and %v, want one", id, twin, analytes[id]["molarMass"], analytes[twin]["molarMass"])
		}
	}
	if pairs < 7 {
		t.Errorf("%d lab twins, want the seven the catalogue lists", pairs)
	}
	for id, a := range analytes {
		if metrics[id] == nil {
			t.Errorf("analyte %s names a metric neither health nor labs ships", id)
		}
		parts, _ := a["loincParts"].(map[string]any)
		ax, known := loincAxes[loincOf(id)]
		if !known || parts["component"] != ax.component || parts["property"] != ax.property || parts["system"] != ax.system {
			t.Errorf("analyte %s has LOINC parts %v; its metric's code %s is %+v", id, parts, loincOf(id), ax)
		}
	}
	if target, formula := conversion("labs/hba1c"); target != "labs/hba1c_ifcc" || formula != "ngsp-to-ifcc" {
		t.Errorf("labs/hba1c converts onto %q by %q", target, formula)
	}
	if target, formula := conversion("labs/hba1c_ifcc"); target != "labs/hba1c" || formula != "ifcc-to-ngsp" {
		t.Errorf("labs/hba1c_ifcc converts onto %q by %q", target, formula)
	}
}

// unitDocs decodes the unit and conversion records of one YAML file.
func unitDocs(t *testing.T, raw []byte) ([]vocabulary.UnitRecord, []vocabulary.ConversionRecord) {
	t.Helper()
	var us []vocabulary.UnitRecord
	var cs []vocabulary.ConversionRecord
	dec := yaml.NewDecoder(bytes.NewReader(raw))
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
		if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decode: %v", err)
		}
		switch doc.Kind {
		case vocabulary.KindUnit:
			u, err := vocabulary.CheckUnitRecord(doc.Metadata.ID, doc.Data.Properties)
			if err != nil {
				t.Fatalf("unit %s: %v", doc.Metadata.ID, err)
			}
			us = append(us, u)
		case vocabulary.KindConversion:
			c, err := vocabulary.CheckConversionRecord(doc.Metadata.ID, doc.Data.Properties)
			if err != nil {
				t.Fatalf("conversion %s: %v", doc.Metadata.ID, err)
			}
			cs = append(cs, c)
		}
	}
	return us, cs
}

// BLOOD GLUCOSE IS ONE METRIC IN TWO UNITS. A meter reports mg/dL or
// mmol/L, and the plain units never convert, because the factor is the
// analyte's molar mass. The health catalogue ships `mmol/L{glucose}` and
// `mg/dL{glucose}` and one conversion between them, and
// `health/blood_glucose` anchors to the first, so a mixed history is one
// series; the labs catalogue's analyte for it carries the molar mass. Over
// the seeded graph with health's units added: the two annotated units
// convert at the analyte record's molar mass, the plain units still convert
// to nothing across, and no annotated unit joins a plain one.
func TestHealthGlucoseIsOneMetricInTwoUnits(t *testing.T) {
	us, cs := unitDocs(t, unitseed.Units())
	more, moreCs := unitDocs(t, unitseed.Conversions())
	us, cs = append(us, more...), append(cs, moreCs...)
	raw, err := fs.ReadFile(kinds.All(), healthPackage+"/analyte_units.yaml")
	if err != nil {
		t.Fatalf("read the glucose units: %v", err)
	}
	more, moreCs = unitDocs(t, raw)
	if len(more) != 2 || len(moreCs) != 1 {
		t.Fatalf("analyte_units.yaml ships %d units and %d conversions, want 2 and 1", len(more), len(moreCs))
	}
	g, problems := vocabulary.BuildUnitGraph(append(us, more...), append(cs, moreCs...))
	if len(problems) > 0 {
		t.Fatalf("the seeded graph with the glucose units: %v", problems)
	}
	for code, want := range map[string]string{"mmol/L{glucose}": "mmol/L_glucose", "mg/dL{glucose}": "mg/dL_glucose"} {
		if id, ok := g.Resolve(code); !ok || id != want {
			t.Errorf("%s resolves to %q, want %s", code, id, want)
		}
	}
	analyte := shippedRecords(t, analyteKind, labsPackage)["health/blood_glucose"]
	molar, ok := new(big.Rat).SetString(fmt.Sprint(analyte["molarMass"]))
	if !ok {
		t.Fatalf("health/blood_glucose's analyte carries molar mass %v", analyte["molarMass"])
	}
	// mmol/L to mg/dL: times the molar mass (mg per mmol), over 10 (dL per L).
	want := new(big.Rat).Quo(molar, big.NewRat(10, 1))
	factor, offset, err := g.Linear("mmol/L_glucose", "mg/dL_glucose", false)
	if err != nil || factor.Cmp(want) != 0 || offset.Sign() != 0 {
		t.Errorf("mmol/L{glucose} to mg/dL{glucose} is %v + %v (%v), want %v from the molar mass %v", factor, offset, err, want, molar)
	}
	for _, pair := range [][2]string{{"mmol/L", "mg/dL"}, {"mmol/L_glucose", "mmol/L"}, {"mg/dL_glucose", "mg/dL"}} {
		if _, _, err := g.Linear(pair[0], pair[1], false); err == nil {
			t.Errorf("%s converts to %s; a plain and an analyte-annotated concentration must stay apart", pair[0], pair[1])
		}
	}
	metrics := shippedRecords(t, metricKind, healthPackage, labsPackage)
	ref, _ := metrics["health/blood_glucose"]["unit"].(map[string]any)
	if ref["ref"] != vocabulary.KindUnit+"/mmol/L_glucose" {
		t.Errorf("health/blood_glucose's unit is %v, want mmol/L_glucose", metrics["health/blood_glucose"]["unit"])
	}
	if _, kept := metrics["health/blood_glucose_mass"]; kept {
		t.Error("health/blood_glucose_mass is still shipped: a mass reading is health/blood_glucose in mg/dL{glucose}")
	}
}
