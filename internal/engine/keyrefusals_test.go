package engine

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/providersecret"
)

func refusedWith(status int) error {
	return providersecret.MarkKeyRefused(status, errors.New("refused"))
}

// A person's own key is one repository's row under one stored value: its
// refusal is not another repository's, nor a later key's on the same row, and
// an answer to a call on the old key does not clear the new one's. The
// operator's key is the same fact for every reader.
func TestKeyRefusalsKeepOwnKeysApart(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	var table keyRefusals
	a := ownKeyID("a.example.com", "openai", "secret:one")
	table.observe(a, refusedWith(http.StatusUnauthorized), now)
	if got := table.status(a); got != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", got)
	}
	if got := table.status(ownKeyID("b.example.com", "openai", "secret:one")); got != 0 {
		t.Fatalf("another repository's row reads %d", got)
	}
	if got := table.status(operatorKeyID("openai")); got != 0 {
		t.Fatalf("the operator's key reads %d", got)
	}
	replaced := ownKeyID("a.example.com", "openai", "secret:two")
	if got := table.status(replaced); got != 0 {
		t.Fatalf("a new key on the row reads %d", got)
	}
	table.observe(replaced, refusedWith(http.StatusForbidden), now)
	table.observe(a, nil, now)
	if got := table.status(replaced); got != http.StatusForbidden {
		t.Fatalf("the new key reads %d after the old one was accepted, want its own 403", got)
	}
	table.observe(replaced, errors.New("a 500"), now)
	if got := table.status(replaced); got != http.StatusForbidden {
		t.Fatalf("a failure that is not a refusal changed the status to %d", got)
	}
	table.observe(replaced, nil, now)
	if got := table.status(replaced); got != 0 {
		t.Fatalf("an accepted call left %d", got)
	}
}

// The drain waits a minute after the first refusal and twice as long after
// each one after it, up to an hour, and one drain is let through when a step
// ends.
func TestKeyRefusalsBackOffTheDrain(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	var table keyRefusals
	k := operatorKeyID("openai")
	if !table.drainMay(k, start) {
		t.Fatal("a key nobody refused is held")
	}
	now := start
	for _, step := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour} {
		table.observe(k, refusedWith(http.StatusUnauthorized), now)
		if table.drainMay(k, now.Add(step-time.Second)) {
			t.Fatalf("let through %s into a %s step", step-time.Second, step)
		}
		now = now.Add(step)
		if !table.drainMay(k, now) {
			t.Fatalf("held at the end of a %s step", step)
		}
		if table.drainMay(k, now) {
			t.Fatal("a second drain was let through at the same step")
		}
	}
	table.observe(k, nil, now)
	if !table.drainMay(k, now) {
		t.Fatal("held after an accepted call")
	}
}

func TestVendorName(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ label, id, want string }{
		{"openai", "openai", "OpenAI"},
		{"OpenAI", "openai", "OpenAI"},
		{"", "anthropic", "Anthropic"},
		{"my gateway", "gw", "my gateway"},
		{"", "gw", "Gw"},
		{"", "", ""},
	} {
		if got := vendorName(c.label, c.id); got != c.want {
			t.Errorf("vendorName(%q, %q) = %q, want %q", c.label, c.id, got, c.want)
		}
	}
}
