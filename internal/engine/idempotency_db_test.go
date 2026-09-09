package engine_test

// The Idempotency-Key contract (docs/api.md "Idempotency and retries"): a
// create without an id, a function call, a merge and a split run once under
// a key and answer the stored outcome on a repeat; the same key with another
// body is refused; the key binds to the repository, not the token, and
// survives a reopen of the same database.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

func keyed(key string) context.Context {
	return substrate.WithIdempotencyKey(context.Background(), key)
}

func countKind(t *testing.T, ds substrate.Dataset, kind string) int {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{kind}}, First: 500,
	})
	if err != nil {
		t.Fatalf("list %s: %v", kind, err)
	}
	return len(page.Records)
}

func TestIdempotencyKeyCreateRunsOnce(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	before := countKind(t, ds, taskType)
	in := substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "once"}}

	first, err := ds.Put(keyed("create-1"), owner, in)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := ds.Put(keyed("create-1"), owner, in)
	if err != nil {
		t.Fatalf("repeat: %v", err)
	}
	// The stored outcome: the record as the first attempt answered it, id
	// and version alike, so the HTTP layer's 201 holds on the repeat.
	if second.ID != first.ID || second.Version != first.Version || second.Properties["name"] != "once" {
		t.Fatalf("repeat answered %+v, want the first attempt's %+v", second, first)
	}
	if got := countKind(t, ds, taskType); got != before+1 {
		t.Fatalf("two creates under one key left %d records, want %d", got, before+1)
	}

	// The same key with a different body is a conflict, and creates nothing.
	_, err = ds.Put(keyed("create-1"), owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "other"}})
	wantErr(t, err, substrate.ErrConflict, "same key, different body")
	if !strings.Contains(err.Error(), "different request") {
		t.Fatalf("the conflict does not say why: %v", err)
	}
	if got := countKind(t, ds, taskType); got != before+1 {
		t.Fatalf("a refused repeat created a record: %d", got)
	}

	// Another key is another attempt.
	third, err := ds.Put(keyed("create-2"), owner, in)
	if err != nil {
		t.Fatalf("second key: %v", err)
	}
	if third.ID == first.ID {
		t.Fatal("a different key answered the first key's record")
	}

	// A key past the bound is refused before anything runs.
	_, err = ds.Put(keyed(strings.Repeat("k", substrate.MaxIdempotencyKeyLength+1)), owner, in)
	wantErr(t, err, substrate.ErrValidation, "oversized key")
	if got := countKind(t, ds, taskType); got != before+2 {
		t.Fatalf("an oversized key created a record: %d", got)
	}

	// A declaration write is not one of the covered operations: refused,
	// never silently run without the key.
	_, err = ds.Put(keyed("vocab-1"), owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/kind", ID: "widgets.test.dev/w/widget",
	})
	wantErr(t, err, substrate.ErrValidation, "key on a vocabulary kind")
	if !strings.Contains(err.Error(), "Idempotency-Key") {
		t.Fatalf("the refusal does not name the header: %v", err)
	}
}

func TestIdempotencyKeyMergeAndSplitReplay(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	a := mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "a"}})
	b := mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "b"}})

	c := mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "c"}})
	d := mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "d"}})

	// A CONDITIONED merge: without the key its retry fails conflict, because
	// the first attempt moved both versions; the key answers the stored
	// merge record instead.
	merge := substrate.MergeInput{Kind: taskType, Winner: a.ID, Loser: b.ID, WinnerVersion: ptr(a.Version), LoserVersion: ptr(b.Version)}
	m1, err := ds.Merge(keyed("merge-1"), owner, merge)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, err := ds.Merge(context.Background(), owner, merge); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("keyless retry of a conditioned merge: %v, want ErrConflict", err)
	}
	m2, err := ds.Merge(keyed("merge-1"), owner, merge)
	if err != nil {
		t.Fatalf("merge repeat: %v", err)
	}
	if m2.ID != m1.ID || m2.Kind != m1.Kind {
		t.Fatalf("merge repeat answered %+v, want %+v", m2, m1)
	}
	// The same key on a merge of two live records the engine would happily
	// perform: the fingerprint refuses it, and c and d stay unmerged.
	_, err = ds.Merge(keyed("merge-1"), owner, substrate.MergeInput{Kind: taskType, Winner: c.ID, Loser: d.ID})
	wantErr(t, err, substrate.ErrConflict, "same merge key, other participants")
	if got := mustGet(t, ds, taskType, d.ID); got.DeletedAt != nil {
		t.Fatalf("a refused repeat merged %s away", d.ID)
	}

	// The same for a conditioned split.
	split := substrate.SplitInput{Merge: m1.ID, IfVersion: ptr(m1.Version)}
	s1, err := ds.Split(keyed("split-1"), owner, split)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if _, err := ds.Split(context.Background(), owner, split); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("keyless retry of a conditioned split: %v, want ErrConflict", err)
	}
	s2, err := ds.Split(keyed("split-1"), owner, split)
	if err != nil {
		t.Fatalf("split repeat: %v", err)
	}
	if s2.ID != s1.ID {
		t.Fatalf("split repeat answered %s, want %s", s2.ID, s1.ID)
	}
	m3, err := ds.Merge(context.Background(), owner, substrate.MergeInput{Kind: taskType, Winner: c.ID, Loser: d.ID})
	if err != nil {
		t.Fatalf("merge c and d: %v", err)
	}
	_, err = ds.Split(keyed("split-1"), owner, substrate.SplitInput{Merge: m3.ID})
	wantErr(t, err, substrate.ErrConflict, "same split key, another merge")
	if rec, err := ds.Get(context.Background(), "substrate.reamde.dev/core/recordmerge", m3.ID); err != nil || rec.DeletedAt != nil {
		t.Fatalf("a refused repeat split %s: %+v %v", m3.ID, rec, err)
	}
	// Both records of the keyed split are live again, exactly once.
	if got := mustGet(t, ds, taskType, b.ID); got.DeletedAt != nil {
		t.Fatalf("the loser is still tombstoned after the split: %+v", got)
	}
}

// The key is looked up before admission: a repeat answers the stored outcome
// after the function's bundle was disabled, where a fresh call is refused.
func TestIdempotencyKeyAnswersAfterTheFunctionIsDisabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, ops := installMailBundle(t)
	fops := ds.(fnOps)
	args := map[string]any{}

	out, effects, err := fops.CallFunction(keyed("echo-1"), mbEchoFn, args)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if err := ops.DisableBundle(ctx, mbPackage); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, _, err := fops.CallFunction(ctx, mbEchoFn, args); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("a fresh call to the disabled function: %v", err)
	}
	again, againEffects, err := fops.CallFunction(keyed("echo-1"), mbEchoFn, args)
	if err != nil {
		t.Fatalf("repeat after the disable: %v", err)
	}
	if againEffects != effects || fmt.Sprint(again) != fmt.Sprint(out) {
		t.Fatalf("repeat answered %v/%d, want the stored %v/%d", again, againEffects, out, effects)
	}
}

// slowFn sleeps long enough for every concurrent caller to arrive while the
// first attempt holds the reservation, and names each run with a fresh id so
// the number of runs is countable.
func slowFn() map[string]any {
	return pyFn("slow", map[string]any{
		"arguments": []any{
			map[string]any{"name": "title", "type": "string", "required": true},
		},
	}, []any{taskType}, `
import time, uuid
def main(input, host):
    time.sleep(2)
    tid = "slow-" + uuid.uuid4().hex
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": tid, "properties": {"name": input["args"]["title"]}}],
            "output": {"id": tid}}
`)
}

func TestIdempotencyKeyFunctionCallRunsTheBodyOnce(t *testing.T) {
	t.Parallel()
	ds, ops := newFnDataset(t, nil, slowFn())
	args := map[string]any{"title": "concurrent"}

	const callers = 4
	outputs := make([]any, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outputs[i], _, errs[i] = ops.CallFunction(keyed("call-1"), fnPackage+"/slow", args)
		}()
	}
	wg.Wait()
	var winner any
	ok, conflicts := 0, 0
	for i := range callers {
		switch {
		case errs[i] == nil:
			ok++
			winner = outputs[i]
		case errors.Is(errs[i], substrate.ErrConflict):
			conflicts++
			if !strings.Contains(errs[i].Error(), "still running") {
				t.Fatalf("the concurrent refusal does not say the first is in flight: %v", errs[i])
			}
		default:
			t.Fatalf("caller %d: %v", i, errs[i])
		}
	}
	if ok != 1 || conflicts != callers-1 {
		t.Fatalf("%d callers succeeded and %d were refused; want 1 and %d", ok, conflicts, callers-1)
	}
	if rows := actorChanges(t, ds, fnPackage+"/slow"); len(rows) != 1 {
		t.Fatalf("the body ran %d times under one key", len(rows))
	}

	// After the first attempt settled, the repeat answers its outcome and
	// applies nothing.
	out, effects, err := ops.CallFunction(keyed("call-1"), fnPackage+"/slow", args)
	if err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if effects != 1 {
		t.Fatalf("repeat reported %d effects, want the first attempt's 1", effects)
	}
	if want, got := winner.(map[string]any)["id"], out.(map[string]any)["id"]; want != got {
		t.Fatalf("repeat answered %v, want %v", got, want)
	}
	if rows := actorChanges(t, ds, fnPackage+"/slow"); len(rows) != 1 {
		t.Fatalf("the repeat ran the body: %d runs", len(rows))
	}
	_, _, err = ops.CallFunction(keyed("call-1"), fnPackage+"/slow", map[string]any{"title": "other"})
	wantErr(t, err, substrate.ErrConflict, "same call key, different input")
}

// A key names an attempt in a repository, not a token: the token that carried
// the first attempt can be revoked, and the retry under the next login still
// matches.
func TestIdempotencyKeyOutlivesTheToken(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)
	ctx := context.Background()
	user, token, secret := registerUser(t, svc, testdb.Username(t))
	ds, _, err := svc.Authenticate(ctx, secret)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	importVocabulary(t, ds, "tasks")
	in := substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "rotated"}}

	first, err := ds.Put(substrate.WithPrincipal(keyed("rotate-1"), token.ID), owner, in)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	// Revoke: a token is a record, and its delete is the logout.
	if _, err := ds.Delete(ctx, owner, "substrate.reamde.dev/core/token", token.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, _, err := svc.Authenticate(ctx, secret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("the revoked token still authenticates: %v", err)
	}
	next, nextSecret, err := svc.Login(ctx, substrate.LoginInput{
		Username: user.username, Password: user.password, TOTPCode: user.code(t), Label: "again",
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	ds2, _, err := svc.Authenticate(ctx, nextSecret)
	if err != nil {
		t.Fatalf("authenticate the new token: %v", err)
	}
	second, err := ds2.Put(substrate.WithPrincipal(keyed("rotate-1"), next.ID), owner, in)
	if err != nil {
		t.Fatalf("retry under the new token: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("the retry under a new token created %s, want the first attempt's %s", second.ID, first.ID)
	}
}

// The key store is Postgres, so a service reopened on the same database
// answers a key the last process stored.
func TestIdempotencyKeySurvivesReopen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	svc, dsn := newService(t, engine.WithDataRoot(root))
	if _, err := svc.CreateRepository(ctx, testdb.Username(t), testdb.Authority(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	importVocabulary(t, ds, "tasks")
	in := substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "durable"}}
	first, err := ds.Put(keyed("reopen-1"), owner, in)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	again, err := engine.OpenForTest(t, ctx, dsn,
		engine.WithDataRoot(root),
		engine.WithKindsDir(engine.CoreKindsDir),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = again.Close() }()
	ds2, err := again.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("reopen dataset: %v", err)
	}
	second, err := ds2.Put(keyed("reopen-1"), owner, in)
	if err != nil {
		t.Fatalf("retry after reopen: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("the retry after a reopen created %s, want %s", second.ID, first.ID)
	}
	_, err = ds2.Put(keyed("reopen-1"), owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "changed"}})
	wantErr(t, err, substrate.ErrConflict, "same key, different body, after reopen")
}
