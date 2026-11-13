package commands

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// An integer property is an INTEGER on the way out. Every property map is
// `map[string]any`, so a plain decode turned each number into a float64 and a
// repository's `totpStep` printed as `5.9545831e+07` — not the value the substrate
// holds, not a value the document applies back, and outright lossy past 2^53.
func TestIntegerPropertiesRenderAsIntegers(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.seed(&substrate.Record{
		ID: "t9", Kind: "samples.substrate.reamde.dev/tasks/task",
		Properties: map[string]any{
			"title": "a task",
			// The repository row's counter that exposed this, and a value past
			// float64's exact-integer range, which no float round trip survives.
			"totpStep":  int64(59545831),
			"nanos":     int64(1786080000123456789),
			"ratio":     0.25,
			"nested":    map[string]any{"count": int64(42)},
			"histogram": []any{int64(1), int64(2)},
		},
		Version: 1, CreatedAt: testNow, UpdatedAt: testNow,
	})

	out, _ := h.mustRun("get", "samples.substrate.reamde.dev/tasks/task", "t9", "-o", "yaml")
	for _, want := range []string{
		"totpStep: 59545831",
		"nanos: 1786080000123456789",
		"count: 42",
		"ratio: 0.25",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("yaml is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "e+") {
		t.Errorf("yaml renders a number in float exponent form:\n%s", out)
	}
}

// The `-o json` document goes through the same rendering, so it carries the
// same integers.
func TestIntegerPropertiesRenderAsIntegersInJSON(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.seed(&substrate.Record{
		ID: "t9", Kind: "samples.substrate.reamde.dev/tasks/task",
		// A value past float64's exact-integer range: the float round trip
		// mangles it into 1.7860800001234568e+18, digits and all.
		Properties: map[string]any{"totpStep": int64(59545831), "nanos": int64(1786080000123456789)},
		Version:    1, CreatedAt: testNow, UpdatedAt: testNow,
	})
	out, _ := h.mustRun("get", "samples.substrate.reamde.dev/tasks/task", "t9", "-o", "json")
	for _, want := range []string{`"totpStep": 59545831`, `"nanos": 1786080000123456789`} {
		if !strings.Contains(out, want) {
			t.Fatalf("json = %s, want %s", out, want)
		}
	}
}

// A `guard` is a refusal, but which one depends on the path. Only a record
// write runs a state machine: schema/apply guards on STORED DATA and the bundle
// verbs on the BUNDLE'S STATE, so telling an operator to "check the state
// transition guards" there sends them hunting for something not in play.
func TestGuardHintsBranchOnWhatFailed(t *testing.T) {
	cases := []struct {
		name          string
		err           *apiError
		wantHeadline  string
		wantHint      string
		unwantedWords []string
	}{{
		name:          "schema apply",
		err:           &apiError{Status: 403, Code: "guard", Path: apiPrefix + "/vocabulary/apply", Method: "POST"},
		wantHeadline:  "stored data",
		wantHint:      "migrate or delete",
		unwantedWords: []string{"transition"},
	}, {
		name:          "bundle lifecycle",
		err:           &apiError{Status: 403, Code: "guard", Path: apiPrefix + "/" + corePackage + "/bundle/providers.substrate.reamde.dev/whoop", Method: "PATCH"},
		wantHeadline:  "bundle's state",
		wantHint:      "substratectl bundle status",
		unwantedWords: []string{"transition"},
	}, {
		name:         "record write keeps the transition advice",
		err:          &apiError{Status: 403, Code: "guard", Path: apiPrefix + "/samples.substrate.reamde.dev/tasks/task/t9", Method: "PATCH"},
		wantHeadline: "transition",
		wantHint:     "state transition guards",
	}, {
		// A token has FULL access to its repository — no scopes, no ACLs — so a
		// forbidden is never about the token's reach. It is the write itself
		// being refused on principle.
		name:          "forbidden is about the write, not the token's reach",
		err:           &apiError{Status: 403, Code: "forbidden", Path: apiPrefix + "/substrate.reamde.dev/core/tokens", Method: "POST"},
		wantHeadline:  "forbidden",
		wantHint:      "refused on principle",
		unwantedWords: []string{"transition", "scope"},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			renderError(&buf, tc.err)
			got := buf.String()
			if !strings.Contains(got, tc.wantHeadline) {
				t.Errorf("rendering is missing headline %q:\n%s", tc.wantHeadline, got)
			}
			if !strings.Contains(got, tc.wantHint) {
				t.Errorf("rendering is missing hint %q:\n%s", tc.wantHint, got)
			}
			for _, unwanted := range tc.unwantedWords {
				if strings.Contains(got, unwanted) {
					t.Errorf("rendering says %q, which is not what failed:\n%s", unwanted, got)
				}
			}
		})
	}
}

// A repository the server set aside at its start answers 503 with the reason
// `set_aside`: the rendering says so and who brings it back, not a bare
// status a person would retry. It promises nothing about the start: a keying
// or a conversion may have committed before the failure, and what holds is
// that the server writes nothing to it from then on.
func TestASetAsideRepositorySaysWhoBringsItBack(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{"Retry-After": []string{"300"}},
		Body: io.NopCloser(strings.NewReader(
			`{"error":{"code":"unavailable","reason":"set_aside","message":"substrate: the repository is set aside"}}`)),
	}
	ae := parseAPIError(resp, http.MethodGet, apiPrefix+"/records")
	if ae.Reason != substrate.ReasonSetAside {
		t.Fatalf("the reason did not arrive: %+v", ae)
	}
	var buf bytes.Buffer
	renderError(&buf, ae)
	for _, want := range []string{"the repository is set aside", "writes nothing to this one", "its operator repairs it and restarts the server"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("the rendering does not say %q:\n%s", want, buf.String())
		}
	}
	if strings.Contains(buf.String(), "has written nothing") {
		t.Errorf("the rendering promises the start wrote nothing:\n%s", buf.String())
	}
}

// `retention` lists what each kind declares, the repository's override and
// the one that applies, a dash where a column has nothing, and narrows to one
// kind by its reference.
func TestRetentionShowsDeclaredOverrideAndEffective(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	out, _ := h.mustRun("retention")
	for _, want := range []string{
		"KIND", "DECLARED", "OVERRIDE", "EFFECTIVE",
		"providers.substrate.reamde.dev/google/message", "P30D",
		"samples.substrate.reamde.dev/tasks/task", "P90D",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the table does not show %q:\n%s", want, out)
		}
	}
	one, _ := h.mustRun("retention", "samples.substrate.reamde.dev/tasks/task")
	if strings.Contains(one, "google") || !strings.Contains(one, "P90D") {
		t.Errorf("the narrowed table:\n%s", one)
	}
	if _, _, err := h.run("retention", "nope.example.com/p/k"); err == nil || !strings.Contains(err.Error(), "substratectl kinds") {
		t.Errorf("an unknown kind: %v", err)
	}
}
