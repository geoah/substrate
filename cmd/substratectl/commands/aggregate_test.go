package commands

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const heartRate = "ada.example.com/observations/metric/health/heart_rate"

// The flags build the aggregate parameter field by field, in the struct's
// order, and nothing the command line did not name: the server's defaults
// stay the server's.
func TestAggregateFlagsBuildTheAggregateParameter(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.mustRun("aggregate", heartRate,
		"--kinds", "ada.example.com/observations/trace",
		"--gte", "2026-04-06T00:00:00+03:00", "--lt", "2026-10-05T00:00:00+03:00",
		"--calendar", "week", "--timezone", "Europe/Athens", "--week-start", "monday",
		"--reducer", "durationIn:95,114", "--reducer", "durationIn:114,133", "--reducer", "percentile:0.05",
		"--policy", "priority", "--order", "garmin-forerunner-265", "--order", "apple-watch-s10",
		"--unit", "min", "--max-gap", "PT5M", "--max-samples", "5000000")
	if got := h.lastRequest(); got != "GET "+pathRecords+"?kinds=ada.example.com/observations/trace" {
		t.Fatalf("request = %q", got)
	}
	q := h.fake.lastQuery
	want := `{"metric":{"ref":"ada.example.com/observations/metric/health/heart_rate"},` +
		`"range":{"gte":"2026-04-06T00:00:00+03:00","lt":"2026-10-05T00:00:00+03:00"},` +
		`"bucket":{"calendar":"week","timezone":"Europe/Athens","weekStart":"monday"},` +
		`"reducers":[{"durationIn":[95,114]},{"durationIn":[114,133]},{"percentile":0.05}],` +
		`"sources":{"policy":"priority","order":["garmin-forerunner-265","apple-watch-s10"]},` +
		`"unit":"min","maxGap":"PT5M","budget":{"maxSamples":5000000}}`
	if got := q.Get("aggregate"); got != want {
		t.Fatalf("aggregate =\n%s\nwant\n%s", got, want)
	}
	if got := q.Get("filter"); got != `{"kinds":["ada.example.com/observations/trace"]}` {
		t.Fatalf("filter = %s", got)
	}
	if q.Has("downsample") {
		t.Fatalf("no --downsample means no downsample parameter: %v", q)
	}

	// The latest read names no metric: the filter says which streams, and no
	// range, bucket or budget is invented for it.
	h.mustRun("aggregate", "--filter", `{"properties":{"metric":{"in":["`+heartRate+`"]}}}`,
		"--group-by", "metric,source", "--reducer", "last")
	if got := h.fake.lastQuery.Get("aggregate"); got != `{"groupBy":["metric","source"],"reducers":["last"]}` {
		t.Fatalf("latest read aggregate = %s", got)
	}
	if got := h.fake.lastQuery.Get("filter"); got != `{"properties":{"metric":{"in":["`+heartRate+`"]}}}` {
		t.Fatalf("latest read filter = %s", got)
	}
}

// --body sends the file's document as it is written, and a flag beside it
// replaces that one field and nothing else.
func TestAggregateBodySendsTheDocumentUnchanged(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	filter := `{"kinds":["ada.example.com/observations/observation"]}`
	aggregate := `{"metric":{"ref":"ada.example.com/observations/metric/health/sleep_stage"},` +
		`"from":"raw","range":{"gte":"2026-09-01T00:00:00+03:00","lt":"2026-10-01T00:00:00+03:00"},` +
		`"bucket":{"records":{"filter":{"kinds":["ada.example.com/health/sleep"]},"around":{"before":"PT1H"}}},` +
		`"groupBy":["code"],"reducers":["sum",{"timeInState":[]}],` +
		`"sources":{"policy":"priority","order":["whoop-4-0","garmin-forerunner-265"]},"unit":"min"}`
	file := filepath.Join(t.TempDir(), "sleep.json")
	doc := `{"filter": ` + filter + `, "aggregate": ` + aggregate + `}`
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	h.mustRun("aggregate", "--body", file)
	q := h.fake.lastQuery
	assertSameJSON(t, "filter", q.Get("filter"), filter)
	assertSameJSON(t, "aggregate", q.Get("aggregate"), aggregate)
	if q.Has("downsample") {
		t.Fatalf("the body carries no downsample: %v", q)
	}

	// --unit replaces the body's unit; every other field is the file's.
	h.mustRun("aggregate", "-f", file, "--unit", "h")
	var sent, written map[string]any
	if err := json.Unmarshal([]byte(h.fake.lastQuery.Get("aggregate")), &sent); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(aggregate), &written); err != nil {
		t.Fatal(err)
	}
	written["unit"] = "h"
	if !reflect.DeepEqual(sent, written) {
		t.Fatalf("--unit beside --body sent\n%v\nwant\n%v", sent, written)
	}

	// A bare aggregate body on stdin is the aggregate parameter whole.
	h.stdin.WriteString(aggregate)
	h.mustRun("aggregate", "-f", "-")
	assertSameJSON(t, "aggregate", h.fake.lastQuery.Get("aggregate"), aggregate)
	if h.fake.lastQuery.Has("filter") {
		t.Fatalf("a bare body names no filter: %v", h.fake.lastQuery)
	}

	// A misspelled key is refused before anything is sent.
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"metric":{"ref":"`+heartRate+`"},"reducer":["avg"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := len(h.fake.requests)
	if _, _, err := h.run("aggregate", "-f", bad); err == nil || !strings.Contains(err.Error(), `"reducer"`) {
		t.Fatalf("a misspelled body key should be refused by name, got %v", err)
	}
	if len(h.fake.requests) != before {
		t.Fatalf("a refused body must send nothing: %v", h.fake.requests[before:])
	}
}

// --downsample N sends the downsample parameter beside the aggregate one.
func TestAggregateDownsampleSendsTheParameter(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.mustRun("aggregate", heartRate, "--gte", "2026-04-06T00:00:00Z", "--lt", "2026-04-07T00:00:00Z", "--downsample", "800")
	q := h.fake.lastQuery
	if got := q.Get("downsample"); got != `{"points":800,"method":"m4"}` {
		t.Fatalf("downsample = %s", got)
	}
	if h.fake.lastDownsample == nil || h.fake.lastDownsample.Points != 800 {
		t.Fatalf("the server decoded downsample = %+v", h.fake.lastDownsample)
	}
	if got := q.Get("aggregate"); got != `{"metric":{"ref":"`+heartRate+`"},"range":{"gte":"2026-04-06T00:00:00Z","lt":"2026-04-07T00:00:00Z"}}` {
		t.Fatalf("aggregate = %s", got)
	}
}

// The table prints one line per row: the bucket, the group, the form, which
// points served it, the coverage, then each value with its unit. A filled
// row's values are null and print as "-"; a coded last prints its code and a
// timeInState value names its state.
func TestAggregateTablePrintsValuesWithUnits(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	out, _ := h.mustRun("aggregate", heartRate, "--reducer", "durationIn:95,114", "--reducer", "percentile:0.05")
	want := "" +
		"START                       END                         GROUP                                                              FORM    FROM   COVERAGE     VALUES\n" +
		"2026-04-06T00:00:00+03:00   2026-04-13T00:00:00+03:00   source=ada.example.com/observations/source/garmin-forerunner-265   gauge   raw    P6DT22H10M   durationIn:95,114=612.4 min   percentile:0.05=52 /min\n" +
		"2026-04-13T00:00:00+03:00   2026-04-20T00:00:00+03:00   source=ada.example.com/observations/source/garmin-forerunner-265   -       -      -            durationIn:95,114=-           percentile:0.05=-\n"
	if out != want {
		t.Fatalf("table:\n%s\nwant:\n%s", out, want)
	}

	h.fake.aggregateAnswer = `{"metric": "ada.example.com/observations/metric/home/door", "exact": true,
	  "rows": [{"start": "2026-10-01T07:12:00Z", "end": "2026-10-01T07:12:00Z", "form": "gauge",
	    "values": [{"reducer": "last", "value": null, "code": "open"},
	               {"reducer": "timeInState", "value": "3.5", "unit": "h", "code": "closed"}],
	    "count": 12, "bounded": 0, "synthetic": false, "unavailable": false}],
	  "head": {"changelog": 41, "generation": "gen-test"}}`
	out, _ = h.mustRun("aggregate", "ada.example.com/observations/metric/home/door", "--reducer", "last", "--reducer", "timeInState")
	want = "" +
		"START                  END                    FORM    FROM   COVERAGE   VALUES\n" +
		"2026-10-01T07:12:00Z   2026-10-01T07:12:00Z   gauge   -      -          last=open   timeInState:closed=3.5 h\n"
	if out != want {
		t.Fatalf("coded table:\n%s\nwant:\n%s", out, want)
	}
}

// -o json prints the answer whole, and it reads back as the wire carried it;
// -o yaml carries the same keys.
func TestAggregateJSONRoundTripsTheResult(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	out, _ := h.mustRun("aggregate", heartRate, "-o", "json")
	assertSameJSON(t, "-o json", out, fakeAggregateAnswer)

	out, _ = h.mustRun("aggregate", heartRate, "-o", "yaml")
	var fromYAML any
	if err := yaml.Unmarshal([]byte(out), &fromYAML); err != nil {
		t.Fatalf("-o yaml: %v\n%s", err, out)
	}
	b, err := json.Marshal(fromYAML)
	if err != nil {
		t.Fatal(err)
	}
	assertSameJSON(t, "-o yaml", string(b), fakeAggregateAnswer)
}

// What the command can refuse without asking is refused before any request;
// a server refusal is surfaced as every other command surfaces one.
func TestAggregateRefusals(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{}, "name what to read"},
		{[]string{heartRate, "--property", "amount"}, "not both"},
		{[]string{"heart_rate"}, "record path"},
		{[]string{heartRate, "--anchor", "at"}, "--anchor"},
		{[]string{heartRate, "--gte", "2026-04-06T00:00:00Z"}, "both --gte and --lt"},
		{[]string{heartRate, "--gte", "2026-04-06", "--lt", "2026-04-07T00:00:00Z"}, "RFC 3339"},
		{[]string{heartRate, "--every", "PT5M", "--calendar", "day"}, "give one"},
		{[]string{heartRate, "--reducer", "durationIn:95"}, "two bounds"},
		{[]string{heartRate, "--reducer", "percentile"}, "percentile takes"},
		{[]string{heartRate, "--reducer", "avg:1"}, "avg takes no argument"},
		{[]string{heartRate, "--rolling-reducer", "avg"}, "--rolling N"},
	} {
		before := len(h.fake.requests)
		_, _, err := h.run(append([]string{"aggregate"}, tc.args...)...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("aggregate %v: err = %v, want it to mention %q", tc.args, err, tc.want)
		}
		if len(h.fake.requests) != before {
			t.Errorf("aggregate %v sent a request: %v", tc.args, h.fake.requests[before:])
		}
	}

	_, _, err := h.run("aggregate", heartRate, "--max-buckets", "20000")
	if err == nil || !strings.Contains(err.Error(), "budget.maxBuckets 20000 is over the cap 10000") {
		t.Fatalf("the server's 422 should be surfaced with its message, got %v", err)
	}
	var ae *apiError
	if !errors.As(err, &ae) || ae.Status != http.StatusUnprocessableEntity {
		t.Fatalf("err = %#v, want the server's 422", err)
	}
}

func assertSameJSON(t *testing.T, what, got, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("%s is not JSON: %v\n%s", what, err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want is not JSON: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("%s =\n%s\nwant the same JSON as\n%s", what, got, want)
	}
}
