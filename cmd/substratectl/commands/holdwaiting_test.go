package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// --hold-waiting-mappings asks the apply door to hold back a mapping whose
// source kind is absent (decision record 0106), and the CLI prints each one it
// held with the source kind it waits on and that kind's package. Without the
// flag the request carries no such key.
func TestApplyHoldWaitingMappings(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.heldMappings = []substrate.SuggestedMapping{{
		ID:      "ada.example.com/people/slackuserperson",
		From:    "providers.substrate.reamde.dev/slack/user",
		To:      "ada.example.com/people/person",
		Package: "providers.substrate.reamde.dev/slack",
		State:   substrate.SuggestedMappingWaiting,
	}}
	file := filepath.Join(t.TempDir(), "people.yaml")
	doc := `kind: substrate.reamde.dev/core/recordmapping
metadata:
  id: ada.example.com/people/slackuserperson
data:
  authority: ada.example.com
  package: people
  from: providers.substrate.reamde.dev/slack/user
  to: ada.example.com/people/person
  property: person
`
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := h.run("apply", "-f", file, "--hold-waiting-mappings")
	if err != nil {
		t.Fatalf("apply --hold-waiting-mappings: %v %s", err, stderr)
	}
	if got := string(h.fake.lastBody["holdWaitingMappings"]); got != "true" {
		t.Fatalf("holdWaitingMappings = %q, want true", got)
	}
	want := "recordmapping/ada.example.com/people/slackuserperson held: waits on providers.substrate.reamde.dev/slack/user from providers.substrate.reamde.dev/slack"
	if !strings.Contains(stdout, want) {
		t.Fatalf("stdout = %q, want a line %q", stdout, want)
	}

	if _, stderr, err := h.run("apply", "-f", file); err != nil {
		t.Fatalf("apply: %v %s", err, stderr)
	}
	if _, sent := h.fake.lastBody["holdWaitingMappings"]; sent {
		t.Fatal("a plain apply sent holdWaitingMappings")
	}
}
