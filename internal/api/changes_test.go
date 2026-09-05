package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The cross-collection feed (the console's stream page): newest-first history
// under `before`/`first`, server-side `q`, and per-row function chips.

// seedChanges commits n puts so the fake changelog holds seqs 1..n.
func seedChanges(ds *fakeDataset, n int) {
	for i := range n {
		ds.commit(substrate.Change{
			TS: time.Unix(int64(i+1), 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
			RecordID: "e" + string(rune('1'+i)), Kind: "samples.substrate.reamde.dev/people/person",
		})
		<-ds.signals // keep the buffered signal channel drained
	}
}

type changesBody struct {
	Changes []struct {
		Seq      int64                     `json:"seq"`
		RecordID string                    `json:"recordId"`
		Triggers []substrate.ChangeTrigger `json:"triggers"`
	} `json:"changes"`
}

func TestChangesHistoryPagesNewestFirst(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	seedChanges(env.svc.datasets["geoah"], 5)

	rec := env.do(t, http.MethodGet, "/api/v1/changes?first=2", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q", got)
	}
	page := decodeJSON[changesBody](t, rec)
	if len(page.Changes) != 2 || page.Changes[0].Seq != 5 || page.Changes[1].Seq != 4 {
		t.Fatalf("first page = %+v, want seqs 5,4", page.Changes)
	}

	// The next page starts strictly below the oldest row already shown.
	rec = env.do(t, http.MethodGet, "/api/v1/changes?first=2&before=4", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	page = decodeJSON[changesBody](t, rec)
	if len(page.Changes) != 2 || page.Changes[0].Seq != 3 || page.Changes[1].Seq != 2 {
		t.Fatalf("second page = %+v, want seqs 3,2", page.Changes)
	}
}

func TestChangesHistoryCarriesTriggerStates(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
	seedChanges(ds, 2)
	ds.trStates[1] = []substrate.ChangeTrigger{
		{Trigger: "on-mirror.widgets.test.dev", Callable: "widgets.test.dev/widgets/mirror", State: substrate.ChangeTriggerParked, Error: "boom"},
	}
	ds.trStates[2] = []substrate.ChangeTrigger{
		{Trigger: "on-mirror.widgets.test.dev", Callable: "widgets.test.dev/widgets/mirror", State: substrate.ChangeTriggerPending},
	}

	rec := env.do(t, http.MethodGet, "/api/v1/changes?first=10", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[changesBody](t, rec)
	if len(page.Changes) != 2 {
		t.Fatalf("changes = %+v", page.Changes)
	}
	if trs := page.Changes[0].Triggers; len(trs) != 1 || trs[0].State != substrate.ChangeTriggerPending {
		t.Fatalf("newest row triggers = %+v", trs)
	}
	if trs := page.Changes[1].Triggers; len(trs) != 1 || trs[0].State != substrate.ChangeTriggerParked || trs[0].Error != "boom" {
		t.Fatalf("parked row triggers = %+v", trs)
	}
}

func TestChangesQFiltersHistory(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
	ds.commit(substrate.Change{
		TS: time.Unix(1, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
		RecordID: "alpha1", Kind: "samples.substrate.reamde.dev/people/person",
		Payload: map[string]any{"properties": map[string]any{"name": "Ada"}},
	})
	ds.commit(substrate.Change{
		TS: time.Unix(2, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
		RecordID: "beta1", Kind: "samples.substrate.reamde.dev/tasks/task",
	})

	// Case-insensitive, and payload text counts as haystack.
	rec := env.do(t, http.MethodGet, "/api/v1/changes?first=10&q=ADA", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[changesBody](t, rec)
	if len(page.Changes) != 1 || page.Changes[0].RecordID != "alpha1" {
		t.Fatalf("q=ADA page = %+v", page.Changes)
	}
}

func TestChangesWatchAppliesQ(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	br, stop := startWatch(t, srv, "/api/v1/changes?watch=1&from=0&q=task", tok)
	defer stop()
	if _, ok := readLine(t, br)["bookmark"]; !ok {
		t.Fatal("no bookmark line")
	}

	ds.commit(substrate.Change{
		TS: time.Unix(1, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
		RecordID: "p1", Kind: "samples.substrate.reamde.dev/people/person",
	})
	ds.commit(substrate.Change{
		TS: time.Unix(2, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
		RecordID: "t1", Kind: "samples.substrate.reamde.dev/tasks/task",
	})

	if got := readLine(t, br); got["recordId"] != "t1" {
		t.Fatalf("watch line = %v, want the task change only", got)
	}
}

func TestChangesWatchRowsCarryTriggers(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	br, stop := startWatch(t, srv, "/api/v1/changes?watch=1&from=0", tok)
	defer stop()
	if _, ok := readLine(t, br)["bookmark"]; !ok {
		t.Fatal("no bookmark line")
	}

	ds.trStates[1] = []substrate.ChangeTrigger{
		{Trigger: "on-mirror.widgets.test.dev", Callable: "widgets.test.dev/widgets/mirror", State: substrate.ChangeTriggerPending},
	}
	ds.commit(substrate.Change{
		TS: time.Unix(1, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
		RecordID: "p1", Kind: "samples.substrate.reamde.dev/people/person",
	})

	line := readLine(t, br)
	trs, ok := line["triggers"].([]any)
	if !ok || len(trs) != 1 {
		t.Fatalf("watch row = %v, want one trigger chip", line)
	}
	if chip := trs[0].(map[string]any); chip["state"] != substrate.ChangeTriggerPending {
		t.Fatalf("chip = %v", chip)
	}
}

func TestChangesBadPagingParams(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	for _, path := range []string{
		"/api/v1/changes?before=nope",
		"/api/v1/changes?first=0",
		"/api/v1/changes?first=x",
	} {
		rec := env.do(t, http.MethodGet, path, tok, nil)
		wantStatus(t, rec, http.StatusBadRequest)
	}
}
