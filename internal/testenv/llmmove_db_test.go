package testenv_test

// The move of the agent runtime's four kinds out of core and into the seeded
// `substrate.reamde.dev/llm` package (decision records 0077 and 0078), seen
// from the one side that cannot be rehearsed anywhere else: a repository that
// was seeded BEFORE the move, holding the rows and the references an agent
// leaves behind, opened by a binary that has it.
//
// A MOVE IS ORDINARY RECORD WRITES. The rows arrive under the new kind with
// the same ids and the same properties, every reference that named an old kind
// names the new one, the old kinds stay declared and empty, and a second boot
// does nothing at all. Nothing here is a repoint by hand: a repository that
// ever ran an agent must upgrade unattended or it is stuck forever, because
// the shipped projection is all-or-nothing and one refused guard withholds the
// whole seed.
//
// The previous tree is a FIXED COPY under testdata, not this tree with the
// move reversed: a fixture derived from the current declarations tracks them,
// so the day one of them changes the drill would quietly stop testing the
// upgrade it exists to test.

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/testenv"
	"github.com/geoah/substrate/kinds"
)

// preMoveKindsDir is the seed authority exactly as the binary before the move
// shipped it: core with the four `llm*` kinds in it and no llm package.
const preMoveKindsDir = "testdata/kinds-before-llm-move"

// movedLLMKinds is each local name after the move beside the one before it.
var movedLLMKinds = []struct{ now, before string }{
	{"provider", "llmprovider"},
	{"thread", "llmthread"},
	{"message", "llmmessage"},
	{"interaction", "llminteraction"},
}

// preMoveAgentPackage is a repository's own closure, declared on the OLD tree:
// one agent, whose `provider` reference is stored at `core/llmprovider` and is
// the reference the move has to repoint. Without the repoint the new
// declaration's pin refuses the upgrade with a live-row count, forever.
const preMoveAgentPackage = `
kind: substrate.reamde.dev/core/package
metadata:
  id: ada.example.com/crew
data:
  authority: ada.example.com
  package: crew
  version: 1
---
kind: substrate.reamde.dev/core/agent
metadata:
  id: ada.example.com/crew/scribe
data:
  authority: ada.example.com
  package: crew
  description: the agent whose provider reference the move repoints
  prompt: you write things down.
  provider: openai
  model: gpt-5
  budgets:
    maxTurns: 2
    deadlineSeconds: 60
---
kind: substrate.reamde.dev/core/agent
metadata:
  id: ada.example.com/crew/asker
data:
  authority: ada.example.com
  package: crew
  description: the ask agent whose stored grant names the pre-move kind
  prompt: you ask before you act.
  provider: openai
  model: gpt-5
  tools:
    - function: substrate.reamde.dev/core/ask
  permissions:
    writes:
      - substrate.reamde.dev/core/llminteraction
  budgets:
    maxTurns: 2
    deadlineSeconds: 60
`

func TestBootUpgradeMovesTheLLMRowsOutOfCore(t *testing.T) {
	dsn := testdb.NewSchema(t)
	dataRoot := t.TempDir()
	key := testenv.MintCredentialKey()

	// --- binary N: the tree before the move --------------------------------
	before := testenv.Start(t,
		testenv.WithDSN(dsn), testenv.WithDataRoot(dataRoot), testenv.WithCredentialKey(key),
		testenv.WithKindsDir(preMoveKindsDir))
	seeded := kindVersions(t, before)
	for _, k := range movedLLMKinds {
		if _, ok := seeded[corePkg+"/"+k.before]; !ok {
			t.Fatalf("the pre-move tree seeded no %s/%s", corePkg, k.before)
		}
		if _, ok := seeded[llmPkg+"/"+k.now]; ok {
			t.Fatalf("the pre-move tree already declares %s/%s; it is not a pre-move tree", llmPkg, k.now)
		}
	}

	// The rows an agent leaves behind, written under the old kinds: a provider,
	// a thread of two messages, an interaction, and a change request whose
	// `thread` reference the move repoints beside the agent's `provider`.
	put := func(kind, id string, props map[string]any) {
		t.Helper()
		if status, body := before.Do(http.MethodPut, "/api/v1/"+kind+"/"+id,
			map[string]any{"properties": props}); status/100 != 2 {
			t.Fatalf("write %s/%s on the pre-move tree: %d %s", kind, id, status, body)
		}
	}
	put(corePkg+"/llmprovider", "openai", map[string]any{
		"label": "the old row", "wire": "openai", "apiKey": "sk-the-original",
	})
	before.ApplyVocabularyYAML(preMoveAgentPackage)
	put(corePkg+"/llmthread", "th-1", map[string]any{
		"agent": "ada.example.com/crew/scribe", "provider": "openai", "model": "gpt-5",
		"mode": "chat", "status": "ok", "agentDepth": 0, "startedAt": "2026-01-01T00:00:00Z",
	})
	// Three threads chained by `parent`, written CHILD FIRST: the move orders
	// rows by their own references, not by kind, so a chain that arrives in
	// reverse insertion order still lands parent-before-child.
	for _, chained := range []struct{ id, parent string }{
		{"th-c", "th-b"}, {"th-b", "th-a"}, {"th-a", ""},
	} {
		props := map[string]any{
			"agent": "ada.example.com/crew/scribe", "provider": "openai", "model": "gpt-5",
			"mode": "subagent", "status": "ok", "agentDepth": 1, "startedAt": "2026-01-01T00:00:00Z",
		}
		if chained.parent != "" {
			props["parent"] = chained.parent
		}
		put(corePkg+"/llmthread", chained.id, props)
	}
	put(corePkg+"/llmmessage", "m-1", map[string]any{
		"role": "user", "turn": 0, "content": "hello", "thread": "th-1",
	})
	put(corePkg+"/llmmessage", "m-2", map[string]any{
		"role": "assistant", "turn": 1, "content": "hi", "thread": "th-1",
	})
	put(corePkg+"/llminteraction", "i-1", map[string]any{
		"thread":    "th-1",
		"questions": []any{map[string]any{"id": "q1", "prompt": "ready?"}},
	})
	// A REPOSITORY THAT HAS NOT TAKEN THE MOVE CANNOT BE WRITTEN TO THROUGH A
	// THREAD. The agent loop addresses its kinds by constant, so a `notifies:`
	// transition here would put a turn into a dormant kind under ordinals
	// nothing else counts: it refuses instead, naming the withheld upgrade
	// (record 0078). This is the one write that can reach the loop before the
	// boot upgrade lands, and resolving an interaction is how it gets there.
	var pending map[string]any
	before.MustJSON(http.MethodGet, "/api/v1/"+corePkg+"/llminteraction/i-1", nil, &pending)
	status, raw := before.Do(http.MethodPatch, "/api/v1/"+corePkg+"/llminteraction/i-1", map[string]any{
		"properties": map[string]any{
			"state":   "answered",
			"answers": []any{map[string]any{"question": "q1", "selected": []any{"yes"}}},
		},
		"ifVersion": pending["version"],
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("resolving a thread-bound record before the upgrade returned %d %s, want a refusal", status, raw)
	}
	if !strings.Contains(string(raw), "does not declare substrate.reamde.dev/llm/thread") {
		t.Fatalf("the refusal does not name the withheld upgrade: %s", raw)
	}
	put(corePkg+"/recordpatchrequest", "r-1", map[string]any{
		"op": "patch", "targetKind": corePkg + "/llmprovider", "targetId": "openai",
		"diff":      map[string]any{"properties": map[string]any{"label": "renamed"}},
		"rationale": "the request whose thread reference the move repoints",
		"thread":    "th-1",
	})
	session := before.Session
	before.Stop()

	// --- binary N+1: this tree, the move landed ----------------------------
	after := testenv.Start(t,
		testenv.WithDSN(dsn), testenv.WithDataRoot(dataRoot), testenv.WithCredentialKey(key),
		testenv.WithoutRegistration())
	after.Session = session

	upgraded := kindVersions(t, after)
	// The seed landed WHOLE, at the version this tree ships. A guard refused
	// anywhere withholds the entire shipped projection, so core arriving at the
	// shipped number is the proof that the llm package's arrival was not bought
	// by skipping the rest.
	wantCore := shippedKindVersion(t, "core", "kind.yaml")
	if got := upgraded[corePkg+"/kind"]; got != wantCore {
		t.Fatalf("core/kind is at version %d after the upgrade, want the shipped %d", got, wantCore)
	}
	for _, k := range movedLLMKinds {
		if _, ok := upgraded[llmPkg+"/"+k.now]; !ok {
			t.Errorf("the boot upgrade did not land %s/%s", llmPkg, k.now)
		}
		// The old kind stays DECLARED, dormant and never pruned (record 0055).
		if _, ok := upgraded[corePkg+"/"+k.before]; !ok {
			t.Errorf("the boot upgrade pruned %s/%s; it must stay declared", corePkg, k.before)
		}
	}

	// EVERY ROW MOVED, same id, same properties.
	moved := map[string]map[string]map[string]any{
		llmPkg + "/provider":    {"openai": {"label": "the old row", "wire": "openai"}},
		llmPkg + "/thread":      {"th-1": {"model": "gpt-5", "status": "ok"}},
		llmPkg + "/message":     {"m-1": {"content": "hello"}, "m-2": {"content": "hi"}},
		llmPkg + "/interaction": {"i-1": {"state": "pending"}},
	}
	for kind, rows := range moved {
		for id, want := range rows {
			var rec map[string]any
			after.MustJSON(http.MethodGet, "/api/v1/"+kind+"/"+id, nil, &rec)
			for name, value := range want {
				if got := propOf(t, rec, id, name); got != value {
					t.Errorf("%s/%s: %s = %v, want %v", kind, id, name, got, value)
				}
			}
		}
	}
	// And the old kinds are EMPTY: the rows moved, they were not copied.
	for _, k := range movedLLMKinds {
		if rows := listRecords(t, after, corePkg+"/"+k.before, false); len(rows) != 0 {
			t.Errorf("%s/%s still holds %d live rows; the move carries them, it does not copy them",
				corePkg, k.before, len(rows))
		}
	}

	// EVERY REFERENCE FOLLOWED. The agent's provider and the request's thread
	// are the two the narrowing guards would otherwise have refused the whole
	// upgrade over.
	// Read through the COLLECTION, not the record path: an agent's id carries
	// the slashes of its own package reference, which no four-segment record
	// path can address.
	wantRef := func(kind, id, prop, want string) {
		t.Helper()
		for _, raw := range listRecords(t, after, kind, false) {
			rec, _ := raw.(map[string]any)
			if rec["id"] != id {
				continue
			}
			got, _ := propOf(t, rec, id, prop).(map[string]any)
			if got["ref"] != want {
				t.Errorf("%s/%s: %s = %v, want a reference at %s", kind, id, prop, got["ref"], want)
			}
			return
		}
		t.Errorf("%s holds no record %q", kind, id)
	}
	wantRef(corePkg+"/agent", "ada.example.com/crew/scribe", "provider", llmPkg+"/provider/openai")
	wantRef(corePkg+"/recordpatchrequest", "r-1", "thread", llmPkg+"/thread/th-1")
	wantRef(llmPkg+"/message", "m-1", "thread", llmPkg+"/thread/th-1")
	wantRef(llmPkg+"/message", "m-2", "thread", llmPkg+"/thread/th-1")
	wantRef(llmPkg+"/interaction", "i-1", "thread", llmPkg+"/thread/th-1")
	wantRef(llmPkg+"/thread", "th-1", "agent", corePkg+"/agent/ada.example.com/crew/scribe")

	// THE MOVED THREAD IS THE AGENT LOOP'S AGAIN: the transcript the loop reads
	// is a filter on the message kind by the thread it points at, which is the
	// read that saw nothing at all while the rows sat under the old kind.
	var page map[string]any
	after.MustJSON(http.MethodGet, listPath(llmPkg+"/message", map[string]any{
		"properties": map[string]any{"thread": map[string]any{"eq": "th-1"}},
	}), nil, &page)
	records, _ := page["records"].([]any)
	if len(records) != 2 {
		t.Fatalf("the moved thread's history is %d messages, want the 2 it was written with", len(records))
	}

	// A STATE travels with the row: it is neither a property nor a column, so a
	// move that copied only props would land every interaction back at its
	// initial state.
	var interaction map[string]any
	after.MustJSON(http.MethodGet, "/api/v1/"+llmPkg+"/interaction/i-1", nil, &interaction)
	if got := propOf(t, interaction, "i-1", "state"); got != "pending" {
		t.Errorf("the moved interaction's state = %v, want the one it carried", got)
	}
	// The chain moved whole, each `parent` naming the moved kind.
	for _, chained := range []struct{ id, parent string }{{"th-c", "th-b"}, {"th-b", "th-a"}} {
		wantRef(llmPkg+"/thread", chained.id, "parent", llmPkg+"/thread/"+chained.parent)
	}
	// The property managers followed the values: who last wrote one, at which
	// tier and behind which token, is a fact about the value and not about the
	// hand that carried it.
	if got := managerOf(t, dsn, after.Repository, llmPkg+"/provider", "openai", "label"); got.actor == "" {
		t.Error("the moved provider's `label` has no manager; the move dropped it")
	} else if got != managerOf(t, dsn, after.Repository, corePkg+"/llmprovider", "openai", "label") {
		t.Errorf("the moved manager is %+v, want the one the old row carried", got)
	}
	// The provider's key is re-sealed under its new owner, so it still opens.
	// Every read surface redacts it, which is what proves it is sealed and not
	// the reference string carried across as plaintext.
	var provider map[string]any
	after.MustJSON(http.MethodGet, "/api/v1/"+llmPkg+"/provider/openai", nil, &provider)
	if got := propOf(t, provider, "openai", "apiKey"); got != "<redacted>" {
		t.Errorf("the moved apiKey reads back %v, want the redaction sentinel", got)
	}
	// The stored ask agent's grant names the NEW kind: nothing else rewrites a
	// kind reference spelled as a plain string, and the loader would quarantine
	// the package at the next open if it still named the old one.
	for _, raw := range listRecords(t, after, corePkg+"/agent", false) {
		rec, _ := raw.(map[string]any)
		if rec["id"] != "ada.example.com/crew/asker" {
			continue
		}
		perms, _ := propOf(t, rec, "asker", "permissions").(map[string]any)
		writes, _ := perms["writes"].([]any)
		// A grant is stored as a REFERENCE at the kind declaration record, so
		// what must have moved is the id inside it.
		if len(writes) != 1 {
			t.Fatalf("the ask agent holds %d write grants, want one", len(writes))
		}
		granted, _ := writes[0].(map[string]any)
		if granted["ref"] != corePkg+"/kind/"+llmPkg+"/interaction" {
			t.Errorf("the ask agent's grant is %v, want the moved interaction kind", granted["ref"])
		}
	}

	head := changelogHead(t, after)
	after.Stop()

	// --- binary N+1 again: the move is IDEMPOTENT --------------------------
	third := testenv.Start(t,
		testenv.WithDSN(dsn), testenv.WithDataRoot(dataRoot), testenv.WithCredentialKey(key),
		testenv.WithoutRegistration())
	third.Session = session
	if got := changelogHead(t, third); got != head {
		t.Fatalf("a second boot moved the changelog from %d to %d; the move must find nothing to do", head, got)
	}

	// THE CHANGELOG IS THE TRUTH. A move that wrote the records table something
	// its own entries do not reproduce would pass every assertion above and
	// fail the first rebuild, so the fold is rebuilt from the segment files and
	// compared against the live one.
	third.Stop()
	rebuildAndCompare(t, dsn, dataRoot, key, after.Repository)
}

// changelogHead is the repository's changelog head, as any list page reports
// it (the list-to-watch handoff): the number a second boot must not move.
func changelogHead(t *testing.T, e *testenv.Env) int64 {
	t.Helper()
	var page struct {
		Head int64 `json:"head"`
	}
	e.MustJSON(http.MethodGet, listPath(corePkg+"/kind", nil)+"&first=1", nil, &page)
	return page.Head
}

// The snapshot is a fixture, so it says so when it stops being one: a tree
// that no longer holds the old kinds tests nothing.
func TestThePreMoveTreeStillHoldsTheOldKinds(t *testing.T) {
	for _, k := range movedLLMKinds {
		path := filepath.Join(preMoveKindsDir, "core", k.before+".yaml")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("the pre-move snapshot no longer holds %s: %v", path, err)
		}
	}
}

// shippedKindVersion is the version this tree's own declaration pins, read off
// the file: the number the boot upgrade must reach, rather than "not zero".
func shippedKindVersion(t *testing.T, pkg, file string) int64 {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(seedAuthorityDir, pkg, file))
	if err != nil {
		t.Fatalf("read the shipped %s/%s: %v", pkg, file, err)
	}
	m := reDeclaredVersion.FindSubmatch(raw)
	if m == nil {
		t.Fatalf("the shipped %s/%s pins no version of its own", pkg, file)
	}
	var v int64
	if _, err := fmt.Sscan(string(m[1]), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// rebuildAndCompare replays the repository's changelog from its segment files
// into a fresh fold and asserts the records table is unchanged. A move writes
// puts, deletes and patches like any other hand, so the rebuild must reproduce
// every one of them.
func rebuildAndCompare(t *testing.T, dsn, dataRoot, key, repository string) {
	t.Helper()
	ctx := context.Background()
	svc, err := engine.Open(ctx, dsn,
		engine.WithKindsFS(kinds.Seed()),
		engine.WithDataRoot(dataRoot),
		engine.WithCredentialKey(key))
	if err != nil {
		t.Fatalf("open the engine for the rebuild: %v", err)
	}
	defer func() { _ = svc.Close() }()
	before := foldDigest(t, dsn, repository)
	rebuilder, ok := svc.(engine.Rebuilder)
	if !ok {
		t.Fatal("the engine no longer offers the rebuild seam")
	}
	if _, err := rebuilder.RebuildRepository(ctx, repository); err != nil {
		t.Fatalf("rebuild %s: %v", repository, err)
	}
	if after := foldDigest(t, dsn, repository); after != before {
		t.Fatal("the rebuilt fold differs from the live one: the move wrote the records table something its own entries do not reproduce")
	}
}

// foldDigest is every record's kind, id, properties, title and liveness,
// ordered, as one string: what a rebuild must reproduce exactly.
func foldDigest(t *testing.T, dsn, repository string) string {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open the raw pool: %v", err)
	}
	defer func() { _ = db.Close() }()
	// EVERY column but the four a rebuild is entitled to differ on:
	//   - `ctid`/`xmin` and friends are Postgres', not the fold's;
	//   - `created_at`/`updated_at`/`deleted_at` are wall-clock stamps the
	//     replay re-derives from the entries' own timestamps, which is the
	//     same value only to the precision the segment file round-trips;
	//   - `fts` is derived from the row AND the declaration at fold time
	//     (AGENTS.md's one exception to "the changelog is the truth"), so a
	//     rebuild under a newer declaration may index differently by design.
	// What is left is what the entries must reproduce exactly.
	rows, err := db.Query(
		`SELECT kind, id, coalesce(props::text, ''), coalesce(title, ''),
		        coalesce(body, ''), coalesce(labels::text, ''), version, kind_version,
		        deleted_at IS NOT NULL, coalesce(array_to_string(finalizers, ','), ''),
		        coalesce(at::text, ''), coalesce(ends_at::text, ''), coalesce(due_at::text, ''),
		        coalesce(states::text, '')
		 FROM records WHERE repository = $1 ORDER BY kind, id`, repository)
	if err != nil {
		t.Fatalf("read the fold: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var b strings.Builder
	for rows.Next() {
		var kind, id, props, title, body, labels, finalizers, at, endsAt, dueAt, states string
		var version, kindVersion int64
		var deleted bool
		if err := rows.Scan(&kind, &id, &props, &title, &body, &labels, &version, &kindVersion,
			&deleted, &finalizers, &at, &endsAt, &dueAt, &states); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%d\x1f%d\x1f%t\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\n",
			kind, id, props, title, body, labels, version, kindVersion, deleted, finalizers, at, endsAt, dueAt, states)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// manager is one property's manager row, as the move must carry it.
type manager struct {
	actor, tier, principal string
}

// managerOf reads one property's manager row straight from the ledger: the
// move's promise is that all three columns travel with the value.
func managerOf(t *testing.T, dsn, repository, kind, id, property string) manager {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open the raw pool: %v", err)
	}
	defer func() { _ = db.Close() }()
	var m manager
	err = db.QueryRow(
		`SELECT actor, tier, coalesce(principal, '') FROM property_managers
		 WHERE repository = $1 AND record_kind = $2 AND record_id = $3 AND property = $4`,
		repository, kind, id, property).Scan(&m.actor, &m.tier, &m.principal)
	if err != nil {
		return manager{}
	}
	return m
}
