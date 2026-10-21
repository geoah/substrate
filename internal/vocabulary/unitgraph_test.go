package vocabulary_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

func gUnit(id string) vocabulary.UnitRecord { return vocabulary.UnitRecord{ID: id, Symbol: id} }

func gUnits(ids ...string) []vocabulary.UnitRecord {
	out := make([]vocabulary.UnitRecord, len(ids))
	for i, id := range ids {
		out[i] = gUnit(id)
	}
	return out
}

func gRat(t *testing.T, s string) *big.Rat {
	t.Helper()
	r, err := vocabulary.ParseRational(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// gLinear is a linear conversion `to = from * factor (+ offset)`, its id
// <from>-<to>.
func gLinear(t *testing.T, from, to, factor string, offset ...string) vocabulary.ConversionRecord {
	t.Helper()
	c := vocabulary.ConversionRecord{ID: from + "-" + to, From: from, To: to, Factor: gRat(t, factor)}
	if len(offset) > 0 {
		c.Offset = gRat(t, offset[0])
	}
	return c
}

func gExpr(from, to, expr, inverse string) vocabulary.ConversionRecord {
	return vocabulary.ConversionRecord{ID: from + "-" + to, From: from, To: to, Expr: expr, Inverse: inverse}
}

func gBuild(t *testing.T, units []vocabulary.UnitRecord, conversions ...vocabulary.ConversionRecord) *vocabulary.UnitGraph {
	t.Helper()
	g, problems := vocabulary.BuildUnitGraph(units, conversions)
	for _, p := range problems {
		t.Errorf("unexpected problem: %v", p)
	}
	return g
}

// gProblem builds a graph that must name a problem about the given record
// carrying the given words.
func gProblem(t *testing.T, kind, id, words string, units []vocabulary.UnitRecord, conversions ...vocabulary.ConversionRecord) *vocabulary.UnitGraph {
	t.Helper()
	g, problems := vocabulary.BuildUnitGraph(units, conversions)
	for _, p := range problems {
		if p.Kind == kind && p.ID == id && strings.Contains(p.Message, words) {
			return g
		}
	}
	t.Fatalf("want a problem on %s/%s naming %q, got %v", kind, id, words, problems)
	return nil
}

func gConvert(t *testing.T, g *vocabulary.UnitGraph, value, from, to string, delta bool) vocabulary.UnitConversion {
	t.Helper()
	got, err := g.Convert(context.Background(), value, from, to, delta)
	if err != nil {
		t.Fatalf("%s %s to %s: %v", value, from, to, err)
	}
	return got
}

func gLinearMap(t *testing.T, g *vocabulary.UnitGraph, from, to string, delta bool) string {
	t.Helper()
	f, o, err := g.Linear(from, to, delta)
	if err != nil {
		t.Fatalf("%s to %s: %v", from, to, err)
	}
	return f.RatString() + " " + o.RatString()
}

func TestParseRational(t *testing.T) {
	for in, want := range map[string]string{
		"0.45359237": "45359237/100000000", "5/9": "5/9", "-273.15": "-5463/20", "1/0.3048": "1250/381",
		"360": "360", "007": "7",
	} {
		r, err := vocabulary.ParseRational(in)
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if r.RatString() != want {
			t.Errorf("%s reads as %s, want %s", in, r.RatString(), want)
		}
	}
	for in, why := range map[string]string{
		"1e3": "exponent", "1/0": "divides by zero", ".5": "not a rational", "5/-9": "not a rational",
		"": "not a rational", "1/3/4": "not a rational", "+1": "not a rational",
		strings.Repeat("9", 65):        "at most 64 digits",
		"1/" + strings.Repeat("9", 65): "at most 64 digits",
	} {
		if _, err := vocabulary.ParseRational(in); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%q: got %v, want an error naming %q", in, err, why)
		}
	}
	if _, err := vocabulary.ParseRational(strings.Repeat("9", 64) + "/" + strings.Repeat("7", 64)); err != nil {
		t.Errorf("64 digits in each part: %v", err)
	}
	for r, want := range map[string]string{"5/9": "5/9", "0.45359237": "0.45359237", "-1/4": "-0.25", "3": "3"} {
		if got := vocabulary.FormatRational(gRat(t, r)); got != want {
			t.Errorf("FormatRational(%s) = %s, want %s", r, got, want)
		}
	}
}

func TestCheckUnitRecord(t *testing.T) {
	u, err := vocabulary.CheckUnitRecord("deg", map[string]any{
		"name": "degree", "symbol": "°", "code": "deg", "aliases": []any{"degree", "degrees"}, "turn": "360",
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.Symbol != "°" || u.Code != "deg" || len(u.Aliases) != 2 || u.Turn.RatString() != "360" || u.Scale != "" {
		t.Errorf("read %+v", u)
	}
	if u, _ := vocabulary.CheckUnitRecord("kg", map[string]any{}); u.Symbol != "kg" {
		t.Errorf("a unit without a symbol prints its id, got %q", u.Symbol)
	}
	if u, _ := vocabulary.CheckUnitRecord("1", map[string]any{"symbol": ""}); u.Symbol != "" {
		t.Errorf("an empty symbol prints nothing, got %q", u.Symbol)
	}
	if u, _ := vocabulary.CheckUnitRecord("dBm", map[string]any{"scale": "powerlevel"}); u.Scale != vocabulary.UnitScalePowerLevel {
		t.Errorf("scale %q", u.Scale)
	}
	many := make([]any, vocabulary.MaxUnitAliases+1)
	for i := range many {
		many[i] = fmt.Sprintf("a%d", i)
	}
	for name, c := range map[string]struct {
		id    string
		props map[string]any
		why   string
	}{
		"id":           {"/min", nil, "not a record id"},
		"long id":      {strings.Repeat("x", vocabulary.MaxIDLen+1), nil, "not a record id"},
		"scale":        {"x", map[string]any{"scale": "logarithmic"}, "not a scale"},
		"turn zero":    {"x", map[string]any{"turn": "0"}, "above zero"},
		"turn number":  {"x", map[string]any{"turn": 360.0}, "as a string"},
		"turn grammar": {"x", map[string]any{"turn": "2pi"}, "not a rational"},
		"aliases":      {"x", map[string]any{"aliases": "lb"}, "a list"},
		"alias type":   {"x", map[string]any{"aliases": []any{1}}, "a list"},
		"many aliases": {"x", map[string]any{"aliases": many}, "at most 32 aliases"},
		"long alias":   {"x", map[string]any{"aliases": []any{strings.Repeat("a", vocabulary.MaxUnitSpelling+1)}}, "at most 128 bytes"},
		"long code":    {"x", map[string]any{"code": strings.Repeat("a", vocabulary.MaxUnitSpelling+1)}, "at most 128 bytes"},
		"empty alias":  {"x", map[string]any{"aliases": []any{""}}, "not empty"},
		"spaced alias": {"x", map[string]any{"aliases": []any{" lb"}}, "space"},
		"name":         {"x", map[string]any{"name": 3}, "name: a string"},
	} {
		if _, err := vocabulary.CheckUnitRecord(c.id, c.props); err == nil || !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s: got %v, want an error naming %q", name, err, c.why)
		}
	}
}

func TestCheckConversionRecord(t *testing.T) {
	ref := func(id string) map[string]any { return map[string]any{"ref": vocabulary.KindUnit + "/" + id} }
	c, err := vocabulary.CheckConversionRecord("degF-K", map[string]any{
		"from": ref("degF"), "to": ref("K"), "factor": "5/9", "offset": "45967/180",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.From != "degF" || c.To != "K" || c.Factor.RatString() != "5/9" || c.Offset.RatString() != "45967/180" || !c.Linear() {
		t.Errorf("read %+v", c)
	}
	// A reference reads as the stored object, the full path or a bare id,
	// and an id may hold a slash.
	c, err = vocabulary.CheckConversionRecord("x", map[string]any{
		"from": vocabulary.KindUnit + "/1/min", "to": "1/s", "factor": "1/60",
	})
	if err != nil || c.From != "1/min" || c.To != "1/s" {
		t.Errorf("read %+v, %v", c, err)
	}
	c, err = vocabulary.CheckConversionRecord("mpg-L/100km", map[string]any{
		"from": ref("mpg"), "to": ref("L/100km"), "expr": "235.214583 / value", "inverse": "235.214583 / value",
	})
	if err != nil || c.Linear() || c.Expr == "" || c.Inverse == "" {
		t.Errorf("read %+v, %v", c, err)
	}
	// A null property is an absent one: a null factor beside an expression
	// leaves an expression conversion.
	c, err = vocabulary.CheckConversionRecord("a-b", map[string]any{
		"from": ref("a"), "to": ref("b"), "factor": nil, "offset": nil, "expr": "value", "inverse": "value",
	})
	if err != nil || c.Linear() {
		t.Errorf("a null factor beside an expression: read %+v, %v", c, err)
	}
	for name, cc := range map[string]struct {
		props map[string]any
		why   string
	}{
		"no from":        {map[string]any{"to": ref("b"), "factor": "2"}, "from: a conversion names"},
		"wrong kind":     {map[string]any{"from": map[string]any{"ref": vocabulary.CoreKind("token") + "/a"}, "to": ref("b"), "factor": "2"}, "not a " + vocabulary.KindUnit},
		"same unit":      {map[string]any{"from": ref("a"), "to": ref("a"), "factor": "1"}, "names a twice"},
		"both forms":     {map[string]any{"from": ref("a"), "to": ref("b"), "factor": "2", "expr": "value", "inverse": "value"}, "never both"},
		"no form":        {map[string]any{"from": ref("a"), "to": ref("b")}, "writes a factor"},
		"bare offset":    {map[string]any{"from": ref("a"), "to": ref("b"), "offset": "1"}, "offset belongs"},
		"null factor":    {map[string]any{"from": ref("a"), "to": ref("b"), "factor": nil}, "writes a factor"},
		"null, offset":   {map[string]any{"from": ref("a"), "to": ref("b"), "factor": nil, "offset": "1"}, "offset belongs"},
		"no inverse":     {map[string]any{"from": ref("a"), "to": ref("b"), "expr": "value * 2.0"}, "together"},
		"zero factor":    {map[string]any{"from": ref("a"), "to": ref("b"), "factor": "0"}, "above zero"},
		"negative":       {map[string]any{"from": ref("a"), "to": ref("b"), "factor": "-2"}, "above zero"},
		"float factor":   {map[string]any{"from": ref("a"), "to": ref("b"), "factor": 0.45359237}, "as a string"},
		"bool expr":      {map[string]any{"from": ref("a"), "to": ref("b"), "expr": "value > 1.0", "inverse": "value"}, "must return a double"},
		"int expr":       {map[string]any{"from": ref("a"), "to": ref("b"), "expr": "2", "inverse": "value"}, "must return a double"},
		"mixed types":    {map[string]any{"from": ref("a"), "to": ref("b"), "expr": "value * 2", "inverse": "value"}, "expr:"},
		"unknown name":   {map[string]any{"from": ref("a"), "to": ref("b"), "expr": "record.x", "inverse": "value"}, "expr:"},
		"empty inverse":  {map[string]any{"from": ref("a"), "to": ref("b"), "expr": "value", "inverse": " "}, "not empty"},
		"long expr":      {map[string]any{"from": ref("a"), "to": ref("b"), "expr": "value" + strings.Repeat(" + 1.0", 200), "inverse": "value"}, "at most 1024 bytes"},
		"bad id in path": {map[string]any{"from": ref("%"), "to": ref("b"), "factor": "2"}, "not a unit id"},
	} {
		if _, err := vocabulary.CheckConversionRecord("a-b", cc.props); err == nil || !strings.Contains(err.Error(), cc.why) {
			t.Errorf("%s: got %v, want an error naming %q", name, err, cc.why)
		}
	}
}

func TestLinearConversionsComposeExactly(t *testing.T) {
	g := gBuild(t, gUnits("kg", "lb", "st", "g"),
		gLinear(t, "lb", "kg", "0.45359237"), gLinear(t, "st", "lb", "14"), gLinear(t, "kg", "g", "1000"))
	if root, _ := g.Root("st"); root != "g" {
		t.Errorf("the root is %s, want g, the smallest id", root)
	}
	if got := gLinearMap(t, g, "st", "kg", false); got != "317514659/50000000 0" {
		t.Errorf("st to kg is %s", got)
	}
	if got := gConvert(t, g, "1", "st", "kg", false); got.Value != "6.35029318" || !got.Exact || got.Unit != "kg" {
		t.Errorf("1 st is %+v", got)
	}
	if got := gConvert(t, g, "6.35029318", "kg", "st", false); got.Value != "1" || !got.Exact {
		t.Errorf("6.35029318 kg is %+v st", got)
	}
	if f, _, ok := g.ToRoot("lb"); !ok || f.RatString() != "45359237/100000" {
		t.Errorf("lb to its root is %v", f)
	}
	if got := g.Component("lb"); strings.Join(got, ",") != "g,kg,lb,st" {
		t.Errorf("component %v", got)
	}

	// Affine maps compose with their offsets, and a difference drops them.
	g = gBuild(t, gUnits("K", "Cel", "degF"),
		gLinear(t, "Cel", "K", "1", "273.15"), gLinear(t, "degF", "K", "5/9", "45967/180"))
	if got := gLinearMap(t, g, "Cel", "degF", false); got != "9/5 32" {
		t.Errorf("Cel to degF is %s", got)
	}
	if got := gConvert(t, g, "37", "Cel", "degF", false); got.Value != "98.6" || !got.Exact {
		t.Errorf("37 Cel is %+v degF", got)
	}
	if got := gLinearMap(t, g, "Cel", "degF", true); got != "9/5 0" {
		t.Errorf("a difference in Cel to degF is %s", got)
	}
	if got := gConvert(t, g, "0.3", "Cel", "degF", true); got.Value != "0.54" {
		t.Errorf("a 0.3 Cel difference is %+v degF", got)
	}
	if got := gLinearMap(t, g, "K", "K", false); got != "1 0" {
		t.Errorf("K to K is %s", got)
	}
}

func TestAnAgreeingCycleIsAdmitted(t *testing.T) {
	gBuild(t, gUnits("kg", "lb", "st"),
		gLinear(t, "lb", "kg", "0.45359237"), gLinear(t, "st", "lb", "14"), gLinear(t, "st", "kg", "6.35029318"))
	gBuild(t, gUnits("K", "Cel", "degF"),
		gLinear(t, "Cel", "K", "1", "273.15"), gLinear(t, "degF", "K", "5/9", "45967/180"),
		gLinear(t, "Cel", "degF", "9/5", "32"))
	// Two conversions between one pair are a cycle too.
	gBuild(t, gUnits("a", "b"), gLinear(t, "a", "b", "2"),
		vocabulary.ConversionRecord{ID: "a-b-again", From: "b", To: "a", Factor: big.NewRat(1, 2)})
}

func TestAnInconsistentCycleIsAProblem(t *testing.T) {
	units := gUnits("kg", "lb", "st")
	g := gProblem(t, vocabulary.KindConversion, "st-lb", "does not agree", units,
		gLinear(t, "lb", "kg", "0.45359237"), gLinear(t, "st", "lb", "14"), gLinear(t, "st", "kg", "6.35"))
	_, problems := vocabulary.BuildUnitGraph(units, []vocabulary.ConversionRecord{
		gLinear(t, "lb", "kg", "0.45359237"), gLinear(t, "st", "lb", "14"), gLinear(t, "st", "kg", "6.35"),
	})
	if msg := problems[0].Message; !strings.Contains(msg, "lb-kg, st-kg") || !strings.Contains(msg, "lb = st * 14") {
		t.Errorf("the problem names neither route: %s", msg)
	}
	// The graph still answers through the conversions the walk took.
	if got := gConvert(t, g, "1", "st", "kg", false); got.Value != "6.35" {
		t.Errorf("1 st is %+v kg", got)
	}
	// An offset that disagrees is a cycle that does not compose too.
	gProblem(t, vocabulary.KindConversion, "degF-K", "does not agree", gUnits("K", "Cel", "degF"),
		gLinear(t, "Cel", "K", "1", "273.15"), gLinear(t, "degF", "K", "5/9", "45967/180"),
		gLinear(t, "Cel", "degF", "9/5", "31"))
}

func TestUnitsOfTwoScalesNeverJoin(t *testing.T) {
	dB := vocabulary.UnitRecord{ID: "dB", Symbol: "dB", Scale: vocabulary.UnitScaleLevel}
	gProblem(t, vocabulary.KindConversion, "dB-1", "share one scale",
		[]vocabulary.UnitRecord{dB, gUnit("1")}, gLinear(t, "dB", "1", "1"))
	dBm := vocabulary.UnitRecord{ID: "dBm", Symbol: "dBm", Scale: vocabulary.UnitScalePowerLevel}
	dBW := vocabulary.UnitRecord{ID: "dBW", Symbol: "dBW", Scale: vocabulary.UnitScalePowerLevel}
	g := gBuild(t, []vocabulary.UnitRecord{dBm, dBW}, gLinear(t, "dBW", "dBm", "1", "30"))
	if g.Scale("dBm") != vocabulary.UnitScalePowerLevel {
		t.Errorf("scale %q", g.Scale("dBm"))
	}
}

func TestAMissingUnitIsAProblem(t *testing.T) {
	gProblem(t, vocabulary.KindConversion, "lb-kg", "to names kg, which is no unit", gUnits("lb"),
		gLinear(t, "lb", "kg", "0.45359237"))
	gProblem(t, vocabulary.KindConversion, "lb-kg", "from names lb", gUnits("kg"),
		gLinear(t, "lb", "kg", "0.45359237"))
}

func TestSpellingsAreUniqueAndResolveInOrder(t *testing.T) {
	lb := vocabulary.UnitRecord{ID: "lb", Symbol: "lb", Code: "[lb_av]", Aliases: []string{"pound", "lbs"}}
	ug := vocabulary.UnitRecord{ID: "ug", Symbol: "μg", Code: "ug", Aliases: []string{"mcg", "μg"}}
	g := gBuild(t, []vocabulary.UnitRecord{lb, ug})
	for in, want := range map[string]string{
		"lb": "lb", "[lb_av]": "lb", "pound": "lb", " lbs ": "lb", "ug": "ug", "mcg": "ug",
		"μg": "ug", "µg": "ug", // GREEK SMALL LETTER MU and MICRO SIGN
	} {
		if got, ok := g.Resolve(in); !ok || got != want {
			t.Errorf("%q resolves to %q, %v; want %s", in, got, ok, want)
		}
	}
	if _, ok := g.Resolve("LB"); ok {
		t.Error("a spelling is case-sensitive")
	}

	// One spelling names one unit, written or normalized; where two units
	// answer one, the id wins, then the code, then the alias.
	a := vocabulary.UnitRecord{ID: "a", Symbol: "a", Aliases: []string{"x"}}
	x := gUnit("x")
	g = gProblem(t, vocabulary.KindUnit, "x", `"x" already names unit a`, []vocabulary.UnitRecord{a, x})
	if got, _ := g.Resolve("x"); got != "x" {
		t.Errorf("x resolves to %s; an id comes before an alias", got)
	}
	micro := vocabulary.UnitRecord{ID: "mcg", Symbol: "mcg", Aliases: []string{"µg"}}
	gProblem(t, vocabulary.KindUnit, "ug", `"ug" already names unit mcg`, []vocabulary.UnitRecord{micro, gUnit("ug")})
	code := vocabulary.UnitRecord{ID: "b", Symbol: "b", Code: "[b]"}
	alias := vocabulary.UnitRecord{ID: "c", Symbol: "c", Aliases: []string{"[b]"}}
	g = gProblem(t, vocabulary.KindUnit, "c", "already names unit b", []vocabulary.UnitRecord{code, alias})
	if got, _ := g.Resolve("[b]"); got != "b" {
		t.Errorf("[b] resolves to %s; a code comes before an alias", got)
	}
}

func TestExpressionEdgesMustFormAForest(t *testing.T) {
	units := gUnits("mpg", "km/L", "L/100km", "L/km", "x")
	fuel := []vocabulary.ConversionRecord{
		gLinear(t, "km/L", "mpg", "112903/48000"), gLinear(t, "L/km", "L/100km", "100"),
		gExpr("mpg", "L/100km", "235.214583 / value", "235.214583 / value"),
	}
	gBuild(t, units, fuel...)
	// A second route between the same two components.
	gProblem(t, vocabulary.KindConversion, "mpg-L/km", "second route", units,
		append(fuel, gExpr("mpg", "L/km", "2.35214583 / value", "2.35214583 / value"))...)
	// An expression between two units a linear route joins.
	gProblem(t, vocabulary.KindConversion, "km/L-mpg-expr", "linear conversions already join", units,
		append(fuel, vocabulary.ConversionRecord{ID: "km/L-mpg-expr", From: "km/L", To: "mpg", Expr: "value", Inverse: "value"})...)
	// A cycle through three components.
	gProblem(t, vocabulary.KindConversion, "x-mpg", "second route", units,
		append(fuel, gExpr("L/100km", "x", "value", "value"), gExpr("x", "mpg", "value", "value"))...)
}

func TestTheReverseRouteUsesTheInverses(t *testing.T) {
	g := gBuild(t, gUnits("mpg", "km/L", "L/100km", "L/km", "a", "b"),
		gLinear(t, "km/L", "mpg", "112903/48000"), gLinear(t, "L/km", "L/100km", "100"),
		gExpr("mpg", "L/100km", "235.214583 / value", "235.214583 / value"),
		// An inverse is the author's contract: the reverse route reads it,
		// even where it does not undo the expression.
		gExpr("a", "b", "value * 2.0", "value / 4.0"))
	if got := gConvert(t, g, "30", "mpg", "L/100km", false); got.Value != "7.840486100000001" || got.Exact {
		t.Errorf("30 mpg is %+v L/100km", got)
	}
	// km/L reaches mpg linearly, then the expression, then L/100km to L/km.
	got := gConvert(t, g, "10", "km/L", "L/km", false)
	if v, _ := strconv.ParseFloat(got.Value, 64); math.Abs(v-0.1) > 1e-6 || got.Unit != "L/km" || got.Exact {
		t.Errorf("10 km/L is %+v L/km", got)
	}
	if got := gConvert(t, g, "3", "a", "b", false); got.Value != "6" || got.Exact {
		t.Errorf("3 a is %+v b", got)
	}
	if got := gConvert(t, g, "8", "b", "a", false); got.Value != "2" {
		t.Errorf("8 b is %+v a: the reverse reads the inverse", got)
	}
	if _, _, err := g.Linear("mpg", "L/100km", false); err == nil || !strings.Contains(err.Error(), "only through an expression") {
		t.Errorf("Linear across an expression: %v", err)
	}
	if _, err := g.Convert(context.Background(), "1", "mpg", "a", false); err == nil || !strings.Contains(err.Error(), "no conversion joins") {
		t.Errorf("two components no route joins: %v", err)
	}
}

func TestANonFiniteExpressionIsRefused(t *testing.T) {
	g := gBuild(t, gUnits("a", "b"), gExpr("a", "b", "1.0 / value", "1.0 / value"))
	if _, err := g.Convert(context.Background(), "0", "a", "b", false); err == nil || !strings.Contains(err.Error(), "conversion a-b: expr") {
		t.Errorf("1/0: %v", err)
	}
	// An expression that never compiled refuses at the first conversion,
	// naming itself.
	g = gBuild(t, gUnits("a", "b"), gExpr("a", "b", "value +", "value"))
	if _, err := g.Convert(context.Background(), "1", "a", "b", false); err == nil || !strings.Contains(err.Error(), "conversion a-b: expr") {
		t.Errorf("a broken expression: %v", err)
	}
	g = gBuild(t, gUnits("a", "b"), gExpr("a", "b", "value", "value"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Convert(ctx, "1", "a", "b", false); !errors.Is(err, context.Canceled) {
		t.Errorf("a canceled conversion: %v", err)
	}
}

func TestADifferenceNeverCrossesAnExpression(t *testing.T) {
	g := gBuild(t, gUnits("a", "b"), gExpr("a", "b", "value * 2.0", "value / 2.0"))
	if _, err := g.Convert(context.Background(), "1", "a", "b", true); err == nil || !strings.Contains(err.Error(), "a difference converts through linear conversions alone") {
		t.Errorf("a difference across an expression: %v", err)
	}
}

func TestCircularComponentNeedsConsistentTurns(t *testing.T) {
	turned := func(id, turn string) vocabulary.UnitRecord {
		u := gUnit(id)
		u.Turn = gRat(t, turn)
		return u
	}
	g := gBuild(t, []vocabulary.UnitRecord{turned("deg", "360"), turned("arcmin", "21600"), gUnit("arcsec")},
		gLinear(t, "arcmin", "deg", "1/60"), gLinear(t, "arcsec", "deg", "1/3600"))
	if got := g.Turn("arcsec"); got == nil || got.RatString() != "1296000" {
		t.Errorf("arcsec's turn, through its component, is %v", got)
	}
	gProblem(t, vocabulary.KindUnit, "deg", "disagrees with arcmin's", []vocabulary.UnitRecord{turned("deg", "360"), turned("arcmin", "21000")},
		gLinear(t, "arcmin", "deg", "1/60"))
	gProblem(t, vocabulary.KindConversion, "compass-deg", "carries an offset inside a circular component",
		[]vocabulary.UnitRecord{turned("deg", "360"), turned("compass", "360")}, gLinear(t, "compass", "deg", "1", "90"))
	// An offset is fine where no unit has a turn.
	gBuild(t, gUnits("deg", "compass"), gLinear(t, "compass", "deg", "1", "90"))
}

func TestARepeatingAnswerIsInexact(t *testing.T) {
	g := gBuild(t, gUnits("a", "b", "lb", "kg"), gLinear(t, "a", "b", "1/3"), gLinear(t, "lb", "kg", "0.45359237"))
	for _, c := range []struct {
		value, from, to, want string
		exact                 bool
	}{
		{"1", "a", "b", "0.33333333333333333333", false},
		{"3", "a", "b", "1", true},
		{"1", "b", "a", "3", true},
		{"1", "lb", "kg", "0.45359237", true},
		{"1", "kg", "lb", "2.2046226218487758072", false},
		{"0.0000", "a", "b", "0", true},
	} {
		if got := gConvert(t, g, c.value, c.from, c.to, false); got.Value != c.want || got.Exact != c.exact {
			t.Errorf("%s %s to %s is %+v, want %s exact %v", c.value, c.from, c.to, got, c.want, c.exact)
		}
	}
	if _, err := g.Convert(context.Background(), "1e3", "a", "b", false); err == nil || !strings.HasPrefix(err.Error(), "value:") {
		t.Errorf("an exponent: %v", err)
	}
	if _, err := g.Convert(context.Background(), "1", "nope", "b", false); err == nil || !strings.HasPrefix(err.Error(), "from:") {
		t.Errorf("an unknown unit: %v", err)
	}
}

func TestUnitGraphLimits(t *testing.T) {
	t.Run("units", func(t *testing.T) {
		units := make([]vocabulary.UnitRecord, vocabulary.MaxUnits+1)
		for i := range units {
			units[i] = gUnit(fmt.Sprintf("u%04d", i))
		}
		gProblem(t, vocabulary.KindUnit, "", "at most 2000 units", units)
		gBuild(t, units[:vocabulary.MaxUnits])
	})
	t.Run("conversions", func(t *testing.T) {
		convs := make([]vocabulary.ConversionRecord, vocabulary.MaxConversions+1)
		for i := range convs {
			convs[i] = vocabulary.ConversionRecord{ID: fmt.Sprintf("c%04d", i), From: "a", To: "b", Factor: big.NewRat(2, 1)}
		}
		gProblem(t, vocabulary.KindConversion, "", "at most 4000 conversions", gUnits("a", "b"), convs...)
		gBuild(t, gUnits("a", "b"), convs[:vocabulary.MaxConversions]...)
	})
	t.Run("composed bits", func(t *testing.T) {
		nines := strings.Repeat("9", vocabulary.MaxRationalDigits)
		var units []vocabulary.UnitRecord
		var convs []vocabulary.ConversionRecord
		for i := range 21 {
			units = append(units, gUnit(fmt.Sprintf("u%02d", i)))
			if i > 0 {
				convs = append(convs, gLinear(t, fmt.Sprintf("u%02d", i), fmt.Sprintf("u%02d", i-1), nines))
			}
		}
		gProblem(t, vocabulary.KindConversion, "u20-u19", "past 4096 bits", units, convs...)
		gBuild(t, units[:20], convs[:19]...)
	})
	t.Run("expression route", func(t *testing.T) {
		var units []vocabulary.UnitRecord
		var convs []vocabulary.ConversionRecord
		for i := range vocabulary.MaxExpressionRoute + 2 {
			units = append(units, gUnit(fmt.Sprintf("e%d", i)))
			if i > 0 {
				convs = append(convs, gExpr(fmt.Sprintf("e%d", i-1), fmt.Sprintf("e%d", i), "value + 1.0", "value - 1.0"))
			}
		}
		g := gBuild(t, units, convs...)
		if got := gConvert(t, g, "0", "e0", "e4", false); got.Value != "4" {
			t.Errorf("four expressions: %+v", got)
		}
		if _, err := g.Convert(context.Background(), "0", "e0", "e5", false); err == nil || !strings.Contains(err.Error(), "at most 4") {
			t.Errorf("five expressions: %v", err)
		}
	})
	t.Run("output digits", func(t *testing.T) {
		g := gBuild(t, gUnits("a", "b"), gLinear(t, "a", "b", "1"+strings.Repeat("0", 60)))
		if _, err := g.Convert(context.Background(), strings.Repeat("9", 990), "a", "b", false); err == nil || !strings.Contains(err.Error(), "at most 1000 digits") {
			t.Errorf("a converted value past MaxDecimalDigits: %v", err)
		}
		if _, err := g.Convert(context.Background(), strings.Repeat("9", 900), "a", "b", false); err != nil {
			t.Errorf("960 digits: %v", err)
		}
	})
}

func TestUnitGraphMemoBuildsOnce(t *testing.T) {
	g := gBuild(t, gUnits("a"))
	builds := 0
	for range 3 {
		v, err := g.Memo("arms:a", func() (any, error) { builds++; return "sql", nil })
		if err != nil || v != "sql" {
			t.Fatalf("memo: %v, %v", v, err)
		}
	}
	if builds != 1 {
		t.Errorf("built %d times", builds)
	}
	if _, err := g.Memo("bad", func() (any, error) { return nil, errors.New("no") }); err == nil {
		t.Error("an error is answered")
	}
	if v, _ := g.Memo("bad", func() (any, error) { return "ok", nil }); v != "ok" {
		t.Error("an error is never stored")
	}
}

// A built graph keeps its own copy of every rational it was built from, so a
// caller mutating its records afterwards changes neither what Conversions
// and Unit report nor what the graph converts by.
func TestABuiltGraphOwnsItsCoefficients(t *testing.T) {
	units := gUnits("a", "b", "deg")
	units[2].Turn = gRat(t, "360")
	conversions := []vocabulary.ConversionRecord{gLinear(t, "a", "b", "2", "1")}
	g := gBuild(t, units, conversions...)
	conversions[0].Factor.SetInt64(3)
	conversions[0].Offset.SetInt64(5)
	units[2].Turn.SetInt64(400)
	got := g.Conversions()[0]
	if got.Factor.RatString() != "2" || got.Offset.RatString() != "1" {
		t.Errorf("Conversions reports %s + %s after the input changed, want 2 + 1", got.Factor.RatString(), got.Offset.RatString())
	}
	if m := gLinearMap(t, g, "a", "b", false); m != "2 1" {
		t.Errorf("a to b is %s, want 2 1", m)
	}
	if u, _ := g.Unit("deg"); u.Turn.RatString() != "360" {
		t.Errorf("deg's turn is %s, want 360", u.Turn.RatString())
	}
}
