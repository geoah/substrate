package engine_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// windBackDialect erases the repository's schema-dialect stamp, simulating a
// store written by a pre-dialect binary. A legacy-shaped row and a
// max-dialect stamp cannot coexist in the wild (the stamp says the ladder
// already ran), so a test that hand-plants legacy content must wind the stamp
// back for the reopen to re-run the promotion steps.
func windBackDialect(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, q := range []string{`DELETE FROM vocabulary_dialect`, `DELETE FROM vocabulary_promotions`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("wind back the schema dialect: %v", err)
		}
	}
}

// Almost every test in this package calls t.Parallel(), and that is what makes
// the suite finish in a third of the time it used to: the work is mostly spent
// waiting on Postgres, so running one test at a time left the machine idle.
// What makes it SAFE is that a test shares nothing it can observe — its own
// schema (testdb.NewSchema), and its own function processes, which the runner
// keys on the repository's ID rather than the name every test happens to reuse.
//
// The exceptions are the tests that write a PACKAGE-LEVEL var: BlobUploadGrace
// (the blob GC tests) and maxPagesPerDrain / maxDrainEffects / pagedSweepGrace
// (the paged-drain tests). A global is not covered by any of the above, so
// those tests stay serial. t.Setenv is the same hazard by another name — it
// panics under t.Parallel, so nothing here reaches for it.

func newService(t *testing.T, opts ...engine.Option) (substrate.Service, string) {
	t.Helper()
	dsn := testdb.NewSchema(t)
	all := []engine.Option{
		engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"),
		// Signing is mandatory and its seed seals under this key, so every
		// test runs the keyed shape: a keyless host cannot create or open a
		// repository at all.
		engine.WithCredentialKey("test-cred-key"),
	}
	all = append(all, opts...)
	svc, err := engine.Open(context.Background(), dsn, all...)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc, dsn
}

// newDataset provisions a repository, imports the shipped vocabulary and
// returns its dataset. Creation seeds CORE ALONE — people/tasks/messaging/
// calendar/media are vocabulary bundles a user imports — so the import here is
// what a repository these tests can exercise actually looks like. Use
// newCoreDataset for the tests that are about the seed itself.
func newDataset(t *testing.T, opts ...engine.Option) (substrate.Service, substrate.Dataset) {
	t.Helper()
	svc, ds := newCoreDataset(t, opts...)
	importVocabulary(t, ds)
	return svc, ds
}

// newCoreDataset provisions a repository and stops there: core, and nothing
// else, exactly as the seed leaves it.
func newCoreDataset(t *testing.T, opts ...engine.Option) (substrate.Service, substrate.Dataset) {
	t.Helper()
	svc, _ := newService(t, opts...)
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	return svc, ds
}

// importVocabulary imports the shipped vocabulary bundles (all of them when
// none are named) through the ordinary install path.
func importVocabulary(t *testing.T, ds substrate.Dataset, names ...string) {
	t.Helper()
	if err := enginetest.ImportVocabulary(context.Background(), ds, names...); err != nil {
		t.Fatalf("import the shipped vocabulary: %v", err)
	}
}

// installShelf installs the shelf fixture vocabulary (enginetest.InstallShelf):
// the shapes the suite exercises that no shipped vocabulary carries — a
// mapping-target kind, the asin/isbn refinements, an embeddable property.
func installShelf(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	if err := enginetest.InstallShelf(context.Background(), ds); err != nil {
		t.Fatalf("install the shelf fixture: %v", err)
	}
}

// installShippedBundle installs one shipped EXTENSION bundle's closure — the
// declaration rows a vocabulary bundle does not bring: the bundle document, its
// functions and its agents.
func installShippedBundle(t *testing.T, ds substrate.Dataset, name string) {
	t.Helper()
	if err := enginetest.InstallBundle(context.Background(), ds, name); err != nil {
		t.Fatalf("install the %s bundle: %v", name, err)
	}
}

// testPassword is what every registered test user gets: past the length
// policy, and the same everywhere so a test that CHANGES it is obvious.
const testPassword = "correct-horse-battery-staple"

// authUser is a registered user and the seed its codes come from.
type authUser struct {
	username string
	password string
	seed     string
	// step is the last step this user consumed. Codes are ONE-TIME, so a
	// second authentication inside one 30-second window has to move on.
	step int64
}

// code returns a fresh, unconsumed code. Verification accepts one step of
// skew either way, so a user may authenticate TWICE inside one 30-second
// window and no more — the same budget a real authenticator app gives. A
// third needs waitStep.
func (u *authUser) code(t *testing.T) string {
	t.Helper()
	step := engine.TOTPStep(time.Now())
	if step <= u.step {
		step = u.step + 1
	}
	u.step = step
	code, err := engine.TOTPCode(u.seed, step)
	if err != nil {
		t.Fatalf("totp code: %v", err)
	}
	return code
}

// waitStep blocks until the TOTP counter ticks, which is what a user does
// when they have spent this window's codes. A test needing a third
// authentication inside one window has to wait exactly as they would.
func waitStep(t *testing.T) {
	t.Helper()
	start := engine.TOTPStep(time.Now())
	for engine.TOTPStep(time.Now()) == start {
		time.Sleep(200 * time.Millisecond)
	}
}

// registerUser walks the REAL registration flow — enrollment, one code, the
// commit — and returns the user plus the token registration minted.
func registerUser(t *testing.T, svc substrate.Service, username string) (*authUser, substrate.TokenInfo, string) {
	t.Helper()
	ctx := context.Background()
	enrollment, err := svc.BeginRegistration(ctx, username)
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	u := &authUser{username: username, password: testPassword, seed: enrollment.Secret}
	res, err := svc.Register(ctx, substrate.RegisterInput{
		Username: username, Password: u.password,
		TOTPSecret: u.seed, TOTPCode: u.code(t), Label: "cli",
	})
	if err != nil {
		t.Fatalf("register %q: %v", username, err)
	}
	return u, res.Token, res.Secret
}

func maxSeq(t *testing.T, ds substrate.Dataset) int64 {
	t.Helper()
	var last int64
	for {
		changes, err := ds.Changes(context.Background(), last, substrate.ChangeFilter{}, 500)
		if err != nil {
			t.Fatalf("changes: %v", err)
		}
		if len(changes) == 0 {
			return last
		}
		last = changes[len(changes)-1].Seq
	}
}

func changesSince(t *testing.T, ds substrate.Dataset, after int64) []substrate.Change {
	t.Helper()
	out, err := ds.Changes(context.Background(), after, substrate.ChangeFilter{}, 500)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	return out
}

func mustPut(t *testing.T, ds substrate.Dataset, actor substrate.Actor, in substrate.PutInput) *substrate.Record {
	t.Helper()
	e, err := ds.Put(context.Background(), actor, in)
	if err != nil {
		t.Fatalf("put %s: %v", in.Kind, err)
	}
	return e
}

func mustPatch(t *testing.T, ds substrate.Dataset, actor substrate.Actor, typ, id string, in substrate.PatchInput) *substrate.Record {
	t.Helper()
	e, err := ds.Patch(context.Background(), actor, typ, id, in)
	if err != nil {
		t.Fatalf("patch %s: %v", id, err)
	}
	return e
}

func mustGet(t *testing.T, ds substrate.Dataset, typ, id string) *substrate.Record {
	t.Helper()
	e, err := ds.Get(context.Background(), typ, id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return e
}

func wantErr(t *testing.T, err, target error, what string) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("%s: expected %v, got %v", what, target, err)
	}
}

func ptr[T any](v T) *T { return &v }

// extID composes a writer's own record id out of a provider key, the way a
// connector does: the id alphabet is URL-path-safe with no slash, so the
// encoding is the writer's job.
func extID(ns, key string) string {
	r := strings.NewReplacer("/", ":", ".", "-", " ", "-", "+", "-")
	return r.Replace(ns) + ":" + r.Replace(key)
}

func ids(records []*substrate.Record) []string {
	out := make([]string, 0, len(records))
	for _, e := range records {
		out = append(out, e.ID)
	}
	return out
}

// typeProvider is the llmprovider kind reference, spelled out here because
// these tests are outside the engine package and the constant is not exported.
const typeProvider = "core.substrate.reamde.dev/llmprovider"

// fakeEmbedServer is an OpenAI-wire embeddings endpoint over httptest. The
// embedder is no longer injectable — a repository resolves it from its own
// llmprovider row — so a test that wants vectors points a row at THIS, which
// exercises the resolution, the key and the wire rather than an interface the
// production path does not use.
//
// The vectors are an L2-normalised bag-of-words hash, so texts sharing words
// score high and a search assertion is deterministic.
type fakeEmbedServer struct {
	srv *httptest.Server

	mu sync.Mutex
	// calls counts requests and texts counts inputs across them.
	calls int
	texts int
	// hook runs before the FIRST answer, so a test can land an edit while the
	// worker is embedding: the exact race the generation fence exists for.
	hook  func()
	fired bool
	// width overrides the answer's vector length, for the guard that refuses
	// an endpoint serving something else under a known model name.
	width int
	// authsSeen records the Authorization header of every request.
	auths []string
}

func newFakeEmbedServer(t *testing.T) *fakeEmbedServer {
	t.Helper()
	f := &fakeEmbedServer{}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeEmbedServer) handle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.calls++
	f.texts += len(req.Input)
	f.auths = append(f.auths, r.Header.Get("Authorization"))
	hook := f.hook
	fire := hook != nil && !f.fired
	if fire {
		f.fired = true
	}
	width := f.width
	f.mu.Unlock()
	if fire {
		hook()
	}
	if width == 0 {
		width = 1536
	}
	type datum struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	}
	out := struct {
		Data []datum `json:"data"`
	}{}
	for i, s := range req.Input {
		out.Data = append(out.Data, datum{Index: i, Embedding: bagOfWordsVector(s, width)})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (f *fakeEmbedServer) counts() (calls, texts int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.texts
}

func (f *fakeEmbedServer) setHook(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hook, f.fired = fn, false
}

func bagOfWordsVector(s string, width int) []float32 {
	vec := make([]float32, width)
	for _, word := range strings.Fields(strings.ToLower(s)) {
		h := 0
		for _, r := range word {
			h = (h*31 + int(r)) % width
		}
		vec[h] += 1
	}
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		vec[0] = 1
		return vec
	}
	norm := float32(math.Sqrt(sum))
	for i := range vec {
		vec[i] /= norm
	}
	return vec
}

// installEmbedProvider writes the llmprovider row that makes a repository
// embed: the one row declaring embedModel. Nothing seeds one, so every test
// that wants vectors writes it, exactly as an owner would.
func installEmbedProvider(t *testing.T, ds substrate.Dataset, id, baseURL, model string) {
	t.Helper()
	if _, err := ds.Put(context.Background(), owner, substrate.PutInput{
		Kind: typeProvider, ID: id,
		Properties: map[string]any{
			"name": id, "wire": "openai", "baseURL": baseURL,
			"apiKey": "row-key-" + id, "embedModel": model,
		},
	}); err != nil {
		t.Fatalf("put the embeddings provider row %q: %v", id, err)
	}
}
