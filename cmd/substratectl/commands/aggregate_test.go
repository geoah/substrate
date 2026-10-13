package commands

import (
	"bytes"
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
// points served it, the coverage, its flags, then each value with its unit.
// A filled row says so and its values are null, printed as "-"; a coded last
// prints its code and a timeInState value names its state.
func TestAggregateTablePrintsValuesWithUnits(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	out, _ := h.mustRun("aggregate", heartRate, "--reducer", "durationIn:95,114", "--reducer", "percentile:0.05")
	want := "" +
		"START                       END                         GROUP                                                              FORM    FROM   COVERAGE     FLAGS    VALUES\n" +
		"2026-04-06T00:00:00+03:00   2026-04-13T00:00:00+03:00   source=ada.example.com/observations/source/garmin-forerunner-265   gauge   raw    P6DT22H10M   -        durationIn:95,114=612.4 min   percentile:0.05=52 /min\n" +
		"2026-04-13T00:00:00+03:00   2026-04-20T00:00:00+03:00   source=ada.example.com/observations/source/garmin-forerunner-265   -       -      -            filled   durationIn:95,114=-           percentile:0.05=-\n"
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
		"START                  END                    FORM    VALUES\n" +
		"2026-10-01T07:12:00Z   2026-10-01T07:12:00Z   gauge   last=open   timeInState:closed=3.5 h\n"
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
		{[]string{heartRate, "--rolling-through", "previous"}, "--rolling N"},
		{[]string{heartRate, "--utc-offset", "utcOffset"}, "--utc-offset is an option of the property read"},
		{[]string{heartRate, "--records", "ada.example.com/health/sleep", "--calendar", "day"}, "give one"},
		{[]string{heartRate, "--fallback-timezone", "Europe/Athens", "--every", "PT1H"}, "give one"},
		{[]string{heartRate, "--record-policy", "priority"}, "choose among records"},
		{[]string{heartRate, "--records", "ada.example.com/health/sleep", "--records-utc-offsets", "a,b,c"}, "--records-utc-offsets names"},
		{[]string{heartRate, "--filter", `{"kinds":["x"]}}`}, "trailing data"},
		{[]string{heartRate, "-o", "yml"}, `unknown output format "yml"`},
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

	// A refused body names the body's field, never a flag it did not set.
	for body, want := range map[string]string{
		`{"metric":{"ref":"` + heartRate + `"},"range":{"gte":"2026-10-01T00:00:00Z"}}`:                   "the range needs both gte and lt",
		`{"metric":{"ref":"` + heartRate + `"},"range":{"gte":"2026-10-01","lt":"2026-10-02T00:00:00Z"}}`: `range.gte "2026-10-01" is not an RFC 3339 instant`,
		`{"Metric":{"ref":"` + heartRate + `"},"reducers":["avg"]}`:                                       `unknown field "Metric"`,
		`{"metric":{"ref":"` + heartRate + `"},"unit":"kg","unit":"lb"}`:                                  `duplicate field "unit"`,
	} {
		h.stdin.WriteString(body)
		before := len(h.fake.requests)
		if _, _, err := h.run("aggregate", "-f", "-"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("body %s: err = %v, want it to mention %q", body, err, want)
		}
		if len(h.fake.requests) != before {
			t.Errorf("body %s sent a request: %v", body, h.fake.requests[before:])
		}
	}

	_, _, err := h.run("aggregate", heartRate, "--max-buckets", "20000")
	if err == nil || !strings.Contains(err.Error(), "aggregate.budget.maxBuckets: 20000 is over the cap of 10000") {
		t.Fatalf("the server's 422 should be surfaced with its message, got %v", err)
	}
	var ae *apiError
	if !errors.As(err, &ae) || ae.Status != http.StatusUnprocessableEntity {
		t.Fatalf("err = %#v, want the server's 422", err)
	}
	// A read's refusal is headlined as a refused read, never a write.
	var buf bytes.Buffer
	renderError(&buf, err)
	if got := buf.String(); !strings.HasPrefix(got, "error: the substrate refused this read as invalid\n") ||
		!strings.Contains(got, "request: GET "+pathRecords) {
		t.Fatalf("rendered:\n%s", got)
	}
}

// Every field the read gained has its flag, sent in the struct's order: the
// property read's options, the records time mode, the record sources, the
// statistic filter, the bucket's place and fallback zone, a rolling window's
// end, several metrics, and a latest read that names no filter at all.
func TestAggregateFlagsBuildEveryField(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	days := []string{"--gte", "2026-09-01T00:00:00+03:00", "--lt", "2026-10-01T00:00:00+03:00"}
	for name, x := range map[string]struct {
		args             []string
		filter, aggregte string
	}{
		"a property read": {
			append([]string{
				"--property", "duration", "--anchor", "endsAt", "--utc-offset", "utcOffsetEnd",
				"--property-group-by", "sport,source", "--kinds", "ada.example.com/health/workout",
				"--calendar", "day", "--fallback-timezone", "Europe/Athens", "--offset", "PT4H", "--reducer", "sum",
				"--record-policy", "priority", "--record-order", "whoop,garmin", "--record-source", "source", "--record-match", "sport",
				"--rolling", "7", "--rolling-reducer", "avg", "--rolling-through", "previous", "--fill", "zero", "--max-groups", "50",
			}, days...),
			`{"kinds":["ada.example.com/health/workout"]}`,
			`{"property":{"name":"duration","anchor":"endsAt","utcOffset":"utcOffsetEnd","groupBy":["sport","source"],` +
				`"sources":{"policy":"priority","order":["whoop","garmin"],"property":"source","match":["sport"]}},` +
				`"range":{"gte":"2026-09-01T00:00:00+03:00","lt":"2026-10-01T00:00:00+03:00"},` +
				`"bucket":{"calendar":"day","fallbackTimezone":"Europe/Athens","offset":"PT4H"},"reducers":["sum"],"fill":"zero",` +
				`"rolling":{"buckets":7,"reducers":["avg"],"through":"previous"},"budget":{"maxGroups":50}}`,
		},
		"the records time mode": {
			append([]string{
				"ada.example.com/observations/metric/health/sleep_stage",
				"--records", "ada.example.com/health/sleep", "--records-filter", `{"properties":{"isNap":{"eq":false}}}`,
				"--around-before", "PT1H", "--around-after", "PT1M", "--max-records", "40",
				"--records-utc-offsets", "utcOffsetStart,utcOffsetEnd",
				"--record-policy", "priority", "--record-order", "whoop", "--record-source", "source",
				"--group-by", "code", "--reducer", "sum", "--reducer", "timeInState:open,closed", "--unit", "min",
			}, days...),
			``,
			`{"metric":{"ref":"ada.example.com/observations/metric/health/sleep_stage"},` +
				`"range":{"gte":"2026-09-01T00:00:00+03:00","lt":"2026-10-01T00:00:00+03:00"},` +
				`"bucket":{"records":{"filter":{"kinds":["ada.example.com/health/sleep"],"properties":{"isNap":{"eq":false}}},` +
				`"around":{"before":"PT1H","after":"PT1M"},"maxRecords":40,` +
				`"sources":{"policy":"priority","order":["whoop"],"property":"source"},` +
				`"utcOffsets":{"start":"utcOffsetStart","end":"utcOffsetEnd"}}},` +
				`"groupBy":["code"],"reducers":["sum",{"timeInState":["open","closed"]}],"unit":"min"}`,
		},
		"a fixed bucket placed by its end, at one statistic": {
			append([]string{
				heartRate, "--every", "PT1H", "--origin", "2026-01-01T00:00:00Z", "--place", "end",
				"--statistic", "avg", "--from", "prefer-raw", "--min-coverage", "0.5", "--group-by", "statistic",
			}, days...),
			`{"properties":{"statistic":{"eq":"avg"}}}`,
			`{"metric":{"ref":"` + heartRate + `"},"from":"prefer-raw","minCoverage":0.5,` +
				`"range":{"gte":"2026-09-01T00:00:00+03:00","lt":"2026-10-01T00:00:00+03:00"},` +
				`"bucket":{"every":"PT1H","origin":"2026-01-01T00:00:00Z","place":"end"},"groupBy":["statistic"]}`,
		},
		"several metrics": {
			append([]string{heartRate, "ada.example.com/observations/metric/health/steps", "--calendar", "day", "--timezone", "UTC", "--reducer", "avg"}, days...),
			``,
			`{"metrics":[{"ref":"` + heartRate + `"},{"ref":"ada.example.com/observations/metric/health/steps"}],` +
				`"range":{"gte":"2026-09-01T00:00:00+03:00","lt":"2026-10-01T00:00:00+03:00"},` +
				`"bucket":{"calendar":"day","timezone":"UTC"},"reducers":["avg"]}`,
		},
		"a latest read naming no filter": {
			[]string{"--reducer", "last", "--group-by", "metric,source"},
			``,
			`{"groupBy":["metric","source"],"reducers":["last"]}`,
		},
	} {
		h.mustRun(append([]string{"aggregate"}, x.args...)...)
		q := h.fake.lastQuery
		if got := q.Get("aggregate"); got != x.aggregte {
			t.Errorf("%s: aggregate =\n%s\nwant\n%s", name, got, x.aggregte)
		}
		if got := q.Get("filter"); got != x.filter {
			t.Errorf("%s: filter = %s, want %s", name, got, x.filter)
		}
	}

	// A metric argument replaces the body's metrics, and two arguments its
	// metric.
	h.stdin.WriteString(`{"metrics":[{"ref":"a.example.com/observations/metric/x","unit":"kg"}],"reducers":["avg"]}`)
	h.mustRun("aggregate", "-f", "-", heartRate)
	if got := h.fake.lastQuery.Get("aggregate"); got != `{"metric":{"ref":"`+heartRate+`"},"reducers":["avg"]}` {
		t.Fatalf("one metric over a body's metrics: %s", got)
	}
}

// The table names what tells two rows apart: the sources of rows the
// separate policy split without a group, a summary's statistic and a property
// read's group properties, and the counts a row carries of values placed by
// the fallback zone and of amounts a window cut. A value's code is its answer
// only on a coded first or last; elsewhere it names the state the value is
// for, on the reducer that owns it, and a rolling value names its window and
// n.
func TestAggregateTableTellsRowsApart(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	head := `"head": {"changelog": 41, "generation": "gen-test"}`
	for name, x := range map[string]struct{ answer, want string }{
		"two sources without a group": {
			`{"metric": "m", "unit": "/min", "exact": true, "rows": [
			  {"start": "2026-10-01T00:00:00+03:00", "end": "2026-10-02T00:00:00+03:00", "form": "gauge", "from": "raw",
			   "sources": ["ada.example.com/observations/source/garmin"], "values": [{"reducer": "avg", "value": "63.7", "unit": "/min"}],
			   "count": 10, "bounded": 0, "synthetic": false, "unavailable": false},
			  {"start": "2026-10-01T00:00:00+03:00", "end": "2026-10-02T00:00:00+03:00", "form": "gauge", "from": "raw",
			   "sources": ["ada.example.com/observations/source/whoop"], "values": [{"reducer": "avg", "value": "64.1", "unit": "/min"}],
			   "count": 12, "bounded": 0, "synthetic": false, "unavailable": false}], ` + head + `}`,
			"" +
				"START                       END                         SOURCES                                      FORM    FROM   VALUES\n" +
				"2026-10-01T00:00:00+03:00   2026-10-02T00:00:00+03:00   ada.example.com/observations/source/garmin   gauge   raw    avg=63.7 /min\n" +
				"2026-10-01T00:00:00+03:00   2026-10-02T00:00:00+03:00   ada.example.com/observations/source/whoop    gauge   raw    avg=64.1 /min\n",
		},
		"statistics, properties, offsetless and prorated": {
			`{"metric": "m", "exact": true, "rows": [
			  {"start": "2026-10-01T00:00:00Z", "end": "2026-10-02T00:00:00Z", "form": "summary",
			   "group": {"source": "s", "statistic": "avg"}, "sources": ["s"], "offsetless": 2,
			   "values": [{"reducer": "max", "value": "88", "unit": "/min"}], "count": 3, "bounded": 0, "synthetic": false, "unavailable": false},
			  {"start": "2026-10-01T00:00:00Z", "end": "2026-10-02T00:00:00Z",
			   "group": {"kind": "k", "properties": {"sport": "running", "device": "d"}}, "prorated": 1,
			   "values": [{"reducer": "sum", "value": "30", "unit": "min"}], "count": 2, "bounded": 0, "synthetic": false, "unavailable": false}], ` + head + `}`,
			"" +
				"START                  END                    GROUP                           FORM      OFFSETLESS   PRORATED   VALUES\n" +
				"2026-10-01T00:00:00Z   2026-10-02T00:00:00Z   source=s,statistic=avg          summary   2            0          max=88 /min\n" +
				"2026-10-01T00:00:00Z   2026-10-02T00:00:00Z   kind=k,device=d,sport=running   -         0            1          sum=30 min\n",
		},
		"coded values": {
			`{"metric": "door", "exact": true, "rows": [
			  {"start": "2026-10-01T00:00:00Z", "end": "2026-10-02T00:00:00Z", "form": "gauge",
			   "values": [{"reducer": "last", "value": null, "code": "open"},
			              {"reducer": "timeInState", "value": null, "unit": "h", "code": "closed"},
			              {"reducer": "avg", "of": "timeInState", "rolling": 3, "n": 2, "value": "2.5", "unit": "h", "code": "closed"},
			              {"reducer": "avg", "of": "timeInState", "rolling": 3, "n": 0, "value": null, "unit": "h", "code": "closed"},
			              {"reducer": "avg", "of": "last", "rolling": 3, "value": null, "code": "open"}],
			   "count": 0, "bounded": 0, "synthetic": true, "unavailable": false}], ` + head + `}`,
			"" +
				"START                  END                    FORM    FLAGS    VALUES\n" +
				"2026-10-01T00:00:00Z   2026-10-02T00:00:00Z   gauge   filled   last=open   timeInState:closed=-   rolling(3,n=2,avg of timeInState:closed)=2.5 h   rolling(3,n=0,avg of timeInState:closed)=-   rolling(3,avg of last:open)=-\n",
		},
		"several metrics": {
			`{"units": {"m/hr": "/min", "m/steps": "{steps}"}, "exact": true, "rows": [
			  {"start": "2026-10-01T00:00:00Z", "end": "2026-10-02T00:00:00Z", "form": "gauge", "group": {"metric": "m/hr"},
			   "values": [{"reducer": "avg", "value": "64", "unit": "/min"}], "count": 9, "bounded": 0, "synthetic": false, "unavailable": false},
			  {"start": "2026-10-01T00:00:00Z", "end": "2026-10-02T00:00:00Z", "form": "amount", "group": {"metric": "m/steps"},
			   "values": [{"reducer": "sum", "value": "9000", "unit": "{steps}"}], "count": 9, "bounded": 0, "synthetic": false, "unavailable": false}], ` + head + `}`,
			"" +
				"START                  END                    GROUP            FORM     VALUES\n" +
				"2026-10-01T00:00:00Z   2026-10-02T00:00:00Z   metric=m/hr      gauge    avg=64 /min\n" +
				"2026-10-01T00:00:00Z   2026-10-02T00:00:00Z   metric=m/steps   amount   sum=9000 {steps}\n",
		},
		// A property read carries no form, from or coverage, so the table
		// prints none of the three.
		"a property read": {
			`{"property": "distance", "unit": "km", "exact": true, "rows": [
			  {"start": "2026-09-01T00:00:00Z", "end": "2026-10-01T00:00:00Z", "group": {"properties": {"sport": "running"}},
			   "values": [{"reducer": "sum", "value": "84.2", "unit": "km"}], "count": 12, "bounded": 0, "synthetic": false, "unavailable": false}], ` + head + `}`,
			"" +
				"START                  END                    GROUP           VALUES\n" +
				"2026-09-01T00:00:00Z   2026-10-01T00:00:00Z   sport=running   sum=84.2 km\n",
		},
		// A filled, an expired, an overlapping and a bounded row each say so
		// in FLAGS, and a plain row beside them prints a dash.
		"flags": {
			`{"metric": "m", "exact": true, "rows": [
			  {"start": "2026-10-01T00:00:00Z", "end": "2026-10-02T00:00:00Z", "form": "gauge", "from": "raw", "coverage": "PT1H",
			   "values": [{"reducer": "avg", "value": "60", "unit": "/min"}], "count": 2, "bounded": 0, "synthetic": false, "unavailable": false},
			  {"start": "2026-10-02T00:00:00Z", "end": "2026-10-03T00:00:00Z", "form": "gauge",
			   "values": [{"reducer": "avg", "value": "60", "unit": "/min"}], "count": 0, "bounded": 0, "synthetic": true, "unavailable": false},
			  {"start": "2026-10-03T00:00:00Z", "end": "2026-10-04T00:00:00Z", "form": "gauge",
			   "values": [{"reducer": "avg", "value": null, "unit": "/min"}], "count": 0, "bounded": 0, "synthetic": false, "unavailable": true},
			  {"start": "2026-10-04T00:00:00Z", "end": "2026-10-05T00:00:00Z", "form": "gauge", "from": "raw", "coverage": "PT1H", "nonAdditive": true,
			   "values": [{"reducer": "avg", "value": "0.01", "unit": "/min"}], "count": 3, "bounded": 2, "synthetic": false, "unavailable": false}], ` + head + `}`,
			"" +
				"START                  END                    FORM    FROM   COVERAGE   FLAGS                VALUES\n" +
				"2026-10-01T00:00:00Z   2026-10-02T00:00:00Z   gauge   raw    PT1H       -                    avg=60 /min\n" +
				"2026-10-02T00:00:00Z   2026-10-03T00:00:00Z   gauge   -      -          filled               avg=60 /min\n" +
				"2026-10-03T00:00:00Z   2026-10-04T00:00:00Z   gauge   -      -          unavailable          avg=-\n" +
				"2026-10-04T00:00:00Z   2026-10-05T00:00:00Z   gauge   raw    PT1H       overlaps,bounded=2   avg=0.01 /min\n",
		},
	} {
		h.fake.aggregateAnswer = x.answer
		out, _ := h.mustRun("aggregate", heartRate)
		if out != x.want {
			t.Errorf("%s:\n%s\nwant:\n%s", name, out, x.want)
		}
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
