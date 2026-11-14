package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const changeFilterTask = "samples.substrate.reamde.dev/tasks/task"

func TestChangesHonorsRepeatedTypeParams(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	const project = "samples.substrate.reamde.dev/tasks/project"
	env.put(t, personKind, "e1", map[string]any{"name": "Ada"})
	env.put(t, project, "e2", map[string]any{"name": "Launch"})
	env.put(t, changeFilterTask, "e3", map[string]any{"name": "Call back"})

	rec := env.do(t, http.MethodGet, "/api/v1/changes"+
		"?kinds="+personKind+"&kinds="+project, env.token, nil)
	wantStatus(t, rec, http.StatusOK)

	lines := 0
	sc := bufio.NewScanner(rec.Body)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			lines++
		}
	}
	if lines != 3 {
		t.Fatalf("got %d ndjson lines, want bookmark + 2 changes (both repeated types honored)", lines)
	}
}

// D2 end-to-end: recordId+recordKind scopes the feed to the one record of
// that type, not every id-sharing row.
func TestChangesRecordIdPlusKindScopesToOneRecord(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	// The same id under two types — exactly the A9 collision.
	env.put(t, personKind, "shared", map[string]any{"name": "Ada"})
	env.put(t, changeFilterTask, "shared", map[string]any{"name": "Call back"})

	rec := env.do(t, http.MethodGet,
		"/api/v1/changes?recordId=shared&recordKind="+personKind, env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	types := changeTypesFromNDJSON(t, rec)
	if len(types) != 1 || types[0] != personKind {
		t.Fatalf("feed types = %v, want only the person row — the pair must not conflate id-sharing records", types)
	}
}

// A merge entry is addressed to the winner and a split entry to the loser, and
// each names both in its payload. The record scope returns them for either id,
// on the history page and the watch drain alike, and for nobody else.
func TestChangesRecordScopeCarriesMergeAndSplitForBothRecords(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	ctx := context.Background()
	w1 := env.put(t, personKind, "w1", map[string]any{"name": "Nina Ray"})
	l1 := env.put(t, personKind, "l1", map[string]any{"name": "N. Ray"})
	env.put(t, personKind, "x1", map[string]any{"name": "Someone Else"})
	merge, err := env.ds.Merge(ctx, substrate.ActorSystem, substrate.MergeInput{Kind: personKind, Winner: w1.ID, Loser: l1.ID})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, err := env.ds.Split(ctx, substrate.ActorSystem, substrate.SplitInput{Merge: merge.ID}); err != nil {
		t.Fatalf("split: %v", err)
	}

	// Where each entry landed: one put per person, then the merge and the
	// split, each one entry of the person kind.
	rows, err := env.ds.Changes(ctx, 0, substrate.ChangeFilter{Kinds: []string{personKind}}, changeBatch)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	seq := map[string]int64{}
	for _, c := range rows {
		key := string(c.Op) + " " + c.RecordID
		if _, dup := seq[key]; dup {
			t.Fatalf("two %s entries in %+v", key, rows)
		}
		seq[key] = c.Seq
	}
	putW, putL, putX := seq["put w1"], seq["put l1"], seq["put x1"]
	mergeSeq, splitSeq := seq["merge w1"], seq["split l1"]
	if len(rows) != 5 || putW == 0 || putL == 0 || putX == 0 || mergeSeq == 0 || splitSeq == 0 {
		t.Fatalf("person entries = %+v, want three puts, the merge addressed to w1 and the split to l1", rows)
	}

	for id, want := range map[string][]float64{
		"w1": {float64(putW), float64(mergeSeq), float64(splitSeq)},
		"l1": {float64(putL), float64(mergeSeq), float64(splitSeq)},
		"x1": {float64(putX)},
	} {
		scope := "recordId=" + id + "&recordKind=" + personKind
		rec := env.do(t, http.MethodGet, "/api/v1/changes?first=50&"+scope, env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		page := decodeJSON[changesBody](t, rec)
		var got []float64
		for i := len(page.Changes) - 1; i >= 0; i-- {
			got = append(got, float64(page.Changes[i].Seq))
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s history seqs = %v, want %v", id, got, want)
		}

		br, stop := startWatch(t, srv, "/api/v1/changes?watch=1&from=0&"+scope, env.token)
		if bm := readLine(t, br); bm["bookmark"] != float64(0) {
			stop()
			t.Fatalf("%s bookmark = %v", id, bm)
		}
		got = got[:0]
		for range want {
			got = append(got, readLine(t, br)["seq"].(float64))
		}
		stop()
		if !slices.Equal(got, want) {
			t.Fatalf("%s watch seqs = %v, want %v", id, got, want)
		}
	}
}
