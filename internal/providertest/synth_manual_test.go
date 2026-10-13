package providertest

// The contract between internal/synth's hand-entered records and the shipped
// health sample: every document of `labs.yaml` written as `load` writes it,
// then the sample's own trigger runs `convertlab`, which writes each lab
// result on a paired metric in the other unit system. A change to the lab
// result's shape, to a metric's codes, or to an analyte pair that the
// emitter does not follow fails here.

import (
	"bytes"
	"errors"
	"io"
	"math/big"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/synth"
	"github.com/geoah/substrate/samples"
)

const healthLabResult = samples.Authority + "/health/labresult"

func TestSynthManualRecordsLandAndConvert(t *testing.T) {
	requirePython(t)
	t.Parallel()
	p, err := synth.NewPersona(synth.Options{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := synth.Manual{Authority: samples.Authority}.Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	_, ds := newCoreDataset(t)
	importVocabulary(t, ds, "health")

	// Every document, in file order, as `synth load` writes it.
	dec := yaml.NewDecoder(bytes.NewReader(out.Files[0].Body))
	inputs := map[string]string{}
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
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatal(err)
		}
		mustPut(t, ds, substrate.PutInput{Kind: doc.Kind, ID: doc.Metadata.ID, Properties: doc.Data.Properties})
		if doc.Kind == healthLabResult {
			inputs[doc.Metadata.ID] = metricOf(doc.Data.Properties)
		}
	}
	healthDrain(t, ds)

	// Each result on a metric of an analyte pair is written once more in
	// the pair's other metric, the US panel's mg/dL as mmol/L and the
	// Athens panels' mmol/L as mg/dL; the rest are not.
	pairs := map[string]string{}
	for _, row := range listLive(t, ds, samples.Authority+"/health/analyte") {
		metric := strings.TrimPrefix(storedRefPath(row.Properties["metric"]), obsMetric+"/")
		convs, _ := row.Properties["conversions"].([]any)
		for _, c := range convs {
			pairs[metric] = strings.TrimPrefix(storedRefPath(c.(map[string]any)["target"]), obsMetric+"/")
		}
	}
	derived := 0
	for _, row := range listLive(t, ds, healthLabResult) {
		if _, ok := inputs[row.ID]; ok {
			continue
		}
		derived++
		from := refIDs(row, "derivedFrom")
		if len(from) != 1 || pairs[inputs[from[0]]] != metricOf(row.Properties) || row.ID != from[0]+"/as/"+metricOf(row.Properties) {
			t.Errorf("derived result %s on %s from %v", row.ID, metricOf(row.Properties), from)
		}
	}
	want := 0
	for _, metric := range inputs {
		if pairs[metric] != "" {
			want++
		}
	}
	if derived != want || want < 20 {
		t.Errorf("%d derived lab results, want %d, one per result on a paired metric", derived, want)
	}

	// Two values end to end, one each way.
	for id, wantValue := range map[string][2]string{
		"harborpoint/2026-08-31/ldl_cholesterol_mass/as/health/ldl_cholesterol": {"2.897", "mmol/L"},
		"bioiatriki/2026-07-20/glucose_fasting/as/health/fasting_glucose_mass":  {"97.3", "mg/dL"},
		"bioiatriki/2026-07-20/hba1c/as/health/hba1c_ifcc":                      {"34.4", "mmol/mol"},
	} {
		v, u := quantityOf(t, mustGet(t, ds, healthLabResult, id).Properties, "value")
		if v != wantValue[0] || u != wantValue[1] {
			t.Errorf("%s is %s %s, want %s %s", id, v, u, wantValue[0], wantValue[1])
		}
	}
	// A converted reference bound is rounded to the converted value's places,
	// so the two compare digit for digit and a result keeps its side of the
	// range: 3 mmol/L of LDL is 116 mg/dL, not 120 beside a value of 120.
	ranged := 0
	for _, row := range listLive(t, ds, healthLabResult) {
		if _, ok := inputs[row.ID]; ok {
			continue
		}
		v, _ := quantityOf(t, row.Properties, "value")
		places := decimalPlaces(v)
		src := mustGet(t, ds, healthLabResult, refIDs(row, "derivedFrom")[0]).Properties
		sv, _ := quantityOf(t, src, "value")
		srcRanges, _ := src["referenceRanges"].([]any)
		ranges, _ := row.Properties["referenceRanges"].([]any)
		for i, r := range ranges {
			for _, bound := range []string{"low", "high"} {
				b, ok := r.(map[string]any)[bound].(map[string]any)
				if !ok {
					continue
				}
				ranged++
				bv, _ := b["value"].(string)
				if decimalPlaces(bv) != places {
					t.Errorf("%s: %s %s has %d places, the value %s has %d", row.ID, bound, bv, decimalPlaces(bv), v, places)
				}
				sb, _ := srcRanges[i].(map[string]any)[bound].(map[string]any)["value"].(string)
				if cmpDecimal(t, sv, sb) != cmpDecimal(t, v, bv) {
					t.Errorf("%s: the source %s sits %+d against its %s %s, the converted %s %+d against %s",
						row.ID, sv, cmpDecimal(t, sv, sb), bound, sb, v, cmpDecimal(t, v, bv), bv)
				}
			}
		}
	}
	if ranged == 0 {
		t.Error("no derived result carries a converted reference bound")
	}
	ldl := mustGet(t, ds, healthLabResult, "bioiatriki/2026-10-05/ldl_cholesterol/as/health/ldl_cholesterol_mass").Properties
	if high := ldl["referenceRanges"].([]any)[0].(map[string]any)["high"].(map[string]any); high["value"] != "116" || high["unit"] != "mg/dL" {
		t.Errorf("3 mmol/L of LDL converted to %v %v, want 116 mg/dL", high["value"], high["unit"])
	}
	weight := mustGet(t, ds, obsObservation, "manual/2026-09-22-weight").Properties
	if v, u := quantityOf(t, weight, "value"); v != "72.0" || u != "kg" || weight["hold"] != "P1D" {
		t.Errorf("the hotel weigh-in is %v", weight)
	}
}

// decimalPlaces is how many digits a decimal's text holds after its point.
func decimalPlaces(s string) int {
	if _, frac, ok := strings.Cut(s, "."); ok {
		return len(frac)
	}
	return 0
}

// cmpDecimal compares two decimals' texts by value.
func cmpDecimal(t *testing.T, a, b string) int {
	t.Helper()
	x, ok1 := new(big.Rat).SetString(a)
	y, ok2 := new(big.Rat).SetString(b)
	if !ok1 || !ok2 {
		t.Fatalf("%q or %q is not a decimal", a, b)
	}
	return x.Cmp(y)
}
