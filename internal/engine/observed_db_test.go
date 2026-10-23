package engine_test

// The observed trait on the write path, against Postgres and the shipped
// samples imported the way a repository imports them (rehomed onto its own
// authority): the rules every write of an observation, a trace and a lab
// result is held to, a patch included; a revision that never falls; the
// managed stats a trace's values produce; and the null a packed list admits.
// The rules themselves are vocabulary.CheckObserved, tested there case by
// case; these tests prove the write path holds a real record to them.

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/samples"
)

// observedRepo is a repository holding the shipped units and the
// observations and health samples, imported through the catalog's door.
type observedRepo struct {
	ds          substrate.Dataset
	observation string
	trace       string
	metric      string
	source      string
	labreport   string
	labresult   string
}

func openObserved(t *testing.T) observedRepo {
	t.Helper()
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	c, err := catalog.Load(catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the sample catalog: %v", err)
	}
	putSampleUnits(t, ds, "observations", "health")
	for _, id := range []string{samples.Authority + "/observations", samples.Authority + "/health"} {
		if _, _, err := c.Import(ctx, substrate.ActorAPI, id, ds); err != nil {
			t.Fatalf("import %s: %v", id, err)
		}
	}
	home := testdb.Repository(t)
	return observedRepo{
		ds:          ds,
		observation: home + "/observations/observation",
		trace:       home + "/observations/trace",
		metric:      home + "/observations/metric",
		source:      home + "/observations/source",
		labreport:   home + "/health/labreport",
		labresult:   home + "/health/labresult",
	}
}

func (r observedRepo) metricRef(id string) map[string]any {
	return map[string]any{"ref": r.metric + "/" + id}
}

func (r observedRepo) put(t *testing.T, kind, id string, props map[string]any) (*substrate.Record, error) {
	t.Helper()
	return r.ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{Kind: kind, ID: id, Properties: props})
}

func (r observedRepo) patch(t *testing.T, kind, id string, props map[string]any) (*substrate.Record, error) {
	t.Helper()
	return r.ds.Patch(context.Background(), substrate.ActorAPI, kind, id, substrate.PatchInput{Properties: props})
}

func (r observedRepo) get(t *testing.T, kind, id string) *substrate.Record {
	t.Helper()
	rec, err := r.ds.Get(context.Background(), kind, id)
	if err != nil {
		t.Fatalf("get %s/%s: %v", kind, id, err)
	}
	return rec
}

func quantity(value, unit string) map[string]any { return map[string]any{"value": value, "unit": unit} }

// refused asserts a validation refusal naming the property and the rule.
func refused(t *testing.T, what string, err error, want string) {
	t.Helper()
	if !errors.Is(err, substrate.ErrValidation) {
		t.Errorf("%s: err = %v, want a validation refusal", what, err)
		return
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("%s: err = %v, want it to say %q", what, err, want)
	}
}

func weighing(r observedRepo) map[string]any {
	return map[string]any{
		"metric": r.metricRef("health/body_mass"),
		"form":   "gauge", "value": quantity("72.40", "kg"),
		"at": "2026-10-07T06:41:12+03:00", "utcOffset": 180, "revision": 1,
	}
}

// The import lands the four base kinds, the health kinds and both catalogues,
// none of the catalogue a vendor's adapter ships, and an observation titles
// itself from its metric and its value.
func TestObservedSamplesImportWithTheirCatalogues(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	ctx := context.Background()
	page, err := r.ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{r.metric}}, First: 500})
	if err != nil {
		t.Fatalf("list metrics: %v", err)
	}
	ids := map[string]bool{}
	for _, rec := range page.Records {
		ids[rec.ID] = true
	}
	for _, id := range []string{
		"air_temperature", "atmospheric_pressure", "health/heart_rate", "health/sleep_stage",
		"health/ldl_cholesterol", "health/hba1c_ifcc",
	} {
		if !ids[id] {
			t.Errorf("metric %s did not land; landed %d", id, len(ids))
		}
	}
	// A vendor's own score ships with its adapter sample (healthwhoop), so a
	// repository that imports health without WHOOP holds no WHOOP metric.
	for id := range ids {
		if strings.Contains(id, "whoop") {
			t.Errorf("the health import landed the WHOOP metric %s", id)
		}
	}
	rec, err := r.put(t, r.observation, "withings/george/5f3a91/weight", weighing(r))
	if err != nil {
		t.Fatalf("put a weighing: %v", err)
	}
	if rec.Title != "Body weight 72.40 kg" {
		t.Errorf("title = %q, want %q", rec.Title, "Body weight 72.40 kg")
	}
}

// decision 0155: `about` points at any kind, and absent means the owner.
func TestObservedAboutAdmitsAnyKind(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	for id, about := range map[string]any{
		"about-a-source": map[string]any{"ref": r.source + "/kitchen-thermometer"},
		"about-a-report": map[string]any{"ref": r.labreport + "/medichecks/george/MC-1"},
		"about-nobody":   nil,
	} {
		props := map[string]any{
			"metric": r.metricRef("air_temperature"), "form": "gauge",
			"value": quantity("21.5", "Cel"), "at": "2026-10-07T14:00:00Z",
		}
		if about != nil {
			props["about"] = about
		}
		if _, err := r.put(t, r.observation, id, props); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
}

// decision 0156: a single value is a number or a code, never both, on a
// create and on a patch that would leave both.
func TestObservedValueAndCodeAreExclusive(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	both := weighing(r)
	both["code"] = "open"
	_, err := r.put(t, r.observation, "both", both)
	refused(t, "a create with both", err, "props.value: an observed value is a number or a code, never both")

	if _, err := r.put(t, r.observation, "door", map[string]any{
		"metric": r.metricRef("air_temperature"), "form": "gauge", "code": "open", "at": "2026-10-07T17:42:05Z",
	}); err != nil {
		t.Fatalf("a coded value: %v", err)
	}
	_, err = r.patch(t, r.observation, "door", map[string]any{"value": quantity("1", "1")})
	refused(t, "a patch adding a value beside the code", err, "never both")
	// Switching shapes in one patch is a write that leaves one.
	if _, err := r.patch(t, r.observation, "door", map[string]any{"code": nil, "value": quantity("21", "Cel")}); err != nil {
		t.Errorf("a patch switching code for value: %v", err)
	}
	_, err = r.patch(t, r.observation, "door", map[string]any{"value": nil})
	refused(t, "a patch leaving neither", err, "props.value: an observed value needs value or code")
	if _, err := r.patch(t, r.observation, "door", map[string]any{"value": nil, "absentReason": "error"}); err != nil {
		t.Errorf("a patch saying why there is none: %v", err)
	}
}

// The other rules reach the write path: a hold on a counter, a summary with
// no statistic, a circular value in a unit with no turn.
func TestObservedRulesHoldOnEveryWrite(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	if _, err := r.put(t, r.observation, "w", weighing(r)); err != nil {
		t.Fatalf("put: %v", err)
	}
	_, err := r.patch(t, r.observation, "w", map[string]any{"form": "counter", "hold": "PT1H"})
	refused(t, "a held counter", err, "props.hold: only a gauge holds its value; a counter does not")
	_, err = r.patch(t, r.observation, "w", map[string]any{"form": "summary", "endsAt": "2026-10-08T00:00:00+03:00", "period": "day"})
	refused(t, "a summary without a statistic", err, "props.statistic: a summary names the statistic")
	_, err = r.patch(t, r.observation, "w", map[string]any{"circular": true})
	refused(t, "a circular value in kilograms", err, "props.circular: averages an angle on the circle, and kg declares no turn")
	_, err = r.patch(t, r.observation, "w", map[string]any{"value": map[string]any{"value": "0.5", "unit": "kg", "comparator": "<"}})
	refused(t, "a comparator where none is declared", err, "admits a comparator only where it declares `comparator: true`")
}

// decision 0162: a rewrite carries a revision at least as high as the stored
// one; an equal one is admitted, a lower one is a stale delivery.
func TestObservedWriteRefusesAFallingRevision(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	props := weighing(r)
	props["revision"] = 3
	if _, err := r.put(t, r.observation, "w", props); err != nil {
		t.Fatalf("put: %v", err)
	}
	props["value"] = quantity("72.10", "kg")
	if _, err := r.put(t, r.observation, "w", props); err != nil {
		t.Errorf("an equal revision: %v", err)
	}
	props["revision"] = 2
	props["value"] = quantity("71.00", "kg")
	_, err := r.put(t, r.observation, "w", props)
	refused(t, "a lower revision", err, "props.revision: 2 is below the stored revision 3")
	if got := r.get(t, r.observation, "w").Properties["value"].(map[string]any)["value"]; got != "72.10" {
		t.Errorf("the stale write landed: value %v", got)
	}
	// A patch that names no revision keeps the stored one.
	if _, err := r.patch(t, r.observation, "w", map[string]any{"utcOffset": 120}); err != nil {
		t.Errorf("a patch without a revision: %v", err)
	}
	_, err = r.patch(t, r.observation, "w", map[string]any{"revision": nil})
	refused(t, "removing the revision", err, "props.revision: the stored revision 3 is never removed")
}

// A trace keeps its nulls, stores its unit as the unit's id, and carries the
// stats the engine computes from its values, whatever the writer sent.
func TestObservedTraceStatsAreComputedAndNullsKept(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	id := "applehealth/iphone/garmin-forerunner-265/heart_rate/2026-10-07T06"
	if _, err := r.put(t, r.trace, id, map[string]any{
		"metric": r.metricRef("health/heart_rate"), "form": "gauge", "unit": "bpm", "hold": "PT4M",
		"at": "2026-10-07T06:00:00Z", "endsAt": "2026-10-07T07:00:00Z",
		"utcOffset": 180,
		"offsets":   []any{0, 118000, 241000, 362000},
		"values":    []any{"58", nil, "61", "57"},
		"revision":  2,
		"stats":     map[string]any{"count": 9999},
	}); err != nil {
		t.Fatalf("put a trace: %v", err)
	}
	got := r.get(t, r.trace, id).Properties
	if got["unit"] != "1/min" {
		t.Errorf("unit = %v, want the unit id 1/min", got["unit"])
	}
	if values := got["values"].([]any); len(values) != 4 || values[1] != nil {
		t.Errorf("values = %v, want the null kept at index 1", values)
	}
	stats, _ := got["stats"].(map[string]any)
	want := map[string]any{
		"count": float64(3), "min": "57", "max": "61", "mean": 58.666666666666664, "m2": 8.666666666666666,
		"first":    map[string]any{"at": "2026-10-07T06:00:00Z", "value": "58"},
		"last":     map[string]any{"at": "2026-10-07T06:06:02Z", "value": "57"},
		"coverage": "PT6M2S",
	}
	if !reflect.DeepEqual(stats, want) {
		t.Errorf("stats = %v\nwant    %v", stats, want)
	}
	// A rewrite of the values moves the stats with them.
	if _, err := r.patch(t, r.trace, id, map[string]any{"values": []any{"60", "60", "60", "60"}}); err != nil {
		t.Fatalf("patch the values: %v", err)
	}
	stats, _ = r.get(t, r.trace, id).Properties["stats"].(map[string]any)
	if stats["count"] != float64(4) || stats["m2"] != float64(0) {
		t.Errorf("stats after the rewrite = %v", stats)
	}
	// A null belongs to a packed list alone.
	_, err := r.patch(t, r.trace, id, map[string]any{"offsets": []any{0, nil, 2, 3}})
	refused(t, "a null offset", err, "props.offsets: [1]")
	_, err = r.patch(t, r.trace, id, map[string]any{"offsets": []any{0, 1, 2}})
	refused(t, "short offsets", err, "props.offsets: 3 offsets for 4 values")
}

// A trace stored at exactly the byte limit, its stamped stats counted, is
// admitted on its create and on its identical rewrite, and one byte more is
// refused on its create. A check that left a stamped property out admitted
// that create, and then refused its identical rewrite.
func TestObservedTraceSizeCountsItsStats(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	trace := func(pad int) map[string]any {
		return map[string]any{
			"metric": r.metricRef("health/heart_rate"), "form": "gauge", "unit": "1/min", "hold": "PT4M",
			"at": "2026-10-07T06:00:00Z", "endsAt": "2026-10-07T07:00:00Z",
			"offsets": []any{0, 118000}, "values": []any{"58", "61"},
			"adapter": strings.Repeat("x", pad),
		}
	}
	if _, err := r.put(t, r.trace, "probe", trace(0)); err != nil {
		t.Fatalf("put the probe: %v", err)
	}
	// The limit holds the row's properties: the hot times are columns
	// beside them and the title is derived, so neither counts.
	size := func(id string) int {
		props := maps.Clone(r.get(t, r.trace, id).Properties)
		if props["stats"] == nil {
			t.Fatalf("%s carries no stats: %v", id, props)
		}
		for _, name := range []string{"at", "endsAt", "title"} {
			delete(props, name)
		}
		raw, err := json.Marshal(props)
		if err != nil {
			t.Fatal(err)
		}
		return len(raw)
	}
	exact := vocabulary.MaxTraceBytes - size("probe")
	if _, err := r.put(t, r.trace, "exact", trace(exact)); err != nil {
		t.Fatalf("create a trace stored at exactly the limit: %v", err)
	}
	if got := size("exact"); got != vocabulary.MaxTraceBytes {
		t.Fatalf("the stored trace is %d bytes, want %d", got, vocabulary.MaxTraceBytes)
	}
	if _, err := r.put(t, r.trace, "exact", trace(exact)); err != nil {
		t.Errorf("the identical rewrite of a trace stored at exactly the limit: %v", err)
	}
	_, err := r.put(t, r.trace, "past", trace(exact+1))
	refused(t, "a trace stored one byte past the limit", err, "a trace holds at most 1048576 (1 MiB)")
}

// A trace spaced by zero is refused as a validation problem on the write
// path, never a panic in the stats its size check computes.
func TestObservedTraceRefusesAZeroSpacing(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	_, err := r.put(t, r.trace, "zero", map[string]any{
		"metric": r.metricRef("health/heart_rate"), "form": "gauge", "unit": "/min", "hold": "PT4M",
		"at": "2026-10-07T06:00:00Z", "endsAt": "2026-10-07T07:00:00Z",
		"every": "PT0S", "values": []any{"1"},
	})
	refused(t, "a zero spacing", err, "props.every: the spacing between points is longer than zero")
}

// A lab result holds exactly one shape on every write, a patch that switches
// shapes included, and its value may carry a comparator.
func TestObservedLabResultHoldsExactlyOneShape(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	id := "medichecks/george/MC-2026-091422/tsh"
	if _, err := r.put(t, r.labresult, id, map[string]any{
		"metric": r.metricRef("health/tsh"), "hold": "PT0S",
		"value": map[string]any{"value": "0.01", "unit": "m[IU]/L", "comparator": "<"},
		"at":    "2026-09-14T08:05:00+01:00", "status": "final", "revision": 1,
	}); err != nil {
		t.Fatalf("put a bounded result: %v", err)
	}
	if form := r.get(t, r.labresult, id).Properties["form"]; form != "gauge" {
		t.Errorf("form = %v, want the default gauge", form)
	}
	_, err := r.patch(t, r.labresult, id, map[string]any{"ratio": map[string]any{"numerator": "1", "denominator": "80"}})
	refused(t, "a second shape", err, "this record holds value and ratio")
	if _, err := r.patch(t, r.labresult, id, map[string]any{
		"value": nil, "codedValue": map[string]any{"system": "http://snomed.info/sct", "code": "260415000", "display": "Not detected"},
		"code": "not-detected",
	}); err != nil {
		t.Errorf("a patch switching to a coded result: %v", err)
	}
	_, err = r.patch(t, r.labresult, id, map[string]any{"codedValue": nil, "code": nil})
	refused(t, "a patch leaving no shape", err, "this record holds none")
	if _, err := r.patch(t, r.labresult, id, map[string]any{
		"codedValue": nil, "code": nil, "absentReason": "not-performed",
	}); err != nil {
		t.Errorf("a patch to an absent result: %v", err)
	}
}
