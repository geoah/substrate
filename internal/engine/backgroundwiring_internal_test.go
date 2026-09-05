package engine

// The WIRING, as opposed to the helper background_internal_test.go holds: that
// Close drains before the pools go, and that each detached site reaches
// service.spawn rather than a bare `go` on context.Background(). Undoing
// either is invisible to every other test in the tree.

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// CLOSE DRAINS BEFORE THE POOLS GO. Without the stopBackground call in Close, a
// task still inside a transaction meets closed pools. The pools here are never
// connected to (sql.Open only parses the DSN), so this holds without a
// database.
func TestCloseDrainsTheDetachedTasksBeforeThePoolsClose(t *testing.T) {
	var logs syncBuffer
	s := backgroundService(&logs)
	s.admin, s.maint = unconnectedPool(t), unconnectedPool(t)
	s.datasets = map[string]*dataset{}

	started, finished := make(chan struct{}), make(chan struct{})
	s.spawn("judge", "repo1", func(context.Context) {
		close(started)
		time.Sleep(50 * time.Millisecond)
		close(finished)
	})
	<-started
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("Close returned with a task still running: it must drain the background before it closes the pools")
	}
	if s.spawn("judge", "repo1", func(context.Context) {}) {
		t.Fatal("Close left the background admitting work")
	}
}

// EVERY DETACHED SITE GOES THROUGH SPAWN. A site that goes back to a bare `go`
// on context.Background() is uncounted, unwaited and unrecovered. The service
// is shut down first, so spawn REFUSES each task and names it: the refusal log
// is the proof the call took the supervised path, and no task body runs against
// this dataset's absent pool.
func TestTheDetachedSitesGoThroughSpawn(t *testing.T) {
	var logs syncBuffer
	s := backgroundService(&logs)
	s.stopBackground(time.Second)
	ds := &dataset{
		svc: s, info: substrate.RepositoryInfo{ID: "repo1"},
		reg: registryWithOneFunction(t), watch: newBroadcaster(),
	}

	ds.maybeJudge("request1", &policyRule{id: "policy1", judge: "judge.test.dev/judge/reviewer"})
	ds.resumeNotifiedThread("thread1", "")
	ds.warmFunctions()

	out := logs.String()
	for site, task := range map[string]string{
		"maybeJudge (judge.go)":                   "task=judge",
		"resumeNotifiedThread (agentdecision.go)": `task="resume notified thread"`,
		"warmFunctions (runner.go)":               `task="warm functions"`,
	} {
		if !strings.Contains(out, task) {
			t.Fatalf("%s did not go through service.spawn: no %s in the log: %s", site, task, out)
		}
	}
}

// unconnectedPool is a *sql.DB that never opens a connection: sql.Open only
// parses the DSN, so Close is real and no database is needed.
func unconnectedPool(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", "postgres://substrate@127.0.0.1:1/substrate")
	if err != nil {
		t.Fatalf("open an unconnected pool: %v", err)
	}
	return db
}

// registryWithOneFunction is the smallest registry warmFunctions acts on: one
// python body, which it never gets far enough to prepare.
func registryWithOneFunction(t *testing.T) *vocabulary.Registry {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, doc := range map[string]map[string]any{
		"authority.yaml": vocabulary.PackageManifest("warm.test.dev/warm", 0),
		"function.yaml": vocabulary.FunctionManifest("warm.test.dev/warm", "prepared", map[string]any{
			"description": "a body warmFunctions would prepare",
			"runtime":     vocabulary.RuntimePython,
			"source":      "def main(input, host): return {\"output\": {}}",
		}),
	} {
		raw, err := yaml.Marshal(doc)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		fsys[name] = &fstest.MapFile{Data: raw}
	}
	reg, err := vocabulary.LoadFS(fsys)
	if err != nil {
		t.Fatalf("load the warm registry: %v", err)
	}
	if len(reg.Functions()) != 1 {
		t.Fatalf("the warm registry holds %d functions, want 1", len(reg.Functions()))
	}
	return reg
}
