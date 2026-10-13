package runner

// host.units and host.observed: the first answers from the engine's unit
// table over the host call, the second is pure Python whose output the
// engine's own rules must admit. Each is held to the Go side here, so the
// SDK and the write path cannot drift apart.

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

// TestPythonUnitsAskTheEnginesTable: a body converts, canonicalizes and
// classifies through the host call, and an error is the engine's.
func TestPythonUnitsAskTheEnginesTable(t *testing.T) {
	r := New()
	spec := Spec{
		Repository: "t1", Function: "units.g.test",
		Runtime: "python",
		Source: `
from decimal import Decimal

def main(input, host):
    out = {
        "lb": host.units.convert("72.4", "kg", "[lb_av]"),
        "fahrenheit": host.units.convert(37, "°C", "°F"),
        "deviation": host.units.convert(Decimal("0.3"), "Cel", "[degF]", measure="temperature-difference"),
        "kcal": host.units.convert(1, "kcal/d", "W"),
        "bpm": host.units.canonical("bpm"),
        "measure": host.units.measure("mmol/L"),
    }
    try:
        host.units.convert("1", "kg", "J")
    except Exception as e:
        out["refused"] = str(e)
    try:
        host.units.convert(1.5e400, "kg", "g")
    except ValueError as e:
        out["inf"] = str(e)
    return {"effects": [], "output": out}
`,
		TimeoutMs: 5000,
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), widgetBackend())
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, _ := res.Output.(map[string]any)
	for key, want := range map[string]string{
		"lb":         "159.61467782185136844",
		"fahrenheit": "98.6",
		"deviation":  "0.54",
		"kcal":       "0.048425925925925925926",
		"bpm":        "/min",
		"measure":    "amount-concentration",
	} {
		if out[key] != want {
			t.Errorf("%s = %v, want %s", key, out[key], want)
		}
	}
	if refused, _ := out["refused"].(string); !strings.Contains(refused, "a value converts only inside one measure") {
		t.Errorf("kg to J was answered %q, want the engine's refusal", refused)
	}
	if inf, _ := out["inf"].(string); !strings.Contains(inf, "not a finite number") {
		t.Errorf("an infinite float was answered %q", inf)
	}
}

// builtTraces runs host.observed in a python3 of its own over the cases the
// probe names, and answers each case's documents and period ids.
func builtTraces(t *testing.T) map[string]struct {
	Docs []map[string]any `json:"docs"`
	IDs  []string         `json:"ids"`
} {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH; host.observed cannot be probed here")
	}
	const probe = `
import json, importlib.util, datetime as dt
from decimal import Decimal
spec = importlib.util.spec_from_file_location("host", "host.py")
host = importlib.util.module_from_spec(spec)
spec.loader.exec_module(host)
o = host.Observed()
athens = dt.timezone(dt.timedelta(hours=3))
winter = dt.timezone(dt.timedelta(hours=2))
utc = dt.timezone.utc
metric = {"ref": "ada.example.com/observations/metric/health/heart_rate"}
source = {"ref": "ada.example.com/observations/source/garmin-forerunner-265"}
cases = {
    # irregular, with an unavailable point, over two hours
    "irregular": (o.trace([(dt.datetime(2026, 10, 7, 9, 0, 0, tzinfo=athens), 58),
                           (dt.datetime(2026, 10, 7, 9, 1, 58, tzinfo=athens), None),
                           (dt.datetime(2026, 10, 7, 9, 4, 1, tzinfo=athens), "61"),
                           (dt.datetime(2026, 10, 7, 10, 30, 0, 250000, tzinfo=athens), 112.5)],
                          metric=metric, source=source, unit="/min", form="gauge", hold="PT4M"), "PT1H"),
    # every five seconds from the minute
    "regular": (o.trace([(dt.datetime(2026, 10, 7, 6, 0, s, tzinfo=utc), Decimal(s) / 2) for s in range(0, 60, 5)],
                        metric=metric, source=source, unit="W", form="gauge", period="PT1M"), "PT1M"),
    # three points every 25 s do not fit a minute under every: offsets
    "overfull": (o.trace([(dt.datetime(2026, 10, 7, 6, 0, s, tzinfo=utc), 1) for s in (0, 25, 50)],
                         metric=metric, source=source, unit="W", form="gauge", period="PT1M"), "PT1M"),
    # sleep stages, coded, as amounts
    "coded": (o.trace([(dt.datetime(2026, 10, 7, 1, 0, tzinfo=utc), "light"),
                       (dt.datetime(2026, 10, 7, 1, 20, tzinfo=utc), "deep"),
                       (dt.datetime(2026, 10, 7, 1, 50, tzinfo=utc), None)],
                      metric=metric, source=source, unit=None, form="amount"), "PT1H"),
    # the device's clock moves an hour back mid-hour, as on a flight west:
    # one UTC hour, two parts
    "split": (o.trace([(dt.datetime(2026, 10, 25, 3, 10, tzinfo=athens), 1),
                       (dt.datetime(2026, 10, 25, 2, 40, tzinfo=winter), 2),
                       (dt.datetime(2026, 10, 25, 2, 50, tzinfo=winter), 3)],
                      metric=metric, source=source, unit="{steps}", form="amount"), "PT1H"),
    # a meter, its counter contract on every trace
    "counter": (o.trace([(dt.datetime(2026, 10, 7, 14, m, tzinfo=utc), v)
                         for m, v in ((0, "18342.112"), (15, "18342.391"), (30, "18342.655"), (45, "0.204"))],
                        metric=metric, source=source, unit="kW.h", form="counter", reset_rule="monotonic"), "PT1H"),
}
out = {}
for name, (docs, period) in cases.items():
    ids = [o.period_id(dt.datetime.fromisoformat(d["at"].replace("Z", "+00:00")), period) for d in docs]
    out[name] = {"docs": docs, "ids": ids}
print(json.dumps(out))
`
	cmd := exec.Command(python, "-c", probe)
	raw, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("probe: %v\n%s", err, stderr)
	}
	var out map[string]struct {
		Docs []map[string]any `json:"docs"`
		IDs  []string         `json:"ids"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode the probe: %v\n%s", err, raw)
	}
	return out
}

// TestObservedTraceBuilderOutputPassesTheEngine: every document
// host.observed.trace builds is one the engine's rules admit, with the
// spacing, the split and the labels the SDK promises.
func TestObservedTraceBuilderOutputPassesTheEngine(t *testing.T) {
	built := builtTraces(t)
	for name, c := range built {
		seen := map[string]bool{}
		for i, doc := range c.Docs {
			at, err := time.Parse(time.RFC3339Nano, doc["at"].(string))
			if err != nil {
				t.Fatalf("%s[%d].at: %v", name, i, err)
			}
			endsAt, err := time.Parse(time.RFC3339Nano, doc["endsAt"].(string))
			if err != nil {
				t.Fatalf("%s[%d].endsAt: %v", name, i, err)
			}
			props := map[string]any{}
			for k, v := range doc {
				if k != "at" && k != "endsAt" {
					props[k] = v
				}
			}
			if problems := vocabulary.CheckObserved(vocabulary.ObservedRecord{
				Variant: vocabulary.ObservedPacked, At: &at, EndsAt: &endsAt, Props: props,
			}); len(problems) > 0 {
				t.Errorf("%s[%d] is refused by the engine: %v\n%v", name, i, problems, doc)
			}
			if _, err := vocabulary.TraceStats(at, props); err != nil {
				t.Errorf("%s[%d]: stats: %v", name, i, err)
			}
			if seen[c.IDs[i]] {
				t.Errorf("%s: two documents share the period id %s", name, c.IDs[i])
			}
			seen[c.IDs[i]] = true
		}
	}
	irregular := built["irregular"]
	if len(irregular.Docs) != 2 || irregular.IDs[0] != "2026-10-07T06" || irregular.IDs[1] != "2026-10-07T07" {
		t.Errorf("irregular: %d documents, ids %v; want the two UTC hours 2026-10-07T06 and T07", len(irregular.Docs), irregular.IDs)
	} else {
		first := irregular.Docs[0]
		if !equalJSON(first["offsets"], []any{0, 118000, 241000}) || !equalJSON(first["values"], []any{"58", nil, "61"}) {
			t.Errorf("irregular hour: offsets %v values %v", first["offsets"], first["values"])
		}
		if first["utcOffsetStart"] != float64(180) || first["hold"] != "PT4M" {
			t.Errorf("irregular hour: offset %v hold %v", first["utcOffsetStart"], first["hold"])
		}
		if !equalJSON(irregular.Docs[1]["offsets"], []any{1800250}) || !equalJSON(irregular.Docs[1]["values"], []any{"112.5"}) {
			t.Errorf("second hour: offsets %v values %v", irregular.Docs[1]["offsets"], irregular.Docs[1]["values"])
		}
	}
	if regular := built["regular"].Docs; len(regular) != 1 || regular[0]["every"] != "PT5S" || regular[0]["offsets"] != nil {
		t.Errorf("regular: %v, want one document spaced by every PT5S", regular)
	} else if values := regular[0]["values"].([]any); values[1] != "2.5" || len(values) != 12 {
		t.Errorf("regular values = %v", values)
	}
	if overfull := built["overfull"].Docs; len(overfull) != 1 || overfull[0]["every"] != nil {
		t.Errorf("overfull: %v, want offsets, because 3 x PT25S does not fit PT1M", overfull)
	}
	if coded := built["coded"].Docs; len(coded) != 1 || coded[0]["unit"] != nil || !equalJSON(coded[0]["codes"], []any{"light", "deep", nil}) {
		t.Errorf("coded: %v", coded)
	}
	split := built["split"]
	if len(split.Docs) != 2 || split.IDs[0] != "2026-10-25T00" || split.IDs[1] != "2026-10-25T00:40" {
		t.Errorf("split: ids %v, want 2026-10-25T00 and the later part 2026-10-25T00:40", split.IDs)
	} else {
		if split.Docs[0]["endsAt"] != "2026-10-25T00:40:00Z" || split.Docs[1]["at"] != "2026-10-25T00:40:00Z" {
			t.Errorf("split: the first part ends %v and the second starts %v; want both at the change", split.Docs[0]["endsAt"], split.Docs[1]["at"])
		}
		if split.Docs[0]["utcOffsetStart"] != float64(180) || split.Docs[1]["utcOffsetStart"] != float64(120) {
			t.Errorf("split: offsets %v and %v", split.Docs[0]["utcOffsetStart"], split.Docs[1]["utcOffsetStart"])
		}
	}
	if counter := built["counter"].Docs; len(counter) != 1 || counter[0]["resetRule"] != "monotonic" || counter[0]["every"] != "PT15M" {
		t.Errorf("counter: %v", counter)
	}
}

func equalJSON(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

// TestObservedBuildersRefuseWhatTheEngineWould: the builder's own refusals
// name the rule before anything is staged.
func TestObservedBuildersRefuseWhatTheEngineWould(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH; host.observed cannot be probed here")
	}
	const probe = `
import json, importlib.util, datetime as dt
spec = importlib.util.spec_from_file_location("host", "host.py")
host = importlib.util.module_from_spec(spec)
spec.loader.exec_module(host)
o = host.Observed()
utc = dt.timezone.utc
at = dt.datetime(2026, 10, 7, 6, tzinfo=utc)
cases = {
    "naive": lambda: o.trace([(dt.datetime(2026, 10, 7, 6), 1)], metric="m", source=None, unit="W", form="gauge"),
    "summary": lambda: o.trace([(at, 1)], metric="m", source=None, unit="W", form="summary"),
    "held counter": lambda: o.trace([(at, 1)], metric="m", source=None, unit="W", form="counter", hold="PT1H"),
    "mixed": lambda: o.trace([(at, 1), (at, "open")], metric="m", source=None, unit="W", form="gauge"),
    "coded with a unit": lambda: o.trace([(at, "open")], metric="m", source=None, unit="W", form="gauge"),
    "no unit": lambda: o.trace([(at, 1)], metric="m", source=None, unit=None, form="gauge"),
    "far offset": lambda: o.local_day(at, 1200),
}
out = {}
for name, call in cases.items():
    try:
        call()
        out[name] = ""
    except ValueError as e:
        out[name] = str(e)
out["day"] = o.local_day(dt.datetime(2026, 10, 7, 22, 30, tzinfo=utc), 180)
out["empty"] = json.dumps(o.trace([], metric="m", source=None, unit="W", form="gauge"))
print(json.dumps(out))
`
	raw, err := exec.Command(python, "-c", probe).Output()
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	var out map[string]string
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v\n%s", err, raw)
	}
	for name, want := range map[string]string{
		"naive":             "is a timezone-aware datetime",
		"summary":           "a summary is one value per record",
		"held counter":      "only a gauge holds its value; a counter does not",
		"mixed":             "mixes a code into a trace of numbers",
		"coded with a unit": "a trace of codes carries no unit",
		"no unit":           "names the unit every value is in",
		"far offset":        "whole minutes within 1080 either way",
	} {
		if !strings.Contains(out[name], want) {
			t.Errorf("%s: %q, want it to say %q", name, out[name], want)
		}
	}
	if out["day"] != "2026-10-08" {
		t.Errorf("local_day = %q, want the civil date 2026-10-08 at +03:00", out["day"])
	}
	if out["empty"] != "[]" {
		t.Errorf("no points built %s, want no documents", out["empty"])
	}
}
