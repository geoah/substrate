package testenv_test

// The repository's horizon over HTTP (decision 0300): once the compaction
// pass forgets a deletion, a resume from below the line it was on is
// answered `410 compacted` with the head to list again from, and the change
// page names the horizon.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testenv"
)

func TestAResumeBelowAForgottenDeletionIsToldCompacted(t *testing.T) {
	clock := time.Now().UTC().Add(2 * time.Hour)
	e := testenv.Start(t, testenv.WithRepository(eventAuthority), testenv.WithEngineOptions(
		engine.WithCompactionInterval(-1), engine.WithDeletionsKept(time.Hour),
		engine.WithTestCompactionClock(func() time.Time { return clock })))
	e.ApplyVocabularyYAML(eventVocabulary)
	const id = "00000000000000000000000001"
	if status, body := e.Do(http.MethodPut, itemsPath+"/"+id, map[string]any{"properties": map[string]any{"name": "gone"}}); status/100 != 2 {
		t.Fatalf("put: %d %s", status, body)
	}
	var page substrate.ChangePage
	mustDecode(t, e, http.MethodGet, "/api/v1/changes?first=10", nil, &page)
	saved, generation := page.Head, page.Generation
	if status, body := e.Do(http.MethodDelete, itemsPath+"/"+id+"?purge=true", nil); status/100 != 2 {
		t.Fatalf("purge: %d %s", status, body)
	}
	// A write after it: the head is never a forgotten line.
	if status, body := e.Do(http.MethodPut, itemsPath+"/00000000000000000000000002", map[string]any{"properties": map[string]any{"name": "after"}}); status/100 != 2 {
		t.Fatalf("put: %d %s", status, body)
	}
	ctx := context.Background()
	ds, err := e.Service.Dataset(ctx, eventAuthority)
	if err != nil {
		t.Fatal(err)
	}
	report, err := ds.(interface {
		CompactionPass(context.Context) (engine.CompactionPassReport, error)
	}).CompactionPass(ctx)
	if err != nil || report.Forgotten != 1 {
		t.Fatalf("the pass forgot %d (%v), want the purged record", report.Forgotten, err)
	}
	mustDecode(t, e, http.MethodGet, "/api/v1/changes?first=10", nil, &page)
	if page.Horizon <= saved {
		t.Fatalf("the change page names horizon %d, want one above the saved position %d", page.Horizon, saved)
	}
	status, body := e.Do(http.MethodGet, "/api/v1/changes?from="+strconv.FormatInt(saved, 10)+"&generation="+generation, nil)
	var problem struct {
		Error substrate.ErrorPayload `json:"error"`
	}
	if err := json.Unmarshal(body, &problem); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if status != http.StatusGone || problem.Error.Code != "compacted" || problem.Error.Head == nil || *problem.Error.Head != page.Head {
		t.Fatalf("a resume below the horizon: %d %s, want 410 compacted with the head %d", status, body, page.Head)
	}
	// From the horizon on, the resume goes on.
	if status, body := e.Do(http.MethodGet, "/api/v1/changes?from="+strconv.FormatInt(page.Horizon, 10)+"&generation="+generation, nil); status != http.StatusOK {
		t.Fatalf("a resume at the horizon: %d %s", status, body)
	}
}

// The measurement a retention is chosen from, on the kept directory: its
// history by kind. Set SUBSTRATE_COMPACTION_TIMING to print it.
func TestTheKeptDirectorysHistoryByKind(t *testing.T) {
	if os.Getenv("SUBSTRATE_COMPACTION_TIMING") == "" {
		t.Skip("set SUBSTRATE_COMPACTION_TIMING to measure the kept directory's history")
	}
	for _, tag := range []string{"v0.123.1"} {
		k := checkKeptDirectory(t, filepath.Join(keptRoot, tag), keptConversions)
		kinds, err := k.e.Service.(engine.Operator).HistoryByKind(context.Background(), k.fixture.Repository)
		if err != nil {
			t.Fatal(err)
		}
		var entries, bytes int64
		for _, h := range kinds {
			entries, bytes = entries+h.Entries, bytes+h.Bytes
		}
		for _, h := range kinds {
			t.Logf("%s: %-62s %6d entries %9d bytes %5.1f%%", tag, h.Kind, h.Entries, h.Bytes, 100*float64(h.Bytes)/float64(bytes))
		}
		t.Logf("%s: total %d entries, %d bytes", tag, entries, bytes)
	}
}
