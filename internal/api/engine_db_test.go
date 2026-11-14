package api

// The API on the real engine. A test whose assertion depends on what the
// engine does with a request (what a write stores or refuses, a guard,
// validation, paging, the changelog, watch, search) runs on a dbEnv, in a
// *_db_test.go file; a test of the handler alone (routing, decoding,
// headers, the envelope for a sentinel) runs on stub_test.go's stub, in the
// short suite. docs/testing.md has the rule and the fixture's costs.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

// dbCredentialKey is the host key every dbEnv opens under: minted once per
// test binary, never committed (decision 0024).
var dbCredentialKey = func() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}()

// dbTemplate is the database every dbEnv copies: Open ran on it once with no
// repository, so a copy beside an empty data root is a fresh install.
var dbTemplate = testdb.NewTemplate("api", func(ctx context.Context, dsn string) error {
	root, err := os.MkdirTemp("", "substrate-api-template-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(root) }()
	svc, err := engine.Open(ctx, dsn,
		engine.WithKindsFS(kinds.Seed()),
		engine.WithDataRoot(root),
		engine.WithCredentialKey(dbCredentialKey))
	if err != nil {
		return err
	}
	return svc.Close()
})

// dbEnv is the handler on the real engine: a database of the test's own, one
// repository named after the test, and a full-access token into it.
type dbEnv struct {
	*testEnv
	// svc is the engine itself; the handler talks to it through seamService.
	svc substrate.Service
	// dsn is the test's database, for a state no route can make.
	dsn string
	// ds is the repository's dataset, for arranging state and reading it back
	// underneath the API.
	ds         substrate.Dataset
	repository string
	// token is a full-access bearer into repository.
	token string
	// wrap, when set, decorates the dataset a request reaches: the seam for a
	// fault the engine cannot be made to produce. A test that sets it names
	// the fault beside it.
	wrap func(substrate.Dataset) substrate.Dataset
}

// dbOption tunes newDBEnv.
type dbOption func(*dbOptions)

type dbOptions struct {
	config       []func(*Config)
	engine       []engine.Option
	service      []func(substrate.Service) substrate.Service
	samples      []string
	noRepository bool
	// password registers the repository and its person, named after it,
	// with this password and no second factor where CreateRepository would
	// make neither, for a test of a door that proves the factors.
	password string
}

// withConfig edits the handler's Config after newDBEnv fills it.
func withConfig(fn func(*Config)) dbOption {
	return func(o *dbOptions) { o.config = append(o.config, fn) }
}

// withEngine appends engine options after newDBEnv's, so they win.
func withEngine(opts ...engine.Option) dbOption {
	return func(o *dbOptions) { o.engine = append(o.engine, opts...) }
}

// withService decorates the service the handler is built on, outside the
// dataset seam: a door's fault, or a count of the calls that reached it.
func withService(fn func(substrate.Service) substrate.Service) dbOption {
	return func(o *dbOptions) { o.service = append(o.service, fn) }
}

// withSamples imports shipped sample packages by bare name, each with the
// samples it requires, under their shipped authority
// (samples.substrate.reamde.dev), so personKind and the other sample kinds
// resolve as written. None are imported unless a test names them: each is a
// vocabulary apply of its own.
func withSamples(names ...string) dbOption {
	return func(o *dbOptions) { o.samples = append(o.samples, names...) }
}

// withPassword registers the repository with this password and no second
// factor, so a door that proves the factors (a delete, a rename) can be
// driven through HTTP.
func withPassword(password string) dbOption {
	return func(o *dbOptions) { o.password = password }
}

// withNameHost configures the public URL a registration's short name is
// completed under (decision record 0288): https:// and host.
func withNameHost(host string) dbOption {
	return withConfig(func(c *Config) { c.PublicURL = "https://" + host })
}

// withoutRepository starts the engine with no repository, for a test of the
// door that creates one. repository, ds and token stay empty.
func withoutRepository() dbOption {
	return func(o *dbOptions) { o.noRepository = true }
}

// newDBEnv opens the engine over a copy of the migrated template and serves
// it. It skips under -short, like every database test. The clock starts at
// the wall clock, so a token's expiry and a record's timestamps, which the
// engine stamps from the wall clock, agree with what the handler reads; the
// engine's TOTP verifier reads the same clock.
func newDBEnv(t *testing.T, opts ...dbOption) *dbEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("db test")
	}
	var o dbOptions
	for _, opt := range opts {
		opt(&o)
	}
	clock := &testClock{t: time.Now().UTC().Truncate(time.Second)}
	ctx := context.Background()
	all := append([]engine.Option{
		engine.WithKindsFS(kinds.Seed()),
		engine.WithDataRoot(t.TempDir()),
		engine.WithCredentialKey(dbCredentialKey),
		engine.WithTestTOTPClock(clock.now),
		// The seeds a fresh repository would carry and no API test reads:
		// several hundred records each test would otherwise write.
		engine.WithLLMProviderSeed(false),
		engine.WithUnitSeed(false),
		engine.WithRecordSeed(false),
	}, o.engine...)
	dsn := dbTemplate.Clone(t)
	svc, err := engine.Open(ctx, dsn, all...)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	env := &dbEnv{svc: svc, dsn: dsn}
	var served substrate.Service = seamService{Service: svc, env: env}
	for _, fn := range o.service {
		served = fn(served)
	}
	cfg := Config{Service: served, Now: clock.now, InviteCode: testInviteCode}
	for _, fn := range o.config {
		fn(&cfg)
	}
	env.testEnv = &testEnv{h: New(cfg), clock: clock}
	if o.noRepository {
		return env
	}

	// The repository is named after the test (testdb.Repository): the
	// function runner keys body processes on it, so two parallel tests under
	// one name would retire each other's.
	env.repository = testdb.Repository(t)
	if o.password != "" {
		if _, err := svc.Register(ctx, substrate.RegisterInput{Identity: env.repository, Repository: env.repository, Password: o.password, SkipTOTP: true}); err != nil {
			t.Fatalf("register repository: %v", err)
		}
	} else if _, err := svc.CreateRepository(ctx, env.repository); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if env.ds, err = svc.Dataset(ctx, env.repository); err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	if len(o.samples) > 0 {
		if err := enginetest.ImportVocabulary(ctx, env.ds, o.samples...); err != nil {
			t.Fatalf("import samples %v: %v", o.samples, err)
		}
	}
	env.token = env.mint(t, substrate.MintTokenInput{Label: "test"})
	return env
}

// mint mints a token into the repository and returns its bearer secret: a
// full-access one unless the input names scopes or an app.
func (e *dbEnv) mint(t *testing.T, in substrate.MintTokenInput) string {
	t.Helper()
	_, secret, err := e.ds.MintToken(context.Background(), in)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return secret
}

// put arranges one record underneath the API and returns what the engine
// stored. It writes as the engine's own hand, which may name a new record's
// id (decision 0228), so a test seeds "p1" and addresses it as "p1"; a test
// about who wrote a value, or about the id rule, writes through the API.
func (e *dbEnv) put(t *testing.T, kind, id string, props map[string]any) *substrate.Record {
	t.Helper()
	rec, err := e.ds.Put(context.Background(), substrate.ActorSystem, substrate.PutInput{Kind: kind, ID: id, Properties: props})
	if err != nil {
		t.Fatalf("put %s/%s: %v", kind, id, err)
	}
	return rec
}

// get reads one record underneath the API; a record that is not there is nil.
func (e *dbEnv) get(t *testing.T, kind, id string) *substrate.Record {
	t.Helper()
	rec, err := e.ds.Get(context.Background(), kind, id)
	if errors.Is(err, substrate.ErrNotFound) {
		return nil
	}
	if err != nil {
		t.Fatalf("get %s/%s: %v", kind, id, err)
	}
	return rec
}

// code is the second factor's current code for an enrolled secret, on the
// clock the engine's verifier reads.
func (e *dbEnv) code(t *testing.T, secret string) string {
	t.Helper()
	code, err := engine.TOTPCode(secret, engine.TOTPStep(e.clock.now()))
	if err != nil {
		t.Fatalf("totp code: %v", err)
	}
	return code
}

// mintedID is a minted record id derived from a readable name, for a create
// through the API, where a person's or an app's write takes a minted id
// (decision 0228). The same name gives the same id.
func mintedID(name string) string { return vocabulary.DerivedMintedID("api-test", name) }

// declare admits vocabulary documents, given as YAML separated by `---`, in
// one apply.
func (e *dbEnv) declare(t *testing.T, docs string) {
	t.Helper()
	dec := yaml.NewDecoder(bytes.NewReader([]byte(docs)))
	var batch []map[string]any
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("parse vocabulary: %v", err)
		}
		if len(doc) > 0 {
			batch = append(batch, doc)
		}
	}
	if _, err := e.ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorPerson, batch); err != nil {
		t.Fatalf("apply vocabulary: %v", err)
	}
}

// seamService is the engine as the handler sees it, with the test's wrap
// around every dataset a request reaches.
type seamService struct {
	substrate.Service
	env *dbEnv
}

func (s seamService) Authenticate(ctx context.Context, secret string) (substrate.Dataset, substrate.TokenInfo, error) {
	ds, info, err := s.Service.Authenticate(ctx, secret)
	if err == nil && s.env.wrap != nil {
		ds = s.env.wrap(ds)
	}
	return ds, info, err
}

func (s seamService) Dataset(ctx context.Context, repository string) (substrate.Dataset, error) {
	ds, err := s.Service.Dataset(ctx, repository)
	if err == nil && s.env.wrap != nil {
		ds = s.env.wrap(ds)
	}
	return ds, err
}
