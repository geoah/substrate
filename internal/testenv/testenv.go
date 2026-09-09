// Package testenv starts a WHOLE substrate for one test: a Postgres+pgvector
// container, the real engine on a throwaway schema, the real HTTP handler on a
// real listener, and one registered user holding a real token. Tests then
// speak the substrate's own HTTP API, no fakes, no in-process shortcuts, no
// handler called directly with a synthetic request.
//
// It exists because a growing class of behavior is only true END TO END. The
// function sandbox is the first: `internal/runner` can prove a body cannot
// read /proc, and `internal/api` can prove a route is wired, but only a real
// server can prove that a function ARRIVING THROUGH A BUNDLE INSTALL, invoked
// over HTTP with a real token, runs confined. The pieces were each right
// before, and the property still would not have held.
//
// The database half reuses internal/testdb, so a package that already runs
// against the shared container costs nothing extra here; the schema is
// throwaway and dropped when the test ends. Everything skips under -short.
package testenv

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/api"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/sandbox"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"
)

// InviteCode is the one door into the substrate a test starts. It is a
// constant rather than a knob because there is exactly one interesting thing
// to say about it: a test registers through it, and a per-test value would
// only make failures harder to read.
const InviteCode = "test-invite"

// mintCredentialKey returns a conforming credential key: standard-base64 of 32
// random bytes, the shape the engine demands (ADR 0024). Generated per call
// rather than committed, because a key checked into the tree is a key everyone
// has; the throwaway substrate is opened once, so a fresh key each time is
// enough.
func mintCredentialKey() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// MintCredentialKey is mintCredentialKey for a test that needs a second host
// key: the one a restored directory is rewrapped under.
func MintCredentialKey() string { return mintCredentialKey() }

// Env is one running substrate and the credentials to talk to it.
type Env struct {
	// URL is the server's base URL, e.g. http://127.0.0.1:38311.
	URL string
	// Token is the bearer secret of the registered user's first token. A token
	// has full access to its repository, so there is nothing else to hold.
	Token string
	// Username is the registered user, which is also the repository's name.
	Username string
	// Password and TOTPSecret are the registered user's factors, so a test can
	// log in again, on this substrate or on one restored from it.
	Password   string
	TOTPSecret string
	// Authority is the repository's authority as registration returned it.
	Authority string
	// DSN is the throwaway schema, for a test that needs to look underneath.
	DSN string
	// DataRoot is the engine's data root, where the repository directory is.
	DataRoot string
	// CredentialKey is the host key the engine was opened with.
	CredentialKey string
	// Service is the engine behind the handler, for the same reason.
	Service substrate.Service

	t      *testing.T
	client *http.Client
	now    func() time.Time
	stop   func()
}

// Option tunes a Start.
type Option func(*options)

type options struct {
	username          string
	password          string
	authority         string
	recoveryPublicKey string
	dsn               string
	dataRoot          string
	credentialKey     string
	kindsDir          string
	engine            []engine.Option
	now               func() time.Time
	noRegister        bool
}

// WithUser names the user Start registers. The default is fine unless a test
// needs two substrates to disagree about who lives in them.
func WithUser(username, password string) Option {
	return func(o *options) { o.username, o.password = username, password }
}

// WithAuthority names the authority registration asks for; absent, the door
// derives one from the username and the listener's host.
func WithAuthority(authority string) Option {
	return func(o *options) { o.authority = authority }
}

// WithRecoveryPublicKey registers with a client-minted age recipient, the way
// substratectl does, so the test holds the identity that opens the
// repository's sealed files on a host with another credential key.
func WithRecoveryPublicKey(recipient string) Option {
	return func(o *options) { o.recoveryPublicKey = recipient }
}

// WithDSN opens the engine over an existing database instead of a fresh
// schema: the restore target a test prepared, or the schema a stopped
// substrate ran on.
func WithDSN(dsn string) Option { return func(o *options) { o.dsn = dsn } }

// WithDataRoot names the data root; absent, a temp directory of the test's.
// A restore points it at a directory another substrate wrote.
func WithDataRoot(root string) Option { return func(o *options) { o.dataRoot = root } }

// WithCredentialKey sets the host key; absent, one is minted.
func WithCredentialKey(key string) Option { return func(o *options) { o.credentialKey = key } }

// WithEngineOptions appends engine options after the ones Start sets, so
// they win where they name the same thing.
func WithEngineOptions(opts ...engine.Option) Option {
	return func(o *options) { o.engine = append(o.engine, opts...) }
}

// WithKindsDir loads the seeded core package from a directory instead of the
// embedded tree: a copy of the shipped tree a test patched, so the next Start
// over the same database runs the boot upgrade against it.
func WithKindsDir(dir string) Option { return func(o *options) { o.kindsDir = dir } }

// WithClock is the clock the TOTP verifier and the registration code read,
// so a test that has spent one window's codes advances it instead of waiting
// thirty seconds.
func WithClock(now func() time.Time) Option { return func(o *options) { o.now = now } }

// WithoutRegistration starts the substrate over a repository that already
// exists, imported from the data root at boot: nothing registers and Token is
// empty until the test logs in.
func WithoutRegistration() Option { return func(o *options) { o.noRegister = true } }

// Start brings up the substrate and returns it registered and logged in.
// Everything is torn down through t.Cleanup: the server, the engine, the
// schema.
func Start(t *testing.T, opts ...Option) *Env {
	t.Helper()
	o := options{username: "tester", password: "correct-horse-battery-staple", now: time.Now}
	for _, opt := range opts {
		opt(&o)
	}

	// testdb skips under -short and shares one container per test binary.
	dsn := o.dsn
	if dsn == "" {
		dsn = testdb.NewSchema(t)
	}
	if o.dataRoot == "" {
		o.dataRoot = t.TempDir()
	}
	if o.credentialKey == "" {
		o.credentialKey = mintCredentialKey()
	}
	ctx := context.Background()

	// The SEED authority alone, exactly as substrated boots: a fresh
	// repository holds core and nothing else, and a test that wants more
	// installs it the way a user would.
	seed := engine.WithKindsFS(kinds.Seed())
	if o.kindsDir != "" {
		seed = engine.WithKindsDir(o.kindsDir)
	}
	engineOpts := append([]engine.Option{
		seed,
		// The data root every repository directory lives under; the blob
		// bytes default to the fs backend inside it.
		engine.WithDataRoot(o.dataRoot),
		// The live-API env runs the keyed shape the server runs. The key is
		// base64 of 32 bytes, minted here rather than committed (ADR 0024).
		engine.WithCredentialKey(o.credentialKey),
		engine.WithTestTOTPClock(o.now),
	}, o.engine...)
	svc, err := engine.Open(ctx, dsn, engineOpts...)
	if err != nil {
		t.Fatalf("testenv: open engine: %v", err)
	}

	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		_ = svc.Close()
		t.Fatalf("testenv: load catalog: %v", err)
	}
	handler := api.New(api.Config{
		Service:             svc,
		Catalog:             cat,
		InviteCode:          InviteCode,
		MaxDialect:          engine.MaxSchemaDialect(),
		MaxChangelogDialect: engine.MaxChangelogDialect(),
	})

	// A real listener on a kernel-assigned port: two tests in one binary must
	// not race for a number, and a test that reads e.URL gets something it
	// could paste into curl.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = svc.Close()
		t.Fatalf("testenv: listen: %v", err)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdown)
			_ = svc.Close()
		})
	}
	t.Cleanup(stop)

	env := &Env{
		URL: "http://" + ln.Addr().String(), Username: o.username,
		Password: o.password, Authority: o.authority,
		DSN: dsn, DataRoot: o.dataRoot, CredentialKey: o.credentialKey,
		Service: svc, t: t, now: o.now, stop: stop,
		client: &http.Client{Timeout: 120 * time.Second},
	}
	if !o.noRegister {
		env.register(o.username, o.password, o.authority, o.recoveryPublicKey)
	}
	return env
}

// For is this substrate as a subtest speaks to it: the same server, token
// and clock, with failures reported to t. A substrate started under a parent
// test outlives its subtests, and a fatal raised on the parent from a
// subtest's goroutine is not a failure the runner can attribute.
func (e *Env) For(t *testing.T) *Env {
	c := *e
	c.t = t
	return &c
}

// Stop shuts the server down and closes the engine, the way a substrated
// exit does: in-flight fires are drained and canceled, the directory lock is
// released, and nothing appends afterwards. Idempotent; the cleanup is the
// same call.
func (e *Env) Stop() { e.stop() }

// Now is the clock the substrate verifies TOTP codes against.
func (e *Env) Now() time.Time { return e.now() }

// TOTPCode is the code the user's authenticator would show at the clock's
// current step.
func (e *Env) TOTPCode() string {
	e.t.Helper()
	code, err := engine.TOTPCode(e.TOTPSecret, engine.TOTPStep(e.now()))
	if err != nil {
		e.t.Fatalf("testenv: totp code: %v", err)
	}
	return code
}

// Login signs the user in with the password and a code, and returns the
// status and body: a test asserting a refusal needs the refusal. On success
// Token is replaced with the new secret.
func (e *Env) Login(username, password, code string) (int, []byte) {
	e.t.Helper()
	saved := e.Token
	e.Token = ""
	status, raw := e.Do(http.MethodPost, "/login", map[string]any{
		"username": username, "password": password, "totpCode": code, "label": "testenv-login",
	})
	e.Token = saved
	if status == http.StatusCreated {
		var out substrate.MintedToken
		if err := json.Unmarshal(raw, &out); err != nil {
			e.t.Fatalf("testenv: decode login: %v (%s)", err, raw)
		}
		e.Token = out.Secret
	}
	return status, raw
}

// register walks the real registration: enroll for a TOTP seed, then commit
// with a code derived from it. Registration ends holding a token, so there is
// no separate login.
func (e *Env) register(username, password, authority, recoveryPublicKey string) {
	e.t.Helper()
	var enrollment substrate.TOTPEnrollment
	e.mustJSON(http.MethodPost, "/register/enroll", map[string]any{
		"inviteCode": InviteCode, "username": username,
	}, &enrollment)

	code, err := engine.TOTPCode(enrollment.Secret, engine.TOTPStep(e.now()))
	if err != nil {
		e.t.Fatalf("testenv: totp code: %v", err)
	}
	body := map[string]any{
		"inviteCode": InviteCode, "username": username, "password": password,
		"totpSecret": enrollment.Secret, "totpCode": code, "label": "testenv",
	}
	if authority != "" {
		body["authority"] = authority
	}
	if recoveryPublicKey != "" {
		body["recoveryPublicKey"] = recoveryPublicKey
	}
	var out substrate.Registered
	e.mustJSON(http.MethodPost, "/register", body, &out)
	if out.Secret == "" {
		e.t.Fatal("testenv: registration returned no token secret")
	}
	e.Token = out.Secret
	e.TOTPSecret = enrollment.Secret
	e.Authority = out.Authority
}

// RegisterUser registers another user on the same substrate and returns an
// Env that speaks as them: the server, the clock and the schema are shared,
// the token, the factors and the authority are theirs. Only WithAuthority and
// WithRecoveryPublicKey are read from opts.
func (e *Env) RegisterUser(username, password string, opts ...Option) *Env {
	e.t.Helper()
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	other := *e
	other.Username, other.Password, other.Token = username, password, ""
	other.register(username, password, o.authority, o.recoveryPublicKey)
	return &other
}

// Do performs one authenticated request and returns the status and body. It
// never fails the test: a test asserting a 403 needs the 403.
func (e *Env) Do(method, path string, body any) (int, []byte) {
	e.t.Helper()
	var raw []byte
	headers := map[string]string{}
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			e.t.Fatalf("testenv: marshal %s %s: %v", method, path, err)
		}
		headers["Content-Type"] = "application/json"
	}
	status, out, _ := e.DoRaw(method, path, raw, headers)
	return status, out
}

// DoRaw is Do with the body and headers as the caller built them: a blob
// upload, a webhook post, a request under another actor. A nil body sends
// none. The bearer header is added unless the headers map carries the
// "Authorization" key: a caller that means "no bearer" sets it to "", and
// the request then goes out with no Authorization header at all.
func (e *Env) DoRaw(method, path string, body []byte, headers map[string]string) (int, []byte, http.Header) {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, e.URL+path, reader)
	if err != nil {
		e.t.Fatalf("testenv: build %s %s: %v", method, path, err)
	}
	for k, v := range headers {
		if v == "" {
			continue
		}
		req.Header.Set(k, v)
	}
	if _, explicit := headers["Authorization"]; !explicit && e.Token != "" {
		req.Header.Set("Authorization", "Bearer "+e.Token)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatalf("testenv: %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatalf("testenv: read %s %s: %v", method, path, err)
	}
	return resp.StatusCode, raw, resp.Header
}

// mustJSON performs a request that has to succeed and decodes it.
func (e *Env) mustJSON(method, path string, body, into any) {
	e.t.Helper()
	status, raw := e.Do(method, path, body)
	if status < 200 || status >= 300 {
		e.t.Fatalf("testenv: %s %s: %d %s", method, path, status, raw)
	}
	if into == nil {
		return
	}
	if err := json.Unmarshal(raw, into); err != nil {
		e.t.Fatalf("testenv: decode %s %s: %v (%s)", method, path, err, raw)
	}
}

// MustJSON is mustJSON for a test outside the package: the request has to
// succeed, and the body decodes into `into` when it is not nil.
func (e *Env) MustJSON(method, path string, body, into any) {
	e.t.Helper()
	e.mustJSON(method, path, body, into)
}

// ApplyVocabulary installs vocabulary documents: kinds, traits, bundles,
// functions: through the one admission path, the batch apply verb. A body
// that cannot compile or load fails HERE, which is the point: registration
// prepares bodies, so an apply that returns 2xx has already run the function
// through the runner and its sandbox.
func (e *Env) ApplyVocabulary(docs ...map[string]any) {
	e.t.Helper()
	e.mustJSON(http.MethodPost, "/api/v1/vocabulary/apply",
		map[string]any{"documents": docs}, nil)
}

// ApplyVocabularyYAML is ApplyVocabulary for documents written as YAML, which
// is how a bundle actually looks and therefore how a test that means "install
// this bundle" should read.
func (e *Env) ApplyVocabularyYAML(docs ...string) {
	e.t.Helper()
	var out []map[string]any
	for i, doc := range docs {
		parsed, err := yamlDocuments(doc)
		if err != nil {
			e.t.Fatalf("testenv: document %d: %v", i, err)
		}
		out = append(out, parsed...)
	}
	e.ApplyVocabulary(out...)
}

// CallFunction invokes a registered function by reference and returns its
// output. The error is returned rather than fataled, because "this function
// was refused" is a thing tests assert.
func (e *Env) CallFunction(ref string, input any) (any, error) {
	e.t.Helper()
	status, raw := e.Do(http.MethodPost,
		"/api/v1/substrate.reamde.dev/core/function/"+ref+"/call",
		map[string]any{"input": input})
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("call %s: %d %s", ref, status, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Output any `json:"output"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("call %s: decode: %w (%s)", ref, err, raw)
	}
	return out.Output, nil
}

// MustCallFunction is CallFunction for the ordinary case.
func (e *Env) MustCallFunction(ref string, input any) any {
	e.t.Helper()
	out, err := e.CallFunction(ref, input)
	if err != nil {
		e.t.Fatalf("testenv: %v", err)
	}
	return out
}

// yamlDocuments decodes a YAML stream into the map shape the apply verb takes,
// honoring `---` separators: a bundle is written as one file of several
// documents, and a helper that silently kept only the first would drop half a
// closure and fail with a message about the OTHER half being undeclared.
func yamlDocuments(stream string) ([]map[string]any, error) {
	dec := yaml.NewDecoder(strings.NewReader(stream))
	var out []map[string]any
	for {
		var m map[string]any
		err := dec.Decode(&m)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(m) > 0 {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no documents")
	}
	return out, nil
}

// RequireSandbox skips a test whose assertion is a DENIAL the kernel has to
// perform. `test:db` runs this package on developer machines too: macOS has
// no Landlock or seccomp at all, and a Linux box can have Landlock left out of
// its lsm= list, and on those hosts a body legitimately opens the socket the
// test expects to be refused. Reporting "this platform cannot enforce the
// policy" is the honest outcome; failing would read as a regression in code
// that is fine.
//
// Tests asserting what a body may still DO need no guard, and neither does the
// harness itself.
func RequireSandbox(t *testing.T) {
	t.Helper()
	rep := sandbox.New(sandbox.ModeBestEffort).Report()
	if !rep.FS() || !rep.Seccomp {
		t.Skipf("this platform cannot enforce the sandbox: %s", rep)
	}
}

// RequireSeccomp is RequireSandbox for a test whose assertion needs only the
// SYSCALL layer, which the network gate does. Asking for the filesystem layer
// too would skip the network tests on a host that can perfectly well run them,
// and a guard that skips more than it has to is a test that quietly stops
// running.
func RequireSeccomp(t *testing.T) {
	t.Helper()
	if rep := sandbox.New(sandbox.ModeBestEffort).Report(); !rep.Seccomp {
		t.Skipf("this platform has no syscall filter: %s", rep)
	}
}
