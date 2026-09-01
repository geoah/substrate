package engine

// THE FOUR HOST FUNCTIONS AS RECORDS. `runtime: host` names no body — the engine
// is the implementation — so what these hold is everything that follows from the
// four being ordinary function records: they are seeded, they arrive in a
// repository that predates them through the boot upgrade, two of them are
// callable under a token and two are not, and a trigger may not name one.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// hostFunctionIDs are the four, in the order the assertions name them.
var hostFunctionIDs = []string{
	vocabulary.HostFunctionGraphQL,
	vocabulary.HostFunctionMutate,
	vocabulary.HostFunctionPropose,
	vocabulary.HostFunctionQuery,
}

// A FRESH REPOSITORY HOLDS THEM. The seed writes the binary's tree into the
// changelog, and a host function is part of that tree like any other declaration:
// the functions COLLECTION is where a console or a CLI finds them.
func TestHostFunctionsAreSeededAsRecords(t *testing.T) {
	ctx := context.Background()
	ds := openInternalDataset(t)
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{kindFunction}}, First: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]*substrate.Record{}
	for _, r := range page.Records {
		rows[r.ID] = r
	}
	for _, id := range hostFunctionIDs {
		row, ok := rows[id]
		if !ok {
			t.Fatalf("the functions collection does not list %s: %v", id, sortedKeys(anyMap(rows)))
		}
		if rt, _ := row.Properties["runtime"].(string); rt != vocabulary.RuntimeHost {
			t.Fatalf("%s runtime = %v, want host", id, row.Properties["runtime"])
		}
		if _, hasBody := row.Properties["source"]; hasBody {
			t.Fatalf("%s carries a body: %v", id, row.Properties["source"])
		}
		// The declaration is the CARD: the description and the arguments are what
		// the model is shown, and they used to be Go string literals.
		if desc, _ := row.Properties["description"].(string); desc == "" {
			t.Fatalf("%s carries no description — the declaration is its card", id)
		}
		if args, _ := row.Properties["arguments"].([]any); len(args) == 0 {
			t.Fatalf("%s declares no arguments: %v", id, row.Properties)
		}
	}
	// propose is the one with a write permission of its own, because it writes one
	// kind and always the same one; mutate declares none, since the ceiling is its
	// caller's.
	perms, _ := rows[vocabulary.HostFunctionPropose].Properties["permissions"].(map[string]any)
	if writes, _ := perms["writes"].([]any); len(writes) != 1 ||
		storedReferencePath(writes[0]) != vocabulary.RecordPath(kindKind, vocabulary.KindRecordPatchRequest) {
		t.Fatalf("propose permissions = %v", rows[vocabulary.HostFunctionPropose].Properties["permissions"])
	}
	if _, has := rows[vocabulary.HostFunctionMutate].Properties["permissions"]; has {
		t.Fatal("mutate declares a grant of its own; the ceiling is the calling agent's")
	}
}

// THE PROPOSE CARD'S OP ENUM IS THE DISPATCHER'S OWN VOCABULARY. The three values
// moved out of a Go literal into the declaration, where nothing type-checks them
// against the constants the dispatcher branches on — so this does.
func TestProposeCardOpsMatchTheDispatcher(t *testing.T) {
	ds := openInternalDataset(t)
	fn, err := ds.registry().ResolveFunction(vocabulary.HostFunctionPropose)
	if err != nil {
		t.Fatal(err)
	}
	props, _ := fn.Input["properties"].(map[string]any)
	op, _ := props["op"].(map[string]any)
	values, _ := op["enum"].([]any)
	want := []any{opPatch, opCreate, opDelete}
	if len(values) != len(want) {
		t.Fatalf("the propose card's op enum is %v, want %v", values, want)
	}
	for i, v := range want {
		if values[i] != v {
			t.Fatalf("the propose card's op enum is %v, want %v", values, want)
		}
	}
}

// AN EXISTING REPOSITORY GETS THEM AT THE NEXT OPEN. The repository is created by
// a tree that ships neither the four declarations nor the `host` value in the
// function kind's runtime enum, so the upgrade has to do two things in ONE
// projection: re-project the function kind (its version moved) and append four
// rows whose `runtime` only the NEW declaration admits. The second can only land
// because a projected row is held to the declaration the projection is installing
// (M1); against the stored one it is refused and the open fails.
func TestBootUpgradeDeliversTheHostFunctions(t *testing.T) {
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	openWith := func(dir string) substrate.Service {
		svc, err := Open(ctx, dsn, WithCredentialKey(TestCredentialKey), WithKindsDir(dir))
		if err != nil {
			t.Fatalf("open with %s: %v", dir, err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc
	}

	// The tree as it stood before host functions existed.
	svc := openWith(preHostKindsDir(t))
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	dsOld, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range hostFunctionIDs {
		if _, err := dsOld.Get(ctx, kindFunction, id); err == nil {
			t.Fatalf("the pre-host tree already seeded %s", id)
		}
	}
	_ = svc.Close()

	// This binary's tree: the open runs the upgrade.
	svc2 := openWith("../../kinds/core.substrate.reamde.dev")
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("the boot upgrade could not deliver the host functions: %v", err)
	}
	for _, id := range hostFunctionIDs {
		row, err := ds2.Get(ctx, kindFunction, id)
		if err != nil {
			t.Fatalf("the upgrade did not deliver %s: %v", id, err)
		}
		if rt, _ := row.Properties["runtime"].(string); rt != vocabulary.RuntimeHost {
			t.Fatalf("%s runtime = %v", id, row.Properties["runtime"])
		}
	}
	// The enum bump landed on the STORED declaration with them, which is what
	// admitted the rows.
	decl, err := ds2.Get(ctx, kindKind, kindFunction)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toJSONString(t, decl.Properties["properties"]), `"host"`) {
		t.Fatalf("the stored runtime enum does not admit host: %v", decl.Properties["properties"])
	}
}

// preHostKindsDir writes core's tree as the binary before this one shipped it:
// no host function declarations, and a function kind whose runtime enum has no
// `host` value and whose `source` is required.
func preHostKindsDir(t *testing.T) string {
	t.Helper()
	const src = "../../kinds/core.substrate.reamde.dev"
	dir := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" || e.Name() == "hostfunctions.yaml" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		body := string(raw)
		if e.Name() == "function.yaml" {
			body = strings.Replace(body, "  version: 13\n", "  version: 4\n", 1)
			body = strings.Replace(body, "        - go\n        - host\n", "        - go\n", 1)
			body = strings.Replace(body,
				"      fts: false\n      description: the inline body, on an inline runtime\n",
				"      fts: false\n      required: true\n      description: the inline body\n", 1)
			// The rewrite is a fixture, so it says so when the declaration moves out
			// from under it instead of silently testing today's tree twice.
			for _, want := range []string{"version: 4", "required: true"} {
				if !strings.Contains(body, want) {
					t.Fatalf("the pre-host function.yaml rewrite missed %q", want)
				}
			}
			if strings.Contains(body, "- host") {
				t.Fatal("the pre-host function.yaml still admits the host runtime")
			}
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// THE DIRECT CALL API SPLITS THE FOUR. `graphql` and `query` are reads and the
// caller is a token that owns the repository, so they answer in process; `propose`
// and `mutate` are bounded by the CALLING AGENT's grants, and a direct call has no
// calling agent to be bounded by, so they refuse and name where the tool works.
func TestCallFunctionOnHostFunctions(t *testing.T) {
	ctx := context.Background()
	ds := openInternalDataset(t)
	importVocabulary(t, ds, "tasks")
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: "tasks.substrate.reamde.dev/task", ID: "t-1",
		Properties: map[string]any{"name": "written by hand"},
	}); err != nil {
		t.Fatal(err)
	}

	out, effects, err := ds.CallFunction(ctx, vocabulary.HostFunctionGraphQL, map[string]any{
		"query": `{ record(kind: "tasks.substrate.reamde.dev/task", id: "t-1") { id } }`,
	})
	if err != nil {
		t.Fatalf("graphql under a token: %v", err)
	}
	if effects != 0 {
		t.Fatalf("a read applied %d effects", effects)
	}
	if !strings.Contains(toJSONString(t, out), "t-1") {
		t.Fatalf("the graphql answer does not hold the record: %v", out)
	}

	out, _, err = ds.CallFunction(ctx, vocabulary.HostFunctionQuery, map[string]any{
		"kind": "tasks.substrate.reamde.dev/task",
	})
	if err != nil {
		t.Fatalf("query under a token: %v", err)
	}
	if !strings.Contains(toJSONString(t, out), "t-1") {
		t.Fatalf("the query answer does not hold the record: %v", out)
	}

	// Each refusal is reached with arguments its own card admits, so what refuses
	// is the missing grant and not the shape.
	for id, args := range map[string]map[string]any{
		vocabulary.HostFunctionPropose: {"op": "patch", "kind": "tasks.substrate.reamde.dev/task", "target": "t-1"},
		vocabulary.HostFunctionMutate:  {"query": `mutation { delete(kind: "tasks.substrate.reamde.dev/task", id: "t-1") { id } }`},
	} {
		_, _, err := ds.CallFunction(ctx, id, args)
		if !errors.Is(err, substrate.ErrForbidden) {
			t.Fatalf("%s answered a direct call: %v", id, err)
		}
		if !strings.Contains(err.Error(), id) || !strings.Contains(err.Error(), "agent") {
			t.Fatalf("%s must refuse by name and point at agents: %v", id, err)
		}
	}

	// The card is enforced on the way in, like every other function's, because it
	// is an ordinary declared `arguments:` list.
	if _, _, err := ds.CallFunction(ctx, vocabulary.HostFunctionGraphQL,
		map[string]any{"nonsense": true}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("an undeclared argument was admitted: %v", err)
	}
}

// A TRIGGER MAY NOT TARGET ONE. The engine runs a host function under the grants
// of whoever called it, and a delivery has no caller to borrow from — so the row
// is refused at admission, naming the shape that works, instead of parking
// forever.
func TestTriggerRefusesAHostFunctionCallable(t *testing.T) {
	ctx := context.Background()
	ds := openInternalDataset(t)
	importVocabulary(t, ds, "tasks")
	_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger, ID: "on-task-query",
		Properties: map[string]any{
			"enabled": true,
			"source": map[string]any{
				"record": map[string]any{"kinds": []any{"tasks.substrate.reamde.dev/task"}, "ops": []any{"create"}},
			},
			"callable": vocabulary.RecordPath(kindFunction, vocabulary.HostFunctionQuery),
		},
	})
	if !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a trigger targeting a built-in landed: %v", err)
	}
	if !strings.Contains(err.Error(), "declare an agent carrying it as a tool") {
		t.Fatalf("the refusal must name the shape that works: %v", err)
	}
}

// A HOST FUNCTION NEVER REACHES THE RUNNER. Every door branches before it, so
// this is the backstop: arriving is a bug in the callers, and it says so as an
// internal error rather than a user's validation failure.
func TestRunCallableRefusesAHostFunction(t *testing.T) {
	ctx := context.Background()
	ds := openInternalDataset(t)
	fn, err := ds.registry().ResolveFunction(vocabulary.HostFunctionQuery)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ds.runCallableRaw(ctx, fn, runner.Input{Mode: runner.ModeCall}); err == nil ||
		!strings.Contains(err.Error(), "reached the runner") {
		t.Fatalf("the runner backstop did not fire: %v", err)
	}
}

// THE QUERY BUILT-IN, DISPATCHED BY REFERENCE, END TO END. The librarian names
// `core.substrate.reamde.dev/query` under `callable:` like any other function and
// carries the `reads:` that grants it; the loop's card is the declaration's, and
// the read is still held to the allowlist.
func TestAgentQueryBuiltinByReference(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-shelved",
		Properties: map[string]any{"name": "shelved"},
	}); err != nil {
		t.Fatal(err)
	}
	fake.script("lib",
		fakeTurn{calls: []fakeCall{{"query", gqlToolArgs(t, map[string]any{
			"kind": crewAuthority + "/widget",
		})}}},
		// A kind OUTSIDE the reads allowlist refuses by naming it.
		fakeTurn{calls: []fakeCall{{"query", gqlToolArgs(t, map[string]any{
			"kind": "tasks.substrate.reamde.dev/task",
		})}}},
		fakeTurn{content: "one widget on the shelf"},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/librarian", "what is on the shelf")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Status != threadOK || res.Reply != "one widget on the shelf" {
		t.Fatalf("result: %+v", res)
	}
	var contents []string
	for _, m := range threadMessages(t, ds, res.Thread) {
		if m["role"] == "tool" {
			content, _ := m["content"].(string)
			contents = append(contents, content)
		}
	}
	if len(contents) != 2 {
		t.Fatalf("tool rows: %v", contents)
	}
	if !strings.Contains(contents[0], "w-shelved") {
		t.Fatalf("the query answer does not carry the row: %s", contents[0])
	}
	if !strings.Contains(contents[1], "not in the reads allowlist") {
		t.Fatalf("the scoped read did not hold: %s", contents[1])
	}
	// The card the model was shown is the DECLARATION's, not a Go literal.
	reqs := fake.requestsOf("lib")
	if len(reqs) == 0 {
		t.Fatal("no completion request reached the fake")
	}
	if !strings.Contains(toJSONString(t, reqs[0]["tools"]), "Records are always addressed by their full reference") {
		t.Fatalf("the query card is not the declaration's: %v", reqs[0]["tools"])
	}
}

// A MIS-SHAPED FILTER IS REFUSED, NOT DROPPED. `query` decoded its filter
// leniently, so a model that wrote a predicate straight onto the filter
// (`{"at": {"gte": …}}` instead of nesting it under `properties`) had the key
// silently discarded and got the WHOLE collection back as if it had asked for
// it — a wrong answer with no sign anything went wrong. The strict decode the
// GraphQL doors already used refuses it, and names the keys so the model can
// correct itself in the next turn.
func TestAgentQueryRefusesAnUnknownFilterKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	for _, id := range []string{"w-mon", "w-tue"} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: crewAuthority + "/widget", ID: id, Properties: map[string]any{"name": id},
		}); err != nil {
			t.Fatal(err)
		}
	}
	fake.script("lib",
		fakeTurn{calls: []fakeCall{{"query", gqlToolArgs(t, map[string]any{
			"kind":   crewAuthority + "/widget",
			"filter": map[string]any{"at": map[string]any{"gte": "2026-08-15T00:00:00Z"}},
		})}}},
		fakeTurn{content: "asked wrong"},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/librarian", "what is on the shelf today")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	tool := lastToolMessage(t, ds, res.Thread)
	if tool["ok"] == true {
		t.Fatalf("the dropped predicate answered as a success: %v", tool["content"])
	}
	content, _ := tool["content"].(string)
	if strings.Contains(content, "w-mon") {
		t.Fatalf("the filter was dropped and the collection answered anyway: %s", content)
	}
	// The content is the JSON tool result, so the quotes around the key
	// arrive escaped.
	for _, want := range []string{`unknown field \"at\"`, "properties"} {
		if !strings.Contains(content, want) {
			t.Fatalf("the refusal does not mention %q: %s", want, content)
		}
	}
}

// A PURE FUNCTION IS A TOOL. It declares no emit, writes nothing and returns an
// output the model reads — the shape `emit:` being required used to forbid.
func TestAgentPureFunctionTool(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("pure",
		fakeTurn{calls: []fakeCall{{"measure", gqlToolArgs(t, map[string]any{"name": "widget"})}}},
		fakeTurn{content: "six"},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/purist", "measure widget")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Status != threadOK || res.Effects != 0 {
		t.Fatalf("result: %+v", res)
	}
	tool := lastToolMessage(t, ds, res.Thread)
	if tool["ok"] != true {
		t.Fatalf("the pure tool failed: %v", tool["content"])
	}
	if content, _ := tool["content"].(string); !strings.Contains(content, `"length":6`) {
		t.Fatalf("the pure tool's output did not reach the model: %s", content)
	}
}

// A TRIGGER WHOSE CALLABLE CAN CHANGE NOTHING IS A WARNING, NOT A REFUSAL. A
// delivery discards the output, so a body with no emit, no call and no network is
// almost certainly a mistake — but a pure function is legal now, and a trigger the
// engine refused would have to be re-created rather than fixed.
func TestTriggerWarnsWhenTheOutputIsDiscarded(t *testing.T) {
	ctx := context.Background()
	var logs syncBuffer
	ds := openInternalDataset(t, WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	docs := []map[string]any{
		vocabulary.AuthorityManifest("pure.test.dev", 0),
		vocabulary.KindManifest("pure.test.dev", map[string]any{"singular": "gizmo", "plural": "gizmos"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		// One writes, one cannot: the pair is what makes the warning a signal.
		vocabulary.FunctionManifest("pure.test.dev", "counter", map[string]any{
			"description": "counts gizmos and returns the count, writing nothing",
			"runtime":     vocabulary.RuntimePython,
			"source":      "def main(input, host): return {\"output\": {\"n\": 1}}",
		}),
		vocabulary.FunctionManifest("pure.test.dev", "stamper", map[string]any{
			"description": "stamps a gizmo",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{"pure.test.dev/gizmo"}},
			"source":      "def main(input, host): return {\"effects\": []}",
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install the pure authority: %v", err)
	}
	trigger := func(id, fn string) {
		t.Helper()
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: typeTrigger, ID: id,
			Properties: map[string]any{
				"enabled": true,
				"source": map[string]any{
					"record": map[string]any{"kinds": []any{"pure.test.dev/gizmo"}, "ops": []any{"create"}},
				},
				"callable": vocabulary.RecordPath(kindFunction, fn),
			},
		}); err != nil {
			t.Fatalf("trigger %s: %v", id, err)
		}
	}
	trigger("on-gizmo-stamp", "pure.test.dev/stamper")
	if got := logs.String(); strings.Contains(got, "output is discarded") {
		t.Fatalf("a granted callable warned: %s", got)
	}
	trigger("on-gizmo-count", "pure.test.dev/counter")
	got := logs.String()
	if !strings.Contains(got, "output is discarded") || !strings.Contains(got, "pure.test.dev/counter") {
		t.Fatalf("the discarded-output warning did not fire: %s", got)
	}
}

// syncBuffer is a log sink a test can read while background loops still write.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func toJSONString(t *testing.T, v any) string {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func anyMap[V any](m map[string]V) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
