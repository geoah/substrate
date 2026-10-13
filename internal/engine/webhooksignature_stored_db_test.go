package engine

// The recheck of a stored webhook request against a signature declared after
// the door stored it.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A body-only declaration signs no timestamp, so the recheck of a stored
// request never needs the time the door received it: it reads no changelog
// entry for a whole-second `fire.at`, and an envelope with no readable
// `fire.at` is checked on its body alone instead of refused.
func TestStoredBodyOnlySignatureNeedsNoReceipt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var statements statementLog
	ds := openInternalDataset(t, WithTestQueryTracer(&statements))
	const secret = "a-body-only-secret"
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: kindSecret, ID: "hooks/bodyonly", Properties: map[string]any{"value": secret},
	}); err != nil {
		t.Fatalf("put the secret: %v", err)
	}
	sig, err := parseWebhookSignature(map[string]any{
		"algorithm": "hmac-sha256", "signatureHeader": "x-sig", "encoding": "hex",
		"secret": kindSecret + "/hooks/bodyonly",
	})
	if err != nil {
		t.Fatal(err)
	}
	tr := &trigger{ID: "hook-bodyonly", Webhook: true, WebhookSignature: sig}
	const body = `{"n":"stored"}`
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	envelope := func(fire map[string]any) map[string]any {
		return map[string]any{
			"fire": fire,
			"request": map[string]any{
				"headers": map[string]any{"x-sig": hex.EncodeToString(mac.Sum(nil))},
				"body":    map[string]any{"text": body},
			},
		}
	}
	h, err := ds.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	head := h.Seq
	statements.take()
	if err := ds.checkStoredWebhookSignature(ctx, tr, envelope(map[string]any{"id": "hook-x", "at": "2026-10-11T12:00:00Z"}), head); err != nil {
		t.Fatalf("a whole-second fire.at: %v", err)
	}
	for _, sql := range statements.take() {
		if strings.Contains(sql, "FROM changelog") && strings.Contains(sql, "WHERE seq = $1") {
			t.Fatalf("the recheck read a changelog entry: %s", strings.Join(strings.Fields(sql), " "))
		}
	}
	if err := ds.checkStoredWebhookSignature(ctx, tr, envelope(map[string]any{"id": "hook-y"}), head); err != nil {
		t.Fatalf("no fire.at: %v", err)
	}
}
