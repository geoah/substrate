package synth

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Manual is the hand-entered data: three lab panels (a `labreport` and one
// `labresult` per analyte, from the health sample) and one weigh-in typed in
// on a hotel scale on the trip's third day when the range holds it, as
// envelope documents for `substratectl apply`. It needs
// the `observations` and `health` samples imported; the authority is the
// repository's own.
type Manual struct {
	// Authority is the repository the documents are written to; empty is
	// ada.example.com, the one the docs use.
	Authority string
}

// Name is the emitter's name.
func (Manual) Name() string { return "manual" }

// loinc is the analyte's code as the lab sends it, one of the codes the
// health metric it lands on lists (samples/health/metrics.yaml), and that
// metric.
var loinc = map[string]struct{ code, display, metric string }{
	"glucose_fasting":        {"14771-0", "Fasting glucose [Moles/volume] in Serum or Plasma", "health/fasting_glucose"},
	"glucose_fasting_mass":   {"1558-6", "Fasting glucose [Mass/volume] in Serum or Plasma", "health/fasting_glucose_mass"},
	"hba1c":                  {"4548-4", "Hemoglobin A1c/Hemoglobin.total in Blood", "health/hba1c"},
	"cholesterol_total":      {"14647-2", "Cholesterol [Moles/volume] in Serum or Plasma", "health/total_cholesterol"},
	"cholesterol_total_mass": {"2093-3", "Cholesterol [Mass/volume] in Serum or Plasma", "health/total_cholesterol_mass"},
	"ldl_cholesterol":        {"22748-8", "Cholesterol in LDL [Moles/volume] in Serum or Plasma", "health/ldl_cholesterol"},
	"ldl_cholesterol_mass":   {"13457-7", "Cholesterol in LDL [Mass/volume] in Serum or Plasma by calculation", "health/ldl_cholesterol_mass"},
	"hdl_cholesterol":        {"14646-4", "Cholesterol in HDL [Moles/volume] in Serum or Plasma", "health/hdl_cholesterol"},
	"hdl_cholesterol_mass":   {"2085-9", "Cholesterol in HDL [Mass/volume] in Serum or Plasma", "health/hdl_cholesterol_mass"},
	"triglycerides":          {"14927-8", "Triglyceride [Moles/volume] in Serum or Plasma", "health/triglycerides"},
	"triglycerides_mass":     {"2571-8", "Triglyceride [Mass/volume] in Serum or Plasma", "health/triglycerides_mass"},
	"ferritin":               {"2276-4", "Ferritin [Mass/volume] in Serum or Plasma", "health/ferritin"},
	"vitamin_d_25oh":         {"68438-1", "25-Hydroxyvitamin D3+25-Hydroxyvitamin D2 [Moles/volume] in Serum or Plasma", "health/vitamin_d"},
	"vitamin_d_25oh_mass":    {"62292-8", "25-Hydroxyvitamin D3+25-Hydroxyvitamin D2 [Mass/volume] in Serum or Plasma", "health/vitamin_d_mass"},
	"creatinine_mass":        {"2160-0", "Creatinine [Mass/volume] in Serum or Plasma", "health/creatinine_mass"},
	"tsh":                    {"3016-3", "Thyrotropin [Units/volume] in Serum or Plasma", "health/tsh"},
	"hemoglobin":             {"718-7", "Hemoglobin [Mass/volume] in Blood", "health/haemoglobin"},
	"crp":                    {"1988-5", "C reactive protein [Mass/volume] in Serum or Plasma", "health/crp"},
}

// Emit writes `labs.yaml`: the sources, the reports, the results and the
// weigh-in, one multi-document file.
func (m Manual) Emit(p *Persona) (*Output, error) {
	a := m.Authority
	if a == "" {
		a = "ada.example.com"
	}
	ref := func(kind, id string) map[string]any { return map[string]any{"ref": a + "/" + kind + "/" + id} }
	doc := func(kind, id string, props map[string]any) map[string]any {
		return map[string]any{"kind": a + "/" + kind, "metadata": map[string]any{"id": id}, "data": map[string]any{"properties": props}}
	}
	docs := []map[string]any{
		doc("observations/source", "manual", map[string]any{"name": "Entered by hand", "class": "app"}),
	}
	sources := map[string]bool{}
	for _, panel := range p.Labs() {
		if !sources[panel.Slug] {
			sources[panel.Slug] = true
			docs = append(docs, doc("observations/source", panel.Slug, map[string]any{"name": panel.Lab, "class": "service"}))
		}
		loc := p.Zone(panel.Date)
		rid := panel.Slug + "/" + panel.Date.String()
		collected := panel.Date.At(loc, 8, 10)
		_, off := collected.Zone()
		docs = append(docs, doc("health/labreport", rid, map[string]any{
			"lab": panel.Lab, "accession": fmt.Sprintf("%s-%04d%02d%02d-4471", strings.ToUpper(panel.Slug[:3]), panel.Date.Year, panel.Date.Month, panel.Date.Day),
			"orderedAt": panel.Date.At(loc, 7, 55).Format(time.RFC3339), "issuedAt": panel.Date.At(loc, 16, 40).Format(time.RFC3339),
			"status": "final", "review": "reviewed", "source": ref("observations/source", panel.Slug), "revision": 1,
		}))
		for _, r := range panel.Results {
			code, ok := loinc[r.Name]
			if !ok {
				return nil, fmt.Errorf("synth: no LOINC for %s", r.Name)
			}
			flag, display := "N", "Normal"
			if r.HasHigh && r.Value > r.High {
				flag, display = "H", "High"
			}
			if r.HasLow && r.Value < r.Low {
				flag, display = "L", "Low"
			}
			rr := map[string]any{}
			if r.HasLow {
				rr["low"] = map[string]any{"value": num(r.Low), "unit": r.Unit}
			}
			if r.HasHigh {
				rr["high"] = map[string]any{"value": num(r.High), "unit": r.Unit}
			}
			material := "serum"
			if r.Name == "hemoglobin" || r.Name == "hba1c" {
				material = "whole blood"
			}
			docs = append(docs, doc("health/labresult", rid+"/"+r.Name, map[string]any{
				"report": ref("health/labreport", rid),
				"metric": ref("observations/metric", code.metric),
				"source": ref("observations/source", panel.Slug),
				"coding": map[string]any{"system": "http://loinc.org", "code": code.code, "display": code.display},
				"form":   "gauge", "hold": "PT0S",
				"value": map[string]any{"value": num(r.Value), "unit": r.Unit},
				"at":    collected.Format(time.RFC3339), "endsAt": collected.Format(time.RFC3339), "utcOffset": off / 60,
				"specimen":        map[string]any{"material": material, "collectedAt": collected.Format(time.RFC3339), "fasting": true},
				"referenceRanges": []any{rr},
				"interpretation": []any{map[string]any{
					"system": "http://terminology.hl7.org/CodeSystem/v3-ObservationInterpretation", "code": flag, "display": display,
				}},
				"status": "final", "revision": 1,
			}))
		}
	}
	// A hotel scale on the trip, typed in by hand, held for the day like the
	// catalogue's body gauges. A range keeps a trip it holds any day of, so
	// the day is checked against the range too.
	if d, ok := p.TripRunDay(); ok && !d.Before(p.Options.Start) && !d.After(p.Options.End) {
		t := p.Day(d).atRest(d.At(p.Zone(d), 7, 25), 5*time.Minute)
		_, off := t.Zone()
		docs = append(docs, doc("observations/observation", "manual/"+d.String()+"-weight", map[string]any{
			"metric": ref("observations/metric", "health/body_mass"),
			"source": ref("observations/source", "manual"),
			"form":   "gauge", "value": map[string]any{"value": "72.0", "unit": "kg"}, "hold": "P1D",
			"at": t.Format(time.RFC3339), "utcOffset": off / 60,
		}))
	}
	var b strings.Builder
	for i, d := range docs {
		if i > 0 {
			b.WriteString("---\n")
		}
		out, err := yaml.Marshal(d)
		if err != nil {
			return nil, err
		}
		b.Write(out)
	}
	return &Output{Files: []File{{Path: "labs.yaml", Body: []byte(b.String())}}}, nil
}

// num is a decimal as the API takes it: a string, shortest form.
func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
