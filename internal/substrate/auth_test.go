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
