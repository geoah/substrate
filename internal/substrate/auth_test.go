package substrate

import (
	"context"
	"testing"
)

func TestWithoutScopesClearsTheMarkAndKeepsThePrincipal(t *testing.T) {
	ctx := WithScopes(WithPrincipal(context.Background(), "tok1"), []string{"agents:*"})
	if _, ok := ScopesFrom(ctx); !ok {
		t.Fatal("the scoped mark did not land")
	}
	cleared := WithoutScopes(ctx)
	if _, ok := ScopesFrom(cleared); ok {
		t.Fatal("WithoutScopes left the mark")
	}
	if PrincipalFrom(cleared) != "tok1" {
		t.Fatal("WithoutScopes dropped the principal")
	}
	plain := context.Background()
	if WithoutScopes(plain) != plain {
		t.Fatal("WithoutScopes rewrapped an unmarked context")
	}
}

func TestWithDecisionNamesOneRecord(t *testing.T) {
	if got := DecisionFrom(context.Background()); got != "" {
		t.Fatalf("an unmarked context decides %q", got)
	}
	const path = "substrate.reamde.dev/core/recordpatchrequest/r1"
	if got := DecisionFrom(WithDecision(context.Background(), path)); got != path {
		t.Fatalf("DecisionFrom = %q", got)
	}
}

func TestWebhookGrantNamesOneTriggerOfOneRepository(t *testing.T) {
	ctx := WithWebhookGrant(context.Background(), "ada.example.com", "applehealth-webhook")
	if !WebhookGranted(ctx, "ada.example.com", "applehealth-webhook") {
		t.Fatal("the grant does not reach the trigger it names")
	}
	for _, c := range [][2]string{
		{"ada.example.com", "other-webhook"},
		{"bob.example.com", "applehealth-webhook"},
		{"", ""},
	} {
		if WebhookGranted(ctx, c[0], c[1]) {
			t.Fatalf("the grant reaches %s/%s", c[0], c[1])
		}
	}
	if WebhookGranted(WithWebhookGrant(context.Background(), "", ""), "", "") {
		t.Fatal("an empty grant reaches an empty trigger")
	}
	if WebhookGranted(context.Background(), "ada.example.com", "applehealth-webhook") {
		t.Fatal("an unmarked context is granted")
	}
}
