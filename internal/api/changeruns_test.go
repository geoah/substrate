package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The history as run summaries (`runs=1`): consecutive rows of the filtered
// feed with one actor, kind and verb, each read to its end. The runs are the
// engine's changelog folded (changeruns_db_test.go); the refusals here are
// the handler's own.

const (
	runTask   = "samples.substrate.reamde.dev/tasks/task"
	runPerson = "samples.substrate.reamde.dev/people/person"
)

// seedRun writes n changes by one actor to one kind, oldest first, through
// the dataset's own verbs. A put writes record, or a new record each time
// when record is empty; a patch patches record and a delete tombstones it.
// Every put and patch names the record afresh, so each one is a change. The
// payload a row carries is the engine's to stamp (substrate.RunVerb reads
// it), so payload is never sent: a put whose created flag disagrees with the
// one payload expects is refused. Its callers hand it no *testing.T, so a
// refused write panics.
func seedRun(ds substrate.Dataset, n int, actor substrate.Actor, kind string, op substrate.Op, payload map[string]any, record string) {
	ctx := context.Background()
	for range n {
		name := map[string]any{"name": fmt.Sprintf("%s %d", op, seedRunNames.Add(1))}
		var err error
		switch op {
		case substrate.OpPut:
			var rec *substrate.Record
			if rec, err = ds.Put(ctx, actor, substrate.PutInput{Kind: kind, ID: record, Properties: name}); err == nil && rec.Created != (payload["created"] == true) {
				err = fmt.Errorf("the put created=%v, want the payload %v", rec.Created, payload)
			}
		case substrate.OpPatch:
			_, err = ds.Patch(ctx, actor, kind, record, substrate.PatchInput{Properties: name})
		case substrate.OpDelete:
			_, err = ds.Delete(ctx, actor, kind, record, substrate.DeleteInput{})
		default:
			err = fmt.Errorf("seedRun writes no %s", op)
		}
		if err != nil {
			panic(fmt.Sprintf("seed %s %s/%s: %v", op, kind, record, err))
		}
	}
}

// seedRunNames numbers the names seedRun writes, across calls and tests.
var seedRunNames atomic.Int64

// runsAs reads the runs through the MCP door's own route, as an assistant.
func runsAs(t *testing.T, env *testEnv, token string) []substrate.ChangeRun {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/changes?runs=1&first=10", nil)
	req = req.WithContext(withMCPDoor(req.Context()))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, req)
	wantStatus(t, rec, http.StatusOK)
	return decodeJSON[substrate.ChangeRunPage](t, rec).Runs
}

// `runs` is a history read with one value, held to the same cursor rules.
func TestChangeRunsRefusals(t *testing.T) {
	env := newStubEnv(t, changesStub{})
	for _, q := range []string{"runs=true", "runs=1&watch=1", "runs=1&from=1", "runs=1&first=0"} {
		rec := env.do(t, http.MethodGet, "/api/v1/changes?"+q, stubToken, nil)
		wantStatus(t, rec, http.StatusBadRequest)
	}
	// A continuation without its generation is the history page's compacted
	// refusal, not an answer from another history.
	rec := env.do(t, http.MethodGet, "/api/v1/changes?runs=1&before=2", stubToken, nil)
	wantStatus(t, rec, http.StatusGone)
}
