package engine

import (
	"errors"
	"fmt"
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

// Two repositories that store the same apiKey value on rows of the same id
// hold two keys, and the table's digest of one is not the other's.
func TestOwnKeyDigestCoversTheRepository(t *testing.T) {
	t.Parallel()
	a := ownKeyID("a.example.com", "openai", "secret:one")
	b := ownKeyID("b.example.com", "openai", "secret:one")
	if a.key == b.key {
		t.Fatalf("both repositories digest the stored value to %s", a.key)
	}
	if again := ownKeyID("a.example.com", "openai", "secret:one"); again.key != a.key {
		t.Fatalf("the digest moved: %s, then %s", a.key, again.key)
	}
}

// The drain waits a minute after the first refusal and twice as long after
// each refused probe, up to an hour. When a wait ends, one drain takes the
// probe, right before its request, and every other drain waits for its
// answer. A probe that meets another failure spends the step without
// doubling it, and an accepted call ends the wait.
func TestKeyRefusalsBackOffTheDrain(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	var table keyRefusals
	k := operatorKeyID("openai")
	if probe, ok := table.takeDrain(k, start); !ok || probe || table.drainHeld(k, start) {
		t.Fatalf("a key nobody refused: takeDrain = %v, %v; held %v", probe, ok, table.drainHeld(k, start))
	}
	table.observe(k, refusedWith(http.StatusUnauthorized), start)
	now := start
	for _, step := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour} {
		early := now.Add(step - time.Second)
		if !table.drainHeld(k, early) {
			t.Fatalf("not held %s into a %s step", step-time.Second, step)
		}
		if _, ok := table.takeDrain(k, early); ok {
			t.Fatalf("a probe taken %s into a %s step", step-time.Second, step)
		}
		now = now.Add(step)
		if table.drainHeld(k, now) {
			t.Fatalf("held at the end of a %s step", step)
		}
		if probe, ok := table.takeDrain(k, now); !ok || !probe {
			t.Fatalf("takeDrain at the end of a %s step = %v, %v, want the probe", step, probe, ok)
		}
		if _, ok := table.takeDrain(k, now); ok || !table.drainHeld(k, now) {
			t.Fatal("a second drain was let through while the probe is out")
		}
		table.observe(k, refusedWith(http.StatusUnauthorized), now)
		table.endProbe(k, now)
	}

	// The vendor answers the probe with a 500: the step stays an hour.
	now = now.Add(time.Hour)
	if probe, ok := table.takeDrain(k, now); !ok || !probe {
		t.Fatal("no probe at the end of the step")
	}
	table.observe(k, errors.New("a 500"), now)
	table.endProbe(k, now)
	if !table.drainHeld(k, now.Add(time.Hour-time.Second)) || table.drainHeld(k, now.Add(time.Hour)) {
		t.Fatal("a probe that met a 500 did not spend exactly its hour")
	}

	table.observe(k, nil, now)
	if table.drainHeld(k, now) {
		t.Fatal("held after an accepted call")
	}
}

// The owner writes a new key while a call on the old one is out. The vendor
// refuses the new key, the change's notice drops the old value, and then the
// refusal of the call on the old key arrives: it lands under the old value,
// so it neither overwrites the new key's refusal nor its drain wait, and the
// old key's late success does not clear it either.
func TestKeyRefusalsIgnoreALateAnswerAboutAReplacedKey(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	var table keyRefusals
	old := ownKeyID("a.example.com", "openai", "secret:one")
	current := ownKeyID("a.example.com", "openai", "secret:two")
	table.observe(current, refusedWith(http.StatusUnauthorized), now)
	table.replaced(old)
	table.observe(old, refusedWith(http.StatusForbidden), now.Add(time.Second))
	if got := table.status(current); got != http.StatusUnauthorized {
		t.Fatalf("the new key reads %d after the old key's late refusal, want its own 401", got)
	}
	if !table.drainHeld(current, now.Add(time.Minute-time.Second)) || table.drainHeld(current, now.Add(time.Minute)) {
		t.Fatal("the late refusal moved the new key's wait")
	}
	table.observe(old, nil, now)
	if got := table.status(current); got != http.StatusUnauthorized {
		t.Fatalf("the old key's late success cleared the new key's refusal: %d", got)
	}
}

// A refusal of the new key that arrives before the change's notice stands
// once the notice arrives: the notice drops the value it replaced and no
// other.
func TestKeyRefusalsKeepANewKeysRefusalThatCameBeforeTheNotice(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	var table keyRefusals
	old := ownKeyID("a.example.com", "openai", "secret:one")
	current := ownKeyID("a.example.com", "openai", "secret:two")
	table.observe(old, refusedWith(http.StatusUnauthorized), now)
	table.observe(current, refusedWith(http.StatusForbidden), now)
	table.replaced(old)
	if got := table.status(current); got != http.StatusForbidden {
		t.Fatalf("the new key reads %d after the notice, want its 403", got)
	}
	if got := table.status(old); got != 0 {
		t.Fatalf("the replaced key reads %d after the notice", got)
	}
}

// Writing a key over a refused one, or deleting the row, drops the replaced
// value's refusal, and the row's memory with it.
func TestKeyRefusalsForgetARowOnChange(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	var table keyRefusals
	k := ownKeyID("a.example.com", "openai", "secret:one")
	table.observe(k, refusedWith(http.StatusUnauthorized), now)
	table.replaced(k)
	if got := table.status(k); got != 0 {
		t.Fatalf("the row reads %d after its change", got)
	}
	if st := table.slots[k.slot]; st != nil {
		t.Fatalf("the slot holds %+v after its only value was replaced", st)
	}
}

// Rows refused over and over never hold more than the table's capacity, the
// operator's entries are never the ones dropped, and one row holds the
// refusals of at most keyRowValues values, the newest kept.
func TestKeyRefusalsHoldAtMostTheCapacity(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	table := keyRefusals{capacity: 8}
	op := operatorKeyID("openai")
	table.observe(op, refusedWith(http.StatusUnauthorized), start)
	for i := range 200 {
		now := start.Add(time.Duration(i+1) * time.Second)
		k := ownKeyID("a.example.com", fmt.Sprintf("row%d", i%50), fmt.Sprintf("secret:%d", i))
		table.observe(k, refusedWith(http.StatusUnauthorized), now)
		if n := len(table.slots); n > 8 {
			t.Fatalf("after %d rows the table holds %d slots, past its capacity of 8", i+1, n)
		}
		if got := table.status(k); got != http.StatusUnauthorized {
			t.Fatalf("the refusal just recorded reads %d", got)
		}
	}
	if got := table.status(op); got != http.StatusUnauthorized {
		t.Fatalf("the operator's refusal reads %d after the churn", got)
	}

	var row keyID
	for i := range 3 * keyRowValues {
		row = ownKeyID("a.example.com", "busy", fmt.Sprintf("secret:%d", i))
		table.observe(row, refusedWith(http.StatusUnauthorized), start.Add(time.Hour+time.Duration(i)*time.Second))
		if n := len(table.slots[row.slot].refusals); n > keyRowValues {
			t.Fatalf("one row holds the refusals of %d values, past %d", n, keyRowValues)
		}
	}
	if got := table.status(row); got != http.StatusUnauthorized {
		t.Fatalf("the newest value's refusal reads %d", got)
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
