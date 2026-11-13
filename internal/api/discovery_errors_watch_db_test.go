package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The `search` feature is a claim about a route, so hold the route to it:
// `GET /records?q=` ranks, and the answer is the ranked page with a score per
// hit.
func TestSearchIsTheRecordsRouteRanking(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	env.put(t, personKind, "p1", map[string]any{"name": "Ada Lovelace"})
	env.put(t, personKind, "p2", map[string]any{"name": "Grace Hopper"})

	rec := env.do(t, http.MethodGet, recordsPath+"?q=ada", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.RankedPage](t, rec)
	if len(page.Records) != 1 || page.Records[0].ID != "p1" {
		t.Fatalf("ranked page = %+v", page)
	}
	if page.Scores[personKind+"/p1"].Lexical == 0 {
		t.Fatalf("the hit carries no score: %+v", page.Scores)
	}
}

// errWatchBoom is a changelog read failure no sentinel names.
var errWatchBoom = errors.New("boom")

// watchFailingChanges is a repository whose changelog reads fail once the
// stream is open: a store gone mid-stream, which the engine cannot be made
// to produce on demand.
type watchFailingChanges struct{ substrate.Dataset }

func (watchFailingChanges) Changes(context.Context, int64, substrate.ChangeFilter, int) ([]substrate.Change, error) {
	return nil, errWatchBoom
}

func TestWatchTerminalErrorFrameOnMidStreamFailure(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	env.wrap = func(ds substrate.Dataset) substrate.Dataset { return watchFailingChanges{ds} }
	srv := httptest.NewServer(env.h)
	defer srv.Close()

	// The head and the bookmark are the engine's, so the 200 and the bookmark
	// go out first; then the drain's Changes read fails, and the stream must
	// end with a terminal error frame carrying the problem object — not a
	// silent EOF.
	br, stop := startWatch(t, srv, changesPath+"?watch=1&from=0", env.token)
	defer stop()

	if first := readLine(t, br); first["bookmark"] == nil {
		t.Fatalf("first line = %v, want a bookmark control frame", first)
	}
	frame := readLine(t, br)
	if _, hasSeq := frame["seq"]; hasSeq {
		t.Fatalf("terminal frame carried a seq: %v", frame)
	}
	errObj, ok := frame["error"].(map[string]any)
	if !ok {
		t.Fatalf("terminal frame = %v, want an error control frame", frame)
	}
	if errObj["code"] != codeInternal {
		t.Fatalf("terminal frame code = %v, want %q", errObj["code"], codeInternal)
	}
}
