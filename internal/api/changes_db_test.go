package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The cross-collection feed on the real changelog: newest-first history
// under `before`/`first`, server-side `q`, the opt-in values, and the per-row
// chips of a trigger the repository holds.

// changesHead reads where the repository's changelog stands.
func changesHead(t *testing.T, env *dbEnv) substrate.ChangelogHead {
	t.Helper()
	head, err := env.ds.Head(context.Background())
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	return head
}

// changesTrigger is a function trigger the repository holds: every write of
// its widget kind is delivered to a Python body that raises "boom", so a
// processed delivery parks and an unprocessed one is pending.
type changesTrigger struct {
	widget  string
	trigger string
}

// changesInstallTrigger declares the widget kind and the failing function
// under the repository's own authority and writes the trigger that binds
// them. Nothing processes triggers until the test calls ProcessTriggers.
func changesInstallTrigger(t *testing.T, env *dbEnv) changesTrigger {
	t.Helper()
	ctx := context.Background()
	pkg := env.repository + "/widgets"
	ct := changesTrigger{widget: pkg + "/widget", trigger: "on-" + pkg + "/fail"}
	docs := []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "widget"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
		vocabulary.FunctionManifest(pkg, "fail", map[string]any{
			"description": "raises on every delivery",
			"runtime":     vocabulary.RuntimePython,
			"source":      "def main(input, host):\n    raise Exception(\"boom\")\n",
		}),
	}
	if _, err := env.ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, docs); err != nil {
		t.Fatalf("declare the widget package: %v", err)
	}
	if _, err := env.ds.Put(ctx, substrate.ActorSystem, substrate.PutInput{
		Kind: corePackage + "/trigger", ID: ct.trigger, Properties: map[string]any{
			"enabled":  true,
			"source":   map[string]any{"record": map[string]any{"kinds": []any{ct.widget}}},
			"callable": vocabulary.RecordPath(corePackage+"/function", pkg+"/fail"),
		},
	}); err != nil {
		t.Fatalf("write the trigger: %v", err)
	}
	return ct
}

// putWidget writes one widget the way the owner does, through the API.
func (ct changesTrigger) putWidget(t *testing.T, env *dbEnv, name string) string {
	t.Helper()
	rec := env.do(t, http.MethodPost, recordsPath, env.token, map[string]any{
		"kind": ct.widget, "properties": map[string]any{"name": name},
	})
	wantStatus(t, rec, http.StatusCreated)
	return decodeJSON[substrate.Record](t, rec).ID
}

func TestChangesHistoryPagesNewestFirst(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	seedChanges(env.ds, 5)
	head := changesHead(t, env)

	rec := env.do(t, http.MethodGet, "/api/v1/changes?first=2", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q", got)
	}
	page := decodeJSON[changesBody](t, rec)
	if len(page.Changes) != 2 || page.Changes[0].Seq != head.Seq || page.Changes[1].Seq != head.Seq-1 ||
		page.Changes[0].RecordID != "e5" || page.Changes[1].RecordID != "e4" {
		t.Fatalf("first page = %+v, want e5 and e4 at seqs %d,%d", page.Changes, head.Seq, head.Seq-1)
	}

	// The next page starts strictly below the oldest row already shown.
	rec = env.do(t, http.MethodGet, fmt.Sprintf("/api/v1/changes?first=2&before=%d&generation=%s", head.Seq-1, head.Generation), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page = decodeJSON[changesBody](t, rec)
	if len(page.Changes) != 2 || page.Changes[0].Seq != head.Seq-2 || page.Changes[1].Seq != head.Seq-3 ||
		page.Changes[0].RecordID != "e3" || page.Changes[1].RecordID != "e2" {
		t.Fatalf("second page = %+v, want e3 and e2 at seqs %d,%d", page.Changes, head.Seq-2, head.Seq-3)
	}
}

// A row a trigger has not reached is pending, and one whose delivery failed
// past its retries is parked with the failure beside it.
func TestChangesHistoryCarriesTriggerStates(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	ct := changesInstallTrigger(t, env)
	parked := ct.putWidget(t, env, "poisoned")
	if _, err := env.ds.ProcessTriggers(context.Background()); err != nil {
		t.Fatalf("process triggers: %v", err)
	}
	pending := ct.putWidget(t, env, "later")

	rec := env.do(t, http.MethodGet, "/api/v1/changes?first=10&kinds="+ct.widget, env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[changesBody](t, rec)
	if len(page.Changes) != 2 || page.Changes[0].RecordID != pending || page.Changes[1].RecordID != parked {
		t.Fatalf("changes = %+v, want the pending widget over the parked one", page.Changes)
	}
	if trs := page.Changes[0].Triggers; len(trs) != 1 || trs[0].Trigger != ct.trigger || trs[0].State != substrate.ChangeTriggerPending {
		t.Fatalf("newest row triggers = %+v", trs)
	}
	if trs := page.Changes[1].Triggers; len(trs) != 1 || trs[0].State != substrate.ChangeTriggerParked || !strings.Contains(trs[0].Error, "boom") {
		t.Fatalf("parked row triggers = %+v, want the parked chip carrying the body's error", trs)
	}
}

func TestChangesQFiltersHistory(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	env.put(t, personKind, "alpha1", map[string]any{"name": "Ada Zwicky"})
	env.put(t, "samples.substrate.reamde.dev/tasks/task", "beta1", map[string]any{"name": "Call the plumber"})

	// Case-insensitive, and payload text counts as haystack: the name is in
	// the person's payload and nowhere else in the changelog.
	rec := env.do(t, http.MethodGet, "/api/v1/changes?first=10&q=ZWICKY", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[changesBody](t, rec)
	if len(page.Changes) != 1 || page.Changes[0].RecordID != "alpha1" {
		t.Fatalf("q=ZWICKY page = %+v", page.Changes)
	}
}

func TestChangesWatchAppliesQ(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	head := changesHead(t, env)

	br, stop := startWatch(t, srv, fmt.Sprintf("/api/v1/changes?watch=1&from=%d&generation=%s&q=task", head.Seq, head.Generation), env.token)
	defer stop()
	if _, ok := readLine(t, br)["bookmark"]; !ok {
		t.Fatal("no bookmark line")
	}

	env.put(t, personKind, "p1", map[string]any{"name": "Ada"})
	env.put(t, "samples.substrate.reamde.dev/tasks/task", "t1", map[string]any{"name": "Call back"})

	if got := readLine(t, br); got["recordId"] != "t1" {
		t.Fatalf("watch line = %v, want the task change only", got)
	}
}

func TestChangesWatchRowsCarryTriggers(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	ct := changesInstallTrigger(t, env)
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	head := changesHead(t, env)

	br, stop := startWatch(t, srv, fmt.Sprintf("/api/v1/changes?watch=1&from=%d&generation=%s", head.Seq, head.Generation), env.token)
	defer stop()
	if _, ok := readLine(t, br)["bookmark"]; !ok {
		t.Fatal("no bookmark line")
	}

	id := ct.putWidget(t, env, "fresh")

	line := readLine(t, br)
	trs, ok := line["triggers"].([]any)
	if line["recordId"] != id || !ok || len(trs) != 1 {
		t.Fatalf("watch row = %v, want the widget with one trigger chip", line)
	}
	if chip := trs[0].(map[string]any); chip["state"] != substrate.ChangeTriggerPending || chip["trigger"] != ct.trigger {
		t.Fatalf("chip = %v", chip)
	}
}

// A row carries each record's before and after values only when the read
// asks for them, and a secret reads redacted on both sides.
func TestChangesCarryValuesOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	pkg := env.repository + "/changes"
	item := pkg + "/item"
	if _, err := env.ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "item"}, map[string]any{
			"properties": map[string]any{
				"priority": map[string]any{"type": "string"},
				"notes":    map[string]any{"type": "string"},
				"apiKey":   map[string]any{"type": "secret"},
			},
		}),
	}); err != nil {
		t.Fatalf("declare the item kind: %v", err)
	}
	env.put(t, item, "t1", map[string]any{"priority": "high", "apiKey": "sk-first-12345"})
	if _, err := env.ds.Patch(context.Background(), substrate.ActorSystem, item, "t1", substrate.PatchInput{Properties: map[string]any{
		"priority": "urgent", "notes": "call back", "apiKey": "sk-second-67890",
	}}); err != nil {
		t.Fatalf("patch: %v", err)
	}

	type body struct {
		Changes []struct {
			Op       substrate.Op               `json:"op"`
			Affected []substrate.AffectedRecord `json:"affected"`
		} `json:"changes"`
	}
	newest := "/api/v1/changes?first=1&kinds=" + item
	for _, path := range []string{newest, newest + "&values=0"} {
		rec := env.do(t, http.MethodGet, path, env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		page := decodeJSON[body](t, rec)
		if len(page.Changes) != 1 || page.Changes[0].Op != substrate.OpPatch || page.Changes[0].Affected[0].Properties != nil {
			t.Fatalf("%s: values without asking: %+v", path, page.Changes)
		}
	}

	rec := env.do(t, http.MethodGet, newest+"&values=1", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), "sk-") {
		t.Fatalf("the values spell the secret: %s", rec.Body.String())
	}
	page := decodeJSON[body](t, rec)
	if len(page.Changes) != 1 {
		t.Fatalf("page = %+v", page.Changes)
	}
	got := map[string]substrate.PropertyChange{}
	for _, pc := range page.Changes[0].Affected[0].Properties {
		got[pc.Name] = pc
	}
	if len(got) != 3 || got["priority"].Before != "high" || got["priority"].After != "urgent" ||
		got["notes"].Before != nil || got["notes"].After != "call back" ||
		got["apiKey"].Before != "<redacted>" || got["apiKey"].After != "<redacted>" {
		t.Fatalf("values = %+v", page.Changes[0].Affected[0].Properties)
	}
}
