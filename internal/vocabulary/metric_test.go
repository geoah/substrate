package vocabulary_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

func TestCheckMetricHoldsTheContract(t *testing.T) {
	g, _ := vocabulary.BuildUnitGraph([]vocabulary.UnitRecord{{ID: "kg"}, {ID: "m"}, {ID: "1/min"}}, nil)
	ok := map[string]any{
		"name": "Body mass", "unit": "kg", "form": "gauge",
		"bucketPlace": "end", "rollups": []any{"5m", "1h", "1d"},
	}
	if problems := vocabulary.CheckMetricIn(g, ok); len(problems) != 0 {
		t.Fatalf("a metric is refused: %v", problems)
	}
	for name, c := range map[string]struct {
		set  map[string]any
		want string
	}{
		"a listed unit of another component": {map[string]any{"units": []any{"m"}}, "props.units[0]: m does not convert to the metric's unit kg"},
		"an unknown unit":                    {map[string]any{"unit": "furlongs"}, `props.unit: "furlongs" names no unit`},
		"an unknown form":                    {map[string]any{"form": "level"}, `props.form: "level" is not a form`},
		"an unknown place":                   {map[string]any{"bucketPlace": "middle"}, `props.bucketPlace: "middle" is not a place`},
		"an unknown tier":                    {map[string]any{"rollups": []any{"15m", "2h"}}, `props.rollups[1]: "2h" is not a rollup tier`},
		"a tier twice":                       {map[string]any{"rollups": []any{"15m", "15m"}}, "props.rollups[1]: 15m is listed twice"},
		"tiers out of order":                 {map[string]any{"rollups": []any{"1h", "15m"}}, "props.rollups[1]: 15m comes after the coarser 1h"},
	} {
		props := map[string]any{}
		for k, v := range ok {
			props[k] = v
		}
		for k, v := range c.set {
			if v == nil {
				delete(props, k)
			} else {
				props[k] = v
			}
		}
		problems := vocabulary.CheckMetricIn(g, props)
		if len(problems) != 1 || !strings.HasPrefix(problems[0], c.want) {
			t.Errorf("%s: %v, want one problem starting %q", name, problems, c.want)
		}
	}
	// An empty rollup list keeps no tier.
	if problems := vocabulary.CheckMetricIn(g, map[string]any{"unit": "1/min", "rollups": []any{}}); len(problems) != 0 {
		t.Errorf("a metric with a unit and no rollups is refused: %v", problems)
	}
}

func TestMetricContractOfReadsTheRollupTiers(t *testing.T) {
	for name, c := range map[string]struct {
		props map[string]any
		want  []time.Duration
	}{
		"absent keeps the default tiers": {map[string]any{}, []time.Duration{15 * time.Minute, time.Hour, 24 * time.Hour}},
		"null keeps the default tiers":   {map[string]any{"rollups": nil}, []time.Duration{15 * time.Minute, time.Hour, 24 * time.Hour}},
		"an empty list keeps none":       {map[string]any{"rollups": []any{}}, []time.Duration{}},
		"the listed tiers, finest first": {map[string]any{"rollups": []any{"5m", "1d"}}, []time.Duration{5 * time.Minute, 24 * time.Hour}},
	} {
		got := vocabulary.MetricContractOf(c.props)
		if !reflect.DeepEqual(got.Rollups, c.want) {
			t.Errorf("%s: rollups %v, want %v", name, got.Rollups, c.want)
		}
	}
	c := vocabulary.MetricContractOf(map[string]any{"unit": map[string]any{"ref": vocabulary.KindUnit + "/min"}, "bucketPlace": "end"})
	if c.Unit != "min" || c.BucketPlace != "end" {
		t.Errorf("contract = %+v", c)
	}
}

func TestCheckSourcePrioritiesKeysByTheMetricsPath(t *testing.T) {
	const hr = "ada.example.com/observations/metric/health/heart_rate"
	isMetric := func(kind string) bool { return kind == "ada.example.com/observations/metric" }
	if problems := vocabulary.CheckSourcePriorities(map[string]any{"priorities": map[string]any{hr: float64(1)}}, isMetric); len(problems) != 0 {
		t.Fatalf("a full metric path is refused: %v", problems)
	}
	for name, c := range map[string]struct {
		priorities map[string]any
		want       string
	}{
		"a bare metric id": {
			map[string]any{"health/heart_rate": float64(1)},
			"props.priorities.health/heart_rate: a priority is keyed by the metric's full record path",
		},
		"a record that is not a metric": {
			map[string]any{"ada.example.com/observations/source/watch": float64(1)},
			"props.priorities.ada.example.com/observations/source/watch: ada.example.com/observations/source does not bind",
		},
		"a rank below 1": {map[string]any{hr: float64(0)}, "props.priorities." + hr + ": a rank is 1 or more"},
	} {
		problems := vocabulary.CheckSourcePriorities(map[string]any{"priorities": c.priorities}, isMetric)
		if len(problems) != 1 || !strings.HasPrefix(problems[0], c.want) {
			t.Errorf("%s: %v, want one problem starting %q", name, problems, c.want)
		}
	}
	if n, ok := vocabulary.SourcePriorityOf(map[string]any{"priorities": map[string]any{hr: float64(2)}}, hr); !ok || n != 2 {
		t.Errorf("SourcePriorityOf = %d, %v", n, ok)
	}
}
