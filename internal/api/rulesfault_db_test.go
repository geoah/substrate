package api

// A READ OF THE RULES THAT CANNOT BE MADE REFUSES (decision 0276). Every door
// that takes a token and every stream that reads its token again is walked
// with the engine's reads of the rules failing (engine.WithTestRulesFault):
// the token's row, then a person's roles. Each refuses, `unavailable`, and
// none answers from what it read before. The tables are held to the code:
// a door or a stream added later without a row fails the test.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// errRulesUnread is what the failing reads answer.
var errRulesUnread = errors.New("the rules' database did not answer")

// faultedEnv is a dbEnv whose engine fails one kind of rules read while
// failing names it ("token", "roles", "grant", "holders").
type faultedEnv struct {
	*dbEnv
	failing atomic.Pointer[string]
}

func newFaultedEnv(t *testing.T) *faultedEnv {
	t.Helper()
	f := &faultedEnv{}
	f.dbEnv = newDBEnv(t, withEngine(engine.WithTestRulesFault(func(what string) error {
		if w := f.failing.Load(); w != nil && *w == what {
			return errRulesUnread
		}
		return nil
	})))
	return f
}

func (f *faultedEnv) fail(what string) { f.failing.Store(&what) }

func (f *faultedEnv) heal() { f.failing.Store(nil) }

// ruleDoors are the functions that take a token (every caller of
// authenticate), each with the requests that reach it: the three doors of
// the routes behind bearer, and the MCP door.
var ruleDoors = map[string][]doorRequest{
	"auth.go:bearer": {
		{http.MethodGet, "/api/v1/catalog", ""},
		{http.MethodGet, "/api/v1/records", ""},
		{http.MethodGet, "/tokens", ""},
	},
	"mcp.go:serveMCP": {
		{http.MethodPost, mcpPath, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`},
	},
}

// doorRequest is one request a door answers.
type doorRequest struct{ method, path, body string }

// leaseReaders are the functions that read a stream's token again
// (tokenlease.go alive), each with how its stream is driven to that read
// with the reads failing: it answers whether the stream refused.
var leaseReaders = map[string]func(t *testing.T, f *faultedEnv, what string) bool{
	"watch.go:streamChanges":      faultWatch,
	"agents.go:streamAgentEvents": faultAgentStream,
	"export.go:getExport":         faultExport,
	"export.go:Write":             faultExport,
	"blobs.go:getBlob":            faultBlobRead,
	"blobs.go:putBlob":            faultBlobUpload,
}

func TestEveryDoorAndStreamRefusesWhenItsRulesCannotBeRead(t *testing.T) {
	t.Parallel()
	doors, readers := apiRuleSites(t)
	if want := sortedKeys(ruleDoors); !slices.Equal(doors, want) {
		t.Fatalf("the functions that take a token are %v, the table holds %v: give each its row", doors, want)
	}
	if want := sortedKeys(leaseReaders); !slices.Equal(readers, want) {
		t.Fatalf("the readers of a stream's lease are %v, the table holds %v: give each its row", readers, want)
	}
	for _, what := range []string{"token", "roles"} {
		t.Run(what, func(t *testing.T) {
			t.Parallel()
			for door, requests := range ruleDoors {
				f := newFaultedEnv(t)
				f.fail(what)
				for _, dr := range requests {
					req := httptest.NewRequest(dr.method, dr.path, strings.NewReader(dr.body))
					req.Header.Set("Authorization", "Bearer "+f.token)
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Accept", "application/json, text/event-stream")
					rec := httptest.NewRecorder()
					f.h.ServeHTTP(rec, req)
					if rec.Code != http.StatusServiceUnavailable {
						t.Errorf("%s (%s %s) with the %s unread answered %d %s, want 503", door, dr.method, dr.path, what, rec.Code, rec.Body.String())
					}
				}
			}
			for reader, drive := range leaseReaders {
				if !drive(t, newFaultedEnv(t), what) {
					t.Errorf("%s went on with the %s unread", reader, what)
				}
			}
		})
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// apiRuleSites reads the package's doors (every function that calls
// authenticate, whatever it is named) and its lease readers (every function
// but leaseEnded that calls alive).
func apiRuleSites(t *testing.T) (doors, readers []string) {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	seen, door := map[string]bool{}, map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
						switch {
						case sel.Sel.Name == "authenticate":
							door[f+":"+fn.Name.Name] = true
						case sel.Sel.Name == "alive" && fn.Name.Name != "leaseEnded":
							seen[f+":"+fn.Name.Name] = true
						}
					}
				}
				return true
			})
		}
	}
	return sortedKeys(door), sortedKeys(seen)
}

func faultWatch(t *testing.T, f *faultedEnv, what string) bool {
	srv := httptest.NewServer(f.h)
	defer srv.Close()
	br, stop := startWatch(t, srv, "/api/v1/changes?watch=1", f.token)
	defer stop()
	if line := readLine(t, br); line["bookmark"] == nil {
		t.Fatalf("the first line = %v, want the bookmark", line)
	}
	f.fail(what)
	f.moveChangelog(t)
	line := readLine(t, br)
	problem, _ := line["error"].(map[string]any)
	return line["recordId"] == nil && problem["code"] == codeUnavailable
}

func faultAgentStream(t *testing.T, f *faultedEnv, what string) bool {
	started := make(chan struct{})
	f.wrap = func(ds substrate.Dataset) substrate.Dataset { return holdingChat{Dataset: ds, started: started} }
	req := httptest.NewRequest(http.MethodPost, helperChatPath, strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		f.h.ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the chat turn never started")
	}
	f.fail(what)
	f.moveChangelog(t)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		return false
	}
	body := rec.Body.String()
	return strings.Contains(body, `"code":"unavailable"`) && !strings.Contains(body, `"code":"auth"`)
}

func faultExport(t *testing.T, f *faultedEnv, what string) bool {
	w := &revokingWriter{ResponseRecorder: httptest.NewRecorder(), revoke: func() {
		f.fail(what)
		f.clock.advance(2 * leaseTransferEvery)
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/export", nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	return serveAborting(f.dbEnv, w, req) && w.revoked && !archiveHoldsManifest(w.Body.Bytes())
}

func faultBlobRead(t *testing.T, f *faultedEnv, what string) bool {
	data := bytes.Repeat([]byte("substrate "), 4*blobChunk/10)
	sum := sha256.Sum256(data)
	digest := "blob-sha256-" + hex.EncodeToString(sum[:])
	blobPut(t, f.dbEnv, "/api/v1/blobs/"+digest, string(data), map[string]string{"Content-Type": "application/octet-stream"})
	w := &revokingWriter{ResponseRecorder: httptest.NewRecorder(), revoke: func() {
		f.fail(what)
		f.clock.advance(2 * leaseTransferEvery)
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/blobs/"+digest, nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	return serveAborting(f.dbEnv, w, req) && w.Body.Len() < len(data)
}

// failingBody turns the reads failing when the upload's body is first read,
// after the door read the token.
type failingBody struct {
	r    *bytes.Reader
	turn func()
	once bool
}

func (b *failingBody) Read(p []byte) (int, error) {
	if !b.once {
		b.once = true
		b.turn()
	}
	return b.r.Read(p)
}

func faultBlobUpload(t *testing.T, f *faultedEnv, what string) bool {
	data := []byte("an upload whose rules stop answering")
	body := &failingBody{r: bytes.NewReader(data), turn: func() { f.fail(what) }}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/blobs", body)
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("Content-Type", "application/octet-stream")
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	f.heal()
	sum := sha256.Sum256(data)
	stored := f.do(t, http.MethodGet, "/api/v1/blobs/blob-sha256-"+hex.EncodeToString(sum[:]), f.token, nil)
	return rec.Code == http.StatusServiceUnavailable && stored.Code == http.StatusNotFound
}

// moveChangelog writes a record as the engine's own hand, which reads no
// rules, so a stream reads its lease again.
func (f *faultedEnv) moveChangelog(t *testing.T) {
	t.Helper()
	if _, err := f.ds.Put(context.Background(), substrate.ActorSystem, substrate.PutInput{
		Kind: vocabulary.CoreKind("contact"), ID: "moved-" + strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""),
		Properties: map[string]any{"name": "moved"},
	}); err != nil {
		t.Fatalf("move the changelog: %v", err)
	}
}
