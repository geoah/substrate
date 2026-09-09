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

// MintCredentialKey returns a conforming credential key: standard-base64 of
// 32 random bytes, the shape the engine demands (ADR 0024). Generated per
// call rather than committed, because a key checked into the tree is a key
// everyone has; a test that restores under a second host key mints that one
// too.
func MintCredentialKey() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// Session is one user's credentials on a running substrate. Every Env bound
// to the same user shares one Session by pointer, so a login through a
// subtest's copy is seen by every other copy.
type Session struct {
	// Token is the bearer secret of the user's current token. A token has
	// full access to its repository, so there is nothing else to hold.
	Token string
	// Repository is the repository the user owns and logs in with: its
	// authority, as registration returned it, or as the option named it when
	// nothing registered.
	Repository string
	// TOTPSecret is the enrolled seed, so a test can log in again, on this
	// substrate or on one restored from it.
	TOTPSecret string
}

// Env is one running substrate and the credentials to talk to it.
type Env struct {
	*Session
	// URL is the server's base URL, e.g. http://127.0.0.1:38311.
	URL string
	// DSN is the schema the engine runs on, for a test that needs to look
	// underneath.
	DSN string
	// Service is the engine behind the handler, for the same reason.
	Service substrate.Service

	t      testing.TB
	client *http.Client
	now    func() time.Time
	// stop is nil on a copy For and RegisterUser return: only the Env Start
	// returned stops the substrate.
	stop func()
}

// Option tunes a Start.
type Option func(*options)

type options struct {
	repository        string
	password          string
	recoveryPublicKey string
	dsn               string
	dataRoot          string
	credentialKey     string
	kindsDir          string
	engine            []engine.Option
	now               func() time.Time
	noRegister        bool
}

// WithUser names the repository Start registers and the password its user
// signs in with. A bare label is completed under the listener's host, exactly
// as the door does it. The default is fine unless a test needs two substrates
// to disagree about who lives in them.
func WithUser(repository, password string) Option {
	return func(o *options) { o.repository, o.password = repository, password }
}

// WithRecoveryPublicKey registers with a client-minted age recipient, the way
// substratectl does, so the test holds the identity that opens the
// repository's sealed files on a host with another credential key.
func WithRecoveryPublicKey(recipient string) Option {
	return func(o *options) { o.recoveryPublicKey = recipient }
}

// WithDSN opens the engine over an existing schema instead of a fresh one:
// the restore target a test prepared, or the schema a stopped substrate ran
// on. Start still skips under -short, as every database test does.
func WithDSN(dsn string) Option { return func(o *options) { o.dsn = dsn } }

// WithDataRoot names the data root; absent, a temp directory of the test's.
// A restore points it at a directory another substrate wrote.
func WithDataRoot(root string) Option { return func(o *options) { o.dataRoot = root } }

// WithCredentialKey sets the host key; absent, one is minted.
func WithCredentialKey(key string) Option { return func(o *options) { o.credentialKey = key } }

// WithKindsDir loads the seeded core package from a directory instead of the
// embedded tree: a copy of the shipped tree a test patched, so the next Start
// over the same database runs the boot upgrade against it.
func WithKindsDir(dir string) Option { return func(o *options) { o.kindsDir = dir } }

// WithEngineOptions appends engine options after the ones Start sets, so
// they win where they name the same thing: a test seam (engine.WithTest*),
// a logger, a segment size.
func WithEngineOptions(opts ...engine.Option) Option {
	return func(o *options) { o.engine = append(o.engine, opts...) }
}

// WithClock is the clock the TOTP verifier and the registration code read,
// so a test that has spent one window's codes advances it instead of waiting
// thirty seconds. A nil clock is the wall clock.
func WithClock(now func() time.Time) Option { return func(o *options) { o.now = now } }

// WithoutRegistration starts the substrate over a repository that already
// exists, imported from the data root at boot: nothing registers and Token is
// empty until the test logs in.
func WithoutRegistration() Option { return func(o *options) { o.noRegister = true } }

// Start brings up the substrate and returns it registered and logged in.
// Everything is torn down through t.Cleanup: the server, the engine, the
// schema. Failures are reported to t, so a test that keeps the substrate past
// one subtest hands Start a TB whose Cleanup is the parent's and whose
// failures are the subtest's.
func Start(t testing.TB, opts ...Option) *Env {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	o := options{repository: "tester", password: "correct-horse-battery-staple"}
	for _, opt := range opts {
		opt(&o)
	}
	if o.now == nil {
		o.now = time.Now
	}

	// testdb shares one container per test binary.
	dsn := o.dsn
	if dsn == "" {
		dsn = testdb.NewSchema(t)
	}
	if o.dataRoot == "" {
		o.dataRoot = t.TempDir()
	}
	if o.credentialKey == "" {
		o.credentialKey = MintCredentialKey()
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
		// The auth rate limiter reads the same clock as the TOTP verifier, so
		// a test spaces two logins by advancing it rather than sleeping.
		Now: o.now,
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
		Session: &Session{Repository: o.repository},
		URL:     "http://" + ln.Addr().String(),
		DSN:     dsn, Service: svc, t: t, now: o.now, stop: stop,
		client: &http.Client{Timeout: 120 * time.Second},
	}
	if !o.noRegister {
		env.register(o.repository, o.password, o.recoveryPublicKey)
	}
	return env
}

// For is this substrate bound to a subtest: the same server, Session and
// clock, with failures reported to t. A substrate started under a parent
// test outlives its subtests, and a fatal raised on the parent from a
// subtest's goroutine is not a failure the runner can attribute. The copy
// cannot Stop the substrate; the Env Start returned does.
func (e *Env) For(t testing.TB) *Env {
	c := *e
	c.t, c.stop = t, nil
	return &c
}

// WithToken is this substrate spoken to with another bearer: a stranger's,
// or none. The copy has a Session of its own, so the token the test holds is
// untouched, and it cannot Stop the substrate.
func (e *Env) WithToken(token string) *Env {
	c := *e
	c.Session = &Session{Token: token, Repository: e.Repository, TOTPSecret: e.TOTPSecret}
	c.stop = nil
	return &c
}

// Stop shuts the server down and closes the engine, the way a substrated
// exit does: in-flight fires are drained and canceled, the directory lock is
// released, and nothing appends afterwards. Idempotent; the cleanup is the
// same call. A copy For or RegisterUser returned refuses it.
func (e *Env) Stop() {
	e.t.Helper()
	if e.stop == nil {
		e.t.Fatal("testenv: Stop on a copy; stop the substrate through the Env Start returned")
	}
	e.stop()
}

// TOTPCode is the code the user's authenticator would show at the clock's
// current step.
func (e *Env) TOTPCode() string {
	e.t.Helper()
	if e.TOTPSecret == "" {
		e.t.Fatal("testenv: TOTPCode with an empty TOTPSecret: the Session holds no enrolled seed")
	}
	code, err := engine.TOTPCode(e.TOTPSecret, engine.TOTPStep(e.now()))
	if err != nil {
		e.t.Fatalf("testenv: totp code: %v", err)
	}
	return code
}

// Login signs the user in with the password and a code, and returns the
// status and body: a test asserting a refusal needs the refusal. On success
// the Session's Token is replaced with the new secret.
func (e *Env) Login(repository, password, code string) (int, []byte) {
	e.t.Helper()
	status, raw, _ := e.DoRaw(http.MethodPost, "/login", marshal(e.t, substrate.LoginRequest{
		Repository: repository, Password: password, TOTPCode: code, Label: "testenv-login",
	}), map[string]string{"Content-Type": "application/json", "Authorization": ""})
	if status == http.StatusCreated {
		var out substrate.MintedToken
		if err := json.Unmarshal(raw, &out); err != nil {
			e.t.Fatalf("testenv: decode login: %v (%s)", err, raw)
		}
		e.Token = out.Secret
	}
	return status, raw
}

// RegisterUser registers another repository on the same substrate and returns
// an Env authenticated as its user: the server, the clock and the schema are
// shared, the Session is theirs. A bare repository label is completed under
// the listener's host; an empty recovery recipient has the server mint the
// pair. The copy cannot Stop the substrate.
func (e *Env) RegisterUser(repository, password, recoveryPublicKey string) *Env {
	e.t.Helper()
	other := *e
	other.Session = &Session{Repository: repository}
	other.stop = nil
	other.register(repository, password, recoveryPublicKey)
	return &other
}

// register walks the real registration: enroll for a TOTP seed, then commit
// with a code derived from it. Registration ends holding a token, so there is
// no separate login.
func (e *Env) register(repository, password, recoveryPublicKey string) {
	e.t.Helper()
	var enrollment substrate.TOTPEnrollment
	e.mustJSON(http.MethodPost, "/register/enroll", map[string]any{
		"inviteCode": InviteCode, "repository": repository,
	}, &enrollment)

	code, err := engine.TOTPCode(enrollment.Secret, engine.TOTPStep(e.now()))
	if err != nil {
		e.t.Fatalf("testenv: totp code: %v", err)
	}
	var out substrate.Registered
	e.mustJSON(http.MethodPost, "/register", substrate.RegisterRequest{
		InviteCode: InviteCode, Repository: repository, Password: password,
		TOTPSecret: enrollment.Secret, TOTPCode: code, Label: "testenv",
		RecoveryPublicKey: recoveryPublicKey,
	}, &out)
	if out.Secret == "" {
		e.t.Fatal("testenv: registration returned no token secret")
	}
	e.Token = out.Secret
	e.TOTPSecret = enrollment.Secret
	e.Repository = out.Repository
}

func marshal(t testing.TB, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("testenv: marshal: %v", err)
	}
	return raw
}

// Do performs one authenticated request and returns the status and body. It
// never fails the test: a test asserting a 403 needs the 403.
func (e *Env) Do(method, path string, body any) (int, []byte) {
	e.t.Helper()
	var raw []byte
	headers := map[string]string{}
	if body != nil {
		raw = marshal(e.t, body)
		headers["Content-Type"] = "application/json"
	}
	status, out, _ := e.DoRaw(method, path, raw, headers)
	return status, out
}

// DoRaw is Do with the body and headers as the caller built them: a blob
// upload, a request under another actor. A nil body sends none. Every header
// is sent as given, an empty value included, except Authorization: the bearer
// is added unless the headers map names that key (in any casing), and an
// empty value there means "no bearer", so the request goes out with no
// Authorization header at all.
func (e *Env) DoRaw(method, path string, body []byte, headers map[string]string) (int, []byte, http.Header) {
	e.t.Helper()
	resp := e.Request(context.Background(), method, path, body, headers)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatalf("testenv: read %s %s: %v", method, path, err)
	}
	return resp.StatusCode, raw, resp.Header
}

// Request is DoRaw with the response body left open, for a stream (a watch)
// the caller reads a frame at a time and closes. ctx bounds the whole
// exchange; the client's own timeout applies as well.
func (e *Env) Request(ctx context.Context, method, path string, body []byte, headers map[string]string) *http.Response {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.URL+path, reader)
	if err != nil {
		e.t.Fatalf("testenv: build %s %s: %v", method, path, err)
	}
	explicit := false
	for k, v := range headers {
		if http.CanonicalHeaderKey(k) == "Authorization" {
			explicit = true
			if v == "" {
				continue
			}
		}
		req.Header.Set(k, v)
	}
	if !explicit && e.Token != "" {
		req.Header.Set("Authorization", "Bearer "+e.Token)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatalf("testenv: %s %s: %v", method, path, err)
	}
	return resp
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
// answer 2xx, and the body decodes into `into` when it is not nil.
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
