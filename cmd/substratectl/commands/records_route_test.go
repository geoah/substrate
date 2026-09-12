package commands

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// A list is the records route with the kind INSIDE the filter, and a filter
// the user wrote rides beside it: --filter is decoded, given its kinds and
// re-encoded as one document, so the server reads one grammar, not a kind in
// the path and a predicate in the query.
func TestGetListNamesTheKindInsideTheFilter(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	// -o yaml, so no registry read follows the list and lastQuery is the
	// list's own.
	h.mustRun("get", "samples.substrate.reamde.dev/tasks/task", "-o", "yaml",
		"--filter", `{"properties":{"lifecycle":{"eq":"open"}}}`, "-l", "owner/pinned=true", "--limit", "5")
	if got := h.lastRequest(); got != listOf(taskKind) {
		t.Fatalf("last request = %q, want %q", got, listOf(taskKind))
	}
	var f substrate.Filter
	if err := json.Unmarshal([]byte(h.fake.lastQuery.Get("filter")), &f); err != nil {
		t.Fatalf("filter is not JSON: %v", err)
	}
	if len(f.Kinds) != 1 || f.Kinds[0] != taskKind {
		t.Errorf("filter.kinds = %v, want [%s]", f.Kinds, taskKind)
	}
	if f.Properties["lifecycle"].Eq != "open" || f.Labels["owner/pinned"].Eq != true {
		t.Errorf("the user's filter was lost when the kind was set: %+v", f)
	}
	if h.fake.lastQuery.Get("first") != "5" {
		t.Errorf("first = %q", h.fake.lastQuery.Get("first"))
	}
}

// A user-supplied kinds list cannot widen `get <kind>`: the command promised
// one kind, so the kind it resolved REPLACES whatever --filter carried.
func TestSetFilterKindsReplacesTheUsersKinds(t *testing.T) {
	q := url.Values{"filter": []string{`{"kinds":["a.example.com/x/y"],"ids":["i1"]}`}}
	if err := setFilterKinds(q, taskKind); err != nil {
		t.Fatal(err)
	}
	var f substrate.Filter
	if err := json.Unmarshal([]byte(q.Get("filter")), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Kinds) != 1 || f.Kinds[0] != taskKind || len(f.IDs) != 1 {
		t.Fatalf("filter = %+v", f)
	}
	if err := setFilterKinds(url.Values{"filter": []string{`{nope`}}, taskKind); err == nil {
		t.Fatal("a --filter that is not JSON must be refused before it is sent")
	}
}

// `get -w` is the records route in its watch mode: watch=1, the kind in
// filter.kinds, and the resume cursor the way `watch` spells it. The fake
// refuses any list parameter beside watch=1, as the server does, so the
// stream arriving is the proof the CLI sent only the tail's grammar.
func TestGetWatchSendsTheTailGrammar(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.changes = []substrate.Change{
		{Seq: 42, TS: testNow, Actor: substrate.ActorAPI, Op: substrate.OpPut, RecordID: "t9", Kind: taskKind},
	}
	out, _ := h.mustRun("get", "samples.substrate.reamde.dev/tasks/task", "-w", "--from", "7", "--generation", fakeGeneration)
	if !strings.Contains(out, "42 ") {
		t.Fatalf("get -w did not stream the change:\n%s", out)
	}
	q := h.fake.lastQuery
	if q.Get("watch") != "1" || q.Get("from") != "7" || q.Get("generation") != fakeGeneration {
		t.Fatalf("watch query = %v", q)
	}
	var f substrate.Filter
	if err := json.Unmarshal([]byte(q.Get("filter")), &f); err != nil || len(f.Kinds) != 1 || f.Kinds[0] != taskKind {
		t.Fatalf("filter = %q (%v), want kinds [%s]", q.Get("filter"), err, taskKind)
	}
	// A cursor from another history is refused with the head to resume from,
	// exactly as the changelog watch is.
	_, _, err := h.run("get", "samples.substrate.reamde.dev/tasks/task", "-w", "--from", "7", "--generation", "gen-old")
	if err == nil || !strings.Contains(err.Error(), "compacted") {
		t.Fatalf("a stale generation should be refused as compacted, got %v", err)
	}
}

// seedProject seeds a task pointing at another record through a reference
// property, and the record it points at, so --expand has a referent to carry
// and --referencing has a pointer to find.
func seedProject(h *harness) {
	h.fake.seed(&substrate.Record{
		ID: "p1", Kind: taskKind,
		Properties: map[string]any{"title": "Rack project", "lifecycle": "open"},
		Version:    1, CreatedAt: testNow.Add(-72 * time.Hour), UpdatedAt: testNow.Add(-72 * time.Hour),
	})
	h.fake.seed(&substrate.Record{
		ID: "t1", Kind: taskKind,
		Properties: map[string]any{
			"title": "Label the racks", "lifecycle": "open",
			"project": map[string]any{"ref": taskKind + "/p1"},
		},
		Version: 1, CreatedAt: testNow.Add(-time.Hour), UpdatedAt: testNow.Add(-time.Hour),
	})
}

// --expand asks the page to carry its referents, one hop. The table prints
// the page alone; -o yaml appends the referents as further documents, and -o
// json turns the array into {records, included} keyed by record path.
func TestGetExpandPrintsIncludedOnlyInYAMLAndJSON(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedProject(h)

	out, _ := h.mustRun("get", "samples.substrate.reamde.dev/tasks/task", "--expand", "project")
	if lines := strings.Split(strings.TrimRight(out, "\n"), "\n"); len(lines) != 3 {
		t.Fatalf("the table must print the page alone (header + 2 rows):\n%s", out)
	}

	out, _ = h.mustRun("get", "samples.substrate.reamde.dev/tasks/task", "--expand", "project", "-o", "yaml")
	if h.fake.lastQuery.Get("expand") != "project" {
		t.Fatalf("expand = %q", h.fake.lastQuery.Get("expand"))
	}
	docs := strings.Split(out, "\n---\n")
	if len(docs) != 3 {
		t.Fatalf("yaml should be the 2 page documents then the 1 referent, got %d:\n%s", len(docs), out)
	}
	if !strings.Contains(docs[2], "id: p1") {
		t.Fatalf("the referent must follow the page:\n%s", docs[2])
	}

	out, _ = h.mustRun("get", "samples.substrate.reamde.dev/tasks/task", "--expand", "project", "-o", "json")
	var body struct {
		Records  []json.RawMessage          `json:"records"`
		Included map[string]json.RawMessage `json:"included"`
	}
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		t.Fatalf("json with --expand should be an object: %v\n%s", err, out)
	}
	if len(body.Records) != 2 || len(body.Included) != 1 {
		t.Fatalf("records = %d, included = %d", len(body.Records), len(body.Included))
	}
	if _, ok := body.Included[taskKind+"/p1"]; !ok {
		t.Fatalf("included is keyed by record path, got %v", body.Included)
	}

	// Without --expand the JSON is the array it always was, even though the
	// same records are on the page.
	out, _ = h.mustRun("get", "samples.substrate.reamde.dev/tasks/task", "-o", "json")
	if !strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Fatalf("json without --expand must stay an array:\n%s", out)
	}
}

// --referencing is the reverse read as a predicate: the list carries
// filter.referencing and only the records pointing at the target come back.
func TestGetReferencingIsAFilterArm(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedProject(h)
	out, _ := h.mustRun("get", "samples.substrate.reamde.dev/tasks/task", "--referencing", taskKind+"/p1", "-o", "wide")
	// The wide table asks the registry for its STATE column after the list,
	// which overwrites lastQuery; the yaml read makes no such request, so its
	// query is the one asserted.
	h.mustRun("get", "samples.substrate.reamde.dev/tasks/task", "--referencing", taskKind+"/p1", "-o", "yaml")
	var f substrate.Filter
	if err := json.Unmarshal([]byte(h.fake.lastQuery.Get("filter")), &f); err != nil {
		t.Fatal(err)
	}
	if f.Referencing == nil || f.Referencing.Ref != taskKind+"/p1" || len(f.Kinds) != 1 {
		t.Fatalf("filter = %+v", f)
	}
	if !strings.Contains(out, "t1 ") || strings.Contains(out, "p1 ") {
		t.Fatalf("only the pointing record should be listed:\n%s", out)
	}
	if _, _, err := h.run("get", "samples.substrate.reamde.dev/tasks/task", "--referencing", "p1"); err == nil {
		t.Fatal("a bare id is not a record path and must be refused before it is sent")
	}
}

// search is the ranked read: q on the records route, the kinds resolved the
// way get resolves them and named in filter.kinds, and the hits printed with
// their per-arm scores.
func TestSearchRanksThroughTheRecordsRoute(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	seedProject(h)

	out, _ := h.mustRun("search", "rack", "--kinds", "task", "--limit", "10")
	q := h.fake.lastQuery
	if q.Get("q") != "rack" || q.Get("first") != "10" || q.Has("mode") {
		t.Fatalf("search query = %v", q)
	}
	var f substrate.Filter
	if err := json.Unmarshal([]byte(q.Get("filter")), &f); err != nil || len(f.Kinds) != 1 || f.Kinds[0] != taskKind {
		t.Fatalf("filter = %q (%v): a bare --kinds name resolves to its reference", q.Get("filter"), err)
	}
	want := "" +
		"KIND                                      ID   TITLE                      LEXICAL   SEMANTIC\n" +
		"samples.substrate.reamde.dev/tasks/task   p1   Rack project               0.500     0.900\n" +
		"samples.substrate.reamde.dev/tasks/task   t1   Label the racks            0.500     0.900\n" +
		"samples.substrate.reamde.dev/tasks/task   t9   Send rack layout to Alex   0.500     0.900\n"
	if out != want {
		t.Fatalf("search table:\n%s\nwant:\n%s", out, want)
	}

	// A lexical ranking has no semantic score, and the column says so.
	out, _ = h.mustRun("search", "alex", "--mode", "lexical")
	if h.fake.lastQuery.Get("mode") != "lexical" || h.fake.lastQuery.Has("filter") {
		t.Fatalf("query = %v: no --kinds means no filter at all", h.fake.lastQuery)
	}
	if !strings.Contains(out, "0.500     -") {
		t.Fatalf("lexical table:\n%s", out)
	}
	if _, _, err := h.run("search", "alex", "--mode", "fuzzy"); err == nil {
		t.Fatal("an unknown mode must be refused before it is sent")
	}

	// -o json is the ranked page with the records as manifests.
	out, _ = h.mustRun("search", "alex", "-o", "json")
	var body struct {
		Records []struct {
			Kind     string `json:"kind"`
			Metadata struct {
				ID string `json:"id"`
			} `json:"metadata"`
		} `json:"records"`
		Scores  map[string]substrate.Scores `json:"scores"`
		Pending int                         `json:"pending"`
	}
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		t.Fatalf("search -o json: %v\n%s", err, out)
	}
	if len(body.Records) != 1 || body.Records[0].Metadata.ID != "t9" || body.Records[0].Kind != taskKind {
		t.Fatalf("records = %+v", body.Records)
	}
	if body.Scores[taskKind+"/t9"].Lexical != 0.5 {
		t.Fatalf("scores = %+v", body.Scores)
	}
}

// The id-less create carries its kind in the body and nothing in the URL:
// the records route is the one write not addressed by (kind, id), so the body
// says where the record lands and the server assigns the id.
func TestPutWithoutAnIDPostsTheKindInTheBody(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	cl := newClient(h.server, "substrate_tok_geoah_test", nil)
	e, err := cl.put(t.Context(), "samples.substrate.reamde.dev/tasks", "task", "", substrate.PutInput{
		ID:         "must-be-dropped",
		Properties: map[string]any{"title": "no id"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.lastRequest() != "POST "+pathRecords {
		t.Fatalf("request = %q", h.lastRequest())
	}
	var kind string
	if err := json.Unmarshal(h.fake.lastBody["kind"], &kind); err != nil || kind != taskKind {
		t.Fatalf("body kind = %q (%v)", kind, err)
	}
	if _, has := h.fake.lastBody["id"]; has {
		t.Fatalf("the body must not carry an id: %s", h.fake.lastBody["id"])
	}
	if e.ID == "" || e.Kind != taskKind {
		t.Fatalf("created = %+v", e)
	}
}

// The registry read is a list like any other: the meta-kind in filter.kinds,
// the page walked whole through `first` and `after`.
func TestRegistryReadsThroughTheRecordsRoute(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.mustRun("kinds")
	if got := h.lastRequest(); got != "GET "+typesPath {
		t.Fatalf("request = %q, want %q", got, "GET "+typesPath)
	}
	if h.fake.lastQuery.Get("first") == "" {
		t.Fatalf("the registry walk names its page size: %v", h.fake.lastQuery)
	}
}
