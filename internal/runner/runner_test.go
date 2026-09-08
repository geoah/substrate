package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The protocol without an engine: registration, invocation, host calls under
// the allowlist and budget, error frames, and crash-then-restart supervision
// — against a fake backend. The dispatcher-facing behavior (park, retry,
// cursor) lives in the engine's db tests.

type fakeBackend struct {
	records   map[string]*substrate.Record
	getCalls  int
	lastFirst int
	lastK     int
	// pending is what Search reports as the embed backlog.
	pending  int
	blockGet bool
	// call is the Call host-call's fake engine half; nil refuses.
	call func(function string, args any) (any, error)
	// calls records every Call the backend served.
	calls []string
	// aliases stands in for the registry: a kind's bare reference against the
	// identity it resolves to.
	aliases map[string]string
}

func (f *fakeBackend) Get(ctx context.Context, typ, id string) (*substrate.Record, error) {
	f.getCalls++
	if f.blockGet {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	e := f.records[id]
	if e != nil && e.Kind != typ {
		return nil, nil
	}
	return e, nil
}

func (f *fakeBackend) List(_ context.Context, q substrate.Query) (*substrate.Page, error) {
	f.lastFirst = q.First
	var out []*substrate.Record
	for _, e := range f.records {
		for _, t := range q.Filter.Kinds {
			if e.Kind == t {
				out = append(out, e)
			}
		}
	}
	return &substrate.Page{Records: out}, nil
}

func (f *fakeBackend) Search(_ context.Context, in substrate.SearchInput) (substrate.SearchResult, error) {
	f.lastK = in.K
	return substrate.SearchResult{Pending: f.pending}, nil
}

func (f *fakeBackend) Call(_ context.Context, function string, args any) (any, error) {
	f.calls = append(f.calls, function)
	if f.call == nil {
		return nil, errors.New("no call backend")
	}
	return f.call(function, args)
}

func (f *fakeBackend) ResolveKind(name string) string {
	if ident, ok := f.aliases[name]; ok {
		return ident
	}
	return name
}

func testInput() Input {
	return Input{
		Mode: "record",
		Envelope: map[string]any{
			"change":     map[string]any{"id": "e1", "op": "create", "seq": int64(7)},
			"record":     map[string]any{"id": "e1", "properties": map[string]any{"name": "x"}},
			"repository": map[string]any{"owner": "geoah"},
		},
		CausalDepth: 1, IdempotencyKey: "geoah/fn/7",
	}
}

func widgetBackend() *fakeBackend {
	return &fakeBackend{
		records: map[string]*substrate.Record{
			"w1": {ID: "w1", Kind: "g.test/widgets/widget"},
		},
		aliases: map[string]string{"widget": "g.test/widgets/widget"},
	}
}

// TestReadAllowlistTakesEitherSpelling: a kind has two names — bare and
// authority-qualified — and they name ONE kind. A body that lists by the bare
// reference is making the read its capability declares, so the allowlist must
// resolve before it compares. `get` has always been held to the resolved kind
// of what it found; this is list and search catching up.
func TestReadAllowlistTakesEitherSpelling(t *testing.T) {
	r := New()
	spec := Spec{
		Repository: "t1", Function: "peek.g.test",
		Runtime: "python",
		Source: `
def main(input, host):
    host.list(filter={"kinds": ["widget"]})
    host.search(q="x", kinds=["widget"])
    return {"effects": []}
`,
		TimeoutMs: 5000,
		ReadTypes: []string{"g.test/widgets/widget"},
	}
	if _, err := r.Invoke(context.Background(), spec, testInput(), widgetBackend()); err != nil {
		t.Fatalf("the bare reference to a declared kind was refused: %v", err)
	}
	// And a kind the registry cannot place is still refused — an unresolvable
	// name must not slip past as itself.
	spec.Source = `
def main(input, host):
    host.list(filter={"kinds": ["gadget"]})
    return {"effects": []}
`
	_, err := r.Invoke(context.Background(), spec, testInput(), widgetBackend())
	if !errors.Is(err, ErrReadForbidden) {
		t.Fatalf("an undeclared kind was allowed: %v", err)
	}
}

func TestPythonInvokeAndHostCalls(t *testing.T) {
	r := New()
	spec := Spec{
		Repository: "t1", Function: "hello.g.test",
		Runtime: "python",
		Source: `
def main(input, host):
    host.log("hello")
    got = host.get("g.test/widgets/widget", "w1")
    return {"effects": [{"action": "put", "kind": "g.test/widgets/widget", "id": "out",
                         "properties": {"from": got["id"], "key": input["idempotencyKey"]}}],
            "output": {"depth": input["causalDepth"]}}
`,
		TimeoutMs: 5000,
		ReadTypes: []string{"g.test/widgets/widget"},
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), widgetBackend())
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(res.Effects) != 1 || len(res.Logs) != 1 || res.Logs[0] != "hello" {
		t.Fatalf("result: %+v", res)
	}
	ef := res.Effects[0].(map[string]any)
	props := ef["properties"].(map[string]any)
	if props["from"] != "w1" || props["key"] != "geoah/fn/7" {
		t.Fatalf("effect: %v", ef)
	}

	// A body error is an error result, and the host process survives it.
	bad := spec
	bad.Function = "bad.g.test"
	bad.Source = "def main(input, host):\n    raise ValueError('boom')\n"
	if _, err := r.Invoke(context.Background(), bad, testInput(), widgetBackend()); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("body error: %v", err)
	}
	if _, err := r.Invoke(context.Background(), spec, testInput(), widgetBackend()); err != nil {
		t.Fatalf("host died with the body error: %v", err)
	}
}

// TestPythonSearchResultCarriesPending: the host's search answer carries the
// embed backlog beside the hits, and the Python SDK hands both to the body:
// the returned list is the hits, and `.pending` rides on it.
func TestPythonSearchResultCarriesPending(t *testing.T) {
	r := New()
	spec := Spec{
		Repository: "t1", Function: "backlog.g.test",
		Runtime: "python",
		Source: `
def main(input, host):
    res = host.records.search("x", ["widget"])
    return {"effects": [], "output": {"pending": res.pending, "hits": len(res), "same": res.hits is res}}
`,
		TimeoutMs: 5000,
		ReadTypes: []string{"g.test/widgets/widget"},
	}
	be := widgetBackend()
	be.pending = 2
	res, err := r.Invoke(context.Background(), spec, testInput(), be)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, _ := res.Output.(map[string]any)
	if out["pending"] != float64(2) || out["hits"] != float64(0) || out["same"] != true {
		t.Fatalf("output = %v, want pending 2 over an empty, self-same hits list", res.Output)
	}
}

func TestPythonReadCapabilityTrips(t *testing.T) {
	r := New()
	spec := Spec{
		Repository: "t1", Function: "peek.g.test",
		Runtime: "python",
		Source: `
def main(input, host):
    try:
        host.list(filter={"kinds": ["task.g.test"]})
    except Exception:
        pass  # swallowing the trip must not un-trip it
    return {"effects": []}
`,
		TimeoutMs: 5000,
		ReadTypes: []string{"g.test/widgets/widget"},
	}
	_, err := r.Invoke(context.Background(), spec, testInput(), widgetBackend())
	if !errors.Is(err, ErrReadForbidden) {
		t.Fatalf("swallowed trip did not surface: %v", err)
	}
	if !Deterministic(err) {
		t.Fatal("a trip is deterministic")
	}
}

func TestGetOutsideAllowlistIsUniformAbsence(t *testing.T) {
	// The existence-and-type oracle: a resolved record
	// outside the allowlist answers exactly like an absent id — same shape,
	// same budget — never an error carrying the resolved type.
	r := New()
	spec := Spec{
		Repository: "t1", Function: "probe.g.test",
		Runtime: "python",
		Source: `
def main(input, host):
    hidden = host.get("secret.g.test", "s1")
    absent = host.get("g.test/widgets/widget", "nope")
    return {"output": [hidden is None, absent is None]}
`,
		TimeoutMs: 5000,
		ReadTypes: []string{"g.test/widgets/widget"},
	}
	backend := &fakeBackend{records: map[string]*substrate.Record{
		"s1": {ID: "s1", Kind: "secret.g.test"},
	}}
	res, err := r.Invoke(context.Background(), spec, testInput(), backend)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, _ := res.Output.([]any)
	if len(out) != 2 || out[0] != true || out[1] != true {
		t.Fatalf("a disallowed get leaked something: %v", res.Output)
	}
}

func TestPythonStdoutLandsInLogsNotProtocol(t *testing.T) {
	// Review W1 #4: an ordinary print writes JSON onto what used to be the
	// protocol stream. It must land in the invocation's capped logs and the
	// REAL response must be the one applied.
	r := New()
	spec := Spec{
		Repository: "t1", Function: "printer.g.test",
		Runtime: "python",
		Source: `
def main(input, host):
    print('{"kind": "response", "reqId": 1, "ok": true, "output": "EVIL"}')
    return {"output": "real"}
`,
		TimeoutMs: 5000,
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if res.Output != "real" {
		t.Fatalf("a print was applied as the response: %v", res.Output)
	}
	if len(res.Logs) != 1 || !strings.Contains(res.Logs[0], "EVIL") || !strings.HasPrefix(res.Logs[0], "[stdout]") {
		t.Fatalf("the print did not land in logs: %v", res.Logs)
	}
}

func TestProtocolDesyncKillsChild(t *testing.T) {
	// A frame with the wrong request id — injected here by writing straight
	// to fd 1, past the rebound sys.stdout — is a desync: the parent kills
	// the child instead of pairing it with the wrong delivery, and the next
	// invocation restarts cleanly.
	r := New()
	evil := Spec{
		Repository: "t1", Function: "evil.g.test",
		Runtime: "python",
		Source: `
import os
def main(input, host):
    os.write(1, b'{"kind": "response", "reqId": 999999, "ok": true, "output": "EVIL"}\n')
    return {"output": "real"}
`,
		TimeoutMs: 5000,
	}
	if _, err := r.Invoke(context.Background(), evil, testInput(), nil); err == nil || !strings.Contains(err.Error(), "desync") {
		t.Fatalf("a stray frame was tolerated: %v", err)
	}
	fine := Spec{
		Repository: "t1", Function: "fine.g.test",
		Runtime:   "python",
		Source:    "def main(input, host):\n    return {\"output\": \"ok\"}\n",
		TimeoutMs: 5000,
	}
	res, err := r.Invoke(context.Background(), fine, testInput(), nil)
	if err != nil || res.Output != "ok" {
		t.Fatalf("post-desync invoke: %+v %v", res, err)
	}
}

func TestPythonTimeoutKillsAndRestarts(t *testing.T) {
	r := New()
	stuck := Spec{
		Repository: "t1", Function: "stuck.g.test",
		Runtime:   "python",
		Source:    "import time\ndef main(input, host):\n    time.sleep(30)\n",
		TimeoutMs: 200,
	}
	if _, err := r.Invoke(context.Background(), stuck, testInput(), nil); err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("timeout: %v", err)
	}
	// The host was killed; the next invocation restarts it and re-registers.
	fine := Spec{
		Repository: "t1", Function: "fine.g.test",
		Runtime:   "python",
		Source:    "def main(input, host):\n    return {\"output\": \"ok\"}\n",
		TimeoutMs: 5000,
	}
	res, err := r.Invoke(context.Background(), fine, testInput(), nil)
	if err != nil || res.Output != "ok" {
		t.Fatalf("post-kill invoke: %+v %v", res, err)
	}
}

func TestTimeoutBoundsStuckHostCall(t *testing.T) {
	// Review W1 #5: the manifest timeout is a deadline over the WHOLE
	// invocation, host calls included — a stuck backend read cannot wedge
	// the delivery past it.
	r := New()
	spec := Spec{
		Repository: "t1", Function: "reader.g.test",
		Runtime: "python",
		Source: `
def main(input, host):
    host.get("g.test/widgets/widget", "w1")
    return {}
`,
		TimeoutMs: 300,
		ReadTypes: []string{"g.test/widgets/widget"},
	}
	backend := widgetBackend()
	backend.blockGet = true
	begin := time.Now()
	_, err := r.Invoke(context.Background(), spec, testInput(), backend)
	if err == nil {
		t.Fatal("a stuck host call returned")
	}
	if elapsed := time.Since(begin); elapsed > 5*time.Second {
		t.Fatalf("timeout did not bound the stuck call: %s (%v)", elapsed, err)
	}
}

func TestTimeoutReapsProcessTree(t *testing.T) {
	// Review W1 #6: killing the child kills its process GROUP, so a
	// grandchild a body spawned dies with the timeout instead of leaking.
	//
	// The spawn and the hang must be the SAME installation. One process per
	// installation means one process group per installation, so a timeout on
	// a NEIGHBOUR's body no longer reaps this body's descendants, which is
	// the isolation working, not a leak. TimeoutMs is not part of Spec.Key, so
	// the two specs below are one installation with two deadlines.
	r := New()
	body := `
import subprocess, time
def main(input, host):
    if (input.get("args") or {}).get("op") == "spawn":
        p = subprocess.Popen(["sleep", "60"])
        return {"output": p.pid}
    time.sleep(30)
`
	spawn := Spec{
		Repository: "t1", Function: "spawner.g.test",
		Runtime: "python", Source: body, TimeoutMs: 5000,
	}
	in := testInput()
	in.Args = map[string]any{"op": "spawn"}
	res, err := r.Invoke(context.Background(), spawn, in, nil)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	pid := int(res.Output.(float64))
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("grandchild %d not alive after spawn: %v", pid, err)
	}
	stuck := spawn
	stuck.TimeoutMs = 200
	if _, err := r.Invoke(context.Background(), stuck, testInput(), nil); err == nil {
		t.Fatal("the stuck body returned")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); err != nil {
			break // reaped
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, 9)
			t.Fatalf("grandchild %d survived the process-group kill", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestRegistrationTiedToProcessGeneration(t *testing.T) {
	// Review W1 #7: registration state lives on the process GENERATION. With
	// one process per installation the generation IS the process, so a killed
	// body's registration cannot outlive it: the map entry is replaced
	// wholesale by the restart.
	ctx := context.Background()
	r := New()
	spec := Spec{
		Repository: "t1", Function: "gen.g.test",
		Runtime:   "python",
		Source:    "def main(input, host):\n    return {\"output\": \"ok\"}\n",
		TimeoutMs: 5000,
	}
	p1, err := r.pythonProc(ctx, spec)
	if err != nil {
		t.Fatalf("python proc: %v", err)
	}
	// The forced interleaving: P1 dies AFTER its registration completed;
	// a fresh generation starts.
	p1.kill()
	p2, err := r.pythonProc(ctx, spec)
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	if p1 == p2 {
		t.Fatal("kill did not force a new generation")
	}
	r.mu.Lock()
	live := r.pys[spec.Key()]
	r.mu.Unlock()
	if live != p2 {
		t.Fatal("the dead generation is still the live process")
	}
	// The live generation re-registers on its own first invocation.
	res, err := r.Invoke(context.Background(), spec, testInput(), nil)
	if err != nil || res.Output != "ok" {
		t.Fatalf("post-restart invoke: %+v %v", res, err)
	}
	r.mu.Lock()
	registered := p2.registered[spec.Key()]
	r.mu.Unlock()
	if !registered {
		t.Fatal("the live generation did not record its own registration")
	}
}

func TestBudgetsChargeBeforeExecutionAndClampReads(t *testing.T) {
	r := New()
	// Calls charge BEFORE executing: with a budget of 1, the second get
	// trips without ever reaching the backend.
	spec := Spec{
		Repository: "t1", Function: "burner.g.test",
		Runtime: "python",
		Source: `
def main(input, host):
    try:
        host.get("g.test/widgets/widget", "w1")
    except Exception:
        pass
    try:
        host.get("g.test/widgets/widget", "w1")
    except Exception:
        pass
    return {}
`,
		TimeoutMs: 5000,
		ReadTypes: []string{"g.test/widgets/widget"},
		ReadCalls: 1, ReadRows: 100,
	}
	backend := widgetBackend()
	_, err := r.Invoke(context.Background(), spec, testInput(), backend)
	if !errors.Is(err, ErrReadBudget) {
		t.Fatalf("call budget did not trip: %v", err)
	}
	if backend.getCalls != 1 {
		t.Fatalf("an over-budget call executed: %d backend calls", backend.getCalls)
	}

	// List `first` and search `k` clamp to the remaining ROW budget before
	// the backend runs; exhausted headroom trips without executing.
	clamp := Spec{
		Repository: "t1", Function: "clamper.g.test",
		Runtime: "python",
		Source: `
def main(input, host):
    host.list(filter={"kinds": ["g.test/widgets/widget"]}, first=1000)
    host.search(q="x", kinds=["g.test/widgets/widget"], k=1000)
    try:
        host.list(filter={"kinds": ["g.test/widgets/widget"]}, first=1)
    except Exception:
        pass
    return {}
`,
		TimeoutMs: 5000,
		ReadTypes: []string{"g.test/widgets/widget"},
		ReadCalls: 10, ReadRows: 1,
	}
	backend = widgetBackend()
	_, err = r.Invoke(context.Background(), clamp, testInput(), backend)
	if !errors.Is(err, ErrReadBudget) {
		t.Fatalf("exhausted rows did not trip: %v", err)
	}
	if backend.lastFirst != 1 {
		t.Fatalf("list first not clamped to the remaining budget: %d", backend.lastFirst)
	}
}

func TestRepositoryIsolationOfLiveState(t *testing.T) {
	// Review W1 #10: identical source in two repositories gets two module
	// namespaces — a module-level cache never leaks across repositories.
	r := New()
	source := `
N = 0
def main(input, host):
    global N
    N += 1
    return {"output": N}
`
	t1 := Spec{Repository: "t1", Function: "count.g.test", Runtime: "python", Source: source, TimeoutMs: 5000}
	t2 := Spec{Repository: "t2", Function: "count.g.test", Runtime: "python", Source: source, TimeoutMs: 5000}
	if res, err := r.Invoke(context.Background(), t1, testInput(), nil); err != nil || res.Output != float64(1) {
		t.Fatalf("t1 first: %+v %v", res, err)
	}
	if res, err := r.Invoke(context.Background(), t1, testInput(), nil); err != nil || res.Output != float64(2) {
		t.Fatalf("t1 second: %+v %v", res, err)
	}
	if res, err := r.Invoke(context.Background(), t2, testInput(), nil); err != nil || res.Output != float64(1) {
		t.Fatalf("t2 saw t1's module state: %+v %v", res, err)
	}
}

func TestReconcileRetiresStaleRegistrations(t *testing.T) {
	// Review W1 #15: after a registry publish, the process serving an
	// installation the registry no longer references stops: scoped to the
	// repository, so a neighbor's identical key survives.
	r := New()
	src := "def main(input, host):\n    return {\"output\": \"ok\"}\n"
	keep := Spec{Repository: "t1", Function: "keep.g.test", Runtime: "python", Source: src, TimeoutMs: 5000}
	drop := Spec{Repository: "t1", Function: "drop.g.test", Runtime: "python", Source: src, TimeoutMs: 5000}
	other := Spec{Repository: "t2", Function: "drop.g.test", Runtime: "python", Source: src, TimeoutMs: 5000}
	for _, spec := range []Spec{keep, drop, other} {
		if _, err := r.Invoke(context.Background(), spec, testInput(), nil); err != nil {
			t.Fatalf("invoke %s: %v", spec.Function, err)
		}
	}
	r.Reconcile(context.Background(), "t1", []Spec{keep})
	r.mu.Lock()
	_, kept := r.pys[keep.Key()]
	_, dropped := r.pys[drop.Key()]
	_, others := r.pys[other.Key()]
	r.mu.Unlock()
	if !kept || dropped || !others {
		t.Fatalf("reconcile: keep=%v drop=%v other-repository=%v", kept, dropped, others)
	}
	// The dropped body starts a fresh process if reinstalled: nothing is
	// wedged, just retired.
	if _, err := r.Invoke(context.Background(), drop, testInput(), nil); err != nil {
		t.Fatalf("reinstalled body: %v", err)
	}
}

func TestGoBuildAndInvoke(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles with the host toolchain")
	}
	r := New()
	spec := Spec{
		Repository: "t1", Function: "gofn.g.test",
		Runtime: "go",
		Source: `
import "substratefn.local/substratefn"

func Main(in *substratefn.Input, host *substratefn.Host) (*substratefn.Result, error) {
	host.Logf("depth %d", in.CausalDepth)
	got, err := host.Get("g.test/widgets/widget", "w1")
	if err != nil {
		return nil, err
	}
	return &substratefn.Result{
		Output: got["id"],
		Effects: []substratefn.Effect{{Action: "put", Kind: "g.test/widgets/widget", ID: "out"}},
	}, nil
}
`,
		TimeoutMs: 30000,
		ReadTypes: []string{"g.test/widgets/widget"},
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), widgetBackend())
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if res.Output != "w1" || len(res.Effects) != 1 || len(res.Logs) != 1 {
		t.Fatalf("result: %+v", res)
	}
	// The second invocation rides the cached binary and the live process.
	if _, err := r.Invoke(context.Background(), spec, testInput(), widgetBackend()); err != nil {
		t.Fatalf("second invoke: %v", err)
	}

	// Review W1 #9: a corrupt cache artifact is invalidated and rebuilt once
	// instead of wedging the installation across restarts.
	key := spec.Key()
	r.mu.Lock()
	p := r.gos[key]
	r.mu.Unlock()
	p.kill()
	// Wait for the reap, not just the signal: while the kernel still has the
	// artifact open for execution, rewriting it is ETXTBSY.
	<-p.waited
	dir, err := r.binDir()
	if err != nil {
		t.Fatalf("bin dir: %v", err)
	}
	bkey, err := buildKey(spec)
	if err != nil {
		t.Fatalf("build key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, bkey), []byte("not a binary"), 0o755); err != nil {
		t.Fatalf("corrupt artifact: %v", err)
	}
	if res, err := r.Invoke(context.Background(), spec, testInput(), widgetBackend()); err != nil || res.Output != "w1" {
		t.Fatalf("corrupt artifact did not rebuild: %+v %v", res, err)
	}
}

func TestDescribeAssertsProtocolVersion(t *testing.T) {
	// Review W2 #15: the describe roundtrip is the version negotiation. A
	// child answering with another protocol version — a stale artifact, an
	// SDK copied off an outdated comment — is refused before anything
	// invokes, and a current child passes the same gate.
	dir := t.TempDir()
	stale := filepath.Join(dir, "stale-child")
	if err := os.WriteFile(stale, []byte(`#!/bin/sh
read line
printf '{"kind":"response","reqId":1,"ok":true,"functions":["main"],"protocol":2}\n'
read hold
`), 0o755); err != nil {
		t.Fatal(err)
	}
	r := New()
	spec := Spec{Repository: "t1", Function: "stale.g.test", Runtime: "go", TimeoutMs: 5000}
	if _, err := r.startVerified(context.Background(), spec, stale); err == nil ||
		!strings.Contains(err.Error(), "protocol 2, want 5") {
		t.Fatalf("a stale protocol child was accepted: %v", err)
	}

	current := filepath.Join(dir, "current-child")
	if err := os.WriteFile(current, []byte(`#!/bin/sh
read line
printf '{"kind":"response","reqId":1,"ok":true,"functions":["main"],"protocol":5}\n'
read hold
`), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := r.startVerified(context.Background(), spec, current)
	if err != nil {
		t.Fatalf("a current protocol child was refused: %v", err)
	}
	p.kill()
}

func TestCallHostCallGatingAndRoundtrip(t *testing.T) {
	// The runner's half of the callable contract: `host.call` is gated on
	// the spec's CallTargets (an ungranted target is a deterministic trip),
	// charged against the call budget, and a granted call round-trips the
	// target's output back into the body.
	r := New()
	spec := Spec{
		Repository: "t1", Function: "caller.g.test",
		Runtime: "python",
		Source: `
def main(input, host):
    out = host.call("callee.g.test", {"n": 2})
    return {"output": out}
`,
		TimeoutMs:   5000,
		CallTargets: []string{"callee.g.test"},
	}
	backend := widgetBackend()
	backend.call = func(function string, args any) (any, error) {
		n := args.(map[string]any)["n"].(float64)
		return map[string]any{"doubled": n * 2}, nil
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), backend)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out := res.Output.(map[string]any)
	if out["doubled"] != float64(4) {
		t.Fatalf("output: %+v", res.Output)
	}
	if len(backend.calls) != 1 || backend.calls[0] != "callee.g.test" {
		t.Fatalf("backend calls: %v", backend.calls)
	}

	// Ungranted: the trip decides the delivery even when the body swallows it.
	ungranted := spec
	ungranted.Function = "sneaky.g.test"
	ungranted.CallTargets = nil
	ungranted.Source = `
def main(input, host):
    try:
        host.call("callee.g.test", {})
    except Exception:
        pass
    return {}
`
	_, err = r.Invoke(context.Background(), ungranted, testInput(), backend)
	if !errors.Is(err, ErrCallForbidden) {
		t.Fatalf("ungranted call: %v", err)
	}
	if !Deterministic(err) {
		t.Fatal("a call allowlist trip must be deterministic")
	}
	if len(backend.calls) != 1 {
		t.Fatalf("the ungranted call reached the backend: %v", backend.calls)
	}

	// Budget: sub-calls charge the caller's call budget before executing.
	thrifty := spec
	thrifty.Function = "thrifty.g.test"
	thrifty.ReadTypes = []string{"g.test/widgets/widget"}
	thrifty.ReadCalls = 1
	thrifty.Source = `
def main(input, host):
    host.call("callee.g.test", {"n": 1})
    host.call("callee.g.test", {"n": 2})
    return {}
`
	_, err = r.Invoke(context.Background(), thrifty, testInput(), backend)
	if !errors.Is(err, ErrReadBudget) {
		t.Fatalf("over-budget call: %v", err)
	}
}

// windowSpec is the body the lookup-and-use window tests run: Invoke takes its
// process out of the map, and a kill can land before roundtrip takes proc.mu.
var windowSpec = Spec{
	Repository: "t1", Function: "window.g.test", Runtime: "python", TimeoutMs: 5000,
	Source: "def main(input, host):\n    return {\"output\": \"ok\"}\n",
}

func liveProc(t *testing.T, r *Runner, spec Spec) *proc {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pys[spec.Key()]
}

// A kill that lands between the lookup and the write used to fail the delivery
// with "write to child: write |1: file already closed" and park it. The frame
// never reached the child, so the delivery restarts the process and runs.
// Each killer is forced into the window on the first attempt: the idle sweep
// with a clock past the TTL, and a sibling that kills under the process lock.
// Reconcile is the killer that must NOT restart; it has its own test below.
func TestKillInLookupWindowRestartsTheDelivery(t *testing.T) {
	ctx := context.Background()
	killers := map[string]func(r *Runner, p *proc){
		"sweep": func(r *Runner, _ *proc) { r.sweep(nowPlus(idleTTL + 1)) },
		"sibling": func(_ *Runner, p *proc) {
			p.mu.Lock()
			p.kill()
			p.mu.Unlock()
		},
	}
	for name, kill := range killers {
		t.Run(name, func(t *testing.T) {
			r := New()
			if err := r.Warm(ctx, windowSpec); err != nil {
				t.Fatalf("warm: %v", err)
			}
			first := liveProc(t, r, windowSpec)
			hits := 0
			r.afterLookup = func(p *proc) {
				if p != first {
					return
				}
				hits++
				kill(r, p)
				if p.alive() {
					t.Fatal("the kill did not land in the window")
				}
			}
			res, err := r.Invoke(ctx, windowSpec, testInput(), nil)
			if err != nil || res.Output != "ok" {
				t.Fatalf("a kill in the lookup window failed the delivery: %+v %v", res, err)
			}
			if hits != 1 {
				t.Fatalf("the doomed process was looked up %d times, want once", hits)
			}
			if second := liveProc(t, r, windowSpec); second == nil || second == first {
				t.Fatal("the delivery did not run on a fresh process")
			}
		})
	}
}

// Reconcile in the window is a retirement, not a loss: the registry no longer
// has the installation, so the retry must not start the retired body (under
// its former sandbox policy) and leave it serving. It answers ErrRetired,
// naming the function, and parks. A later admission runs the body again.
func TestReconcileInLookupWindowDoesNotRestartTheRetiredBody(t *testing.T) {
	ctx := context.Background()
	r := New()
	if err := r.Warm(ctx, windowSpec); err != nil {
		t.Fatalf("warm: %v", err)
	}
	first := liveProc(t, r, windowSpec)
	lookups := 0
	r.afterLookup = func(p *proc) {
		lookups++
		if p == first {
			r.Reconcile(ctx, windowSpec.Repository, nil)
		}
	}
	_, err := r.Invoke(ctx, windowSpec, testInput(), nil)
	if !errors.Is(err, ErrRetired) || !strings.Contains(err.Error(), windowSpec.Function) {
		t.Fatalf("a retired installation returned %v, want ErrRetired naming %s", err, windowSpec.Function)
	}
	if !Deterministic(err) {
		t.Fatal("a retirement must park: a retry with the same spec would restart the retired body")
	}
	if lookups != 1 {
		t.Fatalf("the retry looked a process up %d times, want no second lookup", lookups)
	}
	if p := liveProc(t, r, windowSpec); p != nil || first.alive() {
		t.Fatal("the retired body was started again")
	}
	// Admitting the installation again (a Warm at registration) clears the
	// retirement, and a later kill in the window is back to a restart.
	r.afterLookup = nil
	if err := r.Warm(ctx, windowSpec); err != nil {
		t.Fatalf("re-admit: %v", err)
	}
	if res, err := r.Invoke(ctx, windowSpec, testInput(), nil); err != nil || res.Output != "ok" {
		t.Fatalf("post-readmission invoke: %+v %v", res, err)
	}
}

// The restart is one retry, not a loop: a process lost in the window twice in
// a row surfaces errChildGone for the dispatcher's retry-then-park.
func TestSecondLossInLookupWindowSurfaces(t *testing.T) {
	ctx := context.Background()
	r := New()
	r.afterLookup = func(p *proc) {
		p.mu.Lock()
		p.kill()
		p.mu.Unlock()
	}
	_, err := r.Invoke(ctx, windowSpec, testInput(), nil)
	if !errors.Is(err, errChildGone) {
		t.Fatalf("two losses returned %v, want errChildGone", err)
	}
}

// roundtrip on a process a sweep retired reports errChildGone from the alive
// check, before any write: the closed pipe never becomes the error.
func TestRoundtripOnRetiredProcessIsChildGone(t *testing.T) {
	ctx := context.Background()
	r := New()
	p, err := r.pythonProc(ctx, windowSpec)
	if err != nil {
		t.Fatalf("python proc: %v", err)
	}
	r.sweep(nowPlus(idleTTL + 1))
	in := testInput()
	_, err = p.roundtrip(ctx, windowSpec.timeout(),
		frame{Op: "invoke", ID: windowSpec.Key(), Input: &in}, &readState{spec: windowSpec})
	if !errors.Is(err, errChildGone) {
		t.Fatalf("roundtrip on a swept process: %v, want errChildGone", err)
	}
	if strings.Contains(err.Error(), "file already closed") {
		t.Fatalf("the closed pipe leaked into the error: %v", err)
	}
}

// The unforced race: deliveries against one installation while the idle sweep
// spins beside them. Whether the sweep lands in the window is up to the
// scheduler, so this can pass on the old code by luck; the seam tests
// above are the proof, and this one runs the real interleavings under the race
// detector. A delivery may lose its process twice under this much killing and
// report errChildGone, and on a saturated host every one of them may: how
// many get through is scheduling, not a property, so nothing here counts them.
// No delivery may ever fail on the closed pipe, and once the sweeper has
// stopped, a delivery must get through.
func TestSweepRacingInvokesNeverClosesThePipeUnderThem(t *testing.T) {
	ctx := context.Background()
	r := New()
	if err := r.Warm(ctx, windowSpec); err != nil {
		t.Fatalf("warm: %v", err)
	}
	stop := make(chan struct{})
	var killer sync.WaitGroup
	killer.Add(1)
	go func() {
		defer killer.Done()
		for {
			select {
			case <-stop:
				return
			default:
				r.sweep(nowPlus(idleTTL + 1))
				runtime.Gosched()
			}
		}
	}()
	const workers, rounds = 3, 6
	errs := make(chan error, workers*rounds)
	var deliveries sync.WaitGroup
	for range workers {
		deliveries.Add(1)
		go func() {
			defer deliveries.Done()
			for range rounds {
				res, err := r.Invoke(ctx, windowSpec, testInput(), nil)
				if err == nil && res.Output != "ok" {
					err = fmt.Errorf("output %v", res.Output)
				}
				errs <- err
			}
		}()
	}
	deliveries.Wait()
	close(stop)
	killer.Wait()
	close(errs)
	for err := range errs {
		if err == nil {
			continue
		}
		if strings.Contains(err.Error(), "file already closed") {
			t.Fatalf("a delivery wrote to a swept process's pipe: %v", err)
		}
		if !errors.Is(err, errChildGone) {
			t.Fatalf("a delivery failed with something other than a lost process: %v", err)
		}
	}
	// With the sweeper stopped the installation is invocable again: whatever
	// the race left in the map, dead or live, the lookup restarts or reuses it.
	if res, err := r.Invoke(ctx, windowSpec, testInput(), nil); err != nil || res.Output != "ok" {
		t.Fatalf("a delivery after the sweep stopped did not get through: %+v %v", res, err)
	}
}
