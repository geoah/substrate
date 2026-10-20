package engine

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The refresh table without a database: the backoff's steps, the generation
// that resets them, the entries it forgets, and the turn.

var backoffEpoch = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func newTestRefreshTable() *refreshTable {
	return &refreshTable{entries: map[refreshKey]*refreshEntry{}}
}

// A refusal of one generation arms a minute, then doubles with each refusal
// after the step ended, and stays at six hours once it reaches the cap.
func TestOAuthBackoffStepsDoubleToTheCap(t *testing.T) {
	tbl := newTestRefreshTable()
	k := refreshKey{repository: "ada.example.com", ref: "r1"}
	e, err := tbl.take(context.Background(), k)
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	defer tbl.give(k, e)
	gen := backoffEpoch.Add(-time.Hour)
	refusal := errors.New("provider answered 400")
	want := []time.Duration{
		time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute,
		32 * time.Minute, 64 * time.Minute, 128 * time.Minute, 256 * time.Minute,
		6 * time.Hour, 6 * time.Hour, 6 * time.Hour,
	}
	now := backoffEpoch
	for i, w := range want {
		if got := tbl.backedOff(e, gen, now); got != nil {
			t.Fatalf("refusal %d: backed off at the moment the last step ended: %v", i, got)
		}
		if got := tbl.arm(e, gen, now, refusal); got != w {
			t.Fatalf("refusal %d armed %v, want %v", i, got, w)
		}
		if got := tbl.backedOff(e, gen, now.Add(w-time.Second)); !errors.Is(got, refusal) {
			t.Fatalf("refusal %d: a second before the step ended answered %v, want the refusal", i, got)
		}
		now = now.Add(w)
	}
}

// A backoff armed for one generation answers nothing for another: the
// credential it refused was replaced, and the next refusal starts over.
func TestOAuthBackoffResetsOnANewGeneration(t *testing.T) {
	tbl := newTestRefreshTable()
	k := refreshKey{repository: "ada.example.com", ref: "r1"}
	e, err := tbl.take(context.Background(), k)
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	old, fresh := backoffEpoch.Add(-time.Hour), backoffEpoch
	tbl.arm(e, old, backoffEpoch, errors.New("refused"))
	tbl.arm(e, old, backoffEpoch.Add(time.Minute), errors.New("refused"))
	if got := tbl.backedOff(e, fresh, backoffEpoch.Add(time.Minute)); got != nil {
		t.Fatalf("a new generation was answered with the old one's refusal: %v", got)
	}
	if got := tbl.arm(e, fresh, backoffEpoch.Add(time.Minute), errors.New("refused")); got != time.Minute {
		t.Fatalf("the new generation's first refusal armed %v, want a minute", got)
	}
	tbl.clear(e)
	tbl.give(k, e)
	if n := len(tbl.entries); n != 0 {
		t.Fatalf("%d entries left after the turn was given back with no backoff armed, want 0", n)
	}
}

// An entry nobody uses whose backoff ended more than the cap ago leaves the
// table at the next refusal of any credential, so refused credentials that
// were deleted do not stay in memory for the life of the process.
func TestOAuthBackoffForgetsIdleEntries(t *testing.T) {
	tbl := newTestRefreshTable()
	refuse := func(ref string, at time.Time) {
		t.Helper()
		k := refreshKey{repository: "ada.example.com", ref: ref}
		e, err := tbl.take(context.Background(), k)
		if err != nil {
			t.Fatalf("take: %v", err)
		}
		tbl.arm(e, backoffEpoch, at, errors.New("refused"))
		tbl.give(k, e)
	}
	refuse("gone", backoffEpoch)
	// The step ended a minute in; six hours after that it is still kept.
	refuse("other", backoffEpoch.Add(time.Minute+6*time.Hour))
	if _, ok := tbl.entries[refreshKey{repository: "ada.example.com", ref: "gone"}]; !ok {
		t.Fatal("an entry was forgotten while its doubling could still apply")
	}
	refuse("other", backoffEpoch.Add(time.Minute+6*time.Hour+time.Second))
	if _, ok := tbl.entries[refreshKey{repository: "ada.example.com", ref: "gone"}]; ok {
		t.Fatal("an idle entry outlived its backoff by more than the cap")
	}
	if _, ok := tbl.entries[refreshKey{repository: "ada.example.com", ref: "other"}]; !ok {
		t.Fatal("the entry just refused was forgotten")
	}
}

// One hand holds a credential's turn at a time; a waiter whose context ends
// gives up without it, and the entry leaves the table with its last user.
func TestOAuthRefreshTurnIsExclusive(t *testing.T) {
	tbl := newTestRefreshTable()
	k := refreshKey{repository: "ada.example.com", ref: "r1"}
	first, err := tbl.take(context.Background(), k)
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := tbl.take(ctx, k); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a second take while the turn was held returned %v, want the waiter's deadline", err)
	}
	got := make(chan *refreshEntry, 1)
	go func() {
		e, err := tbl.take(context.Background(), k)
		if err != nil {
			t.Errorf("take after give: %v", err)
		}
		got <- e
	}()
	select {
	case <-got:
		t.Fatal("a second hand took the turn while the first held it")
	case <-time.After(50 * time.Millisecond):
	}
	tbl.give(k, first)
	second := <-got
	tbl.give(k, second)
	if n := len(tbl.entries); n != 0 {
		t.Fatalf("%d entries left after every hand gave the turn back, want 0", n)
	}
}

// The backoff counts the refusals of one generation from the first: seven
// refusals read seven, since the first one, with the step at 64 minutes,
// past oauthParkAfter. A new generation and a refresh that landed reset both.
func TestOAuthBackoffCountsRefusalsOfAGeneration(t *testing.T) {
	tbl := newTestRefreshTable()
	k := refreshKey{repository: "ada.example.com", ref: "r1"}
	e, err := tbl.take(context.Background(), k)
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	defer tbl.give(k, e)
	gen := backoffEpoch.Add(-time.Hour)
	now := backoffEpoch
	for i := 1; i <= 7; i++ {
		step := tbl.arm(e, gen, now, errors.New("refused"))
		if got, _, _ := tbl.refusalState(e); got != step {
			t.Fatalf("refusal %d: the state's step is %v, arm returned %v", i, got, step)
		}
		if i < 7 && step >= oauthParkAfter {
			t.Fatalf("refusal %d armed %v, at or past the park threshold %v", i, step, oauthParkAfter)
		}
		now = now.Add(step)
	}
	step, refusals, since := tbl.refusalState(e)
	if step != 64*time.Minute || refusals != 7 || !since.Equal(backoffEpoch) {
		t.Fatalf("after seven refusals: step %v, %d refusals since %v; want 64m, 7, %v", step, refusals, since, backoffEpoch)
	}
	if step < oauthParkAfter {
		t.Fatalf("the seventh refusal's step %v is short of the park threshold %v", step, oauthParkAfter)
	}

	fresh := backoffEpoch
	if got := tbl.backedOff(e, fresh, now); got != nil {
		t.Fatalf("a new generation was answered with the old one's refusal: %v", got)
	}
	if _, refusals, since := tbl.refusalState(e); refusals != 0 || !since.IsZero() {
		t.Fatalf("a new generation kept %d refusals since %v", refusals, since)
	}
	tbl.arm(e, fresh, now, errors.New("refused"))
	tbl.arm(e, fresh, now.Add(time.Minute), errors.New("refused"))
	if _, refusals, since := tbl.refusalState(e); refusals != 2 || !since.Equal(now) {
		t.Fatalf("the new generation counts %d refusals since %v, want 2 since %v", refusals, since, now)
	}
	tbl.clear(e)
	if step, refusals, since := tbl.refusalState(e); step != 0 || refusals != 0 || !since.IsZero() {
		t.Fatalf("a landed refresh left step %v, %d refusals since %v", step, refusals, since)
	}
}
