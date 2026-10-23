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

// THE BASE KIND NAMES ARE FROZEN (decision 0159). A sample's kind cannot be
// moved by the catalog's upgrade: renaming one after the first import is a
// repository migration in every repository that took it. A new kind is an
// addition, which this pin lets through; a rename or a removal fails here.
func TestObservationsSampleKindNamesAreFrozen(t *testing.T) {
	b := shippedSample(t, "observations")
	for _, name := range []string{"metric", "observation", "trace", "source", "sourcelink"} {
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
func TestHealthLabMetricCodesMatchTheirMeasure(t *testing.T) {
	metrics := healthRecords(t, "observations/metric")
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
func TestHealthGaugesCarryAHold(t *testing.T) {
	spot := map[string]bool{
		"health/body_mass": true, "health/body_fat_fraction": true, "health/fat_mass": true,
		"health/muscle_mass": true, "health/body_water_fraction": true, "health/bone_mass": true,
		"health/bmi": true, "health/height": true, "health/vo2_max": true, "health/hrv_sdnn": true,
		"health/skin_temperature": true, "health/skin_temperature_deviation": true,
		"health/systolic_blood_pressure": true, "health/diastolic_blood_pressure": true,
	}
	gauges := 0
	for id, props := range healthRecords(t, "observations/metric") {
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
// HbA1c's two units convert by a formula, not a molar mass. Blood glucose is
// no pair: its units convert (TestHealthGlucoseIsOneMetricInTwoUnits).
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
	concentration := concentrationOf(t)
	for id, props := range metrics {
		twin := id + "_mass"
		if concentration(props) != "amount-concentration" || metrics[twin] == nil {
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
	if pairs < 7 {
		t.Errorf("%d lab twins, want the seven the catalogue lists", pairs)
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
// analyte's molar mass. The health sample ships `mmol/L{glucose}` and
// `mg/dL{glucose}` and one conversion between them, and
// `health/blood_glucose` anchors to the first, so a mixed history is one
// series. Over the seeded graph with the sample's units added: the two
// annotated units convert at the analyte record's molar mass, the plain
// units still convert to nothing across, and no annotated unit joins a
// plain one.
func TestHealthGlucoseIsOneMetricInTwoUnits(t *testing.T) {
	us, cs := unitDocs(t, unitseed.Units())
	more, moreCs := unitDocs(t, unitseed.Conversions())
	us, cs = append(us, more...), append(cs, moreCs...)
	raw, err := fs.ReadFile(samples.Samples(), "health/analyte_units.yaml")
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
	analyte := healthRecords(t, "health/analyte")["health/blood_glucose"]
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
	metric := healthRecords(t, "observations/metric")["health/blood_glucose"]
	ref, _ := metric["unit"].(map[string]any)
	if ref["ref"] != vocabulary.KindUnit+"/mmol/L_glucose" {
		t.Errorf("health/blood_glucose's unit is %v, want mmol/L_glucose", metric["unit"])
	}
	if _, kept := healthRecords(t, "observations/metric")["health/blood_glucose_mass"]; kept {
		t.Error("health/blood_glucose_mass is still shipped: a mass reading is health/blood_glucose in mg/dL{glucose}")
	}
}
