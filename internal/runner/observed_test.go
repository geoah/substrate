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
    # a 16-bit pulse counter that wraps to zero
    "wrap": (o.trace([(dt.datetime(2026, 10, 7, 14, m, tzinfo=utc), v) for m, v in ((0, 65000), (30, 120))],
                     metric=metric, source=source, unit="{count}", form="counter", reset_rule="wrap",
                     wrap_at=65536), "PT1H"),
    # a deviation in kelvin and a wind direction, flagged on every document
    "delta": (o.trace([(dt.datetime(2026, 10, 7, 3, tzinfo=utc), "0.3")], metric=metric, source=source,
                      unit="K", form="gauge", delta=True), "PT1H"),
    "circular": (o.trace([(dt.datetime(2026, 10, 7, 3, tzinfo=utc), 270)], metric=metric, source=source,
                         unit="deg", form="gauge", circular=True), "PT1H"),
    # a day at 1 Hz in one period: more points than one trace holds
    "long": (o.trace([(dt.datetime(2026, 10, 7, tzinfo=utc) + dt.timedelta(seconds=s), 60 + s % 7)
                      for s in range(86400)],
                     metric=metric, source=source, unit="/min", form="gauge", period="P1D"), "P1D"),
    # long digits: fewer points than the limit, more bytes than one trace holds
    "wide": (o.trace([(dt.datetime(2026, 10, 7, 6, tzinfo=utc) + dt.timedelta(milliseconds=150 * s),
                       "1." + str(s).zfill(6) * 15) for s in range(19000)],
                     metric=metric, source=source, unit="W", form="gauge"), "PT1H"),
    # a delivery covering 05:00 to 05:15, its first reading at 05:03
    "bounded": (o.trace([(dt.datetime(2026, 10, 11, 5, m, tzinfo=utc), v) for m, v in ((3, 66), (8, 58), (13, 59))],
                        metric=metric, source=source, unit="ms", form="gauge", hold="PT10M",
                        start=dt.datetime(2026, 10, 11, 5, tzinfo=utc), end=dt.datetime(2026, 10, 11, 5, 15, tzinfo=utc)),
                "PT1H"),
    # one covering 05:20 to 05:40, a reading every five minutes from its start
    "bounded later": (o.trace([(dt.datetime(2026, 10, 11, 5, m, tzinfo=utc), 60) for m in (20, 25, 30, 35)],
                              metric=metric, source=source, unit="ms", form="gauge",
                              start=dt.datetime(2026, 10, 11, 5, 20, tzinfo=utc),
                              end=dt.datetime(2026, 10, 11, 5, 40, tzinfo=utc)), "PT1H"),
    # one hour written twice: irregular, then regular; numbers, then codes
    "rewrite offsets": (o.trace([(dt.datetime(2026, 10, 7, 6, 0, s, tzinfo=utc), 1) for s in (0, 7, 30)],
                                metric=metric, source=source, unit="W", form="gauge"), "PT1H"),
    "rewrite every": (o.trace([(dt.datetime(2026, 10, 7, 6, 0, s, tzinfo=utc), 1) for s in (0, 10, 20)],
                              metric=metric, source=source, unit="W", form="gauge"), "PT1H"),
    "rewrite codes": (o.trace([(dt.datetime(2026, 10, 7, 6, 0, s, tzinfo=utc), "on") for s in (0, 7, 30)],
                              metric=metric, source=source, unit=None, form="gauge"), "PT1H"),
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
	if wrap := built["wrap"].Docs; len(wrap) != 1 || wrap[0]["resetRule"] != "wrap" || wrap[0]["wrapAt"] != "65536" {
		t.Errorf("wrap: %v, want resetRule wrap and wrapAt 65536", wrap)
	}
	if d := built["delta"].Docs; len(d) != 1 || d[0]["delta"] != true {
		t.Errorf("delta: %v", d)
	}
	if c := built["circular"].Docs; len(c) != 1 || c[0]["circular"] != true {
		t.Errorf("circular: %v", c)
	}
	// A day at 1 Hz splits at the point limit into regular parts that cover
	// the day end to end.
	long := built["long"].Docs
	total := 0
	for i, doc := range long {
		n := len(doc["values"].([]any))
		total += n
		if n > vocabulary.MaxTracePoints || doc["every"] != "PT1S" {
			t.Errorf("long[%d]: %d points every %v", i, n, doc["every"])
		}
		if i > 0 && doc["at"] != long[i-1]["endsAt"] {
			t.Errorf("long[%d] starts at %v, the part before it ends at %v", i, doc["at"], long[i-1]["endsAt"])
		}
	}
	if len(long) != 5 || total != 86400 || long[0]["at"] != "2026-10-07T00:00:00Z" || long[4]["endsAt"] != "2026-10-08T00:00:00Z" {
		t.Errorf("long: %d documents holding %d points from %v to %v, want 5 holding 86400 over the day",
			len(long), total, long[0]["at"], long[len(long)-1]["endsAt"])
	}
	if wide := built["wide"].Docs; len(wide) < 2 {
		t.Errorf("wide: %d documents; 19000 values of 92 digits are more than 1 MiB", len(wide))
	}
	if b := built["bounded"]; len(b.Docs) != 1 || b.Docs[0]["at"] != "2026-10-11T05:00:00Z" ||
		b.Docs[0]["endsAt"] != "2026-10-11T05:15:00Z" || !equalJSON(b.Docs[0]["offsets"], []any{180000, 480000, 780000}) {
		t.Errorf("bounded: %v, want [05:00, 05:15) with offsets from 05:00", b.Docs)
	}
	if b := built["bounded later"]; len(b.Docs) != 1 || b.Docs[0]["at"] != "2026-10-11T05:20:00Z" ||
		b.Docs[0]["endsAt"] != "2026-10-11T05:40:00Z" || b.Docs[0]["every"] != "PT5M" || b.IDs[0] != "2026-10-11T05:20" {
		t.Errorf("bounded later: %v %v, want [05:20, 05:40) every PT5M, labeled 2026-10-11T05:20", b.Docs, b.IDs)
	}
}

// TestObservedTraceBuilderRewritesAPeriodOfAnotherShape: a body that puts
// the builder's output over a period stored in the other shape (offsets then
// every, numbers then codes) is admitted, because each document names the
// pair key it does not use as null and a put of a null clears it.
func TestObservedTraceBuilderRewritesAPeriodOfAnotherShape(t *testing.T) {
	built := builtTraces(t)
	for _, c := range []struct{ stored, written string }{
		{"rewrite offsets", "rewrite every"},
		{"rewrite every", "rewrite offsets"},
		{"rewrite offsets", "rewrite codes"},
		{"rewrite codes", "rewrite every"},
	} {
		stored := map[string]any{}
		for k, v := range built[c.stored].Docs[0] {
			if v != nil {
				stored[k] = v
			}
		}
		for k, v := range built[c.written].Docs[0] {
			if v == nil {
				delete(stored, k)
				continue
			}
			stored[k] = v
		}
		at, _ := time.Parse(time.RFC3339Nano, stored["at"].(string))
		endsAt, _ := time.Parse(time.RFC3339Nano, stored["endsAt"].(string))
		delete(stored, "at")
		delete(stored, "endsAt")
		if problems := vocabulary.CheckObserved(vocabulary.ObservedRecord{
			Variant: vocabulary.ObservedPacked, At: &at, EndsAt: &endsAt, Props: stored,
		}); len(problems) > 0 {
			t.Errorf("%s put over %s is refused: %v", c.written, c.stored, problems)
		}
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
    "wrap without wrap_at": lambda: o.trace([(at, 1)], metric="m", source=None, unit="{count}", form="counter",
                                            reset_rule="wrap"),
    "wrap_at without wrap": lambda: o.trace([(at, 1)], metric="m", source=None, unit="{count}", form="counter",
                                            reset_rule="monotonic", wrap_at=100),
    "outside the bounds": lambda: o.trace([(at, 1)], metric="m", source=None, unit="W", form="gauge",
                                          start=at + dt.timedelta(minutes=1)),
    "coded delta": lambda: o.trace([(at, "open")], metric="m", source=None, unit=None, form="gauge", delta=True),
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
		"naive":                "is a timezone-aware datetime",
		"summary":              "a summary is one value per record",
		"held counter":         "only a gauge holds its value; a counter does not",
		"mixed":                "mixes a code into a trace of numbers",
		"coded with a unit":    "a trace of codes carries no unit",
		"no unit":              "names the unit every value is in",
		"far offset":           "whole minutes within 1080 either way",
		"wrap without wrap_at": "a wrapping counter names the value it wraps at",
		"wrap_at without wrap": "pass reset_rule=\"wrap\"",
		"outside the bounds":   "is outside [start, end)",
		"coded delta":          "a trace of codes has none",
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

// observedProbe runs a python3 probe over host.py's Observed and decodes the
// JSON it prints.
func observedProbe(t *testing.T, probe string, out any) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH; host.observed cannot be probed here")
	}
	raw, err := exec.Command(python, "-c", probe).Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("probe: %v\n%s", err, stderr)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode the probe: %v\n%s", err, raw)
	}
}

// TestObservedSpellsWhatTheEngineStores: host.observed's decimal, duration,
// instant, percent and record_id write the one spelling the engine stores, so
// an output a body composes compares equal to the stored row it rewrites.
func TestObservedSpellsWhatTheEngineStores(t *testing.T) {
	var out map[string]any
	observedProbe(t, `
import json, importlib.util, datetime as dt
from decimal import Decimal
spec = importlib.util.spec_from_file_location("host", "host.py")
host = importlib.util.module_from_spec(spec)
spec.loader.exec_module(host)
o = host.Observed()
def refused(call):
    try:
        call()
        return ""
    except ValueError as e:
        return str(e)
print(json.dumps({
    "tiny": o.decimal(1e-05), "huge": o.decimal(1e16), "float": o.decimal(72.0),
    "decimal": o.decimal(Decimal("1.50")), "digits": o.decimal("61.3"), "int": o.decimal(52),
    "none": o.decimal(None), "bool": refused(lambda: o.decimal(True)),
    "nan": refused(lambda: o.decimal(float("nan"))),
    "night": o.duration(28489), "day and a half": o.duration(Decimal("90061.5")),
    "hours": o.duration(dt.timedelta(hours=36)), "zero": o.duration(0),
    "nanos": o.duration(Decimal("1.0000004")), "negative": refused(lambda: o.duration(-1)),
    "instant": o.instant(dt.datetime(2026, 10, 7, 9, 0, 0, 250000, tzinfo=dt.timezone(dt.timedelta(hours=3)))),
    "short id": o.record_id("garmin", "ada", "ada", "x4b2c9e1-6aa31a50", "steps"),
    "long id": o.record_id("garmin", "ada", "ada", "x" * 130, "steps"),
    "odd id": o.record_id("garmin", "ada", "ada", "a b", "steps"),
    "efficiency": o.percent(25200, 28800), "half up": o.percent(1, 32), "float part": o.percent(7.5, 8),
    "no whole": o.percent(1, 0), "no part": o.percent(None, 8),
}))
`, &out)
	for key, want := range map[string]any{
		"tiny": "0.00001", "huge": "10000000000000000", "float": "72.0", "decimal": "1.50",
		"digits": "61.3", "int": "52", "none": nil,
		"night": "PT7H54M49S", "day and a half": "P1DT1H1M1.5S", "hours": "P1DT12H", "zero": "PT0S",
		"nanos": "PT1S", "instant": "2026-10-07T06:00:00.25Z",
		"short id": "garmin/ada/x4b2c9e1-6aa31a50/steps",
		"long id":  ExternalID("garmin", "ada", "garmin/ada/"+strings.Repeat("x", 130)+"/steps"),
		"odd id":   ExternalID("garmin", "ada", "garmin/ada/a b/steps"),
	} {
		if out[key] != want {
			t.Errorf("%s = %v, want %v", key, out[key], want)
		}
	}
	for key, want := range map[string]any{
		"efficiency": map[string]any{"value": "87.50", "unit": "%"},
		"half up":    map[string]any{"value": "3.13", "unit": "%"},
		"float part": map[string]any{"value": "93.75", "unit": "%"},
		"no whole":   nil, "no part": nil,
	} {
		if !equalJSON(out[key], want) {
			t.Errorf("%s = %v, want %v", key, out[key], want)
		}
	}
	for _, key := range []string{"tiny", "huge", "float", "decimal", "digits", "int"} {
		if got, err := vocabulary.CanonicalDecimal(out[key].(string)); err != nil || got != out[key] {
			t.Errorf("%s: the engine stores %q as %q (%v)", key, out[key], got, err)
		}
	}
	for _, key := range []string{"night", "day and a half", "hours", "zero", "nanos"} {
		d, err := vocabulary.ParseISODuration(out[key].(string))
		if err != nil || vocabulary.FormatISODuration(d) != out[key] {
			t.Errorf("%s: the engine stores %q as %q (%v)", key, out[key], vocabulary.FormatISODuration(d), err)
		}
	}
	for _, key := range []string{"bool", "nan", "negative"} {
		if out[key] == "" {
			t.Errorf("%s was not refused", key)
		}
	}
	for _, key := range []string{"short id", "long id", "odd id"} {
		if !vocabulary.ValidID(out[key].(string)) {
			t.Errorf("%s %q is not a record id", key, out[key])
		}
	}
}

// TestObservedReconcileWritesWhatAnOriginImplies: one call stands for every
// adapter's per-origin loop. It reads the origin's stored outputs page by
// page, leaves an output that already holds its properties unwritten, writes
// a changed one whole (every owned property named, None where the origin no
// longer implies it) at the higher of the given and the stored revision,
// deletes what the origin no longer implies, and leaves a record that only
// points at the origin some other way, or names another origin, alone. An
// output the origin's read does not hold is read by id: one another origin
// holds at a higher revision (an hour's trace a deleted slice raised) keeps
// that revision, which the write path would otherwise refuse.
func TestObservedReconcileWritesWhatAnOriginImplies(t *testing.T) {
	var out struct {
		Counts  map[string]int   `json:"counts"`
		Effects []map[string]any `json:"effects"`
		Queries []map[string]any `json:"queries"`
	}
	observedProbe(t, `
import json, importlib.util
spec = importlib.util.spec_from_file_location("host", "host.py")
host = importlib.util.module_from_spec(spec)
spec.loader.exec_module(host)
OBS = "ada.example.com/observations/observation"
ORIGIN = "providers.example.com/garmin/daily/ada/d1"
def rec(rid, **props):
    return {"kind": OBS, "id": rid, "properties": props}
pages = [
    {"records": [
        rec("same", origin={"ref": ORIGIN}, value="1", note="kept", revision=7, adapter="a@1"),
        rec("changed", origin=ORIGIN, value="1", note="old", revision=9, adapter="a@1"),
    ], "cursor": "1"},
    {"records": [
        rec("gone", origins=[{"ref": ORIGIN}], value="3", revision=1),
        rec("derived", origin={"ref": "providers.example.com/garmin/sleep/ada/s1"},
            derivedFrom=[{"ref": ORIGIN}], value="4"),
    ]},
]
by_id = [
    rec("shared", origins=[{"ref": "providers.example.com/garmin/epoch/ada/e2"}], value="6", revision=12),
    {"kind": "ada.example.com/health/sleep", "id": "new", "properties": {"revision": 99}},
]
class Fake:
    def __init__(self):
        self.effects = host.Effects()
        self.queries = []
    def list(self, **query):
        self.queries.append(query)
        if "ids" in query["filter"]:
            return {"records": [r for r in by_id if r["id"] in query["filter"]["ids"]]}
        return pages[int(query.get("after") or 0)]
fake = Fake()
counts = host.Observed(fake).reconcile(ORIGIN, {
    (OBS, "same"): {"origin": {"ref": ORIGIN}, "value": "1", "note": "kept", "revision": 8},
    (OBS, "changed"): {"origin": {"ref": ORIGIN}, "value": "2", "revision": 8},
    (OBS, "new"): {"origin": {"ref": ORIGIN}, "value": "5", "revision": 8},
    (OBS, "shared"): {"origin": {"ref": ORIGIN}, "value": "6", "revision": 8},
}, owned={OBS: ("origin", "value", "note", "revision")}, adapter="a@1")
print(json.dumps({"counts": counts, "effects": [e for e in fake.effects._staged], "queries": fake.queries}))
`, &out)
	if want := map[string]int{"written": 3, "unchanged": 1, "deleted": 1}; !equalJSON(out.Counts, want) {
		t.Errorf("counts %v, want %v", out.Counts, want)
	}
	if len(out.Queries) != 3 || out.Queries[1]["after"] != "1" {
		t.Fatalf("queries %v, want two pages and one read by id", out.Queries)
	}
	if byID := out.Queries[2]["filter"]; !equalJSON(byID, map[string]any{
		"kinds": []any{"ada.example.com/observations/observation"}, "ids": []any{"new", "shared"},
	}) {
		t.Errorf("the read by id = %v, want the two outputs the origin's read did not hold", byID)
	}
	want := []map[string]any{
		{"action": "put", "kind": "ada.example.com/observations/observation", "id": "changed", "properties": map[string]any{
			"origin": map[string]any{"ref": "providers.example.com/garmin/daily/ada/d1"}, "value": "2", "note": nil,
			"revision": float64(9), "adapter": "a@1",
		}},
		{"action": "put", "kind": "ada.example.com/observations/observation", "id": "new", "properties": map[string]any{
			"origin": map[string]any{"ref": "providers.example.com/garmin/daily/ada/d1"}, "value": "5", "note": nil,
			"revision": float64(8), "adapter": "a@1",
		}},
		{"action": "put", "kind": "ada.example.com/observations/observation", "id": "shared", "properties": map[string]any{
			"origin": map[string]any{"ref": "providers.example.com/garmin/daily/ada/d1"}, "value": "6", "note": nil,
			"revision": float64(12), "adapter": "a@1",
		}},
		{"action": "delete", "kind": "ada.example.com/observations/observation", "id": "gone"},
	}
	if !equalJSON(out.Effects, want) {
		t.Errorf("effects\n%v\nwant\n%v", out.Effects, want)
	}
}
