package synth

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// recordingCases are request shapes the emitters write recordings for, with
// the names the Python rule (internal/providere2e/runner/mockserver.py,
// recording_name) gave them on 2026-10-12.
var recordingCases = []struct {
	method, path string
	pairs        []Pair
	want         string
}{
	{"GET", "/v2/cycle", nil, "GET_v2_cycle.json"},
	{
		"GET", "/v2/cycle",
		[]Pair{{"limit", "25"}, {"start", "2026-07-14T00:00:00Z"}, {"nextToken", "MjAyNjA5MTA2MjM"}},
		"GET_v2_cycle__limit-25_nextToken-MjAyNjA5MTA2MjM_start-2026-07-14T00_00_00Z.json",
	},
	{
		"GET", "/fitness/v1/users/me/dataSources/derived:com.google.heart_rate.bpm:com.google.android.gms:merge_heart_rate_bpm/datasets/1791344581754000000-1791430981754000000",
		[]Pair{{"limit", "1000"}},
		"GET_fitness_v1_users_me_dataSources_derived_com.google.heart_rate.bpm_com.google.android.gms_merge_heart_rate_bpm_datasets_1791344581754000000-1791430981754000000__limit-1000.json",
	},
	{
		"GET", "/fitness/v1/users/me/sessions",
		[]Pair{{"includeDeleted", "true"}, {"pageToken", "1790405742001"}},
		"GET_fitness_v1_users_me_sessions__includeDeleted-true_pageToken-1790405742001.json",
	},
	{
		"GET", "/wellness-api/rest/backfill/dailies",
		[]Pair{{"summaryStartTimeInSeconds", "1789074000"}, {"summaryEndTimeInSeconds", "1789160400"}},
		"GET_wellness-api_rest_backfill_dailies__summaryEndTimeInSeconds-1789160400_summaryStartTimeInSeconds-1789074000.json",
	},
	{"GET", "/", nil, "GET_root.json"},
	{"GET", "/oauth2/v3/userinfo", []Pair{{"access_token", "secret"}, {"alt", "json"}}, "GET_oauth2_v3_userinfo__alt-json.json"},
	{
		"GET", "/calendar/v3/calendars/primary/events",
		[]Pair{{"maxResults", "250"}, {"singleEvents", "true"}, {"q", strings.Repeat("x", 100)}},
		"GET_calendar_v3_calendars_primary_events__maxResults-250_q-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx-ec99ff1f.json",
	},
	{"post", "/conversations.history", nil, "POST_conversations.history.json"},
}

// The Go rule names each case as the Python rule did when the cases were
// written.
func TestRecordingNameMatchesTheMockServer(t *testing.T) {
	t.Parallel()
	for _, c := range recordingCases {
		if got := RecordingName(c.method, c.path, c.pairs...); got != c.want {
			t.Errorf("%s %s %v:\n got %s\nwant %s", c.method, c.path, c.pairs, got, c.want)
		}
	}
}

// mockServerNames runs the mock server's own naming over JSON cases on
// stdin: for each, the name recording_name gives and the names the lookup
// tries, in order (candidate_names).
const mockServerNames = `
import json, sys
sys.path.insert(0, sys.argv[1])
import mockserver
out = []
for c in json.load(sys.stdin):
    pairs = [tuple(p) for p in c["pairs"]]
    out.append({"name": mockserver.recording_name(c["method"], c["path"], pairs),
                "tries": mockserver.candidate_names(c["method"], c["path"], pairs)})
print(json.dumps(out))
`

// The emitters write recordings under the Go rule and the mock server finds
// them under its own, so the two are held to each other by running the
// Python: the name Go writes for a request is the mock's recording_name and
// its exact try, and the name Go writes without the pagination pairs (what
// the WHOOP and Garmin emitters write) is one the mock tries too. A drift in
// either rule, or in the helpers they share, fails here.
func TestRecordingNameIsTheOneTheMockServerTries(t *testing.T) {
	t.Parallel()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH")
	}
	type wire struct {
		Method string      `json:"method"`
		Path   string      `json:"path"`
		Pairs  [][2]string `json:"pairs"`
	}
	in := make([]wire, 0, len(recordingCases))
	for _, c := range recordingCases {
		w := wire{Method: c.method, Path: c.path, Pairs: [][2]string{}}
		for _, p := range c.pairs {
			w.Pairs = append(w.Pairs, [2]string{p.Key, p.Value})
		}
		in = append(in, w)
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "-I", "-B", "-c", mockServerNames, filepath.Join("..", "providere2e", "runner")) //nolint:gosec // the test's own script over the tree's file
	cmd.Stdin = bytes.NewReader(raw)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the mock server's naming: %v\n%s", err, stderr.String())
	}
	var got []struct {
		Name  string   `json:"name"`
		Tries []string `json:"tries"`
	}
	if err := json.Unmarshal(out, &got); err != nil || len(got) != len(recordingCases) {
		t.Fatalf("the probe answered %d names (%v): %s", len(got), err, out)
	}
	for i, c := range recordingCases {
		name := RecordingName(c.method, c.path, c.pairs...)
		if got[i].Name != name || len(got[i].Tries) == 0 || got[i].Tries[0] != name {
			t.Errorf("%s %s %v: Go names it %s, the mock %s and tries %v first", c.method, c.path, c.pairs, name, got[i].Name, got[i].Tries)
		}
		var thin []Pair
		for _, p := range c.pairs {
			if !PaginationParams[p.Key] {
				thin = append(thin, p)
			}
		}
		if paged := RecordingName(c.method, c.path, thin...); !slices.Contains(got[i].Tries, paged) {
			t.Errorf("%s %s %v: the page-relaxed name %s is not one the mock tries: %v", c.method, c.path, c.pairs, paged, got[i].Tries)
		}
	}
}

// mockServerSet reads one set literal (`NAME = {"a", "b", ...}`) from
// mockserver.py, so the Go copies are held to the file the server runs.
func mockServerSet(t *testing.T, name string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "providere2e", "runner", "mockserver.py"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?ms)^` + name + ` = \{(.*?)\}`).FindSubmatch(raw)
	if m == nil {
		t.Fatalf("mockserver.py declares no %s set", name)
	}
	out := map[string]bool{}
	for _, q := range regexp.MustCompile(`"([^"]+)"`).FindAllSubmatch(m[1], -1) {
		out[string(q[1])] = true
	}
	return out
}

func TestParamSetsMatchTheMockServer(t *testing.T) {
	t.Parallel()
	for name, goSet := range map[string]map[string]bool{"PAGINATION_PARAMS": PaginationParams, "SECRET_PARAMS": SecretParams} {
		if py := mockServerSet(t, name); !maps.Equal(py, goSet) {
			t.Errorf("%s differs from the mock server's:\n go %v\n py %v", name, slices.Sorted(maps.Keys(goSet)), slices.Sorted(maps.Keys(py)))
		}
	}
	// The mock's second try drops every pagination pair, so WHOOP's page-two
	// recording is found only because nextToken is not one.
	if PaginationParams["nextToken"] {
		t.Fatal("nextToken is a pagination parameter: the WHOOP pages would collapse onto page one")
	}
}
