package engine

import (
	"testing"
	"time"
)

// begin is a transaction as inTx opens one: it reads the cache's
// invalidation count before it reads any actor record.
func begin(ds *dataset) *txn { return &txn{ds: ds, actorEpoch: ds.actors.now()} }

// The actor cache holds only what no disable retracted: a transaction's
// standing records reach it at its commit, and a disable anywhere in between
// keeps them out.
func TestTheActorCacheTakesOnlyWhatNoLaterDisableRetracted(t *testing.T) {
	ds := &dataset{}
	quiet := begin(ds)
	quiet.standing("a1")
	ds.settleActorCache(quiet)
	if !ds.actorLive("a1") {
		t.Fatal("a committed transaction's standing record was not cached")
	}

	// A reads a2 standing; B disables it after A's commit, before A
	// publishes.
	a := begin(ds)
	a.standing("a2")
	b := begin(ds)
	ds.forgetActorLive("a2")
	b.actorsDisabled = append(b.actorsDisabled, "a2")
	ds.settleActorCache(b)
	ds.settleActorCache(a)
	if ds.actorLive("a2") {
		t.Fatal("a record a later transaction disabled was cached as standing")
	}

	// A disable whose own commit lands after a publish still retracts it.
	ds.markActorLive("a3")
	c := &txn{ds: ds, actorsDisabled: []string{"a3"}}
	ds.settleActorCache(c)
	if ds.actorLive("a3") {
		t.Fatal("a disabled record stayed cached past its disable's commit")
	}

	// A began, then B's disable of a4 committed and invalidated it, and only
	// then did A read a4: A read it as it stood before the disable, so the
	// count A compares against is the one it began with, not the one at its
	// read.
	a = begin(ds)
	ds.forgetActorLive("a4")
	a.standing("a4")
	ds.settleActorCache(a)
	if ds.actorLive("a4") {
		t.Fatal("a record disabled while the reading transaction ran was cached as standing")
	}
}

// A disable that races a publication lands before its check or after its
// stores, never between them: the publication passes its check, B disables
// the record then, and the record must not end up cached.
func TestADisableRacingAPublicationIsNeverUndone(t *testing.T) {
	ds := &dataset{}
	a := begin(ds)
	a.standing("a5")
	done := make(chan struct{})
	ds.actors.afterCheck = func() {
		ds.actors.afterCheck = nil
		go func() {
			ds.forgetActorLive("a5")
			close(done)
		}()
		// Give the disable every chance to land between the check and the
		// stores; it must wait for the publication instead.
		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
		}
	}
	ds.settleActorCache(a)
	<-done
	if ds.actorLive("a5") {
		t.Fatal("a disable that raced the publication was undone by it")
	}
}
