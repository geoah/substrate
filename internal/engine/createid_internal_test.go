package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The refusal of a chosen id says the fix in the terms of the hand that
// chose it: a person or an app is pointed at the API, an agent at leaving
// the id out, and a function body run as an agent's tool at host.ids.minted,
// because its author changes it, not the agent it writes as.
func TestARefusedIDSaysTheFixOfTheHandThatChoseIt(t *testing.T) {
	ty := &vocabulary.Kind{Identity: "providers.example.com/web/document"}
	agent := substrate.AgentActor("substrate.reamde.dev", "assistant", "moss")
	scrape := &vocabulary.Function{Package: "providers.example.com/web", Name: "scrape"}
	for _, tc := range []struct {
		name  string
		tx    *txn
		says  string
		never string
	}{
		{"a person", &txn{actor: substrate.ActorPerson}, "POST /api/v1/records", "host.ids.minted"},
		{"an agent", &txn{actor: agent}, "Leave the id out", "POST"},
		{
			"a function run as an agent's tool", &txn{actor: agent, effectBy: scrape},
			"function providers.example.com/web/scrape runs as " + handWords(agent) + "'s tool", "POST",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.tx.createIDRefusal(ty, "url-188899de28cfc9c926bc558686e0e786")
			if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "is not a minted id") {
				t.Fatalf("refusal = %v", err)
			}
			if !strings.Contains(err.Error(), tc.says) || strings.Contains(err.Error(), tc.never) {
				t.Fatalf("refusal = %q, want it to say %q and never %q", err, tc.says, tc.never)
			}
		})
	}
	// The function's refusal names the helper that fixes it.
	if err := (&txn{actor: agent, effectBy: scrape}).createIDRefusal(ty, "url-1"); !strings.Contains(err.Error(), "host.ids.minted(<key>)") {
		t.Fatalf("function refusal = %q", err)
	}
}
