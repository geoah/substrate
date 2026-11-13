package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The cross-collection feed (the console's stream page): newest-first history
// under `before`/`first`, server-side `q`, and per-row function chips. What the
// feed answers is the engine's (changes_db_test.go); the refusals here are the
// handler's own.

// seedChanges writes n people, e1 to en, as the engine's own hand: n
// changelog entries. Its callers hand it no *testing.T, so a refused write
// panics.
func seedChanges(ds substrate.Dataset, n int) {
	for i := range n {
		id := fmt.Sprintf("e%d", i+1)
		if _, err := ds.Put(context.Background(), substrate.ActorSystem, substrate.PutInput{
			Kind: personKind, ID: id, Properties: map[string]any{"name": "Person " + id},
		}); err != nil {
			panic(fmt.Sprintf("seed change %s: %v", id, err))
		}
	}
}

type changesBody struct {
	Changes []struct {
		Seq      int64                     `json:"seq"`
		RecordID string                    `json:"recordId"`
		Triggers []substrate.ChangeTrigger `json:"triggers"`
	} `json:"changes"`
}

// changesStub is a repository whose changelog stands at seq 3 and never
// moves: the head every cursor check reads before it refuses, and a watch
// signal that never fires. Nothing else is answered, so a refusal that
// reached a read would be a 500.
type changesStub struct{ stubDataset }

func (changesStub) Head(context.Context) (substrate.ChangelogHead, error) {
	return substrate.ChangelogHead{Seq: 3, Generation: "stub-generation"}, nil
}

func (changesStub) WatchSignal(ctx context.Context) <-chan int64 {
	out := make(chan int64)
	go func() {
		<-ctx.Done()
		close(out)
	}()
	return out
}

func TestChangesBadPagingParams(t *testing.T) {
	env := newStubEnv(t, changesStub{})
	for _, path := range []string{
		"/api/v1/changes?before=nope",
		"/api/v1/changes?first=0",
		"/api/v1/changes?first=x",
	} {
		rec := env.do(t, http.MethodGet, path, stubToken, nil)
		wantStatus(t, rec, http.StatusBadRequest)
	}
}

// `values` takes 1 or 0 and nothing else: another spelling is refused rather
// than read as "no", so a client that meant to ask never silently gets names
// alone.
func TestChangesValuesTakesOneSpelling(t *testing.T) {
	env := newStubEnv(t, changesStub{})
	rec := env.do(t, http.MethodGet, "/api/v1/changes?first=10&values=yes", stubToken, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}
