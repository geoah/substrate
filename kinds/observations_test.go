package kinds_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"
)

func shippedSample(t *testing.T, pkg string) *catalog.Bundle {
	t.Helper()
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the shipped catalog: %v", err)
	}
	b, ok := cat.ByID(samples.Authority + "/" + pkg)
	if !ok {
		t.Fatalf("no shipped sample %s", pkg)
	}
	return b
}

// THE FOUR BASE KIND NAMES ARE FROZEN (decision 0159). A sample's kind cannot
// be moved by the catalog's upgrade: renaming one after the first import is a
// repository migration in every repository that took it. A fifth kind is an
// addition, which this pin lets through; a rename or a removal fails here.
func TestObservationsSampleKindNamesAreFrozen(t *testing.T) {
	b := shippedSample(t, "observations")
	for _, name := range []string{"metric", "observation", "trace", "source"} {
		if !slices.Contains(b.Closure.Kinds, samples.Authority+"/observations/"+name) {
			t.Errorf("observations/%s is gone: the base kind names are frozen before the first import", name)
		}
	}
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	if err := installClosure(reg, map[string]*catalog.Bundle{b.ID: b}, b, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	for name, variant := range map[string]string{"observation": vocabulary.ObservedSingle, "trace": vocabulary.ObservedPacked} {
		k, ok := reg.ByIdentity(samples.Authority + "/observations/" + name)
		if !ok {
			t.Fatalf("observations/%s did not install", name)
		}
		if bind, ok := k.Binding(vocabulary.TraitObservedCore); !ok || bind.Variant != variant {
			t.Errorf("observations/%s binds %+v, want %s(%s)", name, bind, vocabulary.TraitObservedCore, variant)
		}
	}
}

// Every domain sample requires the base with a floor (decisions 0070 and
// 0159), so an import of health into a repository holding an older base is
// refused rather than landing kinds that point at properties it lacks.
func TestHealthRequiresObservations(t *testing.T) {
	base := shippedSample(t, "observations")
	health := shippedSample(t, "health")
	if !slices.Contains(health.Requires, base.ID) {
		t.Fatalf("health requires %v, not %s", health.Requires, base.ID)
	}
	floor, ok := health.RequiresAtLeast[base.ID]
	switch {
	case !ok:
		t.Errorf("health requires %s with no requiresAtLeast floor", base.ID)
	case floor < 1 || floor > base.Version:
		t.Errorf("health's floor on %s is %d; the shipped base is version %d", base.ID, floor, base.Version)
	}
}

// The Apple Health adapter writes the observations and health samples' kinds
// from the Apple Health provider's mirrors, so it requires all three with a
// floor, as the other adapters do: an import into a repository missing one,
// or holding an older one, is refused rather than landing a function whose
// trigger and writes name kinds that are not there. The install follows its
// requires: provider, observations, health, then the adapter.
func TestHealthAppleRequiresHealthAndTheProvider(t *testing.T) {
	adapter := shippedSample(t, "healthapple")
	base := shippedSample(t, "observations")
	health := shippedSample(t, "health")
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the shipped catalog: %v", err)
	}
	provider, ok := cat.ByID("providers.substrate.reamde.dev/applehealth")
	if !ok {
		t.Fatal("no shipped applehealth provider")
	}
	for _, req := range []*catalog.Bundle{base, health, provider} {
		if !slices.Contains(adapter.Requires, req.ID) {
			t.Errorf("healthapple requires %v, not %s", adapter.Requires, req.ID)
		}
		if floor, ok := adapter.RequiresAtLeast[req.ID]; !ok || floor < 1 || floor > req.Version {
			t.Errorf("healthapple's floor on %s is %d (set %v); the shipped one is version %d", req.ID, floor, ok, req.Version)
		}
	}
	byPackage := map[string]*catalog.Bundle{}
	for _, b := range cat.Bundles() {
		byPackage[b.ID] = b
	}
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	if err := installClosure(reg, byPackage, adapter, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.ResolveFunction(adapter.ID + "/adaptapplehealth"); err != nil {
		t.Fatalf("the adapter's function did not install: %v", err)
	}
}

// THE GARMIN ADAPTER INSTALLS AFTER WHAT IT READS AND WRITES. It requires the
// garmin provider, whose mirror kinds its trigger watches, and the health
// sample with its observations base, whose kinds it writes, each with a floor;
// installed in `requires:` order on the seed alone, the whole chain admits.
func TestHealthGarminRequiresHealthAndTheProvider(t *testing.T) {
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the shipped catalog: %v", err)
	}
	adapter := shippedSample(t, "healthgarmin")
	byPackage := map[string]*catalog.Bundle{}
	for _, b := range cat.Bundles() {
		byPackage[b.ID] = b
	}
	for _, req := range []string{samples.Authority + "/observations", samples.Authority + "/health", "providers.substrate.reamde.dev/garmin"} {
		b, ok := byPackage[req]
		if !ok {
			t.Fatalf("no shipped bundle %s", req)
		}
		if !slices.Contains(adapter.Requires, req) {
			t.Errorf("healthgarmin requires %v, not %s", adapter.Requires, req)
		}
		if floor, ok := adapter.RequiresAtLeast[req]; !ok || floor < 1 || floor > b.Version {
			t.Errorf("healthgarmin's floor on %s is %d (set %v); the shipped one is version %d", req, floor, ok, b.Version)
		}
	}
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	if err := installClosure(reg, byPackage, adapter, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.ResolveFunction(samples.Authority + "/healthgarmin/adaptgarmin"); err != nil {
		t.Errorf("the adapter function did not install: %v", err)
	}
}

// healthRecords reads the health sample's data records of one kind (a kind
// reference under the samples authority, such as observations/metric), by id.
func healthRecords(t *testing.T, kind string) map[string]map[string]any {
	t.Helper()
	entries, err := fs.ReadDir(samples.Samples(), "health")
	if err != nil {
		t.Fatalf("read the health sample: %v", err)
	}
	out := map[string]map[string]any{}
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".yaml" {
			continue
		}
		raw, err := fs.ReadFile(samples.Samples(), "health/"+e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
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
				t.Fatalf("%s: %v", e.Name(), err)
			}
			if doc.Kind == samples.Authority+"/"+kind {
				out[doc.Metadata.ID] = doc.Data.Properties
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("the health sample ships no %s records", kind)
	}
	return out
}

// loincAxes are the LOINC parts of the lab codes the health catalogue lists,
// as the LOINC terminology publishes them (a FHIR CodeSystem $lookup against
// http://loinc.org, 2026-10-13): what is measured, the kind of quantity, and
// the specimen.
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

// A LAB METRIC'S LOINC CODES MEASURE WHAT THE METRIC DOES. A writer maps a
// result by its LOINC code, so a mass concentration code on an amount
// concentration metric files mg/dL results under a mmol/L stream, and a code
// for one analyte on its twin's metric pools two analytes. Every LOINC code on
// a concentration metric is held to the axes LOINC publishes: its property to
// the metric's measure, and the unsuffixed and `_mass` twins of one analyte to
// one component and one specimen.
func TestHealthLabMetricCodesMatchTheirMeasure(t *testing.T) {
	metrics := healthRecords(t, "observations/metric")
	type axes struct{ component, system string }
	byMetric := map[string]axes{}
	for id, props := range metrics {
		measure, _ := props["measures"].(string)
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
// (decision 0158). Only a level that stands until it is replaced goes
// without, and it is named here.
func TestHealthGaugesCarryAHold(t *testing.T) {
	standUntilReplaced := map[string]bool{
		"health/height":  true, // measured rarely, and true until the next
		"health/vo2_max": true, // an estimate, current until the next
	}
	gauges := 0
	for id, props := range healthRecords(t, "observations/metric") {
		if props["form"] != vocabulary.FormGauge {
			continue
		}
		gauges++
		hold, _ := props["hold"].(string)
		switch {
		case hold == "" && !standUntilReplaced[id]:
			t.Errorf("%s is a gauge with no hold: each value would stand until the next one", id)
		case hold != "" && standUntilReplaced[id]:
			t.Errorf("%s now carries a hold %s: drop it from the list of levels that stand until replaced", id, hold)
		}
	}
	if gauges == 0 {
		t.Fatal("no gauge metrics found")
	}
}

// declaredProperties is every property name the kinds of one shipped package
// declare, nested object properties included.
func declaredProperties(t *testing.T, dir string) map[string]bool {
	t.Helper()
	raw, err := fs.ReadFile(kinds.Bundles(), dir+"/bundle.yaml")
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	out := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if props, ok := x["properties"].(map[string]any); ok {
				for name := range props {
					out[name] = true
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var doc struct {
			Kind string         `yaml:"kind"`
			Data map[string]any `yaml:"data"`
		}
		if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if doc.Kind == "substrate.reamde.dev/core/kind" {
			walk(doc.Data)
		}
	}
	return out
}

// A GARMIN CODE IS A FIELD GARMIN DOCUMENTS. A writer maps a Garmin field to a
// metric by the catalogue's code, so a code the provider does not declare
// (an alias some deliveries use, a guess) finds no metric for the field
// Garmin actually sends.
func TestHealthGarminCodesAreFieldsTheProviderDeclares(t *testing.T) {
	fields := declaredProperties(t, "providers.substrate.reamde.dev/garmin")
	n := 0
	for id, props := range healthRecords(t, "observations/metric") {
		codes, _ := props["codes"].([]any)
		for _, raw := range codes {
			c, _ := raw.(map[string]any)
			if c["system"] != "garmin" {
				continue
			}
			n++
			if code, _ := c["code"].(string); !fields[code] {
				t.Errorf("%s lists the Garmin code %q, which the Garmin provider declares nowhere", id, code)
			}
		}
	}
	if n == 0 {
		t.Fatal("no Garmin codes found; the test reads the wrong place")
	}
}

// A METRIC'S REDUCERS ARE ONES ITS FORM TAKES. `reducers` is the catalogue's
// advice on what answers something useful, and a reader builds its choices
// from it: one the aggregate read refuses for the form is a 422 offered as a
// choice. The table is the read's own (substrate.FormReducers).
func TestHealthMetricReducersAreOnesTheirFormTakes(t *testing.T) {
	for id, props := range healthRecords(t, "observations/metric") {
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
}

func article(form string) string {
	if strings.HasPrefix(form, "amount") {
		return "an"
	}
	return "a"
}

// A KIND AN ADAPTER REWRITES HOLDS ITS REVISION. Every observations and health
// kind that declares `revision` binds `observed` or `revised`, so the write
// path refuses a stale delivery of it (decision 0162) rather than reverting a
// night, a workout or a report to an older upstream version.
func TestRevisionKindsBindTheRuleThatHoldsIt(t *testing.T) {
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the shipped catalog: %v", err)
	}
	health := shippedSample(t, "health")
	byPackage := map[string]*catalog.Bundle{}
	for _, b := range cat.Bundles() {
		byPackage[b.ID] = b
	}
	if err := installClosure(reg, byPackage, health, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, pkg := range []string{"observations", "health"} {
		for _, id := range shippedSample(t, pkg).Closure.Kinds {
			k, ok := reg.ByIdentity(id)
			if !ok {
				t.Fatalf("%s did not install", id)
			}
			if _, declares := k.Prop(vocabulary.ObservedRevision); !declares {
				continue
			}
			checked++
			_, observed := k.Binding(vocabulary.TraitObservedCore)
			_, revised := k.Binding(vocabulary.TraitRevisedCore)
			if !observed && !revised {
				t.Errorf("%s declares revision and binds neither %s nor %s: a stale delivery would revert it",
					id, vocabulary.TraitObservedCore, vocabulary.TraitRevisedCore)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no kind declares revision; the test reads the wrong place")
	}
}

// EVERY LAB TWIN HAS ITS ANALYTES. A lab analyte the catalogue lists in both
// unit systems is two metrics (`ldl_cholesterol` in mmol/L and
// `ldl_cholesterol_mass` in mg/dL), and the health sample's `convertlab`
// function writes a result of one in the other from the `analyte` records:
// one per metric of a pair, its LOINC parts the ones LOINC publishes for the
// metric's code, one molar mass on both and a conversion onto the twin.
// HbA1c's two units convert by a formula, not a molar mass.
func TestHealthAnalytesPairEveryLabTwin(t *testing.T) {
	metrics := healthRecords(t, "observations/metric")
	analytes := healthRecords(t, "health/analyte")
	metricPath := samples.Authority + "/observations/metric/"
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
	for id, props := range metrics {
		twin := id + "_mass"
		if props["measures"] != "amount-concentration" || metrics[twin] == nil {
			continue
		}
		pairs++
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
	if pairs < 8 {
		t.Errorf("%d lab twins, want the eight the catalogue lists", pairs)
	}
	for id, a := range analytes {
		parts, _ := a["loincParts"].(map[string]any)
		ax, known := loincAxes[loincOf(id)]
		if !known || parts["component"] != ax.component || parts["property"] != ax.property || parts["system"] != ax.system {
			t.Errorf("analyte %s has LOINC parts %v; its metric's code %s is %+v", id, parts, loincOf(id), ax)
		}
	}
	if target, formula := conversion("health/hba1c"); target != "health/hba1c_ifcc" || formula != "ngsp-to-ifcc" {
		t.Errorf("hba1c converts onto %q by %q", target, formula)
	}
	if target, formula := conversion("health/hba1c_ifcc"); target != "health/hba1c" || formula != "ifcc-to-ngsp" {
		t.Errorf("hba1c_ifcc converts onto %q by %q", target, formula)
	}
}
