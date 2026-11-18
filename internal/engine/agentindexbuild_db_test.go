package engine

// An agent's declaration of a kind with a reference builds the reference's
// index CONCURRENTLY before its transaction (indices.go), and such a build
// waits for every snapshot older than its last scan. The archive pass took
// its pause for running agents inside its snapshot (archivepace.go): the
// build waited on the pass, the pass on the agent, the agent on the build,
// and the architect's `vocabulary` call ended at its 300 s deadline with
// nothing applied, while the same documents applied through the API, where
// no agent runs for a pass to pause for, finished in seconds (#1496).

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

func TestAnArchitectsDeclarationBuildsItsIndexWhileAnArchivePassPausesForAgents(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The pass is run by hand at the step below; the interval keeps the
	// ticker's own out of the way. armed marks that pass's first line, where
	// it pauses for the agents in flight with its snapshot held.
	var armed atomic.Bool
	var first sync.Once
	pausing := make(chan struct{})
	ds, fake, repo := openMossDataset(t,
		WithArchiveInterval(time.Hour),
		WithTestArchivePaceHook(func(string, int64) {
			if armed.Load() {
				first.Do(func() { close(pausing) })
			}
		}),
	)
	pad := repo + "/scratch/pad"
	note := repo + "/garden/note"
	docs := []any{
		map[string]any{
			"kind":     kindPackage,
			"metadata": map[string]any{"id": repo + "/garden"},
			"data":     map[string]any{"authority": repo, "package": "garden"},
		},
		map[string]any{
			"kind":     kindKind,
			"metadata": map[string]any{"id": note},
			"data": map[string]any{
				"authority": repo, "package": "garden",
				"description":     "A note about the garden, written on a pad.",
				"names":           map[string]any{"singular": "note"},
				"displayTemplate": "{name}",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "what the note is about"},
					"pad":  map[string]any{"type": "reference", "kind": pad, "description": "the pad it is written on"},
				},
			},
		},
	}

	asked, answer := make(chan struct{}), make(chan struct{})
	fake.script(mossModel,
		fakeTurn{calls: []fakeCall{{"architect", toolArgs(t, map[string]any{
			"input": "The owner asked to keep garden notes, each on a pad. The owner asked for this.",
		})}}},
		// The architect's model call waits here while the pass starts, so
		// both loops are in flight when it does.
		fakeTurn{arrived: asked, release: answer, calls: []fakeCall{{"vocabulary", toolArgs(t, map[string]any{"documents": docs})}}},
		fakeTurn{content: "Applied " + note + "."},
		fakeTurn{content: "Your garden notes have a home."},
	)
	type chatted struct {
		res *substrate.AgentResult
		err error
	}
	chat := make(chan chatted, 1)
	go func() {
		res, err := ds.ChatAgent(ctx, substrate.ActorPerson, mossID, substrate.ChatInput{Message: "keep garden notes for me, each on one of my pads"}, nil)
		chat <- chatted{res, err}
	}()
	select {
	case <-asked:
	case <-time.After(30 * time.Second):
		t.Fatal("the architect never called its model")
	}

	// A pass over what the registration and the turn wrote, taken while
	// Moss and its architect run: it pauses at its first line, its snapshot
	// held.
	armed.Store(true)
	passed := make(chan error, 1)
	go func() { passed <- ds.ArchivePass(ctx) }()
	select {
	case <-pausing:
	case err := <-passed:
		t.Fatalf("the archive pass ended before it copied a line: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("the archive pass never reached a line to copy")
	}

	// The architect applies the kind: its reference's index is built with
	// the pass's snapshot older than the build.
	close(answer)
	var got chatted
	select {
	case got = <-chat:
	case <-time.After(45 * time.Second):
		t.Fatalf("the architect's declaration did not finish: %s", indexBuildWaits(t, ds.svc.admin))
	}
	if got.err != nil {
		t.Fatalf("chat: %v", got.err)
	}
	if got.res.Status != threadOK {
		t.Fatalf("Moss settled %q (%s), want ok", got.res.Status, got.res.Reason)
	}
	if _, err := ds.resolveType(note); err != nil {
		t.Fatalf("the architect's kind is not declared: %v", err)
	}
	select {
	case err := <-passed:
		if err != nil {
			t.Fatalf("archive pass: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the archive pass did not finish after the agents returned")
	}
}

// indexBuildWaits names each session an index build in this database waits
// on, with the phase it waits in: the failure says which session held it.
func indexBuildWaits(t *testing.T, admin *sql.DB) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := admin.QueryContext(ctx, `
		SELECT p.pid, p.phase, b.pid, coalesce(b.state, ''), coalesce(b.backend_xmin::text, ''),
		       coalesce(now() - b.xact_start, '0'::interval)::text, left(coalesce(b.query, ''), 200)
		  FROM pg_stat_progress_create_index p
		  CROSS JOIN LATERAL unnest(pg_blocking_pids(p.pid)) AS w(pid)
		  JOIN pg_stat_activity b ON b.pid = w.pid
		 WHERE p.datname = current_database()`)
	if err != nil {
		return "reading what the build waits on: " + err.Error()
	}
	defer func() { _ = rows.Close() }()
	var b strings.Builder
	for rows.Next() {
		var build, waiter int
		var phase, state, xmin, age, query string
		if err := rows.Scan(&build, &phase, &waiter, &state, &xmin, &age, &query); err != nil {
			return "reading what the build waits on: " + err.Error()
		}
		fmt.Fprintf(&b, "\n  the build (pid %d) is %q, on pid %d: %s, snapshot xmin %s, transaction open %s, last query %q",
			build, phase, waiter, state, xmin, age, strings.Join(strings.Fields(query), " "))
	}
	if err := rows.Err(); err != nil {
		return "reading what the build waits on: " + err.Error()
	}
	if b.Len() == 0 {
		return "no index build is waiting on a session"
	}
	return b.String()
}
