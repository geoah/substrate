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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
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
)

// InviteCode is the one door into the substrate a test starts. It is a
// constant rather than a knob because there is exactly one interesting thing
// to say about it: a test registers through it, and a per-test value would
// only make failures harder to read.
const InviteCode = "test-invite"

// Env is one running substrate and the credentials to talk to it.
type Env struct {
	// URL is the server's base URL, e.g. http://127.0.0.1:38311.
	URL string
	// Token is the bearer secret of the registered user's first token. A token
	// has full access to its repository, so there is nothing else to hold.
	Token string
	// Username is the registered user, which is also the repository's name.
	Username string
	// DSN is the throwaway schema, for a test that needs to look underneath.
	DSN string
	// Service is the engine behind the handler, for the same reason.
	Service substrate.Service

	t      *testing.T
	client *http.Client
}

// Option tunes a Start.
type Option func(*options)

type options struct {
	username string
	password string
}

// WithUser names the user Start registers. The default is fine unless a test
// needs two substrates to disagree about who lives in them.
func WithUser(username, password string) Option {
	return func(o *options) { o.username, o.password = username, password }
}

// Start brings up the substrate and returns it registered and logged in.
// Everything is torn down through t.Cleanup: the server, the engine, the
// schema.
func Start(t *testing.T, opts ...Option) *Env {
	t.Helper()
	o := options{username: "tester", password: "correct-horse-battery-staple"}
	for _, opt := range opts {
		opt(&o)
	}

	// testdb skips under -short and shares one container per test binary.
	dsn := testdb.NewSchema(t)
	ctx := context.Background()

	// The SEED authority alone, exactly as substrated boots: a fresh
	// repository holds core and nothing else, and a test that wants more
	// installs it the way a user would.
	svc, err := engine.Open(ctx, dsn, engine.WithKindsFS(kinds.Seed()))
	if err != nil {
		t.Fatalf("testenv: open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	cat, err := catalog.Load(kinds.Bundles())
	if err != nil {
		t.Fatalf("testenv: load catalog: %v", err)
	}
	handler := api.New(api.Config{
		Service:    svc,
		Catalog:    cat,
		InviteCode: InviteCode,
		MaxDialect: engine.MaxSchemaDialect(),
	})

	// A real listener on a kernel-assigned port: two tests in one binary must
	// not race for a number, and a test that reads e.URL gets something it
	// could paste into curl.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("testenv: listen: %v", err)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	})

	env := &Env{
		URL: "http://" + ln.Addr().String(), Username: o.username,
		DSN: dsn, Service: svc, t: t,
		client: &http.Client{Timeout: 120 * time.Second},
	}
	env.register(o.username, o.password)
	return env
}

// register walks the real registration: enroll for a TOTP seed, then commit
// with a code derived from it. Registration ends holding a token, so there is
// no separate login.
func (e *Env) register(username, password string) {
	e.t.Helper()
	var enrollment substrate.TOTPEnrollment
	e.mustJSON(http.MethodPost, "/register/enroll", map[string]any{
		"inviteCode": InviteCode, "username": username,
	}, &enrollment)

	code, err := engine.TOTPCode(enrollment.Secret, engine.TOTPStep(time.Now()))
	if err != nil {
		e.t.Fatalf("testenv: totp code: %v", err)
	}
	var out struct {
		Secret string `json:"secret"`
	}
	e.mustJSON(http.MethodPost, "/register", map[string]any{
		"inviteCode": InviteCode, "username": username, "password": password,
		"totpSecret": enrollment.Secret, "totpCode": code, "label": "testenv",
	}, &out)
	if out.Secret == "" {
		e.t.Fatal("testenv: registration returned no token secret")
	}
	e.Token = out.Secret
}

// Do performs one authenticated request and returns the status and body. It
// never fails the test: a test asserting a 403 needs the 403.
func (e *Env) Do(method, path string, body any) (int, []byte) {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			e.t.Fatalf("testenv: marshal %s %s: %v", method, path, err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.URL+path, reader)
	if err != nil {
		e.t.Fatalf("testenv: build %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if e.Token != "" {
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
	return resp.StatusCode, raw
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

// ApplyVocabulary installs vocabulary documents: kinds, traits, bundles,
// functions: through the one admission path, the batch apply verb. A body
// that cannot compile or load fails HERE, which is the point: registration
// prepares bodies, so an apply that returns 2xx has already run the function
// through the runner and its sandbox.
func (e *Env) ApplyVocabulary(docs ...map[string]any) {
	e.t.Helper()
	e.mustJSON(http.MethodPost, "/api/v1/core.substrate.reamde.dev/vocabulary/apply",
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
		"/api/v1/core.substrate.reamde.dev/functions/"+ref+"/call",
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
