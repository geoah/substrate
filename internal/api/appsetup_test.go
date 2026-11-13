package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// What an app's approval sets up, the handler's half: the check's reading
// of its body and the queue its plans wait in. What a plan reads and an
// approval installs is the engine's, in appsetup_db_test.go.

const (
	applePkg       = "providers.substrate.reamde.dev/apple"
	appleHealthPkg = "providers.substrate.reamde.dev/applehealth"
	healthPkg      = "substrate.reamde.dev/health"
	healthAccount  = appleHealthPkg + "/account"
	healthWebhook  = "applehealth-webhook"
	syncAccountID  = "substrate-sync-1a2b3c4d"
	// What the code the applehealth package runs reaches (decision 0215),
	// as the scopes that cover it, the two internal kinds by their exact
	// scopes; and what Substrate Sync asks for: its own kinds and those.
	appleHealthScopes = "write:" + appleHealthPkg + "/* write:" + healthAccount + " write:" + appleHealthPkg + "/export" +
		" write:" + healthPkg + "/* write:substrate.reamde.dev/observations/*"
	syncScopes = "write:" + applePkg + "/* " + appleHealthScopes
)

func appleHealthApproval() []string { return strings.Fields(appleHealthScopes) }

// syncDetails is what Substrate Sync asks to have set up.
var syncDetails = `[
  {"type": "package", "package": "` + applePkg + `", "version": 3},
  {"type": "package", "package": "` + appleHealthPkg + `", "version": 5},
  {"type": "account", "kind": "` + healthAccount + `", "id": "` + syncAccountID + `",
   "properties": {"displayName": "Ada's iPhone", "timezone": "Europe/London"}},
  {"type": "webhook", "trigger": "` + healthWebhook + `"}
]`

func setupRequest(scopes, details string) map[string]string {
	p := appRequest(scopes)
	p["authorization_details"] = details
	return p
}

func resolvedSyncScopes() []string { return strings.Fields(syncScopes) }

// progress is a request's rows' progress, in order.
func progress(v substrate.AuthorizationRequest) string {
	out := make([]string, len(v.Setup))
	for i, row := range v.Setup {
		out[i] = row.Progress
	}
	return strings.Join(out, ",")
}

// waitEnded is the ids of the next n set ups to end.
func waitEnded(t *testing.T, ended <-chan string, n int) []string {
	t.Helper()
	var out []string
	for range n {
		select {
		case id := <-ended:
			out = append(out, id)
		case <-time.After(10 * time.Second):
			t.Fatalf("%d of %d set ups ended", len(out), n)
		}
	}
	return out
}

// checkRequest is a check's body as an app sends it.
func checkRequest(scopes, details string) []byte {
	return []byte(`{"scope":` + jsonString(scopes) + `,"details":` + details + `}`)
}

// A check reads its details as /oauth/authorize reads its parameter: the
// same strict reading per type and the same bound, from the `details` key.
//
// Each refusal comes before the check records the build: the stub records
// none, so reaching it would answer 500. What a check without details
// answers is the engine's (TestAnAppAsksWhetherItIsStillSetUp).
func TestACheckReadsDetailsAsTheAuthorizationEndpointDoes(t *testing.T) {
	e := newAuthorizeStub(t)
	long := `[{"type":"account","kind":"` + healthAccount + `","id":"a","properties":{"note":"` + strings.Repeat("x", 2100) + `"}}]`
	for name, body := range map[string]string{
		"an unknown scope verb":           `{"scope":"everything","details":[]}`,
		"a url in a request":              `{"scope":"read:*","details":[{"type":"webhook","trigger":"t","url":"https://example.com"}]}`,
		"an empty url":                    `{"scope":"read:*","details":[{"type":"webhook","trigger":"t","url":""}]}`,
		"null properties":                 `{"scope":"read:*","details":[{"type":"account","kind":"` + healthAccount + `","id":"a","properties":null}]}`,
		"another type's member":           `{"scope":"read:*","details":[{"type":"package","package":"` + applePkg + `","version":3,"trigger":"t"}]}`,
		"the default id":                  `{"scope":"read:*","details":[{"type":"account","kind":"` + healthAccount + `","id":"default"}]}`,
		"past the bound":                  `{"scope":"read:*","details":` + long + `}`,
		"the parameter's name, not a key": `{"scope":"read:*","authorization_details":[]}`,
		"an unknown key":                  `{"scope":"read:*","details":[],"extra":1}`,
		"details not a list":              `{"scope":"read:*","details":null}`,
	} {
		if rec := e.do(http.MethodPost, "/api/v1/authorizations/check", e.owner, []byte(body), "application/json"); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: answered %d %s, want 400", name, rec.Code, rec.Body.String())
		}
		if rec := e.do(http.MethodGet, authorizeURL(setupRequest("read:*", strings.TrimSuffix(strings.TrimPrefix(body, `{"scope":"read:*","details":`), "}"))), "", nil, ""); name != "an unknown scope verb" &&
			name != "the parameter's name, not a key" && name != "an unknown key" && rec.Code != http.StatusBadRequest {
			t.Errorf("%s: /oauth/authorize answered %d, want the same refusal", name, rec.Code)
		}
	}
}

// A check's plan waits its turn, never refused: one at a time per token, and
// no more than the server's bound at once, a token's queued checks holding at
// most one of those. A check whose caller left stops waiting and holds
// nothing.
func TestACheckPlanWaitsItsTurn(t *testing.T) {
	q := newCheckTurns(2)
	ctx := context.Background()
	// waiting reports whether principal has n checks holding or wanting its
	// turn, and holds the turn itself.
	waiting := func(principal string, n int) func() bool {
		return func() bool {
			q.mu.Lock()
			defer q.mu.Unlock()
			turn := q.tokens[principal]
			return turn != nil && turn.refs == n && len(turn.slot) == 1
		}
	}
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("never: %s", what)
			}
			time.Sleep(time.Millisecond)
		}
	}
	turns := make(chan func(), 1)
	waitIn := func(ctx context.Context, principal string) chan error {
		errs := make(chan error, 1)
		go func() {
			release, err := q.wait(ctx, principal)
			if err == nil {
				turns <- release
			}
			errs <- err
		}()
		return errs
	}
	ranBeside := func(what string) {
		t.Helper()
		select {
		case <-turns:
			t.Fatalf("%s ran beside the plans holding the turns", what)
		default:
		}
	}

	first, err := q.wait(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	waitIn(ctx, "a")
	until("a's second check waits for its first", waiting("a", 2))
	ranBeside("a's second check")
	// One that leaves while it waits for its token's turn holds nothing.
	left, leave := context.WithCancel(ctx)
	errs := waitIn(left, "a")
	until("a's third check waits", waiting("a", 3))
	leave()
	if err := <-errs; !errors.Is(err, context.Canceled) {
		t.Fatalf("a check whose caller left = %v", err)
	}
	until("a's third check gave its place back", waiting("a", 2))
	first()
	second := <-turns

	// Another token takes the server's second plan, and a third waits for
	// one, holding its own token's turn.
	other, err := q.wait(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	waitIn(ctx, "c")
	until("c waits for one of the server's plans", waiting("c", 1))
	ranBeside("a third token's check")
	// One that leaves while it waits for the server gives its token's turn
	// back.
	left, leave = context.WithCancel(ctx)
	errs = waitIn(left, "d")
	until("d waits for one of the server's plans", waiting("d", 1))
	leave()
	if err := <-errs; !errors.Is(err, context.Canceled) {
		t.Fatalf("a check whose caller left = %v", err)
	}
	other()
	third := <-turns
	second()
	third()
	if len(q.tokens) != 0 || len(q.plans) != 0 {
		t.Fatalf("after every check: %d tokens, %d plans held", len(q.tokens), len(q.plans))
	}
}

func jsonString(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}
