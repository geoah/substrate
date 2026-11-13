package api

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/geoah/substrate/internal/substrate"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files")

const featuresGoldenPath = "testdata/features.golden.json"

// errorsFeature is the one feature that holds no route and is a mode of
// none: the error envelope is every route's refusal.
const errorsFeature = "errors"

// everyOptionalRoute builds the handler with each route a deployment turns
// on: the metrics exposition, and a site whose logo is its tab icon.
func everyOptionalRoute(t *testing.T) http.Handler {
	t.Helper()
	return New(Config{Service: &stubService{}, Metrics: true, Site: siteDir(t), WebDir: consoleDir(t)})
}

// walkRoutes lists a router's routes as `METHOD pattern`, the table's
// spelling. A route registered for every method walks as one line per
// method, and is listed once as `* pattern`.
func walkRoutes(t *testing.T, h http.Handler) []string {
	t.Helper()
	routes, ok := h.(chi.Routes)
	if !ok {
		t.Fatalf("the handler is a %T, not a chi router: nothing can walk its routes", h)
	}
	methods := map[string][]string{}
	err := chi.Walk(routes, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		methods[pattern] = append(methods[pattern], method)
		return nil
	})
	if err != nil {
		t.Fatalf("walk the router: %v", err)
	}
	all := everyMethod(t)
	var out []string
	for pattern, served := range methods {
		slices.Sort(served)
		if slices.Equal(served, all) {
			out = append(out, "* "+pattern)
			continue
		}
		for _, method := range served {
			out = append(out, method+" "+pattern)
		}
	}
	slices.Sort(out)
	return out
}

// everyMethod is the method set chi registers a method-less route under,
// read off a router because the set is chi's to change.
func everyMethod(t *testing.T) []string {
	t.Helper()
	r := chi.NewRouter()
	r.Handle("/", http.NotFoundHandler())
	var out []string
	err := chi.Walk(r, func(method, _ string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		out = append(out, method)
		return nil
	})
	if err != nil {
		t.Fatalf("walk a router: %v", err)
	}
	slices.Sort(out)
	return out
}

// The feature list is a claim about routes, so the table is held to the
// router both ways. A route no feature holds is one no client was told how
// far to trust, and a table route the router does not serve is a promise
// about nothing.
func TestEveryRouteIsInOneFeature(t *testing.T) {
	served := walkRoutes(t, everyOptionalRoute(t))

	holder := map[string]string{}
	for _, f := range featureTable {
		for _, route := range f.routes {
			if other, held := holder[route]; held {
				t.Errorf("%s is in two features, %q and %q: a route is in exactly one (featureTable, internal/api/features.go)", route, other, f.name)
				continue
			}
			holder[route] = f.name
		}
	}

	for _, route := range served {
		if _, held := holder[route]; !held {
			t.Errorf("%s is served and no feature holds it. Add it to the routes of the feature it belongs to "+
				"in featureTable (internal/api/features.go) and raise that feature's revision, or give it a new "+
				"feature at revision 1: a client reads the label and the revision before it uses the route.", route)
		}
	}
	for route, name := range holder {
		if !slices.Contains(served, route) {
			t.Errorf("feature %q names %s, which the router does not serve with every optional route on: "+
				"a removed route leaves featureTable (internal/api/features.go) with it", name, route)
		}
	}

	for _, f := range featureTable {
		switch {
		case f.modeOf != "":
			if _, held := holder[f.modeOf]; !held {
				t.Errorf("feature %q is a mode of %s, which no feature holds", f.name, f.modeOf)
			}
		case len(f.routes) == 0 && f.name != errorsFeature:
			t.Errorf("feature %q holds no route and is a mode of none: it names nothing a client can call", f.name)
		}
	}
}

// What every entry must be before a client can read it: a name of its own,
// one of the three labels, a revision that started at 1, and surfaces the
// document's `surfaces` object carries.
func TestFeatureTableEntriesAreReadable(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range featureTable {
		if f.name == "" || seen[f.name] {
			t.Errorf("feature name %q is empty or listed twice", f.name)
		}
		seen[f.name] = true
		if !slices.Contains(substrate.Stabilities, f.stability) {
			t.Errorf("feature %q stability = %q, want one of %v", f.name, f.stability, substrate.Stabilities)
		}
		if f.revision < 1 {
			t.Errorf("feature %q revision = %d: a revision starts at 1", f.name, f.revision)
		}
		if len(f.surfaces) == 0 {
			t.Errorf("feature %q names no surface", f.name)
		}
		for _, s := range f.surfaces {
			if s != surfaceREST && s != surfaceMCP {
				t.Errorf("feature %q names surface %q, which the surfaces object does not carry", f.name, s)
			}
		}
	}
	// The web app kit refuses a discovery document with no `apps` entry.
	if !seen["apps"] {
		t.Error("the feature table has no `apps` entry")
	}
}

// featureGolden is one feature as the golden records it.
type featureGolden struct {
	Name      string   `json:"name"`
	Stability string   `json:"stability"`
	Revision  int      `json:"revision"`
	Routes    []string `json:"routes"`
}

func goldenFeatures() []featureGolden {
	out := make([]featureGolden, 0, len(featureTable))
	for _, f := range featureTable {
		routes := slices.Clone(f.routes)
		if routes == nil {
			routes = []string{}
		}
		slices.Sort(routes)
		out = append(out, featureGolden{Name: f.name, Stability: f.stability, Revision: f.revision, Routes: routes})
	}
	return out
}

// revisionProblems is why a table may not replace the golden: a revision is
// a number a client compares, so it never goes down, a feature whose routes
// changed carries a higher one, and a new feature starts at 1.
func revisionProblems(before, after []featureGolden) []string {
	old := map[string]featureGolden{}
	for _, f := range before {
		old[f.Name] = f
	}
	var problems []string
	for _, f := range after {
		was, existed := old[f.Name]
		switch {
		case !existed && f.Revision != 1:
			problems = append(problems, fmt.Sprintf("%s is new at revision %d: a new feature starts at 1", f.Name, f.Revision))
		case existed && f.Revision < was.Revision:
			problems = append(problems, fmt.Sprintf("%s went from revision %d down to %d: a revision only goes up", f.Name, was.Revision, f.Revision))
		case existed && f.Revision == was.Revision && !slices.Equal(f.Routes, was.Routes):
			problems = append(problems, fmt.Sprintf("%s changed its routes and stayed at revision %d: raise it to %d in featureTable", f.Name, f.Revision, f.Revision+1))
		}
	}
	return problems
}

func TestRevisionProblems(t *testing.T) {
	one := []string{"GET /a"}
	two := []string{"GET /a", "GET /b"}
	before := []featureGolden{{Name: "x", Revision: 2, Routes: one}}
	for name, tc := range map[string]struct {
		after []featureGolden
		want  string
	}{
		"unchanged":                     {after: []featureGolden{{Name: "x", Revision: 2, Routes: one}}},
		"a route and a higher revision": {after: []featureGolden{{Name: "x", Revision: 3, Routes: two}}},
		"a higher revision alone":       {after: []featureGolden{{Name: "x", Revision: 3, Routes: one}}},
		"a new feature at 1":            {after: []featureGolden{{Name: "x", Revision: 2, Routes: one}, {Name: "y", Revision: 1, Routes: one}}},
		"a route under the same number": {after: []featureGolden{{Name: "x", Revision: 2, Routes: two}}, want: "raise it to 3"},
		"a route gone, same number":     {after: []featureGolden{{Name: "x", Revision: 2, Routes: []string{}}}, want: "raise it to 3"},
		"a revision down":               {after: []featureGolden{{Name: "x", Revision: 1, Routes: two}}, want: "only goes up"},
		"a new feature past 1":          {after: []featureGolden{{Name: "y", Revision: 2, Routes: one}}, want: "starts at 1"},
	} {
		got := strings.Join(revisionProblems(before, tc.after), "\n")
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: problems = %q, want %q", name, got, tc.want)
		}
	}
}

// The golden is what holds "a number that only goes up": the table alone
// cannot say what a feature's routes and revision were before a change. It is
// written by `-update` and never by hand, and `-update` refuses a table that
// revisionProblems refuses. TestFeatureRevisionsAgainstTheBase holds the same
// rule against the base branch.
func TestFeaturesMatchTheGolden(t *testing.T) {
	encoded, err := json.MarshalIndent(goldenFeatures(), "", "  ")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	encoded = append(encoded, '\n')

	want, err := os.ReadFile(featuresGoldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if *updateGolden {
		var before []featureGolden
		if len(want) > 0 {
			if err := json.Unmarshal(want, &before); err != nil {
				t.Fatalf("decode %s: %v", featuresGoldenPath, err)
			}
		}
		if problems := revisionProblems(before, goldenFeatures()); len(problems) > 0 {
			t.Fatalf("%s is not rewritten:\n  %s", featuresGoldenPath, strings.Join(problems, "\n  "))
		}
		if err := os.MkdirAll(filepath.Dir(featuresGoldenPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(featuresGoldenPath, encoded, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", featuresGoldenPath)
		return
	}

	if string(want) != string(encoded) {
		t.Errorf("featureTable has moved and %s is stale.\n\n"+
			"A feature that gained or lost a route raises its revision in featureTable\n"+
			"(internal/api/features.go). Then regenerate the golden:\n"+
			"  go test ./internal/api/ -run TestFeaturesMatchTheGolden -update\n\ngot:\n%s\nwant:\n%s",
			featuresGoldenPath, encoded, want)
	}
}

// The same rule against the BASE branch's golden, which `mise run
// features:check` hands over: the golden in the tree is one a branch may
// rewrite, so two branches that each raise one feature by one would merge
// into a feature that gained twice under one number.
func TestFeatureRevisionsAgainstTheBase(t *testing.T) {
	path := os.Getenv("FEATURES_BASE_GOLDEN")
	if path == "" {
		t.Skip("FEATURES_BASE_GOLDEN is unset: `mise run features:check` sets it to the merge base's golden")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the base golden: %v", err)
	}
	var base []featureGolden
	if err := json.Unmarshal(raw, &base); err != nil {
		t.Fatalf("decode the base golden: %v", err)
	}
	if problems := revisionProblems(base, goldenFeatures()); len(problems) > 0 {
		t.Fatalf("against the base branch:\n  %s", strings.Join(problems, "\n  "))
	}
}

// The served list is the table: every listed entry, in the table's order,
// under the wire's own key names, with `operatorDefaults` only where a
// defaults file is loaded.
func TestDiscoveryServesTheFeatureTable(t *testing.T) {
	for _, loaded := range []bool{false, true} {
		h := New(Config{Service: &stubService{}, OperatorDefaults: loaded})
		rec := serve(h, http.MethodGet, "/.well-known/substrate/server.json", nil)
		wantStatus(t, rec, http.StatusOK)
		var doc struct {
			Features []struct {
				Name      string   `json:"name"`
				Stability string   `json:"stability"`
				Revision  int      `json:"revision"`
				Surfaces  []string `json:"surfaces"`
				Notice    string   `json:"notice"`
			} `json:"features"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}

		var want []feature
		for _, f := range featureTable {
			if f.name == "operatorDefaults" && !loaded {
				continue
			}
			want = append(want, f)
		}
		if len(doc.Features) != len(want) {
			t.Fatalf("defaults file %v: %d features served, want the table's %d", loaded, len(doc.Features), len(want))
		}
		for i, f := range want {
			got := doc.Features[i]
			if got.Name != f.name || got.Stability != f.stability || got.Revision != f.revision ||
				!slices.Equal(got.Surfaces, f.surfaces) || got.Notice != f.notice {
				t.Errorf("defaults file %v: feature %d = %+v, want the table's %+v", loaded, i, got, f.info())
			}
		}
	}
}

// An entry's keys, read as bytes: a renamed key would still decode into the
// Go struct. `revision` is always present, and `notice` only where the entry
// has one.
func TestDiscoveryFeatureEntryKeys(t *testing.T) {
	rec := serve(New(Config{Service: &stubService{}}), http.MethodGet, "/.well-known/substrate/server.json", nil)
	wantStatus(t, rec, http.StatusOK)
	var doc struct {
		Features []map[string]json.RawMessage `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	notices := map[string]bool{}
	for _, f := range featureTable {
		notices[f.name] = f.notice != ""
	}
	for _, entry := range doc.Features {
		var name string
		if err := json.Unmarshal(entry["name"], &name); err != nil {
			t.Fatalf("an entry's name: %v", err)
		}
		want := []string{"name", "revision", "stability", "surfaces"}
		if notices[name] {
			want = []string{"name", "notice", "revision", "stability", "surfaces"}
		}
		got := make([]string, 0, len(entry))
		for key := range entry {
			got = append(got, key)
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("feature %q keys = %v, want %v", name, got, want)
		}
	}

	// The notice's own bytes, apart from the table, which may carry none.
	for want, f := range map[string]feature{
		`{"name":"x","stability":"beta","revision":2,"surfaces":["rest"]}`:                 {name: "x", stability: substrate.StabilityBeta, revision: 2, surfaces: []string{surfaceREST}},
		`{"name":"x","stability":"beta","revision":2,"surfaces":["rest"],"notice":"soon"}`: {name: "x", stability: substrate.StabilityBeta, revision: 2, surfaces: []string{surfaceREST}, notice: "soon"},
	} {
		got, err := json.Marshal(f.info())
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("entry = %s, want %s", got, want)
		}
	}
}

var featureDocRow = regexp.MustCompile("^\\| `([A-Za-z]+)` \\| `([a-z]+)` \\|")

// docs/api.md names every feature with its label, in the table under "The
// features". It is the table's names and labels, in the table's order, so a
// reader of the guide is told what the server says.
func TestDocsListTheFeatures(t *testing.T) {
	raw, err := os.ReadFile(apiDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", apiDocPath, err)
	}
	const heading = "### The features"
	var got []string
	in := false
	for line := range strings.Lines(string(raw)) {
		line = strings.TrimSpace(line)
		switch {
		case line == heading:
			in = true
		case in && strings.HasPrefix(line, "#"):
			in = false
		case in:
			if m := featureDocRow.FindStringSubmatch(line); m != nil {
				got = append(got, m[1]+" "+m[2])
			}
		}
	}
	var want []string
	for _, f := range featureTable {
		want = append(want, f.name+" "+f.stability)
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s, under %q, lists the features as\n  %v\nand featureTable (internal/api/features.go) has\n  %v\n"+
			"Make the table's rows the features and their labels, in that order.",
			apiDocPath, heading, got, want)
	}
}
