package engine

// The public door in front of an agent callable: the bound on what the
// agent reads as its user message.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// putWebhookTrigger writes a webhook trigger on callable, the record path of
// a function or an agent, and returns its id.
func putWebhookTrigger(t *testing.T, ds *dataset, callable string, webhook map[string]any) string {
	t.Helper()
	tr, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"webhook": webhook},
			"callable": callable,
		},
	})
	if err != nil {
		t.Fatalf("put trigger on %s: %v", callable, err)
	}
	return tr.ID
}

// An agent reads a webhook request in its user message, which becomes an
// llm/message record and a model turn, so the door holds what rides inline
// to 1 MiB when the callable is an agent: a body, or multipart inline
// values. Past it the request is refused as too large and nothing is
// stored. A function callable still takes the door's 8 MiB.
func TestAnAgentWebhookIsHeldToOneMiB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)
	agent := putWebhookTrigger(t, ds, vocabulary.RecordPath(kindAgent, crewPackage+"/keeper"), map[string]any{})
	function := putWebhookTrigger(t, ds, vocabulary.RecordPath(kindFunction, crewPackage+"/keyecho"), map[string]any{})
	authority := testdb.Repository(t)
	body := func(n int) substrate.WebhookRequest {
		return substrate.WebhookRequest{
			Method: "POST", ContentType: "text/plain",
			Headers: map[string]string{"content-type": "text/plain"},
			Body:    bytes.Repeat([]byte("x"), n),
		}
	}
	form := substrate.WebhookRequest{
		Method: "POST", ContentType: "multipart/form-data", Headers: map[string]string{},
		Parts: []substrate.WebhookPart{
			{Name: "a", Value: string(bytes.Repeat([]byte("a"), 600<<10))},
			{Name: "b", Value: string(bytes.Repeat([]byte("b"), 600<<10))},
		},
	}
	stored := func(trigger string) int {
		t.Helper()
		rows, err := ds.TriggerFailures(ctx, trigger)
		if err != nil {
			t.Fatal(err)
		}
		return len(rows)
	}
	for name, req := range map[string]substrate.WebhookRequest{
		"a body one byte past 1 MiB":  body(1<<20 + 1),
		"two inline parts of 600 KiB": form,
	} {
		_, err := ds.svc.receiveWebhook(ctx, authority, agent, "", req, webhookFireHeld)
		if !errors.Is(err, substrate.ErrTooLarge) {
			t.Fatalf("%s to an agent: err = %v, want ErrTooLarge", name, err)
		}
	}
	if n := stored(agent); n != 0 {
		t.Fatalf("the refused requests left %d ledger rows", n)
	}
	if _, err := ds.svc.receiveWebhook(ctx, authority, agent, "", body(1<<20), webhookFireHeld); err != nil {
		t.Fatalf("a body of 1 MiB to an agent: %v", err)
	}
	if _, err := ds.svc.receiveWebhook(ctx, authority, function, "", body(2<<20), webhookFireHeld); err != nil {
		t.Fatalf("a body of 2 MiB to a function: %v", err)
	}
	if a, f := stored(agent), stored(function); a != 1 || f != 1 {
		t.Fatalf("pending rows: agent %d, function %d, want one each", a, f)
	}
}

// sigBundlePackage is a bundle with an oauth2 block whose one agent is a
// webhook trigger's callable, so `secret: oauthClientSecret` resolves
// through the agent's package.
const sigBundlePackage = "sighook.test.dev/sighook"

// installSignedAgentBundle installs sigBundlePackage: its OAuth client config
// kind, the config record holding clientSecret, and the `hooked` agent on a
// fresh provider row against fake.
func installSignedAgentBundle(t *testing.T, ds *dataset, fake *fakeLLM, clientSecret string) {
	t.Helper()
	ctx := context.Background()
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "sighookllm",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-sighookllm"},
	}); err != nil {
		t.Fatalf("put llm/provider row: %v", err)
	}
	docs := []map[string]any{
		vocabulary.PackageManifest(sigBundlePackage, 0),
		vocabulary.BundleManifest(sigBundlePackage, map[string]any{
			"description": "an agent behind a signed webhook",
			"inputs":      map[string]any{"client": map[string]any{"kind": sigBundlePackage + "/sigconfig"}},
			"installs":    []any{sigBundlePackage + "/sigconfig", sigBundlePackage + "/hooked"},
			"oauth2": map[string]any{
				"clientInput":           "client",
				"authorizationEndpoint": "https://provider.example/authorize",
				"tokenEndpoint":         "https://provider.example/token",
			},
		}),
		vocabulary.KindManifest(sigBundlePackage,
			map[string]any{"singular": "sigconfig"},
			map[string]any{
				"traits": []any{"substrate.reamde.dev/core/oauth2"},
				"properties": map[string]any{
					"authorizationEndpoint": map[string]any{"type": "url"},
					"tokenEndpoint":         map[string]any{"type": "url"},
					"clientId":              map[string]any{"type": "string"},
					"clientSecret":          map[string]any{"type": "secret"},
				},
			}),
		vocabulary.AgentManifest(sigBundlePackage, "hooked", map[string]any{
			"description": "reads a signed delivery", "prompt": "You read deliveries.",
			"provider": "sighookllm", "model": "sighook",
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install the signed agent bundle: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: sigBundlePackage + "/sigconfig", ID: "default",
		Properties: map[string]any{"clientId": "client-1", "clientSecret": clientSecret},
	}); err != nil {
		t.Fatalf("put the client record: %v", err)
	}
}

// agentSignature is a hex, millisecond-stamped `{timestamp}{body}` scheme
// under the callable's bundle's OAuth client secret, WHOOP's minus its
// base64.
func agentSignature() map[string]any {
	return map[string]any{
		"algorithm": "hmac-sha256", "signatureHeader": "x-agent-signature", "timestampHeader": "x-agent-timestamp",
		"message": "{timestamp}{body}", "encoding": "hex", "secret": "oauthClientSecret",
	}
}

// agentSignedHook is a JSON request signed under secret now.
func agentSignedHook(body, secret string) substrate.WebhookRequest {
	stamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stamp + body))
	return substrate.WebhookRequest{
		Method: "POST", ContentType: "application/json",
		Headers: map[string]string{
			"content-type": "application/json", "x-agent-timestamp": stamp,
			"x-agent-signature": hex.EncodeToString(mac.Sum(nil)),
		},
		Body: []byte(body),
	}
}

// A declared signature in front of an agent callable resolves
// `oauthClientSecret` through the agent's own package, at write time and at
// the door, and holds a stored request to it before the loop runs. On a
// package with no oauth2 block the declaration never lands. At the door a
// request under another secret is refused and one under the client secret
// runs the agent. A request stored before the trigger declared the
// signature is checked on its retry: the one the client secret signed runs,
// and a forged one parks naming the signature without a model turn.
func TestAnAgentWebhookChecksItsBundlesClientSecret(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	const clientSecret = "the-client-secret"
	installSignedAgentBundle(t, ds, fake, clientSecret)
	hooked := vocabulary.RecordPath(kindAgent, sigBundlePackage+"/hooked")
	authority := testdb.Repository(t)

	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"webhook": map[string]any{"signature": agentSignature()}},
			"callable": vocabulary.RecordPath(kindAgent, crewPackage+"/keeper"),
		},
	}); !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "declares no oauth2 block") {
		t.Fatalf("oauthClientSecret on an agent whose package has no oauth2 block: err = %v, want refused", err)
	}

	door := putWebhookTrigger(t, ds, hooked, map[string]any{"signature": agentSignature()})
	if _, err := ds.svc.receiveWebhook(ctx, authority, door, "", agentSignedHook(`{"n":"forged"}`, "not-the-client-secret"), webhookFireInline); !errors.Is(err, substrate.ErrNotFound) || !strings.Contains(err.Error(), "signature does not match") {
		t.Fatalf("a request under another secret: err = %v, want refused, signature does not match", err)
	}
	if n := threadCountOf(t, ds, sigBundlePackage+"/hooked"); n != 0 {
		t.Fatalf("the refused request opened %d threads", n)
	}
	fake.script("sighook", fakeTurn{content: "read it"})
	if _, err := ds.svc.receiveWebhook(ctx, authority, door, "", agentSignedHook(`{"n":"signed"}`, clientSecret), webhookFireInline); err != nil {
		t.Fatalf("a request under the client secret: %v", err)
	}
	if n := threadCountOf(t, ds, sigBundlePackage+"/hooked"); n != 1 {
		t.Fatalf("the signed request opened %d threads, want 1", n)
	}
	if left, err := ds.TriggerFailures(ctx, door); err != nil || len(left) != 0 {
		t.Fatalf("the signed delivery left %+v (%v)", left, err)
	}

	kept := []any{"x-agent-signature", "x-agent-timestamp"}
	later := putWebhookTrigger(t, ds, hooked, map[string]any{"headers": kept})
	signed, err := ds.svc.receiveWebhook(ctx, authority, later, "", agentSignedHook(`{"n":"stored-signed"}`, clientSecret), webhookFireHeld)
	if err != nil {
		t.Fatalf("receive the stored signed request: %v", err)
	}
	forged, err := ds.svc.receiveWebhook(ctx, authority, later, "", agentSignedHook(`{"n":"stored-forged"}`, "not-the-client-secret"), webhookFireHeld)
	if err != nil {
		t.Fatalf("receive the stored forged request: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger, ID: later,
		Properties: map[string]any{
			"source":   map[string]any{"webhook": map[string]any{"headers": kept, "signature": agentSignature()}},
			"callable": hooked,
		},
	}); err != nil {
		t.Fatalf("declare the signature: %v", err)
	}
	rows, err := ds.TriggerFailures(ctx, later)
	if err != nil {
		t.Fatal(err)
	}
	byFire := map[string]int64{}
	for _, r := range rows {
		byFire[r.FireID] = r.ID
	}
	if _, err := ds.RetryTriggerFailure(ctx, later, byFire[forged]); !errors.Is(err, substrate.ErrParked) || !strings.Contains(err.Error(), "signature does not match") {
		t.Fatalf("the stored forged request: err = %v, want parked, signature does not match", err)
	}
	if n := threadCountOf(t, ds, sigBundlePackage+"/hooked"); n != 1 {
		t.Fatalf("the forged stored request opened a thread: %d, want 1", n)
	}
	fake.script("sighook", fakeTurn{content: "read the stored one"})
	if _, err := ds.RetryTriggerFailure(ctx, later, byFire[signed]); err != nil {
		t.Fatalf("the stored signed request: %v", err)
	}
	if n := threadCountOf(t, ds, sigBundlePackage+"/hooked"); n != 2 {
		t.Fatalf("the stored signed request opened %d threads in all, want 2", n)
	}
}

// A request admitted while its trigger called a function rides up to the
// function's 8 MiB. Pointed at an agent afterwards, the trigger holds the
// stored request to the agent's 1 MiB before the loop runs it: a retry by
// hand and the dispatcher's resume each park it on its first attempt with
// no thread and no model turn, and a body of exactly 1 MiB, which rides as
// base64, still runs.
func TestAnAgentWebhookStoredForAFunctionIsHeldToOneMiB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	keeper := vocabulary.RecordPath(kindAgent, crewPackage+"/keeper")
	hook := putWebhookTrigger(t, ds, vocabulary.RecordPath(kindFunction, crewPackage+"/keyecho"), map[string]any{})
	authority := testdb.Repository(t)
	receive := func(name string, req substrate.WebhookRequest) string {
		t.Helper()
		fid, err := ds.svc.receiveWebhook(ctx, authority, hook, "", req, webhookFireHeld)
		if err != nil {
			t.Fatalf("receive %s for the function: %v", name, err)
		}
		return fid
	}
	// 0xff is no UTF-8 byte, so the body rides the envelope as base64.
	binary := func(n int) substrate.WebhookRequest {
		return substrate.WebhookRequest{
			Method: "POST", ContentType: "application/octet-stream",
			Headers: map[string]string{"content-type": "application/octet-stream"},
			Body:    bytes.Repeat([]byte{0xff}, n),
		}
	}
	retried := receive("a 2 MiB body", binary(2<<20))
	resumed := receive("two inline parts of 600 KiB", substrate.WebhookRequest{
		Method: "POST", ContentType: "multipart/form-data", Headers: map[string]string{},
		Parts: []substrate.WebhookPart{
			{Name: "a", Value: string(bytes.Repeat([]byte("a"), 600<<10))},
			{Name: "b", Value: string(bytes.Repeat([]byte("b"), 600<<10))},
		},
	})
	fits := receive("a 1 MiB body", binary(1<<20))
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger, ID: hook,
		Properties: map[string]any{"source": map[string]any{"webhook": map[string]any{}}, "callable": keeper},
	}); err != nil {
		t.Fatalf("point the trigger at the agent: %v", err)
	}
	byFire := func() map[string]substrate.TriggerFailure {
		t.Helper()
		rows, err := ds.TriggerFailures(ctx, hook)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]substrate.TriggerFailure{}
		for _, r := range rows {
			out[r.FireID] = r
		}
		return out
	}
	const capped = "an agent callable reads at most 1 MiB"
	if _, err := ds.RetryTriggerFailure(ctx, hook, byFire()[retried].ID); !errors.Is(err, substrate.ErrParked) || !strings.Contains(err.Error(), capped) {
		t.Fatalf("a hand's retry of the 2 MiB body: err = %v, want parked, %q", err, capped)
	}
	fake.script("keep", fakeTurn{content: "read it"})
	ds.runPendingWebhooks(ctx)
	rows := byFire()
	if r, ok := rows[resumed]; !ok || r.Attempts != 1 || !strings.Contains(r.LastError, capped) {
		t.Fatalf("the resumed 1.2 MiB form: %+v (present %v), want parked on attempt 1, %q", r, ok, capped)
	}
	if r, ok := rows[fits]; ok {
		t.Fatalf("the 1 MiB body did not run: %+v", r)
	}
	if n := threadCountOf(t, ds, crewPackage+"/keeper"); n != 1 {
		t.Fatalf("keeper threads: %d, want 1, the 1 MiB body's", n)
	}
	if n := len(fake.requestsOf("keep")); n != 1 {
		t.Fatalf("model turns: %d, want 1, the 1 MiB body's", n)
	}
}
