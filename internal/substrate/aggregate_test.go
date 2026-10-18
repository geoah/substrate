package substrate_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/strictjson"
	"github.com/geoah/substrate/internal/substrate"
)

// Every key the aggregate read took before v1 is refused by name with what
// replaced it, wherever it sits, by the strict decode every door shares
// (REST, a function's host.records.aggregate, the agent query tool, the
// CLI's --body); it is never dropped. A misspelled key is still unknown.
func TestAggregateRefusesARetiredKeyByName(t *testing.T) {
	for _, x := range []struct {
		doc  string
		into any
		want string
	}{
		{`{"metric":{"ref":"a.example.com/observations/metric/m"},"minCoverage":0.5}`, &substrate.Aggregate{}, "minCoverage was removed: prefer-raw takes"},
		{`{"points":10,"method":"m4"}`, &substrate.Downsample{}, "method was removed: every downsample is M4"},
		{`{"bucket":{"calendar":"day","offset":"PT4H"}}`, &substrate.Aggregate{}, "bucket.offset was removed"},
		{`{"bucket":{"records":{"filter":{},"around":{"before":"PT1H"}}}}`, &substrate.Aggregate{}, "bucket.records.around was removed"},
		{`{"bucket":{"records":{"filter":{},"maxRecords":40}}}`, &substrate.Aggregate{}, "bucket.records.maxRecords was removed: the records a window read matches count against budget.maxSamples"},
		{`{"aggregate":{"minCoverage":0.5}}`, &substrate.AggregateQuery{}, "minCoverage was removed"},
		{`{"bucket":{"calendar":"day","ofset":"PT4H"}}`, &substrate.Aggregate{}, `unknown field "ofset"`},
		{`{"bucket":{"records":{"filter":{},"Sources":{}}}}`, &substrate.Aggregate{}, `unknown field "Sources"`},
	} {
		err := strictjson.DecodeBytes([]byte(x.doc), x.into, false)
		if err == nil || !strings.Contains(err.Error(), x.want) {
			t.Errorf("%s: %v, want it refused naming %q", x.doc, err, x.want)
		}
	}
	var a substrate.Aggregate
	if err := strictjson.DecodeBytes([]byte(`{"latest":true,"durationUnit":"min","bucket":{"calendar":"day","place":"end"}}`), &a, false); err != nil ||
		!a.Latest || a.DurationUnit != "min" || a.Bucket.Place != "end" {
		t.Errorf("a v1 body: %v, %+v", err, a)
	}
}

// LimitRows lowers the latest read's maxGroups, since its rows are its
// groups, and never a bucketed read's.
func TestLimitRowsLowersTheLatestReadsGroups(t *testing.T) {
	latest := substrate.AggregateQuery{Aggregate: &substrate.Aggregate{Latest: true}}
	if got := latest.LimitRows(10); len(got) != 2 || latest.Aggregate.Budget.MaxGroups != 10 {
		t.Errorf("the latest read lowered %v to %+v, want maxBuckets and maxGroups to 10", got, latest.Aggregate.Budget)
	}
	bucketed := substrate.AggregateQuery{Aggregate: &substrate.Aggregate{Range: &substrate.AggregateRange{Gte: "a", Lt: "b"}}}
	if got := bucketed.LimitRows(10); len(got) != 1 || bucketed.Aggregate.Budget.MaxGroups != 0 {
		t.Errorf("a bucketed read lowered %v to %+v, want maxBuckets alone", got, bucketed.Aggregate.Budget)
	}
}

// A client reading an answer from a newer server drops the keys it does not
// know, in the echoed bucket and its records mode too, while the same bucket
// in a request is still refused.
func TestAggregateResultDecodesUnknownKeys(t *testing.T) {
	doc := `{"metric":"a.example.com/observations/metric/m","unit":"kg","exact":true,"rows":[],"head":{"changelog":3,"generation":"g"},` +
		`"bucket":{"calendar":"day","place":"end","newKey":1,"records":{"filter":{"kinds":["a.example.com/x/y"]},"newer":true}},"later":[1]}`
	var res substrate.AggregateResult
	if err := json.Unmarshal([]byte(doc), &res); err != nil {
		t.Fatalf("an answer with unknown keys: %v", err)
	}
	if res.Unit != "kg" || res.Head.Changelog != 3 || res.Bucket == nil || res.Bucket.Place != "end" ||
		res.Bucket.Records == nil || len(res.Bucket.Records.Filter.Kinds) != 1 {
		t.Errorf("the answer decoded to %+v (bucket %+v)", res, res.Bucket)
	}
	var none substrate.AggregateResult
	if err := json.Unmarshal([]byte(`{"exact":false,"rows":[],"head":{"changelog":1,"generation":"g"}}`), &none); err != nil || none.Bucket != nil {
		t.Errorf("an answer with no bucket: %v, %+v", err, none.Bucket)
	}
	var a substrate.Aggregate
	if err := strictjson.DecodeBytes([]byte(`{"bucket":{"calendar":"day","newKey":1}}`), &a, false); err == nil {
		t.Error("a request bucket with an unknown key was admitted")
	}
}
