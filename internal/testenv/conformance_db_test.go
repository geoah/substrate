package testenv_test

// The published error codes, over a real socket, against the real engine.
//
// internal/api is tested against a hand-written fake and internal/engine
// against its own Go surface, so each half is held to its own half of the
// contract and nothing holds the seam. The fake never returns
// substrate.ErrGated, its Put ignores IfVersion, and it never builds a
// *substrate.ValidationError, so "the engine returns sentinel X" and "the API
// maps sentinel X to status Y" can both pass while the engine returns a
// different sentinel for the request that matters.
//
// Every case below is one HTTP request to a running substrate, and the table
// is checked against the code* declarations parsed out of internal/api: a code
// added there with no case here fails this test rather than going untested.

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/testenv"
)

const conformanceAuthority = "conformance.example.substrate.reamde.dev"

// notesPath is the collection every record case writes to.
const notesPath = "/api/v1/" + conformanceAuthority + "/note"

// conformanceVocabulary declares the one kind the record cases use. Each
// property is load-bearing: `subject` is required (a write that omits it is
// the 422), `occurredAt` is a datetime (a year-0000 value is the second 422,
// issue #170), and `phase` is a state machine (a put that moves it is the
// 403 guard, since transitions are patch's job).
const conformanceVocabulary = `
kind: core.substrate.reamde.dev/authority
metadata:
  id: ` + conformanceAuthority + `
data:
  version: 1
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: ` + conformanceAuthority + `/note
data:
  authority: ` + conformanceAuthority + `
  names:
    singular: note
    plural: notes
  displayTemplate: "{subject}"
  properties:
    subject:
      type: string
      required: true
      description: what the note is about
    occurredAt:
      type: datetime
      description: when the thing the note records happened
    phase:
      type: state
      states: [draft, filed]
      initial: draft
      transitions:
        - from: draft
          to: filed
      description: where the note sits in its life
`

// codeCase is one request and the published code it must answer with. An
// empty code is a success case.
type codeCase struct {
	name string
	code string
	run  func(t *testing.T, e *testenv.Env)
}

func TestPublishedErrorCodesEndToEnd(t *testing.T) {
	e := testenv.Start(t)
	e.ApplyVocabularyYAML(conformanceVocabulary)

	cases := conformanceCases()
	// Covered by DECLARATION, not by outcome: a case that exists and fails
	// reports its own failure, and marking it uncovered as well would bury the
	// real message under a drift complaint.
	covered := map[string]bool{}
	for _, c := range cases {
		if c.code != "" {
			covered[c.code] = true
		}
		// Sequential and in order: the lifecycle cases share one record, so
		// the update sees what the create wrote.
		t.Run(c.name, func(t *testing.T) { c.run(t, e) })
	}
	checkEveryPublishedCode(t, covered)
}

func conformanceCases() []codeCase {
	// The record the lifecycle cases walk: created, updated, then refused a
	// stale ifVersion and a put-borne transition.
	const id = "lifecycle"
	const path = notesPath + "/" + id

	return []codeCase{{
		name: "create answers 201",
		run: func(t *testing.T, e *testenv.Env) {
			status, body := e.Do(http.MethodPut, path, map[string]any{
				"properties": map[string]any{"subject": "the first write"},
			})
			rec := wantRecord(t, status, body, http.StatusCreated)
			if rec.Version != 1 || rec.Kind != conformanceAuthority+"/note" {
				t.Fatalf("created record: %+v", rec)
			}
			if rec.Properties["phase"] != "draft" {
				t.Fatalf("the state property was not born in its initial state: %+v", rec.Properties)
			}
		},
	}, {
		name: "update answers 200",
		run: func(t *testing.T, e *testenv.Env) {
			status, body := e.Do(http.MethodPut, path, map[string]any{
				"properties": map[string]any{"subject": "the second write"},
			})
			rec := wantRecord(t, status, body, http.StatusOK)
			if rec.Version != 2 || rec.Properties["subject"] != "the second write" {
				t.Fatalf("updated record: %+v", rec)
			}
		},
	}, {
		name: "a stale ifVersion answers 409 conflict",
		code: "conflict",
		run: func(t *testing.T, e *testenv.Env) {
			// Version 1 was the create; the update above moved it to 2.
			status, body := e.Do(http.MethodPut, path, map[string]any{
				"ifVersion":  1,
				"properties": map[string]any{"subject": "written against a version that has moved"},
			})
			wantError(t, status, body, http.StatusConflict, "conflict")
		},
	}, {
		name: "a put that moves a state answers 403 guard",
		code: "guard",
		run: func(t *testing.T, e *testenv.Env) {
			status, body := e.Do(http.MethodPut, path, map[string]any{
				"properties": map[string]any{"subject": "the second write", "phase": "filed"},
			})
			wantError(t, status, body, http.StatusForbidden, "guard")
		},
	}, {
		name: "a missing required property answers 422 validation",
		code: "validation",
		run: func(t *testing.T, e *testenv.Env) {
			status, body := e.Do(http.MethodPut, notesPath+"/no-subject", map[string]any{
				"properties": map[string]any{"occurredAt": "2026-08-17T09:00:00Z"},
			})
			wantError(t, status, body, http.StatusUnprocessableEntity, "validation",
				"props.subject")
		},
	}, {
		// Issue #170: a year Postgres cannot store used to persist (the
		// columns are jsonb) and then fail every read that CASTs it, as a raw
		// driver error problemFor could only call `internal`.
		name: "a datetime outside the Postgres range answers 422 validation",
		code: "validation",
		run: func(t *testing.T, e *testenv.Env) {
			status, body := e.Do(http.MethodPut, notesPath+"/year-zero", map[string]any{
				"properties": map[string]any{
					"subject":    "an instant with no year",
					"occurredAt": "0000-01-01T00:00:00Z",
				},
			})
			wantError(t, status, body, http.StatusUnprocessableEntity, "validation",
				"props.occurredAt", "year 0000")
			// The refusal has to be the WHOLE write: a record stored here is
			// the unreadable collection the check exists to prevent.
			if status, body := e.Do(http.MethodGet, notesPath+"/year-zero", nil); status != http.StatusNotFound {
				t.Fatalf("the refused write landed: %d %s", status, body)
			}
		},
	}, {
		name: "an unknown record answers 404 not_found",
		code: "not_found",
		run: func(t *testing.T, e *testenv.Env) {
			status, body := e.Do(http.MethodGet, notesPath+"/never-written", nil)
			wantError(t, status, body, http.StatusNotFound, "not_found")
		},
	}, {
		// The generic surface may not forge the records the substrate keeps
		// for itself. This is `forbidden`, NOT `guard`: guardSystemKind wraps
		// substrate.ErrForbidden.
		name: "a system-kind write answers 403 forbidden",
		code: "forbidden",
		run: func(t *testing.T, e *testenv.Env) {
			// A token is a record, and the mint path is the only hand that
			// writes one.
			status, body := e.Do(http.MethodPost, "/api/v1/core.substrate.reamde.dev/token",
				map[string]any{"id": "forged", "properties": map[string]any{"label": "forged"}})
			wantError(t, status, body, http.StatusForbidden, "forbidden")
		},
	}, {
		name: "a body key the input does not declare answers 400 bad_request",
		code: "bad_request",
		run: func(t *testing.T, e *testenv.Env) {
			// Decoding is case-exact on purpose: `ifversion` binding to
			// `ifVersion` would drop a compare-and-set silently.
			status, body := e.Do(http.MethodPut, notesPath+"/miscased", map[string]any{
				"ifversion":  1,
				"properties": map[string]any{"subject": "a precondition nothing reads"},
			})
			wantError(t, status, body, http.StatusBadRequest, "bad_request")
		},
	}, {
		name: "a bearer the substrate never minted answers 401 auth",
		code: "auth",
		run: func(t *testing.T, e *testenv.Env) {
			// The same server and the same client; only the bearer changes.
			stranger := *e
			stranger.Token = "substrate_tok_nope"
			status, body := stranger.Do(http.MethodGet, notesPath, nil)
			wantError(t, status, body, http.StatusUnauthorized, "auth")
		},
	}, {
		// Not a record write: the pacing on the unauthenticated door is the
		// only place a running substrate answers 429.
		name: "a second login inside the interval answers 429 rate_limited",
		code: "rate_limited",
		run: func(t *testing.T, e *testenv.Env) {
			// A username of this case's own, so the per-(IP, username) bucket
			// is untouched by the registration Start already performed.
			attempt := map[string]any{"username": "conformance-pacing", "password": "wrong"}
			// The first attempt spends the whole allowance; whether it is
			// refused for the password or already for the pace is not this
			// case's assertion.
			e.Do(http.MethodPost, "/login", attempt)
			status, body := e.Do(http.MethodPost, "/login", attempt)
			wantError(t, status, body, http.StatusTooManyRequests, "rate_limited")
		},
	}, {
		// The retention horizon is 0 today, so only a NEGATIVE cursor sits
		// below it. The case is still the contract a client depends on: a
		// resume point the changelog can no longer address is a 410 naming the
		// horizon, never an empty 200 a client would read as "the beginning".
		name: "a cursor below the retention horizon answers 410 compacted",
		code: "compacted",
		run: func(t *testing.T, e *testenv.Env) {
			status, body := e.Do(http.MethodGet,
				"/api/v1/changes?from=-1", nil)
			wantError(t, status, body, http.StatusGone, "compacted")
		},
	}, {
		// LAST, and it must stay last: it closes the engine under the running
		// handler, so every request after it answers the same way.
		name: "a repository that cannot be opened answers 503 unavailable",
		code: "unavailable",
		run: func(t *testing.T, e *testenv.Env) {
			// A well-formed token whose repository will not open is a service
			// condition, not a bad credential: reporting it as 401 would read
			// as "your token is wrong" for a fault that is the server's.
			if err := e.Service.Close(); err != nil {
				t.Fatalf("close the service: %v", err)
			}
			status, header, body := rawGet(t, e, notesPath)
			wantError(t, status, body, http.StatusServiceUnavailable, "unavailable")
			// Ruling A6: every unavailable carries Retry-After, the auth path
			// included. e.Do cannot see headers, so this case builds its own
			// request.
			if header.Get("Retry-After") == "" {
				t.Errorf("503 without Retry-After: %v", header)
			}
		},
	}}
}

// rawGet performs one authenticated GET and hands back the status, the headers
// and the body, for the case that asserts a header. The response itself does
// not escape, so its body is closed here.
func rawGet(t *testing.T, e *testenv.Env, path string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.URL+path, nil)
	if err != nil {
		t.Fatalf("build GET %s: %v", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+e.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read GET %s: %v", path, err)
	}
	return resp.StatusCode, resp.Header, body
}

// unreachable names the published codes no request to a substrate started by
// testenv.Start can produce, and what stands in the way. Each entry says which
// of two things it is: the code has no producing path at all, or producing it
// needs a handler configured differently from the one Start builds. "It would
// be awkward to cover" is not an entry here, since an excuse asserts a code
// CANNOT be produced and a wrong one is a false statement this test enforces.
//
// A code leaves this map by gaining a case above; a code in it that a case
// DOES declare fails the check, because the excuse has gone stale.
var unreachable = map[string]string{
	// No producing path: the mapping in api/errors.go is dead on the wire.
	"gated": "the policy door runs only inside the agent loop (engine/agentgql.go door, " +
		"engine/agentloop.go effects), where its refusal becomes a tool result the model " +
		"reads, and engine/agents.go agentEntryError does not pass ErrGated through either, " +
		"so no HTTP response carries this code today (issue #242)",
	// Needs a different configuration: every writeUnsupported site fires on an
	// absent capability, and Start wires all of them.
	"unsupported": "501 answers a capability the deployment omits, and testenv.Start builds a " +
		"handler carrying every one; producing it needs a handler configured without one",
	// Needs deliberate fault injection: 500 is by definition the answer to
	// something no request can ask for.
	"internal": "500 is the unexpected fault, so producing one means breaking the engine " +
		"rather than sending a request",
}

// checkEveryPublishedCode holds the table to the closed set. Both directions
// are failures: a code with neither a case nor a reason, and a reason for a
// code a case now covers or internal/api no longer publishes.
func checkEveryPublishedCode(t *testing.T, covered map[string]bool) {
	t.Helper()
	published := publishedCodes(t)
	for code, name := range published {
		switch {
		case covered[code] && unreachable[code] != "":
			t.Errorf("%s (%q) is excused as unreachable, but a case above produces it: drop the excuse", name, code)
		case !covered[code] && unreachable[code] == "":
			t.Errorf("%s (%q) has no conformance case and no entry in unreachable", name, code)
		}
	}
	for code := range unreachable {
		if _, ok := published[code]; !ok {
			t.Errorf("unreachable excuses %q, which internal/api no longer publishes", code)
		}
	}
}

// publishedCodes reads the closed error-code set out of internal/api instead
// of repeating it, so the set cannot grow behind this test's back. The
// declarations are unexported and stay that way: parsing the package is
// cheaper than widening its surface for a test.
//
// The scan is deliberately wide, because a code this misses is a code with no
// case and no excuse that still passes. It reads EVERY non-test file in the
// package, `var` as well as `const`, and every name of a multi-name spec. A
// `code*` declaration whose value is not a plain string literal fails rather
// than being skipped: this test cannot know the wire word behind an alias or a
// computed value, and skipping it would be the same silent miss.
func publishedCodes(t *testing.T) map[string]string {
	t.Helper()
	const dir = "../api"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, src, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, ident := range vs.Names {
					if !strings.HasPrefix(ident.Name, "code") {
						continue
					}
					out[codeValue(t, src, ident.Name, vs, i)] = ident.Name
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s declares no code* identifier: has the set moved?", dir)
	}
	return out
}

// codeValue reads the wire word one `code*` declaration carries, and fails on
// anything it cannot read literally.
func codeValue(t *testing.T, src, name string, vs *ast.ValueSpec, i int) string {
	t.Helper()
	if i >= len(vs.Values) {
		t.Fatalf("%s: %s carries no value this test can read; the error-code set must stay plain string literals", src, name)
	}
	lit, ok := vs.Values[i].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		t.Fatalf("%s: %s is not a string literal; the error-code set must stay plain string literals so this test can enumerate it", src, name)
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatalf("%s: %s: %v", src, name, err)
	}
	return value
}

// wireRecord is the record envelope as it arrives, mirrored by hand: this
// suite asserts what the socket carries, not what a Go struct round-trips.
type wireRecord struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Version    int64          `json:"version"`
	Properties map[string]any `json:"properties"`
}

func wantRecord(t *testing.T, status int, body []byte, wantStatus int) wireRecord {
	t.Helper()
	if status != wantStatus {
		t.Fatalf("got %d, want %d: %s", status, wantStatus, body)
	}
	var rec wireRecord
	if err := json.Unmarshal(body, &rec); err != nil {
		t.Fatalf("body is not a record: %v (%s)", err, body)
	}
	return rec
}

// wantError holds one response to the wire envelope: the status, the code, and
// a substring of `problems` per fragment given. It prints the whole body on a
// mismatch, because whoever reads the failure does not yet know which half
// moved.
func wantError(t *testing.T, status int, body []byte, wantStatus int, wantCode string, problems ...string) {
	t.Helper()
	var env struct {
		Error struct {
			Code     string   `json:"code"`
			Message  string   `json:"message"`
			Problems []string `json:"problems"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("status %d: body is not the error envelope: %v (%s)", status, err, body)
	}
	if status != wantStatus || env.Error.Code != wantCode {
		t.Fatalf("got %d %q, want %d %q: %s", status, env.Error.Code, wantStatus, wantCode, body)
	}
	if env.Error.Message == "" {
		t.Errorf("%d %s carries no message: %s", status, wantCode, body)
	}
	for _, want := range problems {
		if !slices.ContainsFunc(env.Error.Problems, func(p string) bool { return strings.Contains(p, want) }) {
			t.Errorf("problems %s name nothing matching %q", fmt.Sprint(env.Error.Problems), want)
		}
	}
}
