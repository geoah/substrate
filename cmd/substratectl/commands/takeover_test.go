package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TAKEOVER IS DELIBERATE (decision 0205): without --confirm repeating the
// package it refuses before it sends anything; with it, it posts the package
// twice under the actor the CLI writes as and prints the app it was taken
// from and the new declarer.
func TestTakeoverConfirmsThePackageBeforeItSends(t *testing.T) {
	h := newHarness(t)
	var got []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != pathVocabularyTakeover {
			http.NotFound(w, r)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"substrate.reamde.dev/core/package","id":"geoah/training","properties":{"declaredBy":{"ref":"substrate.reamde.dev/core/actor/6nk6ekkczdgh399nt2pqpccjz0"},"takenOverFrom":"https://workouts.example.org/client.json"}}`))
	}))
	defer srv.Close()
	h.server = srv.URL
	h.writeConfig()

	for _, args := range [][]string{
		{"takeover", "geoah/training"},
		{"takeover", "geoah/training", "--confirm", "training"},
	} {
		if _, _, err := h.run(args...); err == nil || !strings.Contains(err.Error(), "--confirm geoah/training") {
			t.Errorf("substratectl %s = %v, want a refusal naming the confirmation", strings.Join(args, " "), err)
		}
	}
	if len(got) != 0 {
		t.Fatalf("an unconfirmed takeover reached the server: %v", got)
	}
	out, _ := h.mustRun("takeover", "geoah/training", "--confirm", "geoah/training")
	if len(got) != 1 || got[0]["package"] != "geoah/training" || got[0]["confirm"] != "geoah/training" {
		t.Fatalf("the server saw %v", got)
	}
	if !strings.Contains(out, "taken over from https://workouts.example.org/client.json, declared by substrate.reamde.dev/core/actor/6nk6ekkczdgh399nt2pqpccjz0") {
		t.Fatalf("stdout = %q", out)
	}
}
