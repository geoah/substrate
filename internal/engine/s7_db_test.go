package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// A7's PATCH semantics, verified against the engine: a top-level null DELETES
// the property (never stores a null — literal null is unwritable), and a state
// value among the properties is a TRANSITION that stamps its clock.
func TestPatchNullDeleteAndStateTransition_S7(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)

	created := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "tasks.substrate.reamde.dev/task",
		Properties: map[string]any{
			"title":       "ship it",
			"description": "wire the last handler",
		},
	})
	if created.Properties["description"] != "wire the last handler" {
		t.Fatalf("seed description = %v", created.Properties["description"])
	}
	if got := created.Properties["status"]; got != "open" {
		t.Fatalf("initial status = %v, want open", got)
	}

	// null DELETES the property — it must not read back as a stored null.
	patched, err := ds.Patch(ctx, owner, "tasks.substrate.reamde.dev/task", created.ID, substrate.PatchInput{
		Properties: map[string]any{"description": nil},
	})
	if err != nil {
		t.Fatalf("null-delete patch: %v", err)
	}
	if v, present := patched.Properties["description"]; present {
		t.Fatalf("description survived a null-delete as %v (present=%v); literal null is unwritable", v, present)
	}

	// A state value among the properties is a TRANSITION (open → done), and the
	// declared stamp lands.
	done, err := ds.Patch(ctx, owner, "tasks.substrate.reamde.dev/task", created.ID, substrate.PatchInput{
		Properties: map[string]any{"status": "done"},
	})
	if err != nil {
		t.Fatalf("state transition patch: %v", err)
	}
	if done.Properties["status"] != "done" {
		t.Fatalf("status after transition = %v, want done", done.Properties["status"])
	}
	if _, stamped := done.Properties["completedAt"]; !stamped {
		t.Fatalf("the open→done transition must stamp completedAt; properties = %v", done.Properties)
	}
}

// A token's optional expiry, against the engine: a live one round-trips onto
// the TokenInfo and the stored record, and a passed one makes Authenticate
// fail with an auth error — server-enforced, no revoke step.
func TestTokenExpiry_S7(t *testing.T) {
	ctx := context.Background()
	svc, ds := newDataset(t)

	future := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	info, secret, err := ds.MintToken(ctx, "scripted", &future)
	if err != nil {
		t.Fatalf("mint with an expiry: %v", err)
	}
	if info.ExpiresAt == nil || !info.ExpiresAt.Equal(future) {
		t.Fatalf("minted expiresAt = %v, want %v", info.ExpiresAt, future)
	}

	_, got, err := svc.Authenticate(ctx, secret)
	if err != nil {
		t.Fatalf("authenticate live token: %v", err)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(future) {
		t.Fatalf("authenticated expiresAt = %v, want %v", got.ExpiresAt, future)
	}
	if got.Label != "scripted" {
		t.Fatalf("authenticated label = %q", got.Label)
	}
	row := mustGet(t, ds, "core.substrate.reamde.dev/token", info.ID)
	if row.Properties["expiresAt"] == nil {
		t.Fatalf("token row missing expiresAt: %v", row.Properties)
	}
	// The hash is secret-typed: the read surface redacts it, so a token list
	// can never hand back the digest it authenticates against.
	if row.Properties["hash"] != "<redacted>" {
		t.Fatalf("token row exposed its hash: %v", row.Properties["hash"])
	}

	past := time.Now().Add(-time.Hour).UTC()
	_, expiredSecret, err := ds.MintToken(ctx, "stale", &past)
	if err != nil {
		t.Fatalf("mint expired token: %v", err)
	}
	if _, _, err := svc.Authenticate(ctx, expiredSecret); err == nil {
		t.Fatal("an expired token must not authenticate")
	} else {
		wantErr(t, err, substrate.ErrAuth, "expired token")
	}
}
