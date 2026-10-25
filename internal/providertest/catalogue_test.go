package providertest

// The health adapters' Python tables repeat the health catalogue
// (samples/health/metrics.yaml and the workout kind): the metric each vendor
// field lands on, its unit, form and hold, the sleep stage codes and the
// workout sport codes, and for WHOOP the API field each catalogue metric
// lists as its `whoop` code. Nothing at write time compares the two, so a
// metric renamed, a code dropped or a hold changed in one place would leave
// the other writing something the catalogue no longer says. These tests read
// both sides: the tables as written (TestAdapterTablesMatchTheCatalogue) and
// the outputs an adapter wrote (assertCatalogued, called by the adapter
// suites).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/unitseed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// catalogueMetric is what the tests read of one health catalogue metric.
type catalogueMetric struct {
	// Unit is the id of the unit record the metric's `unit` references.
	Unit    string
	Form    string
	Hold    string
	CodeSet []string
	// Codes is every code the metric lists, by system.
	Codes map[string][]string
}

// healthCatalogue reads the shipped catalogue, keyed by metric id
// (`health/heart_rate`), with the metrics a vendor's adapter sample ships
// beside it (`healthwhoop/recovery`).
func healthCatalogue(t *testing.T) map[string]catalogueMetric {
	t.Helper()
	out := map[string]catalogueMetric{}
	for _, file := range []string{"/health/metrics.yaml", "/healthwhoop/metrics.yaml"} {
		n := 0
		for _, m := range loadDocs(t, samplesDir+file) {
			if kind, _ := m["kind"].(string); !strings.HasSuffix(kind, "/observations/metric") {
				continue
			}
			id, _ := m["metadata"].(map[string]any)["id"].(string)
			props, _ := m["data"].(map[string]any)["properties"].(map[string]any)
			out[id] = catalogueMetricOf(props)
			n++
		}
		if n == 0 {
			t.Fatalf("%s read empty", file)
		}
	}
	return out
}

func catalogueMetricOf(props map[string]any) catalogueMetric {
	str := func(k string) string { s, _ := props[k].(string); return s }
	m := catalogueMetric{Form: str("form"), Hold: str("hold"), Codes: map[string][]string{}}
	if ref, ok := props["unit"].(map[string]any); ok {
		path, _ := ref["ref"].(string)
		if kind, id, ok := vocabulary.SplitRecordPath(path); ok && kind == vocabulary.KindUnit {
			m.Unit = id
		}
	}
	for _, c := range asList(props["codeSet"]) {
		m.CodeSet = append(m.CodeSet, fmt.Sprint(c))
	}
	for _, c := range asList(props["codes"]) {
		code, _ := c.(map[string]any)
		m.Codes[fmt.Sprint(code["system"])] = append(m.Codes[fmt.Sprint(code["system"])], fmt.Sprint(code["code"]))
	}
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// workoutSports is the workout kind's sport code set.
func workoutSports(t *testing.T) []string {
	t.Helper()
	for _, m := range loadDocs(t, samplesDir+"/health/workout.yaml") {
		props, _ := m["data"].(map[string]any)["properties"].(map[string]any)
		sport, _ := props["sport"].(map[string]any)
		var out []string
		for _, v := range asList(sport["values"]) {
			out = append(out, fmt.Sprint(v))
		}
		if len(out) > 0 {
			return out
		}
	}
	t.Fatal("the workout kind declares no sport values")
	return nil
}

// adapterTables reads a function body's module-level literal assignments and
// every string literal in it, through Python's own parser: nothing in the
// body runs.
type adapterTables struct {
	Tables  map[string]any `json:"tables"`
	Strings []string       `json:"strings"`
	// MetricRefs is every literal argument of a call to `metric_ref`.
	MetricRefs []string `json:"metricRefs"`
}

const tablesProbe = `
import ast, json, sys
tree = ast.parse(sys.stdin.read())
tables = {}
for node in tree.body:
    if isinstance(node, ast.Assign) and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name):
        try:
            tables[node.targets[0].id] = ast.literal_eval(node.value)
        except ValueError:
            pass
strings = sorted({n.value for n in ast.walk(tree) if isinstance(n, ast.Constant) and isinstance(n.value, str)})
refs = sorted({n.args[0].value for n in ast.walk(tree)
               if isinstance(n, ast.Call) and isinstance(n.func, ast.Name) and n.func.id == "metric_ref"
               and n.args and isinstance(n.args[0], ast.Constant)})
print(json.dumps({"tables": tables, "strings": strings, "metricRefs": refs}))
`

func readAdapterTables(t *testing.T, bundle, function string) adapterTables {
	t.Helper()
	var source string
	for _, m := range loadDocs(t, bundle) {
		if id, _ := m["metadata"].(map[string]any)["id"].(string); strings.HasSuffix(id, "/"+function) {
			source, _ = m["data"].(map[string]any)["source"].(string)
		}
	}
	if source == "" {
		t.Fatalf("%s declares no function %s", bundle, function)
	}
	cmd := exec.Command("python3", "-I", "-c", tablesProbe)
	cmd.Stdin = strings.NewReader(source)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("read %s's tables: %v\n%s", function, err, stderr.String())
	}
	var out adapterTables
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s's tables: %v", function, err)
	}
	return out
}

// rows is a table of tuples as lists of strings, None as "".
func rows(t *testing.T, tables map[string]any, name string) [][]string {
	t.Helper()
	raw, ok := tables[name].([]any)
	if !ok || len(raw) == 0 {
		t.Fatalf("the table %s is missing or empty", name)
	}
	var out [][]string
	for _, r := range raw {
		var row []string
		for _, v := range asList(r) {
			if v == nil {
				row = append(row, "")
			} else {
				row = append(row, fmt.Sprint(v))
			}
		}
		out = append(out, row)
	}
	return out
}

func mapping(t *testing.T, tables map[string]any, name string) map[string]any {
	t.Helper()
	m, ok := tables[name].(map[string]any)
	if !ok || len(m) == 0 {
		t.Fatalf("the table %s is missing or empty", name)
	}
	return m
}

// sameComponent reports whether a unit spelling is the id of a shipped unit
// a linear conversion joins to the metric's unit, which is what the metric
// rules admit and the aggregate read converts (decision 0186). A spelling
// other than the id would be stored as the id, so an adapter's reconcile
// would find every value it compares changed. A metric naming no unit
// admits any.
func sameComponent(t *testing.T, unit string, m catalogueMetric) bool {
	t.Helper()
	g, problems, err := unitseed.Graph()
	if err != nil || len(problems) > 0 {
		t.Fatalf("the shipped unit graph: %v %v", err, problems)
	}
	id, ok := g.Resolve(unit)
	if !ok || id != unit {
		return false
	}
	if m.Unit == "" {
		// A metric naming no unit (a categorical one) admits every unit.
		return true
	}
	_, _, err = g.Linear(id, m.Unit, false)
	return err == nil
}

// snakeCase is a WHOOP mirror field in the API's own spelling, the code the
// catalogue lists: `hrvRmssdMilli` is `hrv_rmssd_milli`.
func snakeCase(field string) string {
	var b strings.Builder
	for i, r := range field {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

var healthMetricLiteral = regexp.MustCompile(`^health(whoop)?/[a-z0-9_]+$`)

// TestAdapterTablesMatchTheCatalogue: every metric an adapter's tables or
// literals name is in the catalogue, in the id of a unit that converts to
// the metric's unit;
// Apple Health's raw readings are in the metric's own unit, form and hold;
// WHOOP's mapped fields are the `whoop` codes the metric lists; every sleep
// stage code is in `health/sleep_stage`'s code set and every sport code in
// the workout kind's.
func TestAdapterTablesMatchTheCatalogue(t *testing.T) {
	requirePython3(t)
	catalogue := healthCatalogue(t)
	sports := workoutSports(t)
	metric := func(t *testing.T, id string) catalogueMetric {
		t.Helper()
		m, ok := catalogue[id]
		if !ok {
			t.Fatalf("%s is not a catalogue metric", id)
		}
		return m
	}
	stages := metric(t, "health/sleep_stage").CodeSet

	t.Run("whoop", func(t *testing.T) {
		got := readAdapterTables(t, samplesDir+"/healthwhoop/bundle.yaml", "adaptwhoop")
		for _, s := range got.Strings {
			if healthMetricLiteral.MatchString(s) {
				metric(t, s)
			}
		}
		// (score field, metric, unit, ...): the field is the metric's
		// `whoop` code, and the unit converts to the metric's.
		check := func(table string, unitAt int) {
			for _, row := range rows(t, got.Tables, table) {
				field, id, unit := row[0], row[1], row[unitAt]
				m := metric(t, id)
				if !sameComponent(t, unit, m) {
					t.Errorf("%s: %s is written in %s, not the id of a unit that converts to %s's unit %s", table, field, unit, id, m.Unit)
				}
				if !slices.Contains(m.Codes["whoop"], snakeCase(field)) {
					t.Errorf("%s: %s maps WHOOP's %s, which the metric does not list as a whoop code (it lists %v)", table, id, snakeCase(field), m.Codes["whoop"])
				}
			}
		}
		check("RECOVERY_READINGS", 2)
		check("CYCLE_READINGS", 2)
		check("SLEEP_READINGS", 2)
		for _, row := range rows(t, got.Tables, "CYCLE_READINGS") {
			if form, m := row[3], metric(t, row[1]); form != "summary" && form != m.Form {
				t.Errorf("CYCLE_READINGS: %s is written as %s, the metric's form is %s", row[1], form, m.Form)
			}
		}
		if id, _ := got.Tables["STAGE_METRIC"].(string); id != "health/sleep_stage" {
			t.Errorf("STAGE_METRIC is %q", id)
		}
		for _, row := range rows(t, got.Tables, "STAGES") {
			if !slices.Contains(stages, row[1]) {
				t.Errorf("STAGES: %s maps to %q, not a health/sleep_stage code %v", row[0], row[1], stages)
			}
		}
		for name, code := range mapping(t, got.Tables, "SPORTS") {
			if !slices.Contains(sports, fmt.Sprint(code)) {
				t.Errorf("SPORTS: %s maps to %v, not a workout sport", name, code)
			}
		}
	})

	t.Run("applehealth", func(t *testing.T) {
		got := readAdapterTables(t, samplesDir+"/healthapple/bundle.yaml", "adaptapplehealth")
		for _, name := range got.MetricRefs {
			metric(t, "health/"+name)
		}
		// A raw reading is written in the metric's own unit, form and hold;
		// a daily value is a summary of a summary metric.
		for name, spec := range mapping(t, got.Tables, "METRICS") {
			row := asList(spec)
			text := func(i int) string {
				if row[i] == nil {
					return ""
				}
				return fmt.Sprint(row[i])
			}
			id, form, unit, hold := "health/"+text(0), text(2), text(3), text(4)
			m := metric(t, id)
			if unit != m.Unit || form != m.Form || hold != m.Hold {
				t.Errorf("METRICS[%s]: %s is written as %s in %s holding %q; the catalogue says %s in %s holding %q",
					name, id, form, unit, hold, m.Form, m.Unit, m.Hold)
			}
		}
		pressure := rows(t, map[string]any{"PRESSURE": []any{got.Tables["PRESSURE"]}}, "PRESSURE")[0]
		for _, id := range pressure[:2] {
			if m := metric(t, "health/"+id); pressure[2] != m.Unit || pressure[3] != m.Hold || m.Form != "gauge" {
				t.Errorf("PRESSURE: %s is a gauge in %s holding %s; the catalogue says %s in %s holding %q", id, pressure[2], pressure[3], m.Form, m.Unit, m.Hold)
			}
		}
		hold, _ := got.Tables["GLUCOSE_HOLD"].(string)
		for measure, spec := range mapping(t, got.Tables, "GLUCOSE") {
			row := asList(spec)
			id, unit := "health/"+fmt.Sprint(row[0]), fmt.Sprint(row[1])
			m := metric(t, id)
			if unit != m.Unit || hold != m.Hold || m.Measures != measure {
				t.Errorf("GLUCOSE[%s]: %s in %s holding %s; the catalogue says %s in %s holding %q", measure, id, unit, hold, m.Measures, m.Unit, m.Hold)
			}
		}
		for value, code := range mapping(t, got.Tables, "STAGES") {
			if !slices.Contains(stages, fmt.Sprint(code)) {
				t.Errorf("STAGES: %q maps to %v, not a health/sleep_stage code %v", value, code, stages)
			}
		}
		for _, row := range rows(t, got.Tables, "NIGHT") {
			if !slices.Contains(stages, row[1]) {
				t.Errorf("NIGHT: %s maps to %q, not a health/sleep_stage code", row[0], row[1])
			}
		}
		for name, code := range mapping(t, got.Tables, "SPORTS") {
			if !slices.Contains(sports, fmt.Sprint(code)) {
				t.Errorf("SPORTS: %s maps to %v, not a workout sport", name, code)
			}
		}
	})
}

// assertCatalogued holds every observation and trace an adapter wrote to the
// metric it names, as the repository stores it: the metric exists; a raw
// value has the metric's form and hold (a summary is any metric's, and
// holds nothing); a number is in a unit of the metric's measure; a code is
// one the metric's code set lists.
func assertCatalogued(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	ctx := context.Background()
	metrics := map[string]catalogueMetric{}
	checked := 0
	for _, kind := range []string{obsObservation, obsTrace} {
		for _, row := range listLive(t, ds, kind) {
			p := row.Properties
			path := storedRefPath(p["metric"])
			m, seen := metrics[path]
			if !seen {
				id := strings.TrimPrefix(path, obsMetric+"/")
				rec, err := ds.Get(ctx, obsMetric, id)
				if err != nil {
					t.Errorf("%s/%s names %s, which the repository does not hold: %v", kind, row.ID, path, err)
					continue
				}
				m = catalogueMetricOf(rec.Properties)
				metrics[path] = m
			}
			checked++
			form, _ := p["form"].(string)
			hold, _ := p["hold"].(string)
			if form != "summary" && (form != m.Form || hold != m.Hold) {
				t.Errorf("%s/%s: a %s holding %q of %s, whose catalogue entry is a %s holding %q", kind, row.ID, form, hold, path, m.Form, m.Hold)
			}
			unit, _ := p["unit"].(string)
			if q, ok := p["value"].(map[string]any); ok {
				unit, _ = q["unit"].(string)
			}
			if unit != "" && !sameComponent(t, unit, m) {
				t.Errorf("%s/%s: %s in %s, not the id of a unit that converts to the metric's unit %s", kind, row.ID, path, unit, m.Unit)
			}
			codes := []any{p["code"]}
			codes = append(codes, asList(p["codes"])...)
			for _, c := range codes {
				if c != nil && !slices.Contains(m.CodeSet, fmt.Sprint(c)) {
					t.Errorf("%s/%s: code %v is not in %s's code set %v", kind, row.ID, c, path, m.CodeSet)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("assertCatalogued found no observation or trace to check")
	}
}
